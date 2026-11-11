package game

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	modsettingsmod "github.com/toddawhittaker/foreman/internal/game/modsettings"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The definitions run's own limits (SPEC 14.3). A heavy mod pack needs far
// more than a map preview does: the game loads every mod's data stage.
const (
	// ModSettingsLimit is how long a definitions run may take before its
	// container is killed.
	ModSettingsLimit = 5 * time.Minute
	// ModSettingsMemory is the container's memory limit in bytes, with no
	// swap.
	ModSettingsMemory = 4 << 30
	// maxDefaultsBytes is the most bytes read of the mod-settings.dat the
	// run writes (SPEC 14.6).
	maxDefaultsBytes = 1 << 20
	// modSettingsSeed is the seed the run's map is created with; the map is
	// thrown away.
	modSettingsSeed = 1
)

// Errors the mod settings page shows when the definitions run fails. Each
// leaves the world untouched; download and upload still work (SPEC 9.6).
var (
	// ErrModSettingsImageMissing means the world's game image is not
	// downloaded.
	ErrModSettingsImageMissing = errors.New("the game image for this Factorio version is not downloaded. Download the game image first")
	// ErrModSettingsOutOfMemory means the run hit its memory limit.
	ErrModSettingsOutOfMemory = errors.New("reading the mod settings ran out of memory")
	// ErrModSettingsTimeout means the run hit its time limit.
	ErrModSettingsTimeout = errors.New("reading the mod settings took too long and was stopped")
	// ErrModSettingsBadOutput means the run's files were missing or invalid.
	ErrModSettingsBadOutput = errors.New("the game did not report the mod settings in a form Foreman can read")
)

// ModSettingDefs is what one definitions run yields for a world: the
// setting definitions, the mod-settings.dat the game wrote with every
// default, and the settings' text in Foreman's eight languages. The value is
// shared between callers and must not be changed.
type ModSettingDefs struct {
	Defs     modsettings.Definitions
	Defaults []byte
	Locale   modsettings.Locale
}

// modSettingsMod is the definitions run's control mod. It needs no params
// file; the container's script runs a no-op where the others write theirs.
var modSettingsMod = controlMod{
	kind:      gameenv.KindModSettings,
	what:      "mod settings",
	name:      modsettingsmod.ModName,
	dir:       modsettingsmod.ModDir,
	control:   modsettingsmod.Control,
	params:    ":",
	save:      "modsettings.zip",
	output:    gameenv.ModSettingsDefsFile,
	maxOutput: modsettings.MaxDefinitionsBytes,
	memory:    ModSettingsMemory,
}

// modSettingsInfo is the control mod's info.json. It needs the base game
// only, whose line it is for.
func modSettingsInfo(line string) []byte {
	return modInfo(modsettingsmod.ModName, modsettingsmod.ModVersion, "Foreman mod settings",
		"Lists the mod settings the world's mods define for Foreman's mod settings page. Adds no prototypes.",
		line, "base")
}

// modSettingsCache holds the definitions of each world, in memory, and the
// runs in flight (SPEC 9.6). The zero value is ready to use.
type modSettingsCache struct {
	// lockMu serializes this package's own takes of a world's lock, which is
	// not waited for: two callers reading the same world must not make each
	// other world.ErrBusy.
	lockMu  sync.Mutex
	mu      sync.Mutex
	entries map[string]modSettingsEntry
	flights map[modSettingsFlightKey]*modSettingsFlight
}

type modSettingsEntry struct {
	key  string
	defs *ModSettingDefs
}

// modSettingsFlightKey names a run: a world's mods and game at one key.
type modSettingsFlightKey struct{ id, key string }

// modSettingsFlight is one run that every caller for the same world and key
// shares. It ends early when no caller is left waiting.
type modSettingsFlight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	defs    *ModSettingDefs
	err     error
}

// ModSettingDefs returns the world's mod setting definitions, running the
// world's own image once and caching the result until the world's game
// version or mods change. Callers who ask while a run for the same world and
// key is going share it. The result is shared and must not be changed.
// Errors are the ErrModSettings values, ErrPreviewBusy when the one preview
// slot stayed taken, or ctx's own error.
func (c *Controller) ModSettingDefs(ctx context.Context, id string) (*ModSettingDefs, error) {
	key, err := c.modSettingsKey(id)
	if err != nil {
		return nil, err
	}
	fk := modSettingsFlightKey{id, key}
	cache := &c.modSettings

	cache.mu.Lock()
	if e, ok := cache.entries[id]; ok && e.key == key {
		cache.mu.Unlock()
		return e.defs, nil
	}
	f := cache.flights[fk]
	if f == nil {
		runCtx, cancel := context.WithCancel(c.ctx)
		f = &modSettingsFlight{done: make(chan struct{}), cancel: cancel}
		if cache.flights == nil {
			cache.flights = map[modSettingsFlightKey]*modSettingsFlight{}
		}
		cache.flights[fk] = f
		go c.runModSettingsFlight(runCtx, id, fk, f)
	}
	f.waiters++
	cache.mu.Unlock()

	select {
	case <-f.done:
		return f.defs, f.err
	case <-ctx.Done():
		cache.mu.Lock()
		f.waiters--
		last := f.waiters == 0
		if last && cache.flights[fk] == f {
			// Nobody wants this run any more. A caller who comes now starts
			// another.
			delete(cache.flights, fk)
		}
		cache.mu.Unlock()
		if last {
			f.cancel()
		}
		return nil, ctx.Err()
	}
}

