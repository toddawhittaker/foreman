package factorymap

// Hostile helper mod output (SPEC 14.6, 5.16; plan fixes-16 "Contracts" and
// the 14.6 limits). The helper mod runs inside a game container that loads a
// player's save and mods, so everything it writes is untrusted: each test
// here feeds the parser a file a hostile save or mod could make and requires
// a refusal (or a bounded, well-formed result), never a panic, a hang or an
// allocation the limits do not allow.
//
// Assumption on the chunk encoding: "data" is helpers.encode_string of the
// game's chart, that is base64 of a zlib stream, decoding to 32 by 32
// RGB565 pixels (2,048 bytes).

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/saveplayers"
)

const chunkBytes = ChunkPixels * ChunkPixels * 2

// encodeData is what helpers.encode_string makes of raw.
func encodeData(raw []byte) string {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	_, _ = w.Write(raw)
	_ = w.Close()
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func chunkLine(x, y int, raw []byte) string {
	return fmt.Sprintf(`{"x":%d,"y":%d,"data":%q}`, x, y, encodeData(raw)) + "\n"
}

func goodRaw() []byte { return bytes.Repeat([]byte{0x1f, 0x00}, chunkBytes/2) }

// oneSurface is a valid manifest of one 2 by 1 chunk surface.
func oneSurface() Manifest {
	return Manifest{Format: ManifestFormat, GameVersion: "2.0.66", Tick: 60, Surfaces: []ManifestSurface{{
		Index: 1, Name: "nauvis", Kind: "planet", Chunks: 2,
		MinX: 0, MinY: 0, MaxX: 1, MaxY: 0,
		Chart: "surface-1.jsonl", Entities: "surface-1-entities.jsonl", EntitiesTotal: 1,
	}}}
}

func manifestJSON(t *testing.T, m Manifest) string {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func goodChunks() string { return chunkLine(0, 0, goodRaw()) + chunkLine(1, 0, goodRaw()) }

// drawChart runs DrawChart under a deadline, failing on a panic or a hang.
func hostileDrawChart(t *testing.T, manifest, chunks io.Reader, surface int) (*image.NRGBA, Surface, error) {
	t.Helper()
	type res struct {
		img *image.NRGBA
		s   Surface
		err error
	}
	ch := make(chan res, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				ch <- res{err: fmt.Errorf("panic: %v", p)}
				t.Errorf("DrawChart panicked: %v", p)
			}
		}()
		img, s, err := DrawChart(manifest, chunks, surface)
		ch <- res{img, s, err}
	}()
	select {
	case r := <-ch:
		return r.img, r.s, r.err
	case <-time.After(60 * time.Second):
		t.Fatal("DrawChart did not return within 60 s")
	}
	return nil, Surface{}, nil
}

func hostileDrawOverlay(t *testing.T, entities io.Reader, s Surface) (map[Category]*image.Paletted, error) {
	t.Helper()
	type res struct {
		m   map[Category]*image.Paletted
		err error
	}
	ch := make(chan res, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				ch <- res{err: fmt.Errorf("panic: %v", p)}
				t.Errorf("DrawOverlay panicked: %v", p)
			}
		}()
		m, err := DrawOverlay(entities, s)
		ch <- res{m, err}
	}()
	select {
	case r := <-ch:
		return r.m, r.err
	case <-time.After(60 * time.Second):
		t.Fatal("DrawOverlay did not return within 60 s")
	}
	return nil, nil
}

// boundedReader serves a prefix, then filler forever, and records whether
// the reader went past limit bytes: a parser that respects the limit stops
// well before.
type boundedReader struct {
	prefix []byte
	filler byte
	limit  int64
	read   int64
	past   bool
}

func (r *boundedReader) Read(p []byte) (int, error) {
	if r.read > r.limit+(1<<20) {
		r.past = true
		return 0, errors.New("test: read far past the limit")
	}
	n := 0
	for n < len(p) {
		if r.read < int64(len(r.prefix)) {
			c := copy(p[n:], r.prefix[r.read:])
			n += c
			r.read += int64(c)
			continue
		}
		m := len(p) - n
		for i := 0; i < m; i++ {
			p[n+i] = r.filler
		}
		n += m
		r.read += int64(m)
	}
	if r.read > r.limit+(1<<20) {
		r.past = true
	}
	return n, nil
}

