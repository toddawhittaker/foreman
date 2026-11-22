package game

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// memMapStore is a world's kept pictures in memory.
type memMapStore struct {
	mu      sync.Mutex
	drawing factorymap.Drawing
	kept    bool
	pngs    map[string][]byte
	fail    error
}

func (s *memMapStore) Read() (factorymap.Drawing, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drawing, s.kept, s.fail
}

func (s *memMapStore) Replace(d factorymap.Drawing, pngs map[string][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drawing, s.kept, s.pngs = d, true, pngs
	return nil
}

func (s *memMapStore) get() (factorymap.Drawing, bool, map[string][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drawing, s.kept, s.pngs
}

// mapRig is a rig whose factory map runs, drawing and store are fakes: a run
// reads the world's LoadTarget as its basis and, unless told to block or
// fail, draws one surface.
type mapRig struct {
	*rig
	mu     sync.Mutex
	stores map[string]*memMapStore
	runs   int
	// block, when set, holds each run until it is closed or the run's
	// context ends.
	block chan struct{}
	// runErr is what a run returns instead of drawing.
	runErr error
	// started gets a value as each run begins.
	started chan string
	// changeSave, when set, is called during a run.
	changeSave func(id string)
}

func newMapRig(t *testing.T) *mapRig {
	t.Helper()
	r := &mapRig{rig: newRig(t, rigOptions{noRun: true}), stores: map[string]*memMapStore{}, started: make(chan string, 16)}
	fm := &r.c.factoryMaps
	fm.every = time.Hour // the tests call factoryMapTick themselves
	fm.openStore = func(id string) (factoryMapStore, error) { return r.store(id), nil }
	fm.removeStore = func(id string) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.stores, id)
		return nil
	}
	fm.runSave = r.runSave
	fm.drawFiles = func(*os.Root) (drawnFactoryMap, error) {
		return drawnFactoryMap{
			gameVersion: "2.1.24",
			surfaces:    []factorymap.Surface{{Index: 1, Name: "nauvis", Kind: "planet", Width: 32, Height: 32, TilesPerPixel: 1}},
			pngs:        map[string][]byte{factorymap.PNGName(1, ""): []byte("base"), factorymap.PNGName(1, factorymap.CategoryBelts): []byte("belts")},
		}, nil
	}
	r.stop = r.run(r.c)
	return r
}

// hasStore is whether the world has kept pictures or a failure.
func (r *mapRig) hasStore(id string) bool {
	r.mu.Lock()
	s := r.stores[id]
	r.mu.Unlock()
	if s == nil {
		return false
	}
	_, kept, _ := s.get()
	return kept
}

func (r *mapRig) store(id string) *memMapStore {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stores[id]
	if s == nil {
		s = &memMapStore{}
		r.stores[id] = s
	}
	return s
}

func (r *mapRig) runCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs
}

func (r *mapRig) runSave(ctx context.Context, id string, m saveCopyMod, read func(*os.Root, string) error) (saveplayers.Basis, error) {
	r.mu.Lock()
	r.runs++
	block, runErr, change := r.block, r.runErr, r.changeSave
	r.mu.Unlock()
	r.started <- id
	if m.kind != gameenv.KindFactoryMap || !m.outputOnDisk || m.memory != FactoryMapMemory {
		return saveplayers.Basis{}, fmt.Errorf("wrong mod %+v", m.controlMod)
	}
	target, _, err := r.worlds.LoadTarget(id)
	if err != nil {
		return saveplayers.Basis{}, err
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return saveplayers.Basis{}, ctx.Err()
		}
	}
	if runErr != nil {
		return saveplayers.Basis{}, runErr
	}
	if change != nil {
		change(id)
	}
	work := r.t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "w", gameenv.FactoryMapDir), 0o750); err != nil {
		return saveplayers.Basis{}, err
	}
	base, err := os.OpenRoot(work)
	if err != nil {
		return saveplayers.Basis{}, err
	}
	defer func() { _ = base.Close() }()
	return basisOf(target), read(base, "w")
}

