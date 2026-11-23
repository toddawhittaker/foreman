package world

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stripPNG makes a mipmap strip: the first square (side by side) is red, the
// rest of the picture is blue.
func stripPNG(t *testing.T, w, h, side int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	red, blue := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}
	for y := range h {
		for x := range w {
			if x < side && y < side {
				img.SetNRGBA(x, y, red)
			} else {
				img.SetNRGBA(x, y, blue)
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// iconZip builds a mod zip with an info.json and the given files, all under
// top.
func iconZip(t *testing.T, top string, files map[string][]byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, b []byte) {
		w, err := zw.Create(top + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	add("info.json", []byte(`{"name":"fakemod","version":"1.0.0"}`))
	for name, b := range files {
		add(name, b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

const iconPath = "__fakemod__/graphics/icons/widget.png"

// iconWorld makes a world with fakemod 1.0.0 holding the given files.
func iconWorld(t *testing.T, top string, files map[string][]byte) (*Manager, string, string) {
	t.Helper()
	m, id, dir := checkWorld(t, "2.0.77", false)
	putZip(t, dir, "fakemod_1.0.0.zip", iconZip(t, top, files))
	putModList(t, dir, "base", "fakemod")
	return m, id, dir
}

func widget(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{"graphics/icons/widget.png": stripPNG(t, 120, 64, 64)}
}

func decodeIcon(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestModIconCropsTheFirstMipmapAndScales(t *testing.T) {
	for _, top := range []string{"fakemod_1.0.0", "fakemod"} {
		m, id, _ := iconWorld(t, top, widget(t))
		b, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32)
		if err != nil {
			t.Fatalf("top %s: %v", top, err)
		}
		img := decodeIcon(t, b)
		if got := img.Bounds(); got.Dx() != 32 || got.Dy() != 32 {
			t.Fatalf("top %s: size %v", top, got)
		}
		// Every pixel comes from the first square, which is red. The blue
		// part of the strip must not leak in, even at the right edge.
		for y := range 32 {
			for x := range 32 {
				r, g, bl, a := img.At(x, y).RGBA()
				if r>>8 < 250 || g>>8 > 5 || bl>>8 > 5 || a>>8 < 250 {
					t.Fatalf("top %s: pixel %d,%d is %d %d %d %d", top, x, y, r>>8, g>>8, bl>>8, a>>8)
				}
			}
		}
	}
}

func TestModIconSameSizeKeepsThePixels(t *testing.T) {
	m, id, _ := iconWorld(t, "fakemod_1.0.0", widget(t))
	b, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 64)
	if err != nil {
		t.Fatal(err)
	}
	img := decodeIcon(t, b)
	if got := img.Bounds(); got.Dx() != 64 || got.Dy() != 64 {
		t.Fatalf("size %v", got)
	}
	if r, _, bl, _ := img.At(63, 63).RGBA(); r>>8 != 255 || bl != 0 {
		t.Fatal("the corner is not from the first square")
	}
}

func TestModIconOutputIsAFreshPNG(t *testing.T) {
	// A PNG with extra bytes after IEND (a polyglot) comes out without them.
	src := append(stripPNG(t, 64, 64, 64), []byte("<script>alert(1)</script>")...)
	m, id, _ := iconWorld(t, "fakemod_1.0.0", map[string][]byte{"graphics/icons/widget.png": src})
	b, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 16)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("script")) {
		t.Fatal("bytes of the zip's file reached the output")
	}
	decodeIcon(t, b)
}

func TestModIconNotFound(t *testing.T) {
	m, id, dir := iconWorld(t, "fakemod_1.0.0", widget(t))
	putZip(t, dir, "other_1.0.0.zip", iconZip(t, "other_1.0.0", nil))
	cases := map[string]struct {
		id, mod, version, path string
	}{
		"path not in the zip": {id, "fakemod", "1.0.0", "__fakemod__/graphics/nothing.png"},
		// Two dots inside a name are allowed; only a ".." part is refused.
		"dots inside a name": {id, "fakemod", "1.0.0", "__fakemod__/graphics/a..b.png"},
		"wrong version":       {id, "fakemod", "1.0.1", iconPath},
		"mod not installed":   {id, "nomod", "1.0.0", "__nomod__/a.png"},
		"built-in mod":        {id, "base", "2.0.77", "__base__/graphics/icons/a.png"},
		"unknown world":       {"nope", "fakemod", "1.0.0", iconPath},
		"a folder in the zip": {id, "fakemod", "1.0.0", "__fakemod__/graphics/icons.png"},
	}
	for name, c := range cases {
		_, err := m.ModIcon(c.id, c.mod, c.version, c.path, 64, 32)
		if !errors.Is(err, ErrModIconNotFound) {
			t.Errorf("%s: got %v, want not found", name, err)
		}
	}
}

func TestModIconPathMismatchWithTheModName(t *testing.T) {
	m, id, dir := iconWorld(t, "fakemod_1.0.0", widget(t))
	putZip(t, dir, "other_1.0.0.zip", iconZip(t, "other_1.0.0", map[string][]byte{
		"graphics/icons/widget.png": stripPNG(t, 64, 64, 64),
	}))
	putModList(t, dir, "base", "fakemod", "other")
	// The path names another mod than the one asked for.
	for _, p := range []string{"__other__/graphics/icons/widget.png", "__base__/graphics/icons/widget.png"} {
		if _, err := m.ModIcon(id, "fakemod", "1.0.0", p, 64, 32); !errors.Is(err, ErrModIconRefused) {
			t.Errorf("%s: got %v, want refused", p, err)
		}
	}
}

func TestModIconRefusesBadPaths(t *testing.T) {
	m, id, _ := iconWorld(t, "fakemod_1.0.0", widget(t))
	bad := map[string]string{
		"no prefix":     "graphics/icons/widget.png",
		"dot dot":       "__fakemod__/../info.json.png",
		"dot dot mid":   "__fakemod__/graphics/../graphics/icons/widget.png",
		"absolute":      "/etc/passwd.png",
		"absolute rest": "__fakemod__//graphics/icons/widget.png",
		"backslash":     `__fakemod__/graphics\icons\widget.png`,
		"not png":       "__fakemod__/info.json",
		"upper png":     "__fakemod__/graphics/icons/widget.PNG",
		"empty rest":    "__fakemod__/",
		"dot segment":   "__fakemod__/./graphics/icons/widget.png",
		"newline":       "__fakemod__/graphics/icons/wid\nget.png",
		"nul":           "__fakemod__/graphics/icons/wid\x00get.png",
		"empty":         "",
	}
	for name, p := range bad {
		if _, err := m.ModIcon(id, "fakemod", "1.0.0", p, 64, 32); !errors.Is(err, ErrModIconRefused) {
			t.Errorf("%s: got %v, want refused", name, err)
		}
	}
	// A bad mod name is refused before any lookup.
	for _, mod := range []string{"", "../x", "a/b", ".hidden"} {
		if _, err := m.ModIcon(id, mod, "1.0.0", "__"+mod+"__/a.png", 64, 32); !errors.Is(err, ErrModIconRefused) {
			t.Errorf("mod %q: got %v, want refused", mod, err)
		}
	}
}

func TestModIconRefusesBadSizes(t *testing.T) {
	m, id, _ := iconWorld(t, "fakemod_1.0.0", widget(t))
	for _, c := range [][2]int{{0, 32}, {-1, 32}, {64, 0}, {64, -5}, {4097, 32}, {64, 1025}} {
		if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, c[0], c[1]); !errors.Is(err, ErrModIconRefused) {
			t.Errorf("size %d out %d: got %v, want refused", c[0], c[1], err)
		}
	}
}

func TestModIconRefusesAPictureSmallerThanTheSize(t *testing.T) {
	m, id, _ := iconWorld(t, "fakemod_1.0.0", map[string][]byte{
		"graphics/icons/widget.png": stripPNG(t, 32, 32, 32),
	})
	if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32); !errors.Is(err, ErrModIconRefused) {
		t.Fatalf("got %v, want refused", err)
	}
	// Wide enough but not tall enough.
	m, id, _ = iconWorld(t, "fakemod_1.0.0", map[string][]byte{
		"graphics/icons/widget.png": stripPNG(t, 128, 32, 32),
	})
	if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32); !errors.Is(err, ErrModIconRefused) {
		t.Fatalf("got %v, want refused", err)
	}
}

func TestModIconRefusesNonPNGBytes(t *testing.T) {
	for name, b := range map[string][]byte{
		"text":      []byte("hello, not a picture"),
		"empty":     {},
		"jpeg head": {0xff, 0xd8, 0xff, 0xe0, 0, 0x10, 'J', 'F', 'I', 'F'},
		"truncated": stripPNG(t, 120, 64, 64)[:100],
	} {
		m, id, _ := iconWorld(t, "fakemod_1.0.0", map[string][]byte{"graphics/icons/widget.png": b})
		if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32); !errors.Is(err, ErrModIconRefused) {
			t.Errorf("%s: got %v, want refused", name, err)
		}
	}
}