// modSettingsKey reads the world's cache key under its lock, which
// ModSettingsKey needs its caller to hold. A world whose lock is taken (a
// start, stop or switch is going on) is world.ErrBusy.
func (c *Controller) modSettingsKey(id string) (string, error) {
	unlock, err := c.lockWorld(id)
	if err != nil {
		return "", err
	}
	defer unlock()
	return c.worlds.ModSettingsKey(id)
}

// lockWorld takes the world's lock for a read of its mods. Only one such
// read holds it at a time, so it is busy only for server control. The
// returned function releases it once.
func (c *Controller) lockWorld(id string) (func(), error) {
	c.modSettings.lockMu.Lock()
	release, err := c.worlds.Lock(id)
	if err != nil {
		c.modSettings.lockMu.Unlock()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			release()
			c.modSettings.lockMu.Unlock()
		})
	}, nil
}

// runModSettingsFlight is the one run for a flight. It caches the result
// when the world's key is the same after the run as before, and under the
// key the run read.
func (c *Controller) runModSettingsFlight(ctx context.Context, id string, fk modSettingsFlightKey, f *modSettingsFlight) {
	defs, before, err := c.runModSettings(ctx, id)
	if err == nil {
		after, kerr := c.modSettingsKey(id)
		cache := &c.modSettings
		cache.mu.Lock()
		switch {
		case kerr != nil || after != before:
			c.log.Info("the mods changed during the mod settings run, so the result is not kept", "world", id)
		default:
			if cache.entries == nil {
				cache.entries = map[string]modSettingsEntry{}
			}
			cache.entries[id] = modSettingsEntry{before, defs}
		}
		cache.mu.Unlock()
	}
	cache := &c.modSettings
	cache.mu.Lock()
	f.defs, f.err = defs, err
	if cache.flights[fk] == f {
		delete(cache.flights, fk)
	}
	cache.mu.Unlock()
	f.cancel()
	close(f.done)
}

// errModsChanged means the world's game version changed between the run
// choosing an image and the world's mods being read, so the image would not
// be the world's own.
var errModsChanged = errors.New("the world's game version changed while the mod settings run was starting")

// runModSettings runs the world's own image once and reads what it wrote. It
// also returns the cache key the world had when its mods were read.
func (c *Controller) runModSettings(ctx context.Context, id string) (*ModSettingDefs, string, error) {
	meta, err := c.readMeta(id)
	if err != nil {
		return nil, "", err
	}
	line, ok := mapgen.Line(meta.GameVersion)
	if !ok {
		return nil, "", fmt.Errorf("%w: there is no game for Factorio %q", ErrModSettingsBadOutput, meta.GameVersion)
	}
	req := PreviewRequest{GameVersion: meta.GameVersion, Seed: modSettingsSeed}
	var (
		key        string
		localeCfgs map[string]map[string][][]byte
	)
	plan := previewPlan{
		gameVersion: meta.GameVersion, kind: gameenv.KindModSettings, limit: c.modSettingsLimit,
		fill: func(base *os.Root) (string, error) {
			var name string
			var err error
			key, name, localeCfgs, err = c.makeModSettingsDir(base, id, meta.GameVersion)
			return name, err
		},
	}
	var defsJSON, defaults []byte
	err = c.previewRunIn(ctx, plan, func(base *os.Root, ref, name string, container func(docker.ContainerSpec) error) error {
		if err := addControlMod(base, name, req, modSettingsMod, modSettingsInfo(line)); err != nil {
			return err
		}
		if err := container(c.controlModSpec(ref, name, req, modSettingsMod, nil)); err != nil {
			return err
		}
		var err error
		defsJSON, defaults, err = readModSettingsOutput(base, name)
		return err
	})
	if err != nil {
		return nil, "", modSettingsError(err)
	}
	defs, err := modsettings.ParseDefinitions(defsJSON)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrModSettingsBadOutput, err)
	}
	if _, err := modsettings.Decode(defaults); err != nil {
		return nil, "", fmt.Errorf("%w: the defaults file: %w", ErrModSettingsBadOutput, err)
	}
	// The control mod has no settings, but a file that lists one is not
	// shown.
	defs = slices.DeleteFunc(defs, func(d modsettings.Definition) bool { return d.Mod == modsettingsmod.ModName })
	return &ModSettingDefs{Defs: defs, Defaults: defaults, Locale: c.modSettingsLocale(id, localeCfgs)}, key, nil
}

