package game

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	factorymapmod "github.com/toddawhittaker/foreman/internal/game/factorymap"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The factory map (SPEC 5.16): a run of a copy of the world's save (SPEC
// 8.16) whose helper mod writes the game's chart and the buildings, drawn
// into pictures that are kept outside the world.

// FactoryMapMinVersion is the oldest game the factory map reads: older
// games lack what the helper mod needs (SPEC 5.16).
const FactoryMapMinVersion = "2.0.61"

// FactoryMapMemory is the factory map run's memory limit, with no swap
// (SPEC 8.16).
const FactoryMapMemory = 12 << 30

// The states of FactoryMapView.
const (
	// FactoryMapNever is a world with no picture and no run yet.
	FactoryMapNever = "never"
	// FactoryMapDrawing is a run in flight.
	FactoryMapDrawing = "drawing"
	// FactoryMapDrawn is a kept picture, also with the mode off.
	FactoryMapDrawn = "drawn"
	// FactoryMapFailed is a last run that failed; Failure says why.
	FactoryMapFailed = "failed"
	// FactoryMapUnsupported is a game older than FactoryMapMinVersion.
	FactoryMapUnsupported = "unsupported"
	// FactoryMapOff is the mode off with no picture kept.
	FactoryMapOff = "off"
)

// FailureTooLarge is the failure id of a run whose output passed the limits
// of SPEC 14.6. The other ids are saveplayers.Result*.
const FailureTooLarge = "too-large"

// The refusals of DrawFactoryMap (SPEC 5.16).
var (
	ErrFactoryMapNotSupported  = errors.New("the factory map needs Factorio " + FactoryMapMinVersion + " or newer")
	ErrFactoryMapOff           = errors.New("the factory map is off for this world")
	ErrFactoryMapPlayersOnline = errors.New("players are online, and this world draws its map only while nobody is")
	ErrFactoryMapNoSave        = errors.New("the world has no save to draw")
	ErrFactoryMapArchived      = errors.New("the world is archived")
)

// errFactoryMapTooLarge is output past the limits of SPEC 14.6.
var errFactoryMapTooLarge = errors.New("the factory map output is too large")

// factoryMapState is the factory map runs in flight and the loop's memory, by
// world id. The zero value is ready to use.
type factoryMapState struct {
	mu sync.Mutex
	// drawing holds each world's run in flight, by the function that
	// cancels it.
	drawing map[string]context.CancelFunc
	// failed is each world's last failed run: the save it tried and why.
	// The store keeps the failure too, but beside the last picture's save,
	// so only this says which save failed; a failed save is therefore tried
	// again once after Foreman restarts.
	failed map[string]factoryMapFailure
	// stoppedSince is when the loop first saw each world stopped.
	stoppedSince map[string]time.Time

	// Tests replace these; nil means the real run, drawing and store.
	runSave     func(ctx context.Context, id string, m saveCopyMod, read func(base *os.Root, dir string) error) (saveplayers.Basis, error)
	drawFiles   func(dir *os.Root) (drawnFactoryMap, error)
	openStore   func(id string) (factoryMapStore, error)
	removeStore func(id string) error
	every       time.Duration
}

type factoryMapFailure struct {
	basis saveplayers.Basis
	id    string
}

// factoryMapStore is what a run and the view need of a world's kept
// pictures.
type factoryMapStore interface {
	Read() (factorymap.Drawing, bool, error)
	Replace(d factorymap.Drawing, pngs map[string][]byte) error
}

// drawnFactoryMap is a finished drawing before it is kept.
type drawnFactoryMap struct {
	gameVersion string
	surfaces    []factorymap.Surface
	pngs        map[string][]byte
}

// factoryMapMod is the factory map run's control mod.
var factoryMapMod = saveCopyMod{
	controlMod: controlMod{
		kind:    gameenv.KindFactoryMap,
		what:    "factory map",
		name:    factorymapmod.ModName,
		dir:     factorymapmod.ModDir,
		control: factorymapmod.Control,
		// The run moves the helper's whole folder into the work folder.
		output: gameenv.FactoryMapDir,
		memory: FactoryMapMemory,
	},
}