// bigHeaderPNG is a tiny, valid-looking PNG whose header claims w by h.
func bigHeaderPNG(w, h uint32) []byte {
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	chunk := func(kind string, data []byte) {
		_ = binary.Write(&buf, binary.BigEndian, uint32(len(data))) //nolint:gosec // a test chunk of a few bytes
		body := append([]byte(kind), data...)
		buf.Write(body)
		_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(body))
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8 bits, RGBA
	chunk("IHDR", ihdr)
	chunk("IEND", nil)
	return buf.Bytes()
}

func TestModIconRefusesAHeaderClaimingAHugePicture(t *testing.T) {
	for _, c := range [][2]uint32{{5000, 64}, {64, 5000}, {4097, 4097}, {1 << 30, 1 << 30}} {
		m, id, _ := iconWorld(t, "fakemod_1.0.0", map[string][]byte{
			"graphics/icons/widget.png": bigHeaderPNG(c[0], c[1]),
		})
		_, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32)
		// Go's own reader refuses a header whose area overflows; that is
		// a refusal too, just not for size.
		tooBig := strings.Contains(err.Error(), "too big") || c[0] > 1<<20
		if !errors.Is(err, ErrModIconRefused) || !tooBig {
			t.Errorf("%dx%d: got %v, want refused as too big", c[0], c[1], err)
		}
	}
}