// The valid baseline: if this fails, the other tests prove nothing.
func TestHostileBaselineValidOutputDraws(t *testing.T) {
	m, err := ParseManifest(strings.NewReader(manifestJSON(t, oneSurface())))
	if err != nil {
		t.Fatalf("ParseManifest of a valid manifest: %v", err)
	}
	if len(m.Surfaces) != 1 || m.Surfaces[0].Name != "nauvis" {
		t.Fatalf("ParseManifest = %+v", m)
	}
	img, s, err := hostileDrawChart(t, strings.NewReader(manifestJSON(t, oneSurface())), strings.NewReader(goodChunks()), 1)
	if err != nil {
		t.Fatalf("DrawChart of valid output: %v", err)
	}
	if s.Width != 2*ChunkPixels || s.Height != ChunkPixels || s.TilesPerPixel != 1 {
		t.Fatalf("surface = %+v, want 64 by 32 at 1 tile a pixel", s)
	}
	if b := img.Bounds(); b.Dx() != s.Width || b.Dy() != s.Height {
		t.Fatalf("image %v does not match surface %+v", b, s)
	}
	layers, err := hostileDrawOverlay(t, strings.NewReader("[\"transport-belt\"]\n[0,0,0,1,1]\n"), s)
	if err != nil {
		t.Fatalf("DrawOverlay of valid output: %v", err)
	}
	for c, l := range layers {
		if b := l.Bounds(); b.Dx() != s.Width || b.Dy() != s.Height {
			t.Errorf("layer %s is %v, want the surface's %dx%d", c, b, s.Width, s.Height)
		}
	}
}

func TestHostileManifestRefused(t *testing.T) {
	mod := func(f func(*Manifest)) string {
		m := oneSurface()
		f(&m)
		return manifestJSON(t, m)
	}
	many := func(n int) string {
		return mod(func(m *Manifest) {
			s := m.Surfaces[0]
			m.Surfaces = nil
			for i := 0; i < n; i++ {
				s.Index = i + 1
				s.Chart = fmt.Sprintf("surface-%d.jsonl", i+1)
				s.Entities = fmt.Sprintf("surface-%d-entities.jsonl", i+1)
				m.Surfaces = append(m.Surfaces, s)
			}
		})
	}
	cases := map[string]string{
		"not JSON":                 "{nope",
		"empty":                    "",
		"trailing value":           manifestJSON(t, oneSurface()) + `{"format":1}`,
		"unknown top field":        strings.Replace(manifestJSON(t, oneSurface()), `{"format"`, `{"extra":1,"format"`, 1),
		"unknown surface field":    strings.Replace(manifestJSON(t, oneSurface()), `"index"`, `"evil":true,"index"`, 1),
		"newer format":             mod(func(m *Manifest) { m.Format = 2 }),
		"no format":                mod(func(m *Manifest) { m.Format = 0 }),
		"too many surfaces":        many(MaxSurfaces + 1),
		"negative chunks":          mod(func(m *Manifest) { m.Surfaces[0].Chunks = -1 }),
		"too many chunks":          mod(func(m *Manifest) { m.Surfaces[0].Chunks = MaxChunksPerSurface + 1 }),
		"more chunks than the box": mod(func(m *Manifest) { m.Surfaces[0].Chunks = 3 }),
		"inverted box":             mod(func(m *Manifest) { m.Surfaces[0].MinX, m.Surfaces[0].MaxX = 5, 1 }),
		"duplicate index": mod(func(m *Manifest) {
			s := m.Surfaces[0]
			s.Chart, s.Entities = "surface-2.jsonl", "surface-2-entities.jsonl"
			m.Surfaces = append(m.Surfaces, s)
		}),
		"too many entities": mod(func(m *Manifest) { m.Surfaces[0].EntitiesTotal = MaxEntities + 1 }),
		"too many entities in all": mod(func(m *Manifest) {
			m.Surfaces[0].EntitiesTotal = MaxEntities
			s := m.Surfaces[0]
			s.Index, s.EntitiesTotal = 2, 1
			s.Chart, s.Entities = "surface-2.jsonl", "surface-2-entities.jsonl"
			m.Surfaces = append(m.Surfaces, s)
		}),
		"negative entities":        mod(func(m *Manifest) { m.Surfaces[0].EntitiesTotal = -1 }),
		"unknown kind":             mod(func(m *Manifest) { m.Surfaces[0].Kind = "moon" }),
		"chart path escapes":       mod(func(m *Manifest) { m.Surfaces[0].Chart = "../../etc/passwd" }),
		"chart path absolute":      mod(func(m *Manifest) { m.Surfaces[0].Chart = "/etc/passwd" }),
		"entities path escapes":    mod(func(m *Manifest) { m.Surfaces[0].Entities = "../rconpw" }),
		"chart path in a folder":   mod(func(m *Manifest) { m.Surfaces[0].Chart = "sub/surface-1.jsonl" }),
		"chart path backslash":     mod(func(m *Manifest) { m.Surfaces[0].Chart = `..\rconpw` }),
		"chart and entities share": mod(func(m *Manifest) { m.Surfaces[0].Entities = m.Surfaces[0].Chart }),
		"huge number":              strings.Replace(manifestJSON(t, oneSurface()), `"chunks":2`, `"chunks":1e400`, 1),
		"fractional count":         strings.Replace(manifestJSON(t, oneSurface()), `"chunks":2`, `"chunks":2.5`, 1),
		"string count":             strings.Replace(manifestJSON(t, oneSurface()), `"chunks":2`, `"chunks":"2"`, 1),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest(strings.NewReader(in)); err == nil {
				t.Errorf("ParseManifest accepted %s", name)
			}
			// DrawChart reads the same manifest and must refuse it too.
			if _, _, err := hostileDrawChart(t, strings.NewReader(in), strings.NewReader(goodChunks()), 1); err == nil {
				t.Errorf("DrawChart accepted a manifest with %s", name)
			}
		})
	}
}

