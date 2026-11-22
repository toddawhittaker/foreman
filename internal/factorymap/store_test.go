package factorymap

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/saveplayers"
)

// pngOf encodes a w by h picture; mark paints the pixel (x, 0).
func pngOf(t testing.TB, w, h int, c color.NRGBA, xs ...int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for _, x := range xs {
		img.SetNRGBA(x, 0, c)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func drawing(version string, surfaces ...int) Drawing {
	d := Drawing{
		Basis:       saveplayers.Basis{Save: "_autosave1.zip", Size: 123, Modified: time.Date(2026, 11, 21, 10, 0, 0, 5, time.UTC)},
		DrawnAt:     time.Date(2026, 11, 21, 10, 5, 0, 0, time.UTC),
		GameVersion: version,
	}
	for _, i := range surfaces {
		d.Surfaces = append(d.Surfaces, Surface{Index: i, Name: "nauvis", Kind: "planet", Width: 4, Height: 2, TilesPerPixel: 1})
	}
	return d
}

func openStore(t *testing.T, id string) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestOpenRefusesBadIDs(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"", "..", ".", "../x", "a/b", "A", "a b", "-a", "a-", "a\x00", strings.Repeat("a", 200)} {
		if _, err := Open(dir, id); !errors.Is(err, ErrInvalid) {
			t.Errorf("Open(%q) = %v, want ErrInvalid", id, err)
		}
	}
	if _, err := Open("", "ok"); !errors.Is(err, ErrInvalid) {
		t.Errorf("empty data dir: %v", err)
	}
	if _, err := Open(dir, "my-world-2"); err != nil {
		t.Errorf("good id refused: %v", err)
	}
}

func TestEmptyStoreReadsNothingAndWritesNothing(t *testing.T) {
	s, dir := openStore(t, "w1")
	d, ok, err := s.Read()
	if err != nil || ok || len(d.Surfaces) != 0 {
		t.Fatalf("Read on empty = %+v %v %v", d, ok, err)
	}
	if _, err := s.OpenPNG(0, ""); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("OpenPNG on empty = %v, want not exist", err)
	}
	if _, err := s.Composite(0, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Composite on empty = %v, want not exist", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("reading made files: %v", entries)
	}
}

func TestReplaceThenReadAndOpen(t *testing.T) {
	s, dir := openStore(t, "w1")
	base := pngOf(t, 4, 2, color.NRGBA{255, 0, 0, 255}, 0)
	belts := pngOf(t, 4, 2, color.NRGBA{0, 255, 0, 255}, 1)
	d := drawing("2.0.77", 1, 5)
	pngs := map[string][]byte{
		PNGName(1, ""):                 base,
		PNGName(1, CategoryBelts):      belts,
		PNGName(5, ""):                 base,
		PNGName(5, CategoryProduction): belts,
	}
	if err := s.Replace(d, pngs); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Read()
	if err != nil || !ok {
		t.Fatalf("Read = %v %v", ok, err)
	}
	if got.Basis != d.Basis || !got.Basis.Modified.Equal(d.Basis.Modified) || !got.DrawnAt.Equal(d.DrawnAt) ||
		got.GameVersion != "2.0.77" || len(got.Surfaces) != 2 || got.Surfaces[1] != d.Surfaces[1] || got.Failure != "" {
		t.Errorf("Read = %+v, want %+v", got, d)
	}
	for name, want := range map[string]struct {
		surface int
		layer   Category
		bytes   []byte
	}{
		"base": {1, "", base}, "layer": {1, CategoryBelts, belts}, "second": {5, CategoryProduction, belts},
	} {
		f, err := s.OpenPNG(want.surface, want.layer)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := io.ReadAll(f)
		_ = f.Close()
		if !bytes.Equal(b, want.bytes) {
			t.Errorf("%s: bytes differ", name)
		}
	}
	if _, err := s.OpenPNG(2, ""); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("surface not in the drawing: %v", err)
	}
	if _, err := s.OpenPNG(1, CategoryDefence); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("layer with no picture: %v", err)
	}
	if _, err := s.OpenPNG(1, "../x"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad layer: %v", err)
	}
	// The store sits under factorymaps/<id> and nowhere else.
	if _, err := os.Stat(filepath.Join(dir, "factorymaps", "w1", "factorymap.json")); err != nil {
		t.Error(err)
	}
}