// makeModSettingsDir makes the run's work folder: the game's config and the
// world's mods, linked in (SPEC 9.6). The control mod is added after. It
// holds the world's lock, which the world's mod functions need, only while it
// reads the world: the cache key, the mods and their locale files. All three
// are read in the one hold, so they describe the same mods. The game runs
// without the lock.
func (c *Controller) makeModSettingsDir(base *os.Root, id, gameVersion string) (key, name string, locale map[string]map[string][][]byte, err error) {
	name, err = newWorkDir(base)
	if err != nil {
		return "", "", nil, err
	}
	fail := func(err error) (string, string, map[string]map[string][][]byte, error) {
		_ = base.RemoveAll(name)
		return "", "", nil, err
	}
	dir, err := base.OpenRoot(name)
	if err != nil {
		return fail(err)
	}
	werr := dir.WriteFile("config.ini", []byte(previewConfig), world.FileMode)
	_ = dir.Close()
	if werr != nil {
		return fail(werr)
	}
	unlock, err := c.lockWorld(id)
	if err != nil {
		return fail(err)
	}
	defer unlock()
	meta, err := c.readMeta(id)
	if err != nil {
		return fail(err)
	}
	if meta.GameVersion != gameVersion {
		return fail(errModsChanged)
	}
	if key, err = c.worlds.ModSettingsKey(id); err != nil {
		return fail(err)
	}
	if err := c.worlds.LinkModsInto(id, filepath.Join(c.previewPath(), name)); err != nil {
		return fail(fmt.Errorf("could not put the world's mods in the work folder: %w", err))
	}
	locale, err = c.worlds.ModLocaleFiles(id, factorioLocaleCodes())
	if err != nil {
		// Locale text is decoration: the page shows setting names instead.
		c.log.Warn("could not read the mods' locale files", "world", id, "error", err)
		locale = nil
	}
	return key, name, locale, nil
}

// readModSettingsOutput reads the definitions JSON and the defaults file the
// run left in the work folder. The game's script is untrusted (SPEC 14.1,
// 14.6): each must be a regular file within its limit, so a link or a named
// pipe cannot send the read elsewhere or hold it.
func readModSettingsOutput(base *os.Root, dir string) (defs, defaults []byte, err error) {
	root, err := base.OpenRoot(dir)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = root.Close() }()
	defs, err = world.ReadSmallFile(root, gameenv.ModSettingsDefsFile, modsettings.MaxDefinitionsBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: no usable definitions file: %w", ErrModSettingsBadOutput, err)
	}
	mods := world.ModsDir
	if info, err := root.Lstat(mods); err != nil || !info.IsDir() {
		return nil, nil, fmt.Errorf("%w: the game left no mods folder", ErrModSettingsBadOutput)
	}
	defaults, err = world.ReadSmallFile(root, mods+"/"+gameenv.ModSettingsDefaultsFile, maxDefaultsBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: no usable defaults file: %w", ErrModSettingsBadOutput, err)
	}
	return defs, defaults, nil
}

// modSettingsError words a failed run as the page knows it. Errors the page
// handles on its own (a busy slot, a cancelled caller) and errors already
// wrapped pass through.
func modSettingsError(err error) error {
	var pe *PreviewError
	switch {
	case errors.Is(err, ErrPreviewImageMissing):
		return ErrModSettingsImageMissing
	case errors.Is(err, ErrPreviewTimeout):
		return ErrModSettingsTimeout
	case errors.As(err, &pe) && pe.OOM:
		return fmt.Errorf("%w: %w", ErrModSettingsOutOfMemory, err)
	case errors.As(err, &pe):
		return fmt.Errorf("%w: %w", ErrModSettingsBadOutput, err)
	}
	return err
}

// factorioLocaleCodes is Factorio's locale code of each of Foreman's
// languages, sorted.
func factorioLocaleCodes() []string {
	codes := make([]string, 0, len(modsettings.FactorioLocales))
	for _, code := range modsettings.FactorioLocales {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// modSettingsLocale builds the settings' names and descriptions in Foreman's
// languages from the enabled mods' raw locale files. Locale text is
// decoration: a file that does not parse is logged and skipped, so a world
// shows the setting names instead. Files merge in the order of mod name, then
// as given.
func (c *Controller) modSettingsLocale(id string, files map[string]map[string][][]byte) modsettings.Locale {
	mods := make([]string, 0, len(files))
	for mod := range files {
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	out := modsettings.Locale{}
	for lang, code := range modsettings.FactorioLocales {
		texts := map[string]modsettings.LocaleText{}
		for _, mod := range mods {
			for _, data := range files[mod][code] {
				names, descs, err := modsettings.ParseLocaleCfg(data)
				if err != nil {
					c.log.Warn("a mod's locale file does not parse", "world", id, "mod", mod, "locale", code, "error", err)
					continue
				}
				for n, v := range names {
					t := texts[n]
					t.Name = v
					texts[n] = t
				}
				for n, v := range descs {
					t := texts[n]
					t.Description = v
					texts[n] = t
				}
			}
		}
		if len(texts) > 0 {
			out[lang] = texts
		}
	}
	return out
}