// A manifest that never ends is refused once it passes the output limit,
// without reading the whole stream.
func TestHostileOversizedManifestStops(t *testing.T) {
	r := &boundedReader{prefix: []byte(`{"format":1,"game_version":"`), filler: 'a', limit: MaxOutputBytes}
	if _, err := ParseManifest(r); err == nil {
		t.Fatal("ParseManifest accepted an endless manifest")
	}
	if r.past {
		t.Fatalf("ParseManifest read %d bytes, past the %d-byte limit", r.read, MaxOutputBytes)
	}
	r = &boundedReader{prefix: []byte(manifestJSON(t, oneSurface())), filler: ' ', limit: MaxOutputBytes}
	if _, err := ParseManifest(r); err == nil {
		t.Fatal("ParseManifest accepted a manifest followed by endless padding")
	}
	if r.past {
		t.Fatalf("ParseManifest read %d bytes past the limit", r.read)
	}
}

func TestHostileChunksRefused(t *testing.T) {
	raw := goodRaw()
	line00 := chunkLine(0, 0, raw)
	cases := map[string]string{
		"fewer chunks than the count": line00,
		"more chunks than the count":  goodChunks() + chunkLine(0, 0, raw),
		"repeated chunk":              line00 + line00,
		"chunk left of the box":       line00 + chunkLine(-1, 0, raw),
		"chunk right of the box":      line00 + chunkLine(2, 0, raw),
		"chunk below the box":         line00 + chunkLine(1, 1, raw),
		"chunk far outside":           line00 + chunkLine(1<<30, -(1<<30), raw),
		"short chunk":                 line00 + chunkLine(1, 0, raw[:chunkBytes-1]),
		"long chunk":                  line00 + chunkLine(1, 0, append(append([]byte{}, raw...), 0)),
		"empty chunk":                 line00 + chunkLine(1, 0, nil),
		"compressed bomb":             line00 + chunkLine(1, 0, make([]byte, 64<<20)),
		"not base64":                  line00 + `{"x":1,"y":0,"data":"!!!not base64!!!"}` + "\n",
		"base64 not zlib":             line00 + fmt.Sprintf(`{"x":1,"y":0,"data":%q}`, base64.StdEncoding.EncodeToString(raw)) + "\n",
		"unknown chunk field":         line00 + strings.Replace(chunkLine(1, 0, raw), `{"x"`, `{"z":1,"x"`, 1),
		"missing data":                line00 + `{"x":1,"y":0}` + "\n",
		"fractional coordinate":       line00 + strings.Replace(chunkLine(1, 0, raw), `"x":1`, `"x":1.5`, 1),
		"not JSON line":               line00 + "garbage\n",
		"array line":                  line00 + "[1,0,\"x\"]\n",
		"two values on one line":      strings.TrimSuffix(line00, "\n") + chunkLine(1, 0, raw),
		"no chunks":                   "",
	}
	for name, chunks := range cases {
		t.Run(name, func(t *testing.T) {
			img, _, err := hostileDrawChart(t, strings.NewReader(manifestJSON(t, oneSurface())), strings.NewReader(chunks), 1)
			if err == nil {
				t.Errorf("DrawChart accepted %s (image %v)", name, img.Bounds())
			}
		})
	}
}