func TestReplaceKeepsOneGeneration(t *testing.T) {
	s, dir := openStore(t, "w1")
	for _, v := range []string{"2.0.70", "2.0.71", "2.0.72"} {
		if err := s.Replace(drawing(v, 0), map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "factorymaps", "w1"))
	if len(entries) != 2 {
		t.Errorf("folder holds %d entries, want factorymap.json and one generation: %v", len(entries), entries)
	}
}

func TestReplaceRefusalsLeaveThePreviousDrawing(t *testing.T) {
	s, _ := openStore(t, "w1")
	good := map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}
	if err := s.Replace(drawing("1", 0), good); err != nil {
		t.Fatal(err)
	}
	pic := pngOf(t, 4, 2, color.NRGBA{A: 255})
	cases := map[string]struct {
		d    Drawing
		pngs map[string][]byte
	}{
		"no base for a surface":     {drawing("2", 0, 1), good},
		"name leaves the folder":    {drawing("2", 0), map[string][]byte{PNGName(0, ""): pic, "../evil.png": pic}},
		"name with a slash":         {drawing("2", 0), map[string][]byte{PNGName(0, ""): pic, "a/surface-0-belts.png": pic}},
		"unknown category":          {drawing("2", 0), map[string][]byte{PNGName(0, ""): pic, "surface-0-nope.png": pic}},
		"layer for another surface": {drawing("2", 0), map[string][]byte{PNGName(0, ""): pic, PNGName(3, CategoryBelts): pic}},
		"leading zero":              {drawing("2", 0), map[string][]byte{PNGName(0, ""): pic, "surface-00.png": pic}},
		"not a PNG":                 {drawing("2", 0), map[string][]byte{PNGName(0, ""): []byte("GIF89a")}},
		"negative surface":          {drawing("2", -1), map[string][]byte{"surface--1.png": pic}},
		"surface twice":             {drawing("2", 0, 0), good},
	}
	for name, c := range cases {
		if err := s.Replace(c.d, c.pngs); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	got, ok, err := s.Read()
	if err != nil || !ok || got.GameVersion != "1" {
		t.Errorf("previous drawing lost: %+v %v %v", got, ok, err)
	}
}

func TestFailureKeepsThePreviousPicture(t *testing.T) {
	s, _ := openStore(t, "w1")
	pics := map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}
	old := drawing("2.0.77", 0)
	if err := s.Replace(old, pics); err != nil {
		t.Fatal(err)
	}
	if err := s.Replace(Drawing{Failure: "mods-differ", GameVersion: "other"}, nil); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.Read()
	if got.Failure != "mods-differ" || got.GameVersion != "2.0.77" || got.Basis != old.Basis || len(got.Surfaces) != 1 {
		t.Errorf("after a failure: %+v", got)
	}
	f, err := s.OpenPNG(0, "")
	if err != nil {
		t.Fatalf("picture lost after a failure: %v", err)
	}
	_ = f.Close()
	// A good drawing clears the failure.
	if err := s.Replace(drawing("2.0.78", 0), pics); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = s.Read(); got.Failure != "" || got.GameVersion != "2.0.78" {
		t.Errorf("failure not cleared: %+v", got)
	}
}

func TestFailureWithNoPreviousDrawing(t *testing.T) {
	s, _ := openStore(t, "w1")
	d := Drawing{Failure: "timeout", Basis: saveplayers.Basis{Save: "a.zip"}}
	if err := s.Replace(d, nil); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Read()
	if err != nil || !ok || got.Failure != "timeout" || len(got.Surfaces) != 0 || got.Basis.Save != "a.zip" {
		t.Errorf("Read = %+v %v %v", got, ok, err)
	}
	if _, err := s.OpenPNG(0, ""); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("OpenPNG = %v", err)
	}
}

