package game

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
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

// Why a world's planet list is the built-in one (ModSettingDefs.PlanetsErr).
// Neither fails the run: the settings are still shown (SPEC 5.6, 14.6).
var (
	// ErrPlanetsMissing means the run left no usable planet file.
	ErrPlanetsMissing = errors.New("the game did not report the planets of this world's mods")
	// ErrPlanetsRefused means the planet file was past a limit or not of
	// the expected shape.
	ErrPlanetsRefused = errors.New("the planets this world's mods report could not be used")
)

// ModSettingDefs is what one definitions run yields for a world: the
// setting definitions, the mod-settings.dat the game wrote with every
// default, the settings' text in Foreman's eight languages, and the world's
// planets. The value is shared between callers and must not be changed.
type ModSettingDefs struct {
	Defs     modsettings.Definitions
	Defaults []byte
	Locale   modsettings.Locale
	// Catalogues holds the world's planets, controls and resources with
	// labels in each of Foreman's languages. When the run gave no usable
	// planet file it holds the built-in catalogue under "en" only, and
	// PlanetsErr says why (SPEC 5.6).
	Catalogues map[string]mapgen.Catalogue
	PlanetsErr error
}

// Catalogue returns the world's catalogue labelled in lang, or in English
// when there is none for lang.
func (d *ModSettingDefs) Catalogue(lang string) mapgen.Catalogue {
	if c, ok := d.Catalogues[lang]; ok {
		return c
	}
	return d.Catalogues["en"]
}

// planetsCopy copies the planet file out of the container. A missing file is
// not a failed run: the Go side falls back to the built-in catalogue.
const planetsCopy = `cp ` + previewWrite + `/script-output/` + gameenv.PlanetsFile + ` ` + gameenv.PreviewMount + `/` + gameenv.PlanetsFile + ` 2>/dev/null || :`

