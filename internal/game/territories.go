package game

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/game/territories"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/territory"
)

const (
	// territoriesPlanet is the planet whose demolisher territories are read.
	territoriesPlanet = "vulcanus"
)

// territoryMod is the territory run's control mod. Its radius file returns
// the picture's half width in chunks.
var territoryMod = controlMod{
	kind:      gameenv.KindTerritories,
	what:      "territory",
	name:      territories.ModName,
	dir:       territories.ModDir,
	control:   territories.Control,
	params:    `printf 'return %d\n' "$` + gameenv.TerritoryRadius + `" > ` + modFile(territories.ModDir, territories.RadiusFile),
	save:      "territories.zip",
	output:    gameenv.TerritoryFile,
	maxOutput: territory.MaxJSON,
}

// territoryInfo is the territory mod's info.json. It needs Space Age, which
// has Vulcanus.
func territoryInfo(line string) []byte {
	return modInfo(territories.ModName, territories.ModVersion, "Foreman territories",
		"Writes Vulcanus's demolisher territories for Foreman's map preview. Adds no prototypes.",
		line, "base", "space-age")
}

// Territories reads where Vulcanus's demolisher territories lie in the map a
// preview of req would draw (SPEC 5.6). It is a one-off container like
// Preview's, with the same single slot, stale cleanup, work folder and
// limits (SPEC 14.3), that creates the map with a control-only mod and
// leaves a JSON file. req must be for Space Age on a line with Vulcanus, and
// Size no more than territory.MaxSize; Planets is ignored. The result is
// checked by territory.Parse against the picture's size. Errors are
// ErrPreviewInvalid, ErrPreviewBusy, ErrPreviewImageMissing,
// ErrPreviewTimeout or a *PreviewError, as for Preview.
func (c *Controller) Territories(ctx context.Context, req PreviewRequest) (territory.Result, error) {
	line, err := checkTerritories(req)
	if err != nil {
		return territory.Result{}, err
	}
	b, err := c.runControlMod(ctx, req, territoryMod, territoryInfo(line), territoriesEnv(req))
	if err != nil {
		return territory.Result{}, err
	}
	return territory.Parse(b, req.size())
}

// checkTerritories refuses a request that has no territories to read, then
// checks it as a preview, and returns the game line.
func checkTerritories(req PreviewRequest) (line string, err error) {
	line, ok := mapgen.Line(req.GameVersion)
	switch {
	case !ok:
		return "", fmt.Errorf("%w: there are no territories for Factorio %q", ErrPreviewInvalid, req.GameVersion)
	case !req.SpaceAge || !slices.Contains(mapgen.Planets(line, true), territoriesPlanet):
		return "", fmt.Errorf("%w: Factorio %s without Space Age has no demolisher territories", ErrPreviewInvalid, line)
	case req.size() > territory.MaxSize:
		return "", fmt.Errorf("%w: size %d is more than %d, the most territories are read for", ErrPreviewInvalid, req.size(), territory.MaxSize)
	}
	req.Planets = []string{territoriesPlanet}
	return checkPreview(req)
}

// territoriesEnv is the territory run's own environment: the radius.
func territoriesEnv(req PreviewRequest) []string {
	return []string{gameenv.TerritoryRadius + "=" + strconv.Itoa(chunkRadius(req))}
}