// setMeta changes a world's world.json.
func (r *mapRig) setMeta(id string, f func(*world.Meta)) {
	r.t.Helper()
	root, err := r.worlds.OpenWorld(id)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	m, err := world.ReadMeta(root)
	if err != nil {
		r.t.Fatal(err)
	}
	f(&m)
	if err := world.WriteMeta(root, m); err != nil {
		r.t.Fatal(err)
	}
}

func (r *mapRig) setMode(id, mode string, skipOnline bool) {
	r.setMeta(id, func(m *world.Meta) { m.FactoryMap = &world.FactoryMapMode{Mode: mode, SkipOnline: skipOnline} })
}

// waitIdle waits for the world's run to end.
func (r *mapRig) waitIdle(id string) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.c.factoryMaps.mu.Lock()
		busy := r.c.factoryMaps.drawing[id] != nil
		r.c.factoryMaps.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatal("the factory map run never ended")
}

func (r *mapRig) view(id string) FactoryMapView {
	r.t.Helper()
	v, err := r.c.FactoryMap(id)
	if err != nil {
		r.t.Fatal(err)
	}
	return v
}

// waitListed waits for the monitor's first listing of Docker, before which
// no game can be known to be stopped.
func (r *mapRig) waitListed() {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if listed, err := r.c.dockerState(); listed && err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.t.Fatal("Docker was never listed")
}

// SPEC 5.16: a world without a factory map setting is manual and skips
// while players are online.
func TestFactoryMapDefaultMode(t *testing.T) {
	r := newMapRig(t)
	id := r.newWorld("Base")
	v := r.view(id)
	if v.Mode != (world.FactoryMapMode{Mode: world.FactoryMapManual, SkipOnline: true}) {
		t.Errorf("mode %+v, want manual and skip online", v.Mode)
	}
	if v.State != FactoryMapNever {
		t.Errorf("state %q, want never", v.State)
	}
}