func TestReplaceFailsAndLeavesOldDrawing(t *testing.T) {
	s, dir := openStore(t, "w1")
	pics := map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}
	if err := s.Replace(drawing("1", 0), pics); err != nil {
		t.Fatal(err)
	}
	// A read-only world folder makes the new generation impossible to write.
	w := filepath.Join(dir, "factorymaps", "w1")
	if err := os.Chmod(w, 0o500); err != nil { //nolint:gosec // a read-only folder is the point
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(w, 0o700) }() //nolint:gosec // lets the test folder be cleaned up
	if os.Geteuid() == 0 {
		t.Skip("root ignores folder modes")
	}
	if err := s.Replace(drawing("2", 0), pics); err == nil {
		t.Fatal("Replace into a read-only folder succeeded")
	}
	got, ok, err := s.Read()
	if err != nil || !ok || got.GameVersion != "1" {
		t.Errorf("old drawing lost: %+v %v %v", got, ok, err)
	}
	if f, err := s.OpenPNG(0, ""); err != nil {
		t.Errorf("old picture lost: %v", err)
	} else {
		_ = f.Close()
	}
}

func TestReplaceRefusesALinkInThePlaceOfTheFolder(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "factorymaps"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "factorymaps", "w1")); err != nil {
		t.Fatal(err)
	}
	s, _ := Open(dir, "w1")
	err := s.Replace(drawing("1", 0), map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})})
	if err == nil {
		t.Fatal("Replace followed a link")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("wrote through the link: %v", entries)
	}
	if _, ok, err := s.Read(); ok || err == nil {
		// A link is an error, not an empty store.
		t.Errorf("Read through a link: ok=%v err=%v", ok, err)
	}
}

func TestDamagedFactsAreAnErrorAndReplaceRepairsThem(t *testing.T) {
	s, dir := openStore(t, "w1")
	w := filepath.Join(dir, "factorymaps", "w1")
	if err := os.MkdirAll(w, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"junk":          "not json",
		"unknown field": `{"format":1,"dir":"g-0123456789abcdef","surfaces":[],"extra":1}`,
		"escaping dir":  `{"format":1,"dir":"../..","surfaces":[]}`,
		"wrong format":  `{"format":2,"dir":"g-0123456789abcdef","surfaces":[]}`,
		"negative":      `{"format":1,"dir":"g-0123456789abcdef","surfaces":[{"index":-1}]}`,
	} {
		if err := os.WriteFile(filepath.Join(w, "factorymap.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := s.Read(); ok || err == nil {
			t.Errorf("%s: Read = %v %v, want an error", name, ok, err)
		}
		if _, err := s.OpenPNG(0, ""); err == nil {
			t.Errorf("%s: OpenPNG worked", name)
		}
	}
	if err := s.Replace(drawing("3", 0), map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}); err != nil {
		t.Fatalf("Replace over a damaged record: %v", err)
	}
	if got, ok, err := s.Read(); err != nil || !ok || got.GameVersion != "3" {
		t.Errorf("after repair: %+v %v %v", got, ok, err)
	}
}

func TestOpenPNGNeverHangsOnAPipe(t *testing.T) {
	s, dir := openStore(t, "w1")
	if err := s.Replace(drawing("1", 0), map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}); err != nil {
		t.Fatal(err)
	}
	gens, _ := filepath.Glob(filepath.Join(dir, "factorymaps", "w1", "g-*"))
	if len(gens) != 1 {
		t.Fatalf("generations: %v", gens)
	}
	p := filepath.Join(gens[0], PNGName(0, CategoryBelts))
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip("no named pipes here:", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.OpenPNG(0, CategoryBelts)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a pipe was opened as a picture")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OpenPNG hung on a named pipe")
	}
}