// FactoryMapView is what the map page shows of a world's factory map.
type FactoryMapView struct {
	// State and Failure say what the map is doing and why a drawing failed.
	State, Failure string
	// Mode is the world's setting.
	Mode world.FactoryMapMode
	// Drawing is the kept picture's facts; its DrawnAt is zero when none.
	Drawing factorymap.Drawing
	// Running is whether a game runs on this Foreman, and PlayersOnline
	// whether this world's game has players (SPEC 5.16).
	Running, PlayersOnline bool
}

// factoryMapModeOf is the world's mode; a world without one is manual and
// skips while players are online (SPEC 5.16).
func factoryMapModeOf(meta world.Meta) world.FactoryMapMode {
	if meta.FactoryMap == nil || meta.FactoryMap.Mode == "" {
		return world.FactoryMapMode{Mode: world.FactoryMapManual, SkipOnline: true}
	}
	return *meta.FactoryMap
}

// factoryMapSupported is whether the world's game can be read for a map.
func factoryMapSupported(gameVersion string) bool {
	if _, ok := world.ParseGameVersion(gameVersion); !ok {
		return false
	}
	return world.CompareGameVersions(gameVersion, FactoryMapMinVersion) >= 0
}

// FactoryMap returns the view. It never starts a run and makes no Docker or
// RCON call.
func (c *Controller) FactoryMap(id string) (FactoryMapView, error) {
	meta, err := c.readMeta(id)
	if err != nil {
		return FactoryMapView{}, err
	}
	v := FactoryMapView{Mode: factoryMapModeOf(meta), Running: c.anyGameRunning()}
	v.PlayersOnline = c.playersSeenOnline(id)
	store, err := c.factoryMapStore(id)
	if err != nil {
		return FactoryMapView{}, err
	}
	// A damaged record shows as no picture; the next drawing replaces it.
	d, kept, err := store.Read()
	if err != nil {
		c.log.Warn("the factory map record could not be read", "world", id, "error", err)
		kept = false
	}
	picture := kept && len(d.Surfaces) > 0
	if picture {
		v.Drawing = d
	}
	fm := &c.factoryMaps
	fm.mu.Lock()
	drawing := fm.drawing[id] != nil
	failed, hasFailed := fm.failed[id]
	fm.mu.Unlock()
	if !hasFailed && kept && d.Failure != "" {
		failed, hasFailed = factoryMapFailure{id: d.Failure}, true
	}
	switch {
	case !factoryMapSupported(meta.GameVersion):
		v.State = FactoryMapUnsupported
	case drawing:
		v.State = FactoryMapDrawing
	case v.Mode.Mode == world.FactoryMapOff && !picture:
		v.State = FactoryMapOff
	case hasFailed:
		v.State, v.Failure = FactoryMapFailed, failed.id
	case picture:
		v.State = FactoryMapDrawn
	default:
		v.State = FactoryMapNever
	}
	return v, nil
}

// DrawFactoryMap starts drawing the newest save's factory map and returns
// once it has started; UpdateFactoryMap says when it ends. A world already
// being drawn is not drawn twice. Beside a running game of this world it
// asks the game who is online, and refuses while anyone is when the world
// says so (SPEC 5.16); a failure to ask refuses too.
func (c *Controller) DrawFactoryMap(ctx context.Context, id string) error {
	meta, err := c.readMeta(id)
	if err != nil {
		return err
	}
	mode := factoryMapModeOf(meta)
	switch {
	case meta.Archived:
		return ErrFactoryMapArchived
	case !factoryMapSupported(meta.GameVersion):
		return ErrFactoryMapNotSupported
	case mode.Mode == world.FactoryMapOff:
		return ErrFactoryMapOff
	}
	if mode.SkipOnline && c.Status(id).State != StateStopped {
		players, err := c.Players(ctx, id)
		if err != nil && !errors.Is(err, ErrNotRunning) {
			return err
		}
		if len(players) > 0 {
			return ErrFactoryMapPlayersOnline
		}
	}
	_, found, err := c.worlds.LoadTarget(id)
	if err != nil {
		return err
	}
	if !found {
		return ErrFactoryMapNoSave
	}
	c.startFactoryMap(id)
	return nil
}

