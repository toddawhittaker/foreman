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
// one of the request's known planets, of the shape checkPreview holds names
// to, so it cannot break out of the Lua string.
var resourceMod = controlMod{
	kind:      gameenv.KindResources,
	what:      "resource",
	name:      resources.ModName,
	dir:       resources.ModDir,
	control:   resources.Control,
	params:    resourceParams(""),
	save:      "resources.zip",
	output:    gameenv.ResourceFile,
	maxOutput: resource.MaxJSON,
}

// resourceParams is the count mod's params line, with extra Lua fields when
// a test sets a gap. It carries Parse's limits, so the script keeps its
// spots inside them (SPEC 5.6).
func resourceParams(extra string) string {
	return `printf 'return { radius = %d, planet = "%s", max_patches = ` + strconv.Itoa(resource.MaxPatches) +
		`, max_bytes = ` + strconv.Itoa(resource.MaxJSON) + extra + ` }\n' "$` + gameenv.ResourceRadius + `" "$` + gameenv.ResourcePlanet + `" > ` +
		modFile(resources.ModDir, resources.ParamsFile)
}

// resourceInfo is the count mod's info.json. It depends on Space Age only
// when the map has it.
func resourceInfo(line string, spaceAge bool) []byte {
	deps := []string{"base"}
	if spaceAge {
		deps = append(deps, "space-age")
	}
	return modInfo(resources.ModName, resources.ModVersion, "Foreman resources",
		"Counts one planet's resource patches for Foreman's map preview. Adds no prototypes.",
		line, deps...)
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
	b, err := c.runControlMod(ctx, req, resourceMod, resourceInfo(line, req.SpaceAge), resourcesEnv(req, planet))
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
	if !slices.Contains(req.knownPlanets(line), planet) {
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