func TestComposite(t *testing.T) {
	s, _ := openStore(t, "w1")
	red, green := color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 255, 0, 255}
	pngs := map[string][]byte{
		PNGName(0, ""):            pngOf(t, 4, 2, red, 0),
		PNGName(0, CategoryBelts): pngOf(t, 4, 2, green, 1),
		PNGName(0, CategoryRail):  pngOf(t, 4, 2, green, 2),
	}
	if err := s.Replace(drawing("1", 0), pngs); err != nil {
		t.Fatal(err)
	}
	img, err := s.Composite(0, []Category{CategoryBelts, CategoryBelts, CategoryPower})
	if err != nil {
		t.Fatal(err)
	}
	at := func(x int) color.NRGBA { return color.NRGBAModel.Convert(img.At(x, 0)).(color.NRGBA) }
	if at(0) != red || at(1) != green || at(2) != (color.NRGBA{}) {
		t.Errorf("pixels = %v %v %v; the unticked rail layer must not show and the missing power layer is skipped", at(0), at(1), at(2))
	}
	both, err := s.Composite(0, []Category{CategoryBelts, CategoryRail})
	if err != nil || color.NRGBAModel.Convert(both.At(2, 0)).(color.NRGBA) != green {
		t.Errorf("two layers: %v", err)
	}
	if _, err := s.Composite(0, []Category{"../x"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad layer: %v", err)
	}
	if _, err := s.Composite(9, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("no such surface: %v", err)
	}
	plain, err := s.Composite(0, nil)
	if err != nil || color.NRGBAModel.Convert(plain.At(0, 0)).(color.NRGBA) != red {
		t.Errorf("no layers: %v", err)
	}
}

func TestCompositeRefusesWrongSizesAndHugePictures(t *testing.T) {
	s, _ := openStore(t, "w1")
	c := color.NRGBA{255, 0, 0, 255}
	if err := s.Replace(drawing("1", 0), map[string][]byte{
		PNGName(0, ""):            pngOf(t, 4, 2, c),
		PNGName(0, CategoryBelts): pngOf(t, 5, 2, c),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Composite(0, []Category{CategoryBelts}); err == nil {
		t.Error("a layer of another size was stacked")
	}
	if err := s.Replace(drawing("1", 0), map[string][]byte{PNGName(0, ""): pngOf(t, MaxSidePixels+1, 1, c)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Composite(0, nil); err == nil {
		t.Error("a picture over the side limit was decoded")
	}
}

// A reader running while drawings are swapped sees the old drawing or the new
// one: the base and the layer of one Composite always come from the same one.
func TestReplaceIsAtomicForReaders(t *testing.T) {
	s, _ := openStore(t, "w1")
	red, blue := color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 255, 255}
	make1 := func(c color.NRGBA) map[string][]byte {
		return map[string][]byte{
			PNGName(0, ""):            pngOf(t, 4, 2, c, 0),
			PNGName(0, CategoryBelts): pngOf(t, 4, 2, c, 1),
		}
	}
	a, b := make1(red), make1(blue)
	if err := s.Replace(drawing("a", 0), a); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				img, err := s.Composite(0, []Category{CategoryBelts})
				if err != nil {
					errs <- err.Error()
					return
				}
				if img.At(0, 0) != img.At(1, 0) {
					errs <- "mixed drawing: base and layer differ"
					return
				}
				if f, err := s.OpenPNG(0, ""); err != nil {
					errs <- err.Error()
					return
				} else {
					_ = f.Close()
				}
			}
		}()
	}
	for i := range 60 {
		set, v := a, "a"
		if i%2 == 1 {
			set, v = b, "b"
		}
		if err := s.Replace(drawing(v, 0), set); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	pics := map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}
	for _, id := range []string{"w1", "w2"} {
		s, _ := Open(dir, id)
		if err := s.Replace(drawing("1", 0), pics); err != nil {
			t.Fatal(err)
		}
	}
	if err := Remove(dir, "w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "factorymaps", "w1")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("w1 kept: %v", err)
	}
	s2, _ := Open(dir, "w2")
	if _, ok, err := s2.Read(); !ok || err != nil {
		t.Errorf("w2 lost: %v %v", ok, err)
	}
	if err := Remove(dir, "w1"); err != nil {
		t.Errorf("removing none: %v", err)
	}
	if err := Remove(t.TempDir(), "w1"); err != nil {
		t.Errorf("removing from a data dir with no store: %v", err)
	}
	if err := Remove(filepath.Join(dir, "missing"), "w1"); err != nil {
		t.Errorf("removing from no data dir: %v", err)
	}
	for _, id := range []string{"..", "", "../w2", "factorymaps/w2"} {
		if err := Remove(dir, id); !errors.Is(err, ErrInvalid) {
			t.Errorf("Remove(%q) = %v, want ErrInvalid", id, err)
		}
	}
	if _, ok, _ := s2.Read(); !ok {
		t.Error("a bad id removed another world's pictures")
	}
}