// SPEC 5.16: games from 2.0.61 only.
func TestFactoryMapSupportedVersions(t *testing.T) {
	for v, want := range map[string]bool{
		"1.1.110": false, "2.0.60": false, "2.0.61": true, "2.0.77": true, "2.1.24": true, "": false, "junk": false,
	} {
		if got := factoryMapSupported(v); got != want {
			t.Errorf("factoryMapSupported(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestFactoryMapUnsupportedWorld(t *testing.T) {
	r := newMapRig(t)
	id, err := r.worlds.Create(context.Background(), "Old", "1.1.110", false)
	if err != nil {
		t.Fatal(err)
	}
	r.putSave(id, "2.1.24-no-space-age.zip", "base.zip", time.Now().Add(-time.Hour))
	if v := r.view(id); v.State != FactoryMapUnsupported {
		t.Errorf("state %q, want unsupported", v.State)
	}
	if err := r.c.DrawFactoryMap(context.Background(), id); !errors.Is(err, ErrFactoryMapNotSupported) {
		t.Errorf("draw: %v, want not supported", err)
	}
	r.setMode(id, world.FactoryMapAutomatic, true)
	if r.c.factoryMapDue(id) {
		t.Error("an unsupported world is due for an automatic drawing")
	}
}

func TestDrawFactoryMapRefusals(t *testing.T) {
	ctx := context.Background()
	t.Run("off", func(t *testing.T) {
		r := newMapRig(t)
		id := r.newWorld("Base")
		r.setMode(id, world.FactoryMapOff, true)
		if err := r.c.DrawFactoryMap(ctx, id); !errors.Is(err, ErrFactoryMapOff) {
			t.Errorf("draw: %v, want off", err)
		}
		if v := r.view(id); v.State != FactoryMapOff {
			t.Errorf("state %q, want off", v.State)
		}
	})
	t.Run("archived", func(t *testing.T) {
		r := newMapRig(t)
		id := r.newWorld("Base")
		r.setMeta(id, func(m *world.Meta) { m.Archived = true })
		if err := r.c.DrawFactoryMap(ctx, id); !errors.Is(err, ErrFactoryMapArchived) {
			t.Errorf("draw: %v, want archived", err)
		}
	})
	t.Run("no save", func(t *testing.T) {
		r := newMapRig(t)
		id, err := r.worlds.Create(ctx, "Empty", "2.1.24", false)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.c.DrawFactoryMap(ctx, id); !errors.Is(err, ErrFactoryMapNoSave) {
			t.Errorf("draw: %v, want no save", err)
		}
	})
}

// SPEC 5.16: with "skip while players are online" on, a click to draw a
// running world is refused while anyone is on; with it off, it runs.
func TestDrawFactoryMapPlayersOnline(t *testing.T) {
	ctx := context.Background()
	r := newMapRig(t)
	id := r.newWorld("Base")
	r.start(id)
	if err := r.game.Join(ContainerName(id), "dana"); err != nil {
		t.Fatal(err)
	}
	if err := r.c.DrawFactoryMap(ctx, id); !errors.Is(err, ErrFactoryMapPlayersOnline) {
		t.Fatalf("draw with a player on: %v, want players online", err)
	}
	if n := r.runCount(); n != 0 {
		t.Errorf("%d runs after a refusal", n)
	}
	v := r.view(id)
	if !v.Running {
		t.Error("the view says no game runs")
	}
	r.setMode(id, world.FactoryMapManual, false)
	if err := r.c.DrawFactoryMap(ctx, id); err != nil {
		t.Fatalf("draw with skip off: %v", err)
	}
	r.waitIdle(id)
	if _, kept, _ := r.store(id).get(); !kept {
		t.Error("no picture kept")
	}
}

// A running world with nobody on is drawn on a click, even with skip on.
func TestDrawFactoryMapRunningNobodyOn(t *testing.T) {
	r := newMapRig(t)
	id := r.newWorld("Base")
	r.start(id)
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatalf("draw: %v", err)
	}
	r.waitIdle(id)
	if _, kept, _ := r.store(id).get(); !kept {
		t.Error("no picture kept")
	}
}

// SPEC 5.16, 8.16: a drawing is kept with the save it was read from, and
// the page hears of it.
func TestDrawFactoryMapKeepsDrawing(t *testing.T) {
	r := newMapRig(t)
	id := r.newWorld("Base")
	_, updates, stopWatch := r.c.Watch(id)
	defer stopWatch()
	r.block = make(chan struct{})
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	<-r.started
	if v := r.view(id); v.State != FactoryMapDrawing {
		t.Errorf("state %q while drawing, want drawing", v.State)
	}
	// A second click while drawing starts no second run.
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	close(r.block)
	r.waitIdle(id)
	if n := r.runCount(); n != 1 {
		t.Errorf("%d runs, want 1", n)
	}
	target, _, err := r.worlds.LoadTarget(id)
	if err != nil {
		t.Fatal(err)
	}
	d, kept, pngs := r.store(id).get()
	if !kept || !sameBasis(d.Basis, basisOf(target)) || d.GameVersion != "2.1.24" || len(d.Surfaces) != 1 || d.DrawnAt.IsZero() {
		t.Errorf("kept %v drawing %+v, want the save %+v", kept, d, target)
	}
	if string(pngs["surface-1.png"]) != "base" || string(pngs["surface-1-belts.png"]) != "belts" {
		t.Errorf("pictures %v", pngs)
	}
	v := r.view(id)
	if v.State != FactoryMapDrawn || v.Failure != "" || v.Drawing.GameVersion != "2.1.24" {
		t.Errorf("view %+v, want drawn", v)
	}
	kinds := 0
	timeout := time.After(2 * time.Second)
	for kinds < 2 {
		select {
		case u := <-updates:
			if u.Kind == UpdateFactoryMap {
				kinds++
			}
		case <-timeout:
			t.Fatalf("%d factory map updates, want 2 (start and end)", kinds)
		}
	}
}

// With the mode off, a kept picture still shows, as drawn (SPEC 5.16).
func TestFactoryMapOffKeepsPicture(t *testing.T) {
	r := newMapRig(t)
	id := r.newWorld("Base")
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(id)
	r.setMode(id, world.FactoryMapOff, true)
	if v := r.view(id); v.State != FactoryMapDrawn {
		t.Errorf("state %q, want drawn", v.State)
	}
}

// SPEC 8.16: the result is kept only if the save that loads next is still
// the save that was read.
func TestFactoryMapNotKeptWhenSaveChanged(t *testing.T) {
	r := newMapRig(t)
	id := r.newWorld("Base")
	r.changeSave = func(id string) {
		r.putSave(id, "2.1.24-no-space-age.zip", "newer.zip", time.Now())
	}
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(id)
	if _, kept, _ := r.store(id).get(); kept {
		t.Error("a drawing of an older save was kept")
	}
	if v := r.view(id); v.State != FactoryMapNever {
		t.Errorf("state %q, want never", v.State)
	}
}

func TestFactoryMapFailure(t *testing.T) {
	r := newMapRig(t)
	id := r.newWorld("Base")
	r.runErr = fmt.Errorf("run: %w", ErrSaveCopyOutOfMemory)
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(id)
	v := r.view(id)
	if v.State != FactoryMapFailed || v.Failure != saveplayers.ResultOutOfMemory {
		t.Errorf("view %+v, want failed out-of-memory", v)
	}
	// A later success clears it.
	r.runErr = nil
	if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	r.waitIdle(id)
	if v := r.view(id); v.State != FactoryMapDrawn || v.Failure != "" {
		t.Errorf("view %+v, want drawn", v)
	}
}

// A run cancelled (by a start or shutdown), or one that never got the
// preview slot, is not a failure, so an automatic run tries the save again.
func TestFactoryMapCancelledIsNotAFailure(t *testing.T) {
	for _, runErr := range []error{context.Canceled, fmt.Errorf("slot: %w", ErrPreviewBusy)} {
		r := newMapRig(t)
		id := r.newWorld("Base")
		r.runErr = runErr
		if err := r.c.DrawFactoryMap(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		r.waitIdle(id)
		if v := r.view(id); v.State != FactoryMapNever {
			t.Errorf("%v: state %q, want never", runErr, v.State)
		}
	}
}

func TestFactoryMapFailureIDs(t *testing.T) {
	for err, want := range map[error]string{
		ErrSaveCopyImageMissing: saveplayers.ResultImageMissing,
		ErrSaveCopyOutOfMemory:  saveplayers.ResultOutOfMemory,
		ErrSaveCopyTimeout:      saveplayers.ResultTimeout,
		ErrSaveCopyModsDiffer:   saveplayers.ResultModsDiffer,
		ErrSaveCopySaveNewer:    saveplayers.ResultSaveNewer,
		ErrSaveCopyBadOutput:    saveplayers.ResultBadOutput,
		errFactoryMapTooLarge:   FailureTooLarge,
		errors.New("anything"):  saveplayers.ResultBadOutput,
	} {
		if got := factoryMapFailureID(fmt.Errorf("wrapped: %w", err)); got != want {
			t.Errorf("%v gives %q, want %q", err, got, want)
		}
	}
}

// SPEC 14.6: output files are plain regular files in the folder, read under
// one budget for all of them.
func TestOpenOutputFileRefuses(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.jsonl"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("ok.jsonl", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	for _, name := range []string{"", ".", "..", "sub", "sub/x", "../ok.jsonl", "link", "pipe", "missing"} {
		f, err := openOutputFile(root, name, &outputBudget{left: 100})
		if !errors.Is(err, ErrSaveCopyBadOutput) {
			t.Errorf("%q: %v, want bad output", name, err)
		}
		if f != nil {
			_ = f.Close()
		}
	}
	if b, err := readOutputFile(root, "ok.jsonl", &outputBudget{left: 10}); err != nil || string(b) != "0123456789" {
		t.Errorf("a file the size of the budget: %q, %v", b, err)
	}
	if _, err := readOutputFile(root, "ok.jsonl", &outputBudget{left: 9}); !errors.Is(err, errFactoryMapTooLarge) {
		t.Errorf("a file past the budget: %v, want too large", err)
	}
	// The budget is shared: two reads of 10 bytes pass 16.
	shared := &outputBudget{left: 16}
	if _, err := readOutputFile(root, "ok.jsonl", shared); err != nil {
		t.Fatal(err)
	}
	if _, err := readOutputFile(root, "ok.jsonl", shared); !errors.Is(err, errFactoryMapTooLarge) {
		t.Errorf("the second read: %v, want too large", err)
	}
}

// A file that grows past the budget after it was checked is still cut off.
func TestBudgetReaderStopsAtBudget(t *testing.T) {
	b := &outputBudget{left: 4}
	r := &budgetReader{r: &endless{}, b: b}
	buf := make([]byte, 16)
	total := 0
	for {
		n, err := r.Read(buf)
		total += n
		if err != nil {
			if !errors.Is(err, errFactoryMapTooLarge) {
				t.Fatalf("err %v, want too large", err)
			}
			break
		}
	}
	if total != 4 {
		t.Errorf("read %d bytes, want 4", total)
	}
	if !errors.Is(b.wrap(errors.New("parse")), errFactoryMapTooLarge) {
		t.Error("a parse error after the budget ran out is not too large")
	}
}

type endless struct{}

func (endless) Read(p []byte) (int, error) { return len(p), nil }

// fakeDrawing replaces the drawing steps for one test. The manifest names
// a chart and an entity file per surface; each step reads its whole input,
// so the budget counts it, and records how many charts are decoded at once.
type fakeDrawing struct {
	mu            sync.Mutex
	open, maxOpen int
	order         []string
}

func useFakeDrawing(t *testing.T, surfaces []factorymap.ManifestSurface) *fakeDrawing {
	t.Helper()
	fd := &fakeDrawing{}
	oldM, oldC, oldO := parseManifest, drawChart, drawOverlay
	t.Cleanup(func() { parseManifest, drawChart, drawOverlay = oldM, oldC, oldO })
	parseManifest = func(r io.Reader) (factorymap.Manifest, error) {
		_, _ = io.ReadAll(r)
		return factorymap.Manifest{Format: 1, GameVersion: "2.0.77", Surfaces: surfaces}, nil
	}
	drawChart = func(_, chunks io.Reader, surface int) (*image.NRGBA, factorymap.Surface, error) {
		fd.mu.Lock()
		fd.open++
		fd.maxOpen = max(fd.maxOpen, fd.open)
		fd.order = append(fd.order, fmt.Sprintf("chart %d", surface))
		fd.mu.Unlock()
		defer func() {
			fd.mu.Lock()
			fd.open--
			fd.mu.Unlock()
		}()
		if _, err := io.ReadAll(chunks); err != nil {
			return nil, factorymap.Surface{}, err
		}
		return image.NewNRGBA(image.Rect(0, 0, 2, 2)), factorymap.Surface{Index: surface, Width: 2, Height: 2, TilesPerPixel: 1}, nil
	}
	drawOverlay = func(entities io.Reader, s factorymap.Surface) (map[factorymap.Category]*image.Paletted, error) {
		fd.mu.Lock()
		fd.order = append(fd.order, fmt.Sprintf("overlay %d", s.Index))
		fd.mu.Unlock()
		if _, err := io.ReadAll(entities); err != nil {
			return nil, err
		}
		p := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Transparent, color.White})
		return map[factorymap.Category]*image.Paletted{factorymap.CategoryRail: p}, nil
	}
	return fd
}