func TestHostileChartSurfaceNotInManifest(t *testing.T) {
	for _, s := range []int{0, 2, -1, 1 << 30} {
		if _, _, err := hostileDrawChart(t, strings.NewReader(manifestJSON(t, oneSurface())), strings.NewReader(goodChunks()), s); err == nil {
			t.Errorf("DrawChart drew surface %d, which the manifest does not list", s)
		}
	}
}

// An endless chunk line, or endless lines, stop at the output limit.
func TestHostileOversizedChunksStop(t *testing.T) {
	for name, r := range map[string]*boundedReader{
		"endless line":   {prefix: []byte(`{"x":0,"y":0,"data":"`), filler: 'A', limit: MaxOutputBytes},
		"endless blanks": {prefix: []byte(goodChunks()), filler: '\n', limit: MaxOutputBytes},
		"endless spaces": {prefix: []byte(chunkLine(0, 0, goodRaw())), filler: ' ', limit: MaxOutputBytes},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := hostileDrawChart(t, strings.NewReader(manifestJSON(t, oneSurface())), r, 1); err == nil {
				t.Errorf("DrawChart accepted %s", name)
			}
			if r.past {
				t.Errorf("DrawChart read %d bytes, past the %d-byte limit", r.read, MaxOutputBytes)
			}
		})
	}
}

// A huge box with few chunks must not allocate the box: the picture never
// passes MaxSidePixels a side (SPEC 5.16), and the scale says so.
func TestHostileHugeBoxStaysWithinSideLimit(t *testing.T) {
	m := oneSurface()
	m.Surfaces[0].MinX, m.Surfaces[0].MinY = -1<<20, -1<<20
	m.Surfaces[0].MaxX, m.Surfaces[0].MaxY = 1<<20, 1<<20
	img, s, err := hostileDrawChart(t, strings.NewReader(manifestJSON(t, m)), strings.NewReader(goodChunks()), 1)
	if err != nil {
		return // refusing such a box is also within the rules
	}
	b := img.Bounds()
	if b.Dx() > MaxSidePixels || b.Dy() > MaxSidePixels || s.Width > MaxSidePixels || s.Height > MaxSidePixels {
		t.Fatalf("picture %v (surface %+v) passes %d pixels a side", b, s, MaxSidePixels)
	}
	if s.TilesPerPixel <= 1 {
		t.Fatalf("TilesPerPixel = %d for a scaled-down picture", s.TilesPerPixel)
	}
}

func TestHostileEntitiesRefused(t *testing.T) {
	s := Surface{Index: 1, Name: "nauvis", Kind: "planet", Width: 64, Height: 32, TilesPerPixel: 1}
	types := `["transport-belt","assembling-machine"]` + "\n"
	cases := map[string]string{
		"no types line":         "[0,1,1,2,2]\n",
		"types not strings":     "[1,2]\n[0,1,1,2,2]\n",
		"types an object":       `{"a":1}` + "\n",
		"type index past list":  types + "[2,1,1,2,2]\n",
		"negative type index":   types + "[-1,1,1,2,2]\n",
		"fractional type index": types + "[0.5,1,1,2,2]\n",
		"short box":             types + "[0,1,1,2]\n",
		"long box":              types + "[0,1,1,2,2,3]\n",
		"box a string":          types + `[0,"1",1,2,2]` + "\n",
		"box an object":         types + `{"t":0}` + "\n",
		"not JSON":              types + "garbage\n",
		"huge number":           types + "[0,1e400,1,2,2]\n",
		"inverted box":          types + "[0,5,5,1,1]\n",
		"two values a line":     types + "[0,1,1,2,2][0,1,1,2,2]\n",
		"fractional box":        types + "[0,0.5,0.5,1.5,1.5]\n",
		"box over 64 wide":      types + "[0,0,0,65,1]\n",
		"box over 64 tall":      types + "[0,0,0,1,65]\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := hostileDrawOverlay(t, strings.NewReader(in), s); err == nil {
				t.Errorf("DrawOverlay accepted %s", name)
			}
		})
	}
}

