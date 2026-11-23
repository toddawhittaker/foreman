package game

import (
	"context"
	"fmt"
	"strings"

	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// warmNewMapCatalogue runs or waits for the planet run of an Unfinished
// world before an action takes the world's lock, since the run needs that
// lock and staleControlsCheck, under it, reads only the cache.
func (c *Controller) warmNewMapCatalogue(ctx context.Context, id string) {
	if sum, err := c.worlds.Get(ctx, id); err != nil || !sum.Unfinished {
		return
	}
	_, _ = c.WorldCatalogue(ctx, id, "en")
}

// staleControlsCheck adds a refusal when the world is Unfinished and its
// saved map settings name an autoplace control its current planet list
// lacks: the game's --create stops on an unknown control and writes no save
// (SPEC 5.6, 7.4). A world with a save, or without the file, is left alone,
// and so is a world whose planet run failed: the create then fails without
// writing a save, and the start's error says why. The caller holds the
// world's lock, so the planets come from the cache only.
func (c *Controller) staleControlsCheck(ctx context.Context, id string, check world.PreCheck) (world.PreCheck, error) {
	sum, err := c.worlds.Get(ctx, id)
	if err != nil {
		return check, err
	}
	if !sum.Unfinished {
		return check, nil
	}
	file, err := c.worlds.ReadConfig(id, world.MapGenSettingsFile)
	if err != nil {
		return check, err
	}
	if !file.Found {
		return check, nil
	}
	cat, err := c.cachedWorldCatalogue(id, "en")
	if err != nil {
		c.log.Warn("did not check the map settings against the world's planets", "world", id, "error", err)
		return check, nil
	}
	stale := mapgen.StaleControls(file.Data, cat)
	if len(stale) == 0 {
		return check, nil
	}
	check.Problems = append(check.Problems, world.Problem{
		Kind:  world.ProblemStaleControls,
		Names: stale,
		Text: fmt.Sprintf("The world's map settings name controls its mods no longer have: %s. The game cannot make the map with them. Save the map settings again to drop them.",
			strings.Join(stale, ", ")),
	})
	return check, nil
}
