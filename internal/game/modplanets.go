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
// cannot be read is a miss, and the run reports the cause.
func (c *Controller) modPlanetDefs(ctx context.Context, id string) (*ModSettingDefs, error) {
	if key, _, err := c.worlds.ModPlanetsFile(id); err == nil && key == "" {
		return c.ModSettingDefs(ctx, id)
	}
	return c.sharedModSettingsRun(ctx, id, c.planetsRun())
}

// cachedModPlanetDefs is modPlanetDefs without starting a run.
func (c *Controller) cachedModPlanetDefs(id string) (*ModSettingDefs, bool) {
	k := c.definitionsRun()
	if key, _, err := c.worlds.ModPlanetsFile(id); err != nil || key != "" {
		k = c.planetsRun()
	}
	key, err := k.key(id)
	if err != nil || key == "" {
		return nil, false
	}
	k.cache.mu.Lock()
	defer k.cache.mu.Unlock()
	return k.cache.get(id, key)
}