// Boxes outside the chart, or enormous ones, draw nothing outside the
// surface's pixel grid and allocate nothing past it.
func TestHostileEntityBoxesOutsideTheChart(t *testing.T) {
	s := Surface{Index: 1, Name: "nauvis", Kind: "planet", Width: 64, Height: 32, TilesPerPixel: 1}
	in := `["transport-belt"]` + "\n" +
		"[0,-1000000,-1000000,-999999,-999999]\n" +
		"[0,1000000,1000000,1000001,1000001]\n" +
		"[0,63,31,65,33]\n"
	layers, err := hostileDrawOverlay(t, strings.NewReader(in), s)
	if err != nil {
		return // refusing them is also within the rules
	}
	for c, l := range layers {
		if b := l.Bounds(); b.Dx() != s.Width || b.Dy() != s.Height {
			t.Errorf("layer %s is %v, want %dx%d", c, b, s.Width, s.Height)
		}
	}
}

func TestHostileTooManyEntitiesStops(t *testing.T) {
	if testing.Short() {
		t.Skip("writes two million entity lines")
	}
	s := Surface{Index: 1, Name: "nauvis", Kind: "planet", Width: 64, Height: 32, TilesPerPixel: 1}
	line := []byte("[0,1,1,2,2]\n")
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte(`["transport-belt"]` + "\n"))
		for i := 0; i <= MaxEntities; i++ {
			if _, err := pw.Write(line); err != nil {
				return
			}
		}
		_ = pw.Close()
	}()
	_, err := hostileDrawOverlay(t, pr, s)
	_ = pr.Close()
	if err == nil {
		t.Fatalf("DrawOverlay accepted %d entities, past the %d limit", MaxEntities+1, MaxEntities)
	}
	r := &boundedReader{prefix: []byte(`["transport-belt"]` + "\n"), filler: ' ', limit: MaxOutputBytes}
	if _, err := hostileDrawOverlay(t, r, s); err == nil {
		t.Error("DrawOverlay accepted an endless entity file")
	}
	if r.past {
		t.Errorf("DrawOverlay read %d bytes, past the %d-byte limit", r.read, MaxOutputBytes)
	}
	r = &boundedReader{prefix: []byte(`["`), filler: 'a', limit: MaxOutputBytes}
	if _, err := hostileDrawOverlay(t, r, s); err == nil {
		t.Error("DrawOverlay accepted an endless types line")
	}
	if r.past {
		t.Errorf("DrawOverlay read %d bytes of the types line, past the limit", r.read)
	}
}

// The store: a world id that is a path, a picture name that is a path, and
// a store whose files were swapped for a symlink or a named pipe are all
// refused, and opening never blocks on a pipe.

func validDrawing() (Drawing, map[string][]byte) {
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 4, 4)))
	d := Drawing{
		Basis:       saveplayers.Basis{Save: "auto.zip", Size: 10, Modified: time.Unix(1700000000, 0).UTC()},
		DrawnAt:     time.Unix(1700000100, 0).UTC(),
		GameVersion: "2.0.66",
		Surfaces:    []Surface{{Index: 1, Name: "nauvis", Kind: "planet", Width: 4, Height: 4, TilesPerPixel: 1}},
	}
	return d, map[string][]byte{"surface-1.png": b.Bytes()}
}

func TestHostileStoreWorldIDs(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"", ".", "..", "../x", "a/b", `a\b`, "/abs", "a\x00b"} {
		if _, err := Open(dir, id); err == nil {
			t.Errorf("Open accepted world id %q", id)
		}
		if err := Remove(dir, id); err == nil {
			t.Errorf("Remove accepted world id %q", id)
		}
	}
}

func TestHostileStorePictureNames(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir, "w1")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	d, pngs := validDrawing()
	for _, name := range []string{"../evil.png", "/tmp/evil.png", "sub/x.png", `..\evil.png`, ""} {
		bad := map[string][]byte{name: pngs["surface-1.png"]}
		if err := st.Replace(d, bad); err == nil {
			t.Errorf("Replace accepted picture name %q", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.png")); err == nil {
		t.Error("Replace wrote a file outside the store")
	}
}

func openWithin(t *testing.T, f func() error) error {
	t.Helper()
	ch := make(chan error, 1)
	go func() { ch <- f() }()
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the store blocked, likely opening a named pipe")
		return nil
	}
}

