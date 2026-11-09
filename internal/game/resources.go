package game

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/game/resources"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/resource"
)

// resourceMod is the resource count's control mod. Its params file returns
// the picture's half width in chunks and the planet to count. The planet is
// one of mapgen.Planets, checked before the run, so it cannot break out of
// the Lua string.
var resourceMod = controlMod{
	kind:    gameenv.KindResources,
	what:    "resource",
	name:    resources.ModName,
	dir:     resources.ModDir,
	control: resources.Control,
	params: `printf 'return { radius = %d, planet = "%s" }\n' "$` + gameenv.ResourceRadius + `" "$` + gameenv.ResourcePlanet + `" > ` +
		modFile(resources.ModDir, resources.ParamsFile),
	save:      "resources.zip",
	output:    gameenv.ResourceFile,
	maxOutput: resource.MaxJSON,
}

// Resources counts the resource patches of planet in the map a preview of
// req would draw (SPEC 5.6). It is a one-off container like Preview's, with
// the same single slot, stale cleanup, work folder and limits (SPEC 14.3),
// that creates the map with a control-only mod and leaves a JSON file of at
// most resource.MaxJSON bytes. planet must be one the request's map has;
// req.Planets is ignored. The file is checked by resource.Parse against the
// picture's size and the planet (SPEC 14.1). Errors are ErrPreviewInvalid,
// ErrPreviewBusy, ErrPreviewImageMissing, ErrPreviewTimeout, a *PreviewError
// (OOM set when the run ran out of memory), or one wrapping
// resource.ErrInvalid.
func (c *Controller) Resources(ctx context.Context, req PreviewRequest, planet string) (resource.Result, error) {
	line, err := checkResources(req, planet)
	if err != nil {
		return resource.Result{}, err
	}
	b, err := c.runControlMod(ctx, req, resourceMod, resources.InfoJSON(line, req.SpaceAge), resourcesEnv(req, planet))
	if err != nil {
		return resource.Result{}, err
	}
	return resource.Parse(b, req.size(), planet)
}

// checkResources refuses a planet the request's map does not have, then
// checks the request as a preview of that planet, and returns the game line.
func checkResources(req PreviewRequest, planet string) (string, error) {
	line, ok := mapgen.Line(req.GameVersion)
	if !ok {
		return "", fmt.Errorf("%w: there are no resources for Factorio %q", ErrPreviewInvalid, req.GameVersion)
	}
	if !slices.Contains(mapgen.Planets(line, req.SpaceAge), planet) {
		return "", fmt.Errorf("%w: planet %q", ErrPreviewInvalid, planet)
	}
	req.Planets = []string{planet}
	return checkPreview(req)
}

// resourcesEnv is the count run's own environment: the radius and the planet.
func resourcesEnv(req PreviewRequest, planet string) []string {
	return []string{
		gameenv.ResourceRadius + "=" + strconv.Itoa(chunkRadius(req)),
		gameenv.ResourcePlanet + "=" + planet,
	}
}