func TestRemoveDoesNotFollowALink(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "factorymaps"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "factorymaps", "w1")); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir, "w1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Errorf("Remove followed a link: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "factorymaps", "w1")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("link kept: %v", err)
	}
}

func TestFailureTextIsCutAndNeverMakesTheRecordUnreadable(t *testing.T) {
	s, _ := openStore(t, "w1")
	pics := map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}
	if err := s.Replace(drawing("1", 0), pics); err != nil {
		t.Fatal(err)
	}
	// 3-byte characters, so the cut at 4096 bytes falls inside one.
	huge := strings.Repeat("€", 2<<20)
	for name, prior := range map[string]bool{"with a previous drawing": true, "with none": false} {
		if !prior {
			s, _ = openStore(t, "w2")
		}
		if err := s.Replace(Drawing{Failure: huge}, nil); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, ok, err := s.Read()
		if err != nil || !ok {
			t.Fatalf("%s: record unreadable: %v %v", name, ok, err)
		}
		if len(got.Failure) == 0 || len(got.Failure) > MaxFailureBytes || !utf8.ValidString(got.Failure) {
			t.Errorf("%s: failure is %d bytes, valid=%v", name, len(got.Failure), utf8.ValidString(got.Failure))
		}
	}
	// A failure sent together with pictures is cut too.
	if err := s.Replace(Drawing{Failure: huge}, pics); !errors.Is(err, ErrInvalid) {
		t.Errorf("failure with pictures but no surface base: %v", err)
	}
}

