package game

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/docker"
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

// territoriesScript is the territory run's shell script (controlMod.script).
var territoriesScript = territoryMod.script()

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
	b, err := c.runControlMod(ctx, req, territoryMod, territories.InfoJSON(line), territoriesEnv(req))
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
	return []string{gameenv.TerritoryRadius + "=" + strconv.Itoa(req.size()/(2*territory.ChunkTiles))}
}

// territoriesSpec is the territory run's container: a preview container with
// its own command and environment, and the same limits (SPEC 14.3).
func (c *Controller) territoriesSpec(image, dir string, req PreviewRequest) docker.ContainerSpec {
	return c.controlModSpec(image, dir, req, territoryMod, territoriesEnv(req))
}

// readTerritories reads the run's JSON from the work folder, at most
// territory.MaxJSON bytes, and parses it for a picture of size pixels.
func readTerritories(base *os.Root, dir string, size int) (territory.Result, error) {
	b, err := readControlModOutput(base, dir, territoryMod)
	if err != nil {
		return territory.Result{}, err
	}
	return territory.Parse(b, size)
}