// FactoryMapStore opens the world's kept pictures, for the map page to serve.
// It is the web layer's way to the factorymap.Store.
func (c *Controller) FactoryMapStore(id string) (*factorymap.Store, error) {
	if !world.ValidID(id) {
		return nil, fs.ErrNotExist
	}
	return factorymap.Open(c.dataDir(), id)
}

// dataDir is Foreman's data directory, which holds the worlds folder.
func (c *Controller) dataDir() string {
	return filepath.Dir(c.worlds.Dirs().Worlds)
}

func (c *Controller) factoryMapStore(id string) (factoryMapStore, error) {
	if open := c.factoryMaps.openStore; open != nil {
		return open(id)
	}
	return c.FactoryMapStore(id)
}

// playersSeenOnline is whether this world's game runs with anyone whose join
// the log showed and whose leave it has not. It is a guess for the page; the
// refusal asks the game itself.
func (c *Controller) playersSeenOnline(id string) bool {
	w := c.world(id)
	w.mu.Lock()
	running := w.dockerStatus == "running"
	w.mu.Unlock()
	if !running {
		return false
	}
	w.joins.mu.Lock()
	defer w.joins.mu.Unlock()
	return len(w.joins.joined) > 0
}

// startFactoryMap starts the world's run unless one is in flight, and says
// so to watching pages.
func (c *Controller) startFactoryMap(id string) bool {
	fm := &c.factoryMaps
	fm.mu.Lock()
	if fm.drawing[id] != nil {
		fm.mu.Unlock()
		return false
	}
	if fm.drawing == nil {
		fm.drawing = map[string]context.CancelFunc{}
	}
	ctx, cancel := context.WithCancel(c.ctx)
	fm.drawing[id] = cancel
	fm.mu.Unlock()
	if !c.spawn(func() { c.runFactoryMap(ctx, id) }) {
		cancel()
		fm.mu.Lock()
		delete(fm.drawing, id)
		fm.mu.Unlock()
		return false
	}
	c.publishFactoryMap(id)
	return true
}

func (c *Controller) publishFactoryMap(id string) {
	c.world(id).hub.Publish(Update{Kind: UpdateFactoryMap})
}

// runFactoryMap is one run, in ctx, which a delete of the world or shutdown
// cancels. The drawing is kept only if the save that loads next is still the
// save that was read (SPEC 8.16), and only while the world exists (SPEC
// 5.16). A run cancelled by a start, a delete or shutdown records nothing.
func (c *Controller) runFactoryMap(ctx context.Context, id string) {
	fm := &c.factoryMaps
	defer func() {
		fm.mu.Lock()
		if cancel := fm.drawing[id]; cancel != nil {
			cancel()
		}
		delete(fm.drawing, id)
		fm.mu.Unlock()
		c.publishFactoryMap(id)
	}()
	before, _, err := c.worlds.LoadTarget(id)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.log.Warn("the factory map could not read the world", "world", id, "error", err)
		}
		return
	}
	tried := basisOf(before)
	run := fm.runSave
	if run == nil {
		run = c.runSaveCopy
	}
	draw := fm.drawFiles
	if draw == nil {
		draw = drawFactoryMapFiles
	}
	var drawn drawnFactoryMap
	basis, err := run(ctx, id, factoryMapMod, func(base *os.Root, dir string) error {
		out, err := base.OpenRoot(path.Join(dir, gameenv.FactoryMapDir))
		if err != nil {
			return fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
		}
		defer func() { _ = out.Close() }()
		drawn, err = draw(out)
		return err
	})
	if err == nil {
		err = c.keepFactoryMap(id, basis, drawn)
	}
	switch {
	case err == nil:
		fm.mu.Lock()
		delete(fm.failed, id)
		fm.mu.Unlock()
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		c.log.Info("the factory map run was cancelled", "world", id)
	case errors.Is(err, errFactoryMapWorldGone) || !c.worldExists(id):
		c.log.Info("the world was deleted during the factory map run", "world", id)
	case errors.Is(err, ErrPreviewBusy):
		c.log.Info("the factory map run waited too long for the preview slot", "world", id)
	case errors.Is(err, errFactoryMapSaveChanged):
		c.log.Info("the save changed during the factory map run, so the picture is not kept", "world", id)
	default:
		c.log.Warn("the factory map run failed", "world", id, "error", err)
		fm.mu.Lock()
		if fm.failed == nil {
			fm.failed = map[string]factoryMapFailure{}
		}
		failure := factoryMapFailureID(err)
		fm.failed[id] = factoryMapFailure{basis: tried, id: failure}
		fm.mu.Unlock()
		// The store keeps the failure beside the last picture, which stays.
		err := c.replaceIfWorld(id, factorymap.Drawing{Basis: tried, DrawnAt: c.now().UTC(), Failure: failure}, nil)
		if err != nil && !errors.Is(err, errFactoryMapWorldGone) {
			c.log.Warn("the factory map failure could not be kept", "world", id, "error", err)
		}
	}
}