// outputDir writes the manifest and size bytes into each named file.
func outputDir(t *testing.T, files map[string]int) *os.Root {
	t.Helper()
	dir := t.TempDir()
	files["manifest.json"] = 10
	for name, size := range files {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func twoSurfaces() []factorymap.ManifestSurface {
	return []factorymap.ManifestSurface{
		{Index: 1, Name: "nauvis", Kind: "planet", Chart: "surface-1.jsonl", Entities: "surface-1-entities.jsonl"},
		{Index: 2, Name: "vulcanus", Kind: "planet", Chart: "surface-2.jsonl", Entities: "surface-2-entities.jsonl"},
	}
}

// SPEC 5.16: surfaces are drawn one at a time, each chart then its layers,
// and every picture is kept by its store name.
func TestDrawFactoryMapFilesOneSurfaceAtATime(t *testing.T) {
	fd := useFakeDrawing(t, twoSurfaces())
	dir := outputDir(t, map[string]int{"surface-1.jsonl": 100, "surface-1-entities.jsonl": 100, "surface-2.jsonl": 100, "surface-2-entities.jsonl": 100})
	out, err := drawFactoryMapFilesUnder(dir, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if fd.maxOpen != 1 {
		t.Errorf("%d charts decoded at once, want 1", fd.maxOpen)
	}
	if want := []string{"chart 1", "overlay 1", "chart 2", "overlay 2"}; fmt.Sprint(fd.order) != fmt.Sprint(want) {
		t.Errorf("order %v, want %v", fd.order, want)
	}
	for _, name := range []string{"surface-1.png", "surface-1-rail.png", "surface-2.png", "surface-2-rail.png"} {
		if len(out.pngs[name]) == 0 {
			t.Errorf("no picture %s in %v", name, out.pngs)
		}
	}
	if out.gameVersion != "2.0.77" || len(out.surfaces) != 2 {
		t.Errorf("drawn %+v", out)
	}
}

// SPEC 14.6: the 512 MiB limit is the total of all output files, so files
// each under it but together over it are refused, before any is drawn.
func TestDrawFactoryMapFilesTotalLimit(t *testing.T) {
	fd := useFakeDrawing(t, twoSurfaces())
	dir := outputDir(t, map[string]int{"surface-1.jsonl": 300, "surface-1-entities.jsonl": 300, "surface-2.jsonl": 300, "surface-2-entities.jsonl": 300})
	_, err := drawFactoryMapFilesUnder(dir, 1000)
	if !errors.Is(err, errFactoryMapTooLarge) || factoryMapFailureID(err) != FailureTooLarge {
		t.Fatalf("err %v, want too large", err)
	}
	if len(fd.order) != 0 {
		t.Errorf("drew %v before refusing", fd.order)
	}
}

// The manifest counts toward the total too.
func TestDrawFactoryMapFilesManifestCounts(t *testing.T) {
	useFakeDrawing(t, twoSurfaces())
	dir := outputDir(t, map[string]int{"surface-1.jsonl": 245, "surface-1-entities.jsonl": 245, "surface-2.jsonl": 245, "surface-2-entities.jsonl": 245})
	if _, err := drawFactoryMapFilesUnder(dir, 989); !errors.Is(err, errFactoryMapTooLarge) {
		t.Fatalf("err %v, want too large", err)
	}
	if _, err := drawFactoryMapFilesUnder(dir, 990); err != nil {
		t.Fatalf("exactly the limit: %v", err)
	}
}

// A file the manifest names that is not a plain file in the folder refuses
// the whole drawing.
func TestDrawFactoryMapFilesBadName(t *testing.T) {
	useFakeDrawing(t, []factorymap.ManifestSurface{{Index: 1, Chart: "../escape.jsonl"}})
	dir := outputDir(t, map[string]int{})
	if _, err := drawFactoryMapFilesUnder(dir, 1000); !errors.Is(err, ErrSaveCopyBadOutput) {
		t.Fatalf("err %v, want bad output", err)
	}
}

// SPEC 14.6: a link, a pipe or a folder in place of the manifest, a chart
// or an entity file refuses the drawing at once; a pipe never blocks it.
func TestDrawFactoryMapFilesRefuseNonRegular(t *testing.T) {
	makers := map[string]func(path string) error{
		"symlink": func(p string) error { return os.Symlink("/etc/passwd", p) },
		"fifo":    func(p string) error { return syscall.Mkfifo(p, 0o600) },
		"folder":  func(p string) error { return os.Mkdir(p, 0o750) },
	}
	for _, file := range []string{"manifest.json", "surface-1.jsonl", "surface-1-entities.jsonl"} {
		for kind, mk := range makers {
			t.Run(file+" "+kind, func(t *testing.T) {
				useFakeDrawing(t, []factorymap.ManifestSurface{{Index: 1, Chart: "surface-1.jsonl", Entities: "surface-1-entities.jsonl"}})
				dir := t.TempDir()
				for _, f := range []string{"manifest.json", "surface-1.jsonl", "surface-1-entities.jsonl"} {
					p := filepath.Join(dir, f)
					var err error
					if f == file {
						err = mk(p)
					} else {
						err = os.WriteFile(p, []byte("{}"), 0o600)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				root, err := os.OpenRoot(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = root.Close() }()
				done := make(chan error, 1)
				go func() {
					_, err := drawFactoryMapFilesUnder(root, 1000)
					done <- err
				}()
				select {
				case err := <-done:
					if !errors.Is(err, ErrSaveCopyBadOutput) {
						t.Errorf("err %v, want bad output", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("the drawing hung")
				}
			})
		}
	}
}