// withPlanetsCopy puts planetsCopy before the script's last line, the
// definitions copy, so the container still exits non-zero when that fails.
func withPlanetsCopy(script string) string {
	i := strings.LastIndex(script, "\n")
	return script[:i+1] + planetsCopy + "\n" + script[i+1:]
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

// maxModSettingsEntries is how many worlds' definitions are kept. A result
// is small but a deployment has few worlds that anyone edits; the least
// recently used entry goes first.
const maxModSettingsEntries = 8

// modSettingsCache holds the definitions of each world, in memory, and the
// runs in flight (SPEC 9.6). The zero value is ready to use.
type modSettingsCache struct {
	mu      sync.Mutex
	entries map[string]*modSettingsEntry
	// clock counts uses, so the entry with the lowest lastUsed is the least
	// recently used.
	clock   uint64
	flights map[modSettingsFlightKey]*modSettingsFlight
}

type modSettingsEntry struct {
	key      string
	defs     *ModSettingDefs
	lastUsed uint64
}

// get returns the world's entry when it is for key, and marks it used.
// mu is held.
func (m *modSettingsCache) get(id, key string) (*ModSettingDefs, bool) {
	e, ok := m.entries[id]
	if !ok || e.key != key {
		return nil, false
	}
	m.clock++
	e.lastUsed = m.clock
	return e.defs, true
}

// put keeps defs as the world's entry, and drops the least recently used
// entries of other worlds beyond maxModSettingsEntries. mu is held.
func (m *modSettingsCache) put(id, key string, defs *ModSettingDefs) {
	if m.entries == nil {
		m.entries = map[string]*modSettingsEntry{}
	}
	m.clock++
	m.entries[id] = &modSettingsEntry{key, defs, m.clock}
	for len(m.entries) > maxModSettingsEntries {
		oldest := ""
		for other, e := range m.entries {
			if oldest == "" || e.lastUsed < m.entries[oldest].lastUsed {
				oldest = other
			}
		}
		delete(m.entries, oldest)
	}
}

// modSettingsFlightKey names a run: a world's mods and game at one key.
type modSettingsFlightKey struct{ id, key string }

// modSettingsFlight is one run that every caller for the same world and key
// shares. It runs to its end and is cached even if every caller leaves, so
// a page that is reloaded during a long run finds the result.
type modSettingsFlight struct {
	done chan struct{}
	defs *ModSettingDefs
	err  error
}

// ModSettingDefs returns the world's mod setting definitions, running the
// world's own image once and caching the result until the world's game
// version or mods change. Callers who ask while a run for the same world and
// key is going share it. The result is shared and must not be changed.
//
// The cache is read without the world's lock, so a hit works while the world
// starts or stops; a key read in the middle of a change is only a miss. A
// miss runs the game, which takes the world's lock for a moment to read the
// world's mods.
//
// Errors a caller can get: ErrModSettingsImageMissing (also for a game
// version Foreman has no image for), ErrModSettingsOutOfMemory,
// ErrModSettingsTimeout, ErrModSettingsBadOutput (also for a world mod named
// like Foreman's reader); world.ErrBusy when server control holds the world
// or the world's game version changed while the run started;
// world.ErrModsTooBig when the world's mods are too big to link; and
// ErrPreviewBusy when the one preview slot stayed taken. A caller whose ctx
// ends gets ctx's error; the run goes on and its result is cached. Any other
// error is a failure to read the world or to make the work folder.
func (c *Controller) ModSettingDefs(ctx context.Context, id string) (*ModSettingDefs, error) {
	// A key that cannot be read (the world is missing, or being changed) is
	// a miss; the run reads the world again and reports the cause.
	key, _ := c.worlds.ModSettingsKey(id)
	fk := modSettingsFlightKey{id, key}
	cache := &c.modSettings

	cache.mu.Lock()
	if key != "" {
		if defs, ok := cache.get(id, key); ok {
			cache.mu.Unlock()
			return defs, nil
		}
	}
	f := cache.flights[fk]
	if f == nil {
		f = &modSettingsFlight{done: make(chan struct{})}
		if cache.flights == nil {
			cache.flights = map[modSettingsFlightKey]*modSettingsFlight{}
		}
		cache.flights[fk] = f
		if !c.spawn(func() { c.runModSettingsFlight(id, fk, f) }) {
			delete(cache.flights, fk)
			cache.mu.Unlock()
			return nil, context.Canceled
		}
	}
	cache.mu.Unlock()

	select {
	case <-f.done:
		return f.defs, f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// runModSettingsFlight is the one run for a flight, in the controller's own
// context. It caches the result when the world's key after the run is the one
// the run read with the mods. Both reads are without the world's lock: a
// key read in the middle of a change differs, and the result is not kept.
func (c *Controller) runModSettingsFlight(id string, fk modSettingsFlightKey, f *modSettingsFlight) {
	defs, before, err := c.runModSettings(c.ctx, id)
	cache := &c.modSettings
	if err == nil {
		after, kerr := c.worlds.ModSettingsKey(id)
		cache.mu.Lock()
		if kerr == nil && after == before {
			cache.put(id, before, defs)
		} else {
			c.log.Info("the mods changed during the mod settings run, so the result is not kept", "world", id)
		}
		cache.mu.Unlock()
	}
	cache.mu.Lock()
	f.defs, f.err = defs, err
	if cache.flights[fk] == f {
		delete(cache.flights, fk)
	}
	cache.mu.Unlock()
	close(f.done)
}

// runModSettings runs the world's own image once and reads what it wrote. It
// also returns the cache key the world had when its mods were read.
func (c *Controller) runModSettings(ctx context.Context, id string) (*ModSettingDefs, string, error) {
	meta, err := c.readMeta(id)
	if err != nil {
		return nil, "", err
	}
	line, ok := mapgen.Line(meta.GameVersion)
	if !ok {
		return nil, "", fmt.Errorf("%w: there is no game for Factorio %q", ErrModSettingsImageMissing, meta.GameVersion)
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
	var defsJSON, defaults, planetsJSON []byte
	var planetsErr error
	err = c.previewRunIn(ctx, plan, func(base *os.Root, ref, name string, container func(docker.ContainerSpec) error) error {
		if err := addControlMod(base, name, modSettingsMod, modSettingsInfo(line)); err != nil {
			return err
		}
		mounts, err := c.readOnlyZipMounts(base, name)
		if err != nil {
			return err
		}
		spec := c.controlModSpec(ref, name, req, modSettingsMod, nil)
		spec.HostConfig.Mounts = mounts
		spec.Cmd[len(spec.Cmd)-1] = withPlanetsCopy(spec.Cmd[len(spec.Cmd)-1])
		if err := container(spec); err != nil {
			return err
		}
		defsJSON, defaults, err = readModSettingsOutput(base, name)
		if err != nil {
			return err
		}
		planetsJSON, planetsErr = readPlanetsOutput(base, name)
		return nil
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
	sections := c.localeSections(id, localeCfgs)
	out := &ModSettingDefs{Defs: defs, Defaults: defaults, Locale: modSettingsLocale(sections, defs)}
	out.Catalogues, out.PlanetsErr = worldCatalogues(planetsJSON, planetsErr, sections, line, meta.SpaceAge)
	if out.PlanetsErr != nil {
		c.log.Warn("using the built-in planets for the world", "world", id, "error", out.PlanetsErr)
	}
	return out, key, nil
}

// WorldCatalogue returns the world's planets, controls and resources
// labelled in lang (SPEC 5.6). A world whose enabled mods are all built in
// gets the built-in catalogue without a game run. It fails safe: when the
// world's mods cannot be read, or the run fails or its planet file is
// refused, it returns the built-in catalogue of the world's line, and the
// error is only the reason to show. Space Age is whether the world's mods
// enable it, which an imported world's flag may not say.
func (c *Controller) WorldCatalogue(ctx context.Context, id, lang string) (mapgen.Catalogue, error) {
	meta, err := c.readMeta(id)
	if err != nil {
		return mapgen.Builtin(mapgen.Lines[len(mapgen.Lines)-1], false), err
	}
	line, _ := mapgen.Line(meta.GameVersion)
	enabled, err := c.enabledModNames(id)
	if err != nil {
		return mapgen.Builtin(line, meta.SpaceAge), err
	}
	builtin := mapgen.Builtin(line, enabled[world.ModSpaceAge])
	shipped := world.BuiltinMods(meta.GameVersion)
	onlyShipped := true
	for name := range enabled {
		if !slices.Contains(shipped, name) {
			onlyShipped = false
		}
	}
	if onlyShipped {
		return builtin, nil
	}
	defs, err := c.ModSettingDefs(ctx, id)
	if err != nil {
		return builtin, err
	}
	if defs.PlanetsErr != nil {
		return builtin, defs.PlanetsErr
	}
	return defs.Catalogue(lang), nil
}

// enabledModNames is the names of the mods the game loads for the world: each
// mod file the game turns on and each mod mod-list.json enables, even one
// with no file, such as a built-in mod.
func (c *Controller) enabledModNames(id string) (map[string]bool, error) {
	mods, err := c.enabledMods(id)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, m := range mods {
		out[m.Name] = true
	}
	root, err := c.worlds.OpenWorld(id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	states, err := world.ModListStates(root)
	if err != nil {
		return nil, err
	}
	for name, on := range states {
		if on {
			out[name] = true
		}
	}
	return out, nil
}

// worldCatalogues decodes the run's planet file once and labels it in each of
// Foreman's languages, each with English behind it. A file that is missing
// or refused gives the built-in catalogue and the reason (SPEC 5.6, 14.6).
func worldCatalogues(data []byte, readErr error, sections map[string]modsettings.LocaleSections, line string, spaceAge bool) (map[string]mapgen.Catalogue, error) {
	builtin := map[string]mapgen.Catalogue{"en": mapgen.Builtin(line, spaceAge)}
	if readErr != nil {
		return builtin, readErr
	}
	f, err := mapgen.DecodeWorldPlanets(line, data)
	if err != nil {
		return builtin, fmt.Errorf("%w: %w", ErrPlanetsRefused, err)
	}
	out := map[string]mapgen.Catalogue{}
	for lang := range modsettings.FactorioLocales {
		layers := []modsettings.LocaleSections{sections[lang]}
		if lang != "en" && sections["en"] != nil {
			layers = append(layers, sections["en"])
		}
		out[lang] = f.Labelled(layers...)
	}
	return out, nil
}

// readPlanetsOutput reads the planet file the run left in the work folder,
// under the same rules as the definitions: a regular file within its limit
// (SPEC 14.6). A file over the limit is ErrPlanetsRefused; any other failure
// is ErrPlanetsMissing.
func readPlanetsOutput(base *os.Root, dir string) ([]byte, error) {
	root, err := base.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPlanetsMissing, err)
	}
	defer func() { _ = root.Close() }()
	if info, err := root.Lstat(gameenv.PlanetsFile); err == nil && info.Mode().IsRegular() && info.Size() > mapgen.MaxPlanetFileBytes {
		return nil, fmt.Errorf("%w: the file is over %d bytes", ErrPlanetsRefused, mapgen.MaxPlanetFileBytes)
	}
	b, err := world.ReadSmallFile(root, gameenv.PlanetsFile, mapgen.MaxPlanetFileBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPlanetsMissing, err)
	}
	return b, nil
}

// makeModSettingsDir makes the run's work folder: the game's config and the
// world's mods, linked in (SPEC 9.6). The control mod is added after. It
// holds the world's lock, which the world's mod functions need, only while it
// reads the world: the cache key, the mods and their locale files. All three
// are read in the one hold, so they describe the same mods. The preview slot
// is held, so one fill runs at a time. The game runs without the lock. A
// world that server control holds is world.ErrBusy.
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
	unlock, err := c.worlds.Lock(id)
	if err != nil {
		return fail(err)
	}
	defer unlock()
	meta, err := c.readMeta(id)
	if err != nil {
		return fail(err)
	}
	if meta.GameVersion != gameVersion {
		// The image was chosen for another version.
		return fail(world.ErrBusy)
	}
	if key, err = c.worlds.ModSettingsKey(id); err != nil {
		return fail(err)
	}
	if err := c.worlds.LinkModsInto(id, filepath.Join(c.previewPath(), name)); err != nil {
		return fail(fmt.Errorf("could not put the world's mods in the work folder: %w", err))
	}
	locale, err = c.worlds.ModLocaleFiles(id)
	if err != nil {
		// Locale text is decoration: the page shows setting names instead.
		c.log.Warn("could not read the mods' locale files", "world", id, "error", err)
		locale = nil
	}
	return key, name, locale, nil
}

// readOnlyZipMounts mounts each mod zip in the work folder dir read-only
// over itself. The zips are hard links to the world's own files, and the
// game must still write mod-settings.dat into the same folder, so the folder
// stays writable and only the zips are locked (SPEC 9.6, 14).
func (c *Controller) readOnlyZipMounts(base *os.Root, dir string) ([]docker.BindMount, error) {
	mods := filepath.Join(dir, world.ModsDir)
	entries, err := fs.ReadDir(base.FS(), filepath.ToSlash(mods))
	if err != nil {
		return nil, err
	}
	var out []docker.BindMount
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		out = append(out, c.readOnlyMount(filepath.Join(mods, e.Name()), gameenv.PreviewMount+"/"+world.ModsDir+"/"+e.Name()))
	}
	return out, nil
}

// readOnlyMount mounts the file rel, a path under the preview folder, at
// target in the container, read-only. A save read mounts the save's link
// this way (SPEC 8.16).
func (c *Controller) readOnlyMount(rel, target string) docker.BindMount {
	return docker.BindMount{
		Type:     "bind",
		Source:   filepath.Join(c.hostData, previewDir, rel),
		Target:   target,
		ReadOnly: true,
	}
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
	defaults, err = world.ReadSmallFile(root, mods+"/"+world.ModSettingsFile, world.MaxModSettings)
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
	case errors.Is(err, ErrControlModTaken):
		return fmt.Errorf("%w: %w", ErrModSettingsBadOutput, err)
	case errors.As(err, &pe) && pe.OOM:
		return fmt.Errorf("%w: %w", ErrModSettingsOutOfMemory, err)
	case errors.As(err, &pe):
		return fmt.Errorf("%w: %w", ErrModSettingsBadOutput, err)
	}
	return err
}

// localeSections merges the enabled mods' raw locale files into sections for
// each of Foreman's languages. Locale text is decoration: a file that does
// not parse is logged and skipped, so a world shows names instead. Files
// merge in the order of mod name, then as given.
func (c *Controller) localeSections(id string, files map[string]map[string][][]byte) map[string]modsettings.LocaleSections {
	mods := make([]string, 0, len(files))
	for mod := range files {
		mods = append(mods, mod)
	}
	sort.Strings(mods)
	all := map[string]modsettings.LocaleSections{}
	for lang, code := range modsettings.FactorioLocales {
		secs := modsettings.LocaleSections{}
		for _, mod := range mods {
			for _, data := range files[mod][code] {
				parsed, err := modsettings.ParseLocaleCfg(data)
				if err != nil {
					c.log.Warn("a mod's locale file does not parse", "world", id, "mod", mod, "locale", code, "error", err)
					continue
				}
				secs.Merge(parsed)
			}
		}
		if len(secs) > 0 {
			all[lang] = secs
		}
	}
	return all
}

// modSettingsLocale builds the settings' names and descriptions in Foreman's
// languages from the mods' merged locale sections. As in the game, a
// setting's own localised name or description, when it has one, is the text:
// resolved in the language with English behind it key by key, and empty when
// it does not resolve. Only a setting without one takes its
// [mod-setting-name] or [mod-setting-description] entry (SPEC 9.6). The page
// falls back to English, then to the setting's name.
func modSettingsLocale(all map[string]modsettings.LocaleSections, defs modsettings.Definitions) modsettings.Locale {
	out := modsettings.Locale{}
	for lang, secs := range all {
		texts := map[string]modsettings.LocaleText{}
		for n, v := range secs["mod-setting-name"] {
			t := texts[n]
			t.Name = v
			texts[n] = t
		}
		for n, v := range secs["mod-setting-description"] {
			t := texts[n]
			t.Description = v
			texts[n] = t
		}
		layers := []modsettings.LocaleSections{secs}
		if lang != "en" && all["en"] != nil {
			layers = append(layers, all["en"])
		}
		for _, d := range defs {
			t, had := texts[d.Name]
			if hasLocalised(d.LocalisedName) {
				t.Name, _ = modsettings.ResolveLocalised(d.LocalisedName, layers...)
			}
			if hasLocalised(d.LocalisedDescription) {
				t.Description, _ = modsettings.ResolveLocalised(d.LocalisedDescription, layers...)
			}
			if t != (modsettings.LocaleText{}) {
				texts[d.Name] = t
			} else if had {
				delete(texts, d.Name)
			}
		}
		if len(texts) > 0 {
			out[lang] = texts
		}
	}
	return out
}

// hasLocalised reports whether the game gave a localised string.
func hasLocalised(raw []byte) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null"
}