var (
	errFactoryMapSaveChanged = errors.New("the save changed during the run")
	errFactoryMapWorldGone   = errors.New("the world was deleted")
)

// ForgetFactoryMap cancels the run of a deleted world and removes its kept
// pictures, so no drawing outlives its world (SPEC 5.16). A delete of a world
// calls it once the world's folder is gone.
func (c *Controller) ForgetFactoryMap(id string) error {
	fm := &c.factoryMaps
	fm.mu.Lock()
	if cancel := fm.drawing[id]; cancel != nil {
		cancel()
	}
	delete(fm.failed, id)
	delete(fm.stoppedSince, id)
	fm.mu.Unlock()
	return c.removeFactoryMap(id)
}

func (c *Controller) removeFactoryMap(id string) error {
	if remove := c.factoryMaps.removeStore; remove != nil {
		return remove(id)
	}
	return factorymap.Remove(c.dataDir(), id)
}

// worldExists is whether the world's folder is there. Only a folder known to
// be gone counts as no world, so a passing fault never removes a picture.
func (c *Controller) worldExists(id string) bool {
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return !errors.Is(err, fs.ErrNotExist)
	}
	_ = root.Close()
	return true
}

// replaceIfWorld keeps d while the world exists. It looks again after the
// write: a delete that removed the pictures between the look and the write
// would leave these behind, so they are removed then.
func (c *Controller) replaceIfWorld(id string, d factorymap.Drawing, pngs map[string][]byte) error {
	if !c.worldExists(id) {
		return errFactoryMapWorldGone
	}
	store, err := c.factoryMapStore(id)
	if err != nil {
		return err
	}
	if err := store.Replace(d, pngs); err != nil {
		return err
	}
	if !c.worldExists(id) {
		if err := c.removeFactoryMap(id); err != nil {
			c.log.Warn("the factory map of a deleted world could not be removed", "world", id, "error", err)
		}
		return errFactoryMapWorldGone
	}
	return nil
}

// keepFactoryMap replaces the kept drawing when the save that loads next is
// still the one that was read.
func (c *Controller) keepFactoryMap(id string, basis saveplayers.Basis, drawn drawnFactoryMap) error {
	now, found, err := c.worlds.LoadTarget(id)
	if errors.Is(err, fs.ErrNotExist) {
		return errFactoryMapWorldGone
	}
	if err != nil {
		return err
	}
	if !found || !sameBasis(basisOf(now), basis) {
		return errFactoryMapSaveChanged
	}
	return c.replaceIfWorld(id, factorymap.Drawing{
		Basis: basis, DrawnAt: c.now().UTC(), GameVersion: drawn.gameVersion, Surfaces: drawn.surfaces,
	}, drawn.pngs)
}

func basisOf(s world.Save) saveplayers.Basis {
	return saveplayers.Basis{Save: s.Name, Size: s.Size, Modified: s.ModTime}
}

func sameBasis(a, b saveplayers.Basis) bool {
	return a.Save == b.Save && a.Size == b.Size && a.Modified.Equal(b.Modified)
}