func TestHostileStoreSpecialFiles(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			st, err := Open(dir, "w1")
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			d, pngs := validDrawing()
			if err := st.Replace(d, pngs); err != nil {
				t.Fatalf("Replace: %v", err)
			}
			secret := filepath.Join(dir, "secret")
			if err := os.WriteFile(secret, []byte("rcon"), 0o600); err != nil {
				t.Fatal(err)
			}
			// Swap every file in the store for the special file.
			swapped := 0
			err = filepath.WalkDir(filepath.Join(dir, "factorymaps", "w1"), func(p string, e os.DirEntry, err error) error {
				if err != nil || e.IsDir() {
					return err
				}
				if err := os.Remove(p); err != nil { //nolint:gosec // test-owned temp dir
					return err
				}
				swapped++
				if kind == "symlink" {
					return os.Symlink(secret, p) //nolint:gosec // test-owned temp dir
				}
				return syscall.Mkfifo(p, 0o600)
			})
			if err != nil {
				t.Fatalf("swapping files: %v", err)
			}
			if swapped == 0 {
				t.Fatal("the store kept no files under factorymaps/w1")
			}
			st, err = Open(dir, "w1")
			if err != nil {
				return // refusing at Open is fine
			}
			_ = openWithin(t, func() error {
				if _, ok, err := st.Read(); err == nil && ok {
					t.Errorf("Read followed a %s", kind)
				}
				return nil
			})
			_ = openWithin(t, func() error {
				f, err := st.OpenPNG(1, "")
				if err == nil {
					_ = f.Close()
					t.Errorf("OpenPNG opened a %s", kind)
				}
				return nil
			})
			_ = openWithin(t, func() error {
				if _, err := st.Composite(1, nil); err == nil {
					t.Errorf("Composite read a %s", kind)
				}
				return nil
			})
		})
	}
}

// The world's store folder itself swapped for a symlink to elsewhere: the
// store neither reads nor writes through it, and Remove does not delete the
// target's files.
func TestHostileStoreFolderSymlink(t *testing.T) {
	dir := t.TempDir()
	elsewhere := t.TempDir()
	keep := filepath.Join(elsewhere, "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "factorymaps"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(dir, "factorymaps", "w1")); err != nil {
		t.Fatal(err)
	}
	if st, err := Open(dir, "w1"); err == nil {
		d, pngs := validDrawing()
		if err := st.Replace(d, pngs); err == nil {
			ents, _ := os.ReadDir(elsewhere)
			if len(ents) > 1 {
				t.Error("Replace wrote through a symlinked store folder")
			}
		}
	}
	_ = Remove(dir, "w1")
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("Remove deleted a file through a symlinked store folder: %v", err)
	}
}

// A sparse chart spread over the widest box the limits allow costs memory
// bounded by the picture, not the box: under 256 MiB of peak heap.
func TestHostileSparseChartMemoryBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("draws 65,536 chunks")
	}
	m := oneSurface()
	const side = 1 << 12 // chunks a side of the box
	m.Surfaces[0].MinX, m.Surfaces[0].MinY = 0, 0
	m.Surfaces[0].MaxX, m.Surfaces[0].MaxY = side-1, side-1
	m.Surfaces[0].Chunks = MaxChunksPerSurface
	data := encodeData(goodRaw())
	var b strings.Builder
	for i := 0; i < MaxChunksPerSurface; i++ {
		// Spread the chunks over the whole box, 16 apart each way.
		fmt.Fprintf(&b, `{"x":%d,"y":%d,"data":%q}`+"\n", (i%256)*16, (i/256)*16, data)
	}
	chunks := b.String()
	var base runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&base)
	done := make(chan struct{})
	peak := make(chan uint64, 1)
	go func() {
		var top uint64
		var ms runtime.MemStats
		for {
			select {
			case <-done:
				peak <- top
				return
			case <-time.After(5 * time.Millisecond):
				runtime.ReadMemStats(&ms)
				if ms.HeapInuse > top {
					top = ms.HeapInuse
				}
			}
		}
	}()
	img, s, err := hostileDrawChart(t, strings.NewReader(manifestJSON(t, m)), strings.NewReader(chunks), 1)
	close(done)
	used := <-peak - base.HeapInuse
	if err != nil {
		return // refusing is within the rules
	}
	t.Logf("picture %v, %d tiles a pixel, peak heap %d MiB over the input", img.Bounds(), s.TilesPerPixel, used>>20)
	if used > 256<<20 {
		t.Errorf("drawing a sparse chart peaked at %d MiB of heap, past the 256 MiB ceiling", used>>20)
	}
}