func TestModIconAcceptsTheLargestPicture(t *testing.T) {
	// 4096 by 4096 is the limit and is allowed. A flat picture compresses to
	// a few KiB, so the file limit is not what is tested here.
	img := image.NewNRGBA(image.Rect(0, 0, 4096, 4096))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > maxModIconBytes {
		t.Skip("the flat picture is over the file limit")
	}
	m, id, _ := iconWorld(t, "fakemod_1.0.0", map[string][]byte{"graphics/icons/widget.png": buf.Bytes()})
	b, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 4096, 128)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeIcon(t, b).Bounds(); got.Dx() != 128 || got.Dy() != 128 {
		t.Fatalf("size %v", got)
	}
}

// rawZip builds a zip with an info.json and one entry whose header says
// declared bytes, compressed and uncompressed, while data is what is stored
// (flate-compressed from real).
func rawZip(t *testing.T, name string, real []byte, declaredUncompressed uint64, declaredCompressed uint64) string {
	t.Helper()
	var packed bytes.Buffer
	fw, err := flate.NewWriter(&packed, flate.BestCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(real); err != nil {
		t.Fatal(err)
	}
	if err := fw.Close(); err != nil {
		t.Fatal(err)
	}
	if declaredCompressed == 0 {
		declaredCompressed = uint64(packed.Len()) //nolint:gosec // a length is never negative
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("fakemod_1.0.0/info.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte(`{"name":"fakemod","version":"1.0.0"}`))
	rw, err := zw.CreateRaw(&zip.FileHeader{
		Name:               "fakemod_1.0.0/" + name,
		Method:             zip.Deflate,
		CRC32:              crc32.ChecksumIEEE(real),
		CompressedSize64:   declaredCompressed,
		UncompressedSize64: declaredUncompressed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rw.Write(packed.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestModIconRefusesAZipBombEntry(t *testing.T) {
	// 8 MiB of zeros packs to a few KiB.
	real := make([]byte, 8<<20)
	cases := map[string]struct{ uncompressed, compressed uint64 }{
		"declared small, really large":  {1000, 0},
		"declared as the real size":     {uint64(len(real)), 0},
		"declared compressed too large": {1000, maxModIconBytes + 1},
	}
	for name, c := range cases {
		m, id, dir := checkWorld(t, "2.0.77", false)
		putZip(t, dir, "fakemod_1.0.0.zip", rawZip(t, "graphics/icons/widget.png", real, c.uncompressed, c.compressed))
		putModList(t, dir, "base", "fakemod")
		if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32); !errors.Is(err, ErrModIconRefused) {
			t.Errorf("%s: got %v, want refused", name, err)
		}
	}
}

func TestModIconRefusesAFileOverTheLimitEvenIfHonest(t *testing.T) {
	// Random-ish bytes that do not shrink: a file over 2 MiB, declared right.
	real := make([]byte, maxModIconBytes+1)
	x := uint32(1)
	for i := range real {
		x = x*1664525 + 1013904223
		real[i] = byte(x >> 24)
	}
	m, id, _ := iconWorld(t, "fakemod_1.0.0", map[string][]byte{"graphics/icons/widget.png": real})
	if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32); !errors.Is(err, ErrModIconRefused) {
		t.Fatalf("got %v, want refused", err)
	}
}

func TestModIconRefusesAZipOverTheEntryLimit(t *testing.T) {
	files := widget(t)
	for i := range maxModZipEntries + 1 {
		files[fmt.Sprintf("pad/%05d", i)] = nil
	}
	m, id, _ := iconWorld(t, "fakemod_1.0.0", files)
	if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32); !errors.Is(err, ErrModIconRefused) {
		t.Fatalf("got %v, want refused", err)
	}
}

func TestModIconRefusesAZipWithoutAnInfoJSONFolder(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("graphics/icons/widget.png")
	_, _ = w.Write(stripPNG(t, 64, 64, 64))
	_ = zw.Close()
	putZip(t, dir, "fakemod_1.0.0.zip", buf.String())
	putModList(t, dir, "base", "fakemod")
	if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32); err == nil {
		t.Fatal("read an icon from a zip with no top folder")
	}
}

func TestModIconRefusesABrokenZip(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putZip(t, dir, "fakemod_1.0.0.zip", "this is not a zip")
	putModList(t, dir, "base", "fakemod")
	if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32); !errors.Is(err, ErrModIconRefused) {
		t.Fatalf("got %v, want refused", err)
	}
}

func TestModIconDoesNotFollowASymlinkedZip(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	outside := filepath.Join(t.TempDir(), "real.zip")
	writeFile(t, outside, iconZip(t, "fakemod_1.0.0", widget(t)))
	if err := os.MkdirAll(filepath.Join(dir, ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, ModsDir, "fakemod_1.0.0.zip")); err != nil {
		t.Fatal(err)
	}
	putModList(t, dir, "base", "fakemod")
	b, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32)
	if err == nil || len(b) != 0 {
		t.Fatalf("read through a link: %v", err)
	}
	if !errors.Is(err, ErrModIconNotFound) && !errors.Is(err, ErrModIconRefused) {
		t.Fatalf("got %v, want not found or refused", err)
	}
}

func TestModIconRefusesAFolderMod(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	folder := filepath.Join(dir, ModsDir, "fakemod_1.0.0")
	if err := os.MkdirAll(filepath.Join(folder, "graphics", "icons"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(folder, "info.json"), `{"name":"fakemod","version":"1.0.0"}`)
	writeFile(t, filepath.Join(folder, "graphics", "icons", "widget.png"), string(stripPNG(t, 64, 64, 64)))
	putModList(t, dir, "base", "fakemod")
	if _, err := m.ModIcon(id, "fakemod", "1.0.0", iconPath, 64, 32); !errors.Is(err, ErrModIconRefused) {
		t.Fatalf("got %v, want refused", err)
	}
}