// factoryMapFailureID names a failed run for the page (SPEC 8.16).
func factoryMapFailureID(err error) string {
	switch {
	case errors.Is(err, errFactoryMapTooLarge):
		return FailureTooLarge
	case errors.Is(err, ErrSaveCopyImageMissing):
		return saveplayers.ResultImageMissing
	case errors.Is(err, ErrSaveCopyOutOfMemory):
		return saveplayers.ResultOutOfMemory
	case errors.Is(err, ErrSaveCopyTimeout):
		return saveplayers.ResultTimeout
	case errors.Is(err, ErrSaveCopyModsDiffer):
		return saveplayers.ResultModsDiffer
	case errors.Is(err, ErrSaveCopySaveNewer):
		return saveplayers.ResultSaveNewer
	default:
		return saveplayers.ResultBadOutput
	}
}

// The drawing steps; tests replace them to watch how they are called.
var (
	parseManifest = factorymap.ParseManifest
	drawChart     = factorymap.DrawChart
	drawOverlay   = factorymap.DrawOverlay
)

// drawFactoryMapFiles draws every surface the helper mod wrote into dir. The
// files are untrusted game output: each must be a regular file named in the
// folder itself, and together they are read under MaxOutputBytes (SPEC 14.6).
// Surfaces are drawn one at a time, and only each one's encoded PNGs are
// kept, since one decoded 4096-pixel picture takes hundreds of megabytes.
func drawFactoryMapFiles(dir *os.Root) (drawnFactoryMap, error) {
	return drawFactoryMapFilesUnder(dir, factorymap.MaxOutputBytes)
}

func drawFactoryMapFilesUnder(dir *os.Root, limit int64) (drawnFactoryMap, error) {
	budget := &outputBudget{left: limit}
	mb, err := readManifest(dir, budget)
	if err != nil {
		return drawnFactoryMap{}, err
	}
	manifest, err := parseManifest(bytes.NewReader(mb))
	if err != nil {
		return drawnFactoryMap{}, fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
	}
	if err := checkOutputSizes(dir, manifest, budget.left); err != nil {
		return drawnFactoryMap{}, err
	}
	out := drawnFactoryMap{gameVersion: manifest.GameVersion, pngs: map[string][]byte{}}
	for _, ms := range manifest.Surfaces {
		s, err := drawFactoryMapSurface(dir, mb, ms, budget, out.pngs)
		if err != nil {
			return drawnFactoryMap{}, err
		}
		out.surfaces = append(out.surfaces, s)
	}
	return out, nil
}

// drawFactoryMapSurface draws one surface's base picture and its layers into
// pngs.
func drawFactoryMapSurface(dir *os.Root, manifest []byte, ms factorymap.ManifestSurface,
	budget *outputBudget, pngs map[string][]byte) (factorymap.Surface, error) {
	chart, err := openOutputFile(dir, ms.Chart, budget)
	if err != nil {
		return factorymap.Surface{}, err
	}
	img, s, err := drawChart(bytes.NewReader(manifest), chart, ms.Index)
	_ = chart.Close()
	if err = budget.wrap(err); err != nil {
		return factorymap.Surface{}, err
	}
	if pngs[factorymap.PNGName(s.Index, "")], err = encodePNG(img); err != nil {
		return factorymap.Surface{}, err
	}
	if ms.Entities == "" {
		return s, nil
	}
	entities, err := openOutputFile(dir, ms.Entities, budget)
	if err != nil {
		return factorymap.Surface{}, err
	}
	layers, err := drawOverlay(entities, s)
	_ = entities.Close()
	if err = budget.wrap(err); err != nil {
		return factorymap.Surface{}, err
	}
	for _, cat := range factorymap.Categories {
		layer, ok := layers[cat]
		if !ok {
			continue
		}
		if pngs[factorymap.PNGName(s.Index, cat)], err = encodePNG(layer); err != nil {
			return factorymap.Surface{}, err
		}
	}
	return s, nil
}

