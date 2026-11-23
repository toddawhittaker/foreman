package game

import (
	"context"
	"os"
	"path"

	"github.com/toddawhittaker/foreman/internal/world"
)

// modSettingsRunKind is one kind of mod-settings run: the definitions run
// on the mods' default values, or the planets run on the world's own
// mod-settings.dat, whose startup values can add or remove planets (SPEC
// 5.6, 9.6). Each has its own cache, so the definitions keep the defaults.
type modSettingsRunKind struct {
	cache    *modSettingsCache
	key      func(id string) (string, error)
	settings bool
}

func (c *Controller) definitionsRun() modSettingsRunKind {
	return modSettingsRunKind{cache: &c.modSettings, key: c.worlds.ModSettingsKey}
}

func (c *Controller) planetsRun() modSettingsRunKind {
	return modSettingsRunKind{cache: &c.modPlanets, key: c.modPlanetsKey, settings: true}
}

// modPlanetsKey is the planets run's key; empty when it cannot be read.
func (c *Controller) modPlanetsKey(id string) (string, error) {
	key, _, err := c.worlds.ModPlanetsFile(id)
	return key, err
}

// addWorldSettings copies the world's mod-settings.dat into the work
// folder's mods folder and returns the planets key read with it. It is a
// copy, not a link: the game writes the file back.
func (c *Controller) addWorldSettings(dir *os.Root, id string) (string, error) {
	key, data, err := c.worlds.ModPlanetsFile(id)
	if err != nil || data == nil {
		return key, err
	}
	return key, world.WriteFileAtomic(dir, path.Join(world.ModsDir, world.ModSettingsFile), data)
}

// modPlanetDefs is the run the world's planets come from: the planets run
// when the world has a mod-settings.dat, otherwise the definitions run, so a
// world on its defaults runs the game once (SPEC 5.6, 9.6). A file that
// cannot be read is a miss, and the run reports the cause. The planets key
// is read once and passed on.
func (c *Controller) modPlanetDefs(ctx context.Context, id string) (*ModSettingDefs, error) {
	key, _, err := c.worlds.ModPlanetsFile(id)
	if err == nil && key == "" {
		return c.ModSettingDefs(ctx, id)
	}
	return c.sharedModSettingsRun(ctx, id, c.planetsRun(), key)
}

// cachedModPlanetDefs is modPlanetDefs without starting a run: one cache
// lookup, with one read of each key it needs.
func (c *Controller) cachedModPlanetDefs(id string) (*ModSettingDefs, bool) {
	key, _, err := c.worlds.ModPlanetsFile(id)
	if err != nil {
		return nil, false
	}
	if key == "" {
		return c.CachedModSettingDefs(id)
	}
	return c.modPlanets.lookup(id, key)
}

// CachedModPlanetDefs is the cached result of the run the world's planets come
// from (the planets run, or the definitions run for a world on its defaults),
// without starting one; false when it is not cached.
func (c *Controller) CachedModPlanetDefs(id string) (*ModSettingDefs, bool) {
	return c.cachedModPlanetDefs(id)
}

// CachedModSettingDefs is the world's cached setting definitions, without
// starting a run; false when they are not cached.
func (c *Controller) CachedModSettingDefs(id string) (*ModSettingDefs, bool) {
	key, err := c.worlds.ModSettingsKey(id)
	if err != nil {
		return nil, false
	}
	return c.modSettings.lookup(id, key)
}

// lookup is get under the cache's lock; an empty key is a miss.
func (m *modSettingsCache) lookup(id, key string) (*ModSettingDefs, bool) {
	if key == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.get(id, key)
}