func TestFailureOnlyPathChecksTheDrawingToo(t *testing.T) {
	s, _ := openStore(t, "w1")
	pics := map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}
	if err := s.Replace(drawing("1", 0), pics); err != nil {
		t.Fatal(err)
	}
	year := func(y int) time.Time { return time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC) }
	manySurfaces := make([]Surface, MaxSurfaces+1)
	for i := range manySurfaces {
		manySurfaces[i] = Surface{Index: i, Kind: "planet"}
	}
	cases := map[string]Drawing{
		"year past 9999":    {Failure: "x", DrawnAt: year(10000)},
		"modified too old":  {Failure: "x", Basis: saveplayers.Basis{Modified: year(-1)}},
		"negative size":     {Failure: "x", Basis: saveplayers.Basis{Size: -1}},
		"long save name":    {Failure: "x", Basis: saveplayers.Basis{Save: strings.Repeat("a", 256)}},
		"long version":      {Failure: "x", GameVersion: strings.Repeat("1", 33)},
		"too many surfaces": {Failure: "x", Surfaces: manySurfaces},
		"negative surface":  {Failure: "x", Surfaces: []Surface{{Index: -1, Kind: "planet"}}},
		"repeated surface":  {Failure: "x", Surfaces: []Surface{{Kind: "planet"}, {Kind: "planet"}}},
		"unknown kind":      {Failure: "x", Surfaces: []Surface{{Kind: "galaxy"}}},
		"long surface name": {Failure: "x", Surfaces: []Surface{{Kind: "planet", Name: strings.Repeat("n", 201)}}},
		"huge picture":      {Failure: "x", Surfaces: []Surface{{Kind: "planet", Width: MaxSidePixels + 1}}},
	}
	for name, d := range cases {
		if err := s.Replace(d, nil); !errors.Is(err, ErrInvalid) {
			t.Errorf("failure only, %s: err = %v, want ErrInvalid", name, err)
		}
		d.Failure = ""
		if err := s.Replace(d, pics); !errors.Is(err, ErrInvalid) {
			t.Errorf("drawing, %s: err = %v, want ErrInvalid", name, err)
		}
	}
	got, ok, err := s.Read()
	if err != nil || !ok || got.GameVersion != "1" || got.Failure != "" {
		t.Errorf("previous drawing damaged: %+v %v %v", got, ok, err)
	}
	// With no previous drawing a failure keeps no surfaces, so the record
	// never lists a picture it does not have.
	s2, _ := openStore(t, "w2")
	if err := s2.Replace(Drawing{Failure: "x", Surfaces: []Surface{{Index: 3, Kind: "planet"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s2.Read(); len(got.Surfaces) != 0 || got.Failure != "x" {
		t.Errorf("failure alone: %+v", got)
	}
}

func TestReplaceClearsWhatACrashLeftBehind(t *testing.T) {
	pics := map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}
	for name, replace := range map[string]func(*Store) error{
		"a drawing": func(s *Store) error { return s.Replace(drawing("2", 0), pics) },
		"a failure": func(s *Store) error { return s.Replace(Drawing{Failure: "timeout"}, nil) },
	} {
		s, dir := openStore(t, "w1")
		if err := s.Replace(drawing("1", 0), pics); err != nil {
			t.Fatal(err)
		}
		w := filepath.Join(dir, "factorymaps", "w1")
		partial := filepath.Join(w, ".partial-0123456789abcdef")
		orphan := filepath.Join(w, "g-fedcba9876543210")
		stray := filepath.Join(w, "notes.txt")
		if err := os.WriteFile(partial, []byte(`{"format":1`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(orphan, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(stray, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := replace(s); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, p := range []string{partial, orphan} {
			if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s: %s kept: %v", name, filepath.Base(p), err)
			}
		}
		if _, err := os.Lstat(stray); err != nil {
			t.Errorf("%s: removed a file that is not the store's: %v", name, err)
		}
		if _, ok, err := s.Read(); !ok || err != nil {
			t.Errorf("%s: record lost: %v %v", name, ok, err)
		}
		if f, err := s.OpenPNG(0, ""); err != nil {
			t.Errorf("%s: picture lost: %v", name, err)
		} else {
			_ = f.Close()
		}
	}
}

func TestSweepRemovesStoresOfWorldsThatAreGone(t *testing.T) {
	dir := t.TempDir()
	pics := map[string][]byte{PNGName(0, ""): pngOf(t, 4, 2, color.NRGBA{A: 255})}
	for _, id := range []string{"keep-me", "gone-1", "gone-2"} {
		s, _ := Open(dir, id)
		if err := s.Replace(drawing("1", 0), pics); err != nil {
			t.Fatal(err)
		}
	}
	top := filepath.Join(dir, "factorymaps")
	if err := os.WriteFile(filepath.Join(top, "Not_An_ID.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var asked []string
	kept, _ := Open(dir, "keep-me")
	err := Sweep(dir, func(id string) bool {
		asked = append(asked, id)
		// The callback may use a store without deadlocking.
		_, _, _ = kept.Read()
		return id == "keep-me"
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"gone-1", "gone-2"} {
		if _, err := os.Lstat(filepath.Join(top, id)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s kept: %v", id, err)
		}
	}
	if _, ok, err := kept.Read(); !ok || err != nil {
		t.Errorf("a kept world lost its pictures: %v %v", ok, err)
	}
	if _, err := os.Lstat(filepath.Join(top, "Not_An_ID.txt")); err != nil {
		t.Errorf("a file that is no store was removed: %v", err)
	}
	for _, id := range asked {
		if id == "Not_An_ID.txt" {
			t.Error("keep was asked about a name that is no world id")
		}
	}
	if len(asked) != 3 {
		t.Errorf("keep asked about %v, want the three stores", asked)
	}
}

func TestSweepWithNothingToSweep(t *testing.T) {
	never := func(string) bool { t.Error("keep called"); return true }
	if err := Sweep(filepath.Join(t.TempDir(), "missing"), never); err != nil {
		t.Errorf("no data dir: %v", err)
	}
	if err := Sweep(t.TempDir(), never); err != nil {
		t.Errorf("no store folder: %v", err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "factorymaps"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Sweep(dir, never); err == nil {
		t.Error("a file in place of the store folder was not reported")
	}
}

func TestSweepDoesNotFollowALink(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "factorymaps"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "factorymaps", "gone")); err != nil {
		t.Fatal(err)
	}
	if err := Sweep(dir, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Errorf("Sweep followed a link: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "factorymaps", "gone")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("link kept: %v", err)
	}
}