// checkOutputSizes refuses output whose files together pass left before any
// is drawn; the budget still counts each byte as it is read, in case a file
// grows.
func checkOutputSizes(dir *os.Root, m factorymap.Manifest, left int64) error {
	var total int64
	for _, ms := range m.Surfaces {
		for _, name := range []string{ms.Chart, ms.Entities} {
			if name == "" {
				continue
			}
			f, err := openOutputFile(dir, name, &outputBudget{left: left})
			if err != nil {
				return err
			}
			info, err := f.f.Stat()
			_ = f.Close()
			if err != nil {
				return fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
			}
			if total += info.Size(); total > left {
				return errFactoryMapTooLarge
			}
		}
	}
	return nil
}

func encodePNG(img image.Image) ([]byte, error) {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// outputBudget is how many bytes of output may still be read.
type outputBudget struct{ left int64 }

// wrap turns a drawing error into a bad-output one, unless the budget ran
// out, which is too-large.
func (b *outputBudget) wrap(err error) error {
	switch {
	case err == nil:
		return nil
	case b.left < 0 || errors.Is(err, errFactoryMapTooLarge):
		return errFactoryMapTooLarge
	default:
		return fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
	}
}

// budgetReader reads r, taking each byte from b; past the budget it fails.
type budgetReader struct {
	r io.Reader
	b *outputBudget
}

func (r *budgetReader) Read(p []byte) (int, error) {
	if r.b.left <= 0 {
		// One byte more tells a file that ends exactly at the budget from
		// one that goes on.
		var one [1]byte
		if n, _ := r.r.Read(one[:]); n > 0 {
			r.b.left = -1
			return 0, errFactoryMapTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > r.b.left {
		p = p[:r.b.left]
	}
	n, err := r.r.Read(p)
	r.b.left -= int64(n)
	return n, err
}

// outputFile is an output file read under the budget.
type outputFile struct {
	budgetReader
	f *os.File
}

func (f *outputFile) Close() error { return f.f.Close() }

// openOutputFile opens a plain regular file named in dir, never a link, a
// folder, a pipe or a path.
func openOutputFile(dir *os.Root, name string, budget *outputBudget) (*outputFile, error) {
	if name == "" || name == "." || name == ".." || path.Base(name) != name || filepath.Base(name) != name {
		return nil, fmt.Errorf("%w: file name %q", ErrSaveCopyBadOutput, name)
	}
	info, err := dir.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrSaveCopyBadOutput, name)
	}
	if info.Size() > budget.left {
		return nil, errFactoryMapTooLarge
	}
	f, err := dir.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
	}
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %s is not a regular file", ErrSaveCopyBadOutput, name)
	}
	return &outputFile{budgetReader{f, budget}, f}, nil
}

// maxManifestBytes is the most manifest.json may hold (SPEC 14.6), the
// limit factorymap.ParseManifest applies.
const maxManifestBytes = 1 << 20

// readManifest reads manifest.json under its own limit, so an oversized one
// is refused without being read into memory, and then charges it to budget.
// Any mod in the world can write the file, so past the limit it is bad
// output.
func readManifest(dir *os.Root, budget *outputBudget) ([]byte, error) {
	f, err := openOutputFile(dir, "manifest.json", &outputBudget{left: maxManifestBytes})
	if errors.Is(err, errFactoryMapTooLarge) {
		return nil, fmt.Errorf("%w: manifest.json is over %d bytes", ErrSaveCopyBadOutput, maxManifestBytes)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f.f, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSaveCopyBadOutput, err)
	}
	if len(b) > maxManifestBytes {
		return nil, fmt.Errorf("%w: manifest.json is over %d bytes", ErrSaveCopyBadOutput, maxManifestBytes)
	}
	if budget.left -= int64(len(b)); budget.left < 0 {
		return nil, errFactoryMapTooLarge
	}
	return b, nil
}

// readOutputFile reads a whole output file under the budget.
func readOutputFile(dir *os.Root, name string, budget *outputBudget) ([]byte, error) {
	f, err := openOutputFile(dir, name, budget)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, budget.wrap(err)
	}
	return b, nil
}
