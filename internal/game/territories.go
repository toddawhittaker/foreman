package game

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game/territories"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/territory"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	// territoriesPlanet is the planet whose demolisher territories are read.
	territoriesPlanet = "vulcanus"
	// territoriesSave is the save the territory run makes. It goes to the work
	// folder, not the tmpfs, which counts against the memory limit.
	territoriesSave = "territories.zip"
	// territoriesScriptOutput is where the game's script writes its file.
	territoriesScriptOutput = previewWrite + "/script-output/" + gameenv.TerritoryFile
)

// territoriesScript makes the radius file the mod reads, creates a map from
// the same seed, preset and settings files a preview uses, and copies the
// mod's JSON to the work folder. The values come from the environment
// (gameenv).
var territoriesScript = strings.Join([]string{
	`printf 'return %d\n' "$` + gameenv.TerritoryRadius + `" > ` + gameenv.PreviewMount + `/` + world.ModsDir + `/` + territories.ModDir + `/` + territories.RadiusFile + ` || exit 1`,
	`set -- --config ` + gameenv.PreviewMount + `/config.ini --mod-directory ` + gameenv.PreviewMount + `/` + world.ModsDir +
		` --create ` + gameenv.PreviewMount + `/` + territoriesSave + ` --map-gen-seed "$` + gameenv.Seed + `"`,
	`if [ -n "$` + gameenv.Preset + `" ]; then set -- "$@" --preset "$` + gameenv.Preset + `"; fi`,
	`if [ -f ` + gameenv.PreviewMount + `/` + world.MapGenSettingsFile + ` ]; then set -- "$@" --map-gen-settings ` + gameenv.PreviewMount + `/` + world.MapGenSettingsFile + `; fi`,
	`if [ -f ` + gameenv.PreviewMount + `/` + world.MapSettingsFile + ` ]; then set -- "$@" --map-settings ` + gameenv.PreviewMount + `/` + world.MapSettingsFile + `; fi`,
	gameBinary + ` "$@" || exit 1`,
	`cp ` + territoriesScriptOutput + ` ` + gameenv.PreviewMount + `/` + gameenv.TerritoryFile,
}, "\n")

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
	if err := c.takePreviewSlot(ctx); err != nil {
		return territory.Result{}, err
	}
	defer func() { <-c.previewSlot }()

	ref, err := c.imageFor(req.GameVersion)
	if err != nil {
		return territory.Result{}, err
	}
	if _, err := c.docker.InspectImage(ctx, ref); errors.Is(err, docker.ErrNotFound) {
		return territory.Result{}, ErrPreviewImageMissing
	} else if err != nil {
		return territory.Result{}, err
	}
	base, err := c.previewBase()
	if err != nil {
		return territory.Result{}, err
	}
	defer func() { _ = base.Close() }()
	c.removeStalePreviews(ctx, base)

	name, err := makePreviewDir(base, req)
	if err != nil {
		return territory.Result{}, err
	}
	defer func() {
		if err := base.RemoveAll(name); err != nil {
			c.log.Warn("could not remove a territory run's work folder", "folder", name, "error", err)
		}
	}()
	if err := addTerritoriesMod(base, name, line, req); err != nil {
		return territory.Result{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, c.previewLimit)
	defer cancel()
	if err := c.runOneOff(runCtx, "foreman-territories-"+name, c.territoriesSpec(ref, name, req)); err != nil {
		return territory.Result{}, err
	}
	return readTerritories(base, name, req.size())
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

// modEntry is one mod in mod-list.json, in the game's own shape.
type modEntry struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// addTerritoriesMod puts the control-only mod in the work folder and enables
// it in the mod list that makePreviewDir wrote.
func addTerritoriesMod(base *os.Root, name, line string, req PreviewRequest) error {
	dir, err := base.OpenRoot(name)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	var list struct {
		Mods []modEntry `json:"mods"`
	}
	if err := json.Unmarshal(world.DefaultModList(req.GameVersion, req.SpaceAge), &list); err != nil {
		return err
	}
	list.Mods = append(list.Mods, modEntry{territories.ModName, true})
	modList, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	modDir := path.Join(world.ModsDir, territories.ModDir)
	if err := world.Mkdir(dir, modDir); err != nil {
		return err
	}
	for file, data := range map[string][]byte{
		path.Join(world.ModsDir, world.ModListFile): append(modList, '\n'),
		path.Join(modDir, "info.json"):              territories.InfoJSON(line),
		path.Join(modDir, territories.ControlFile):  territories.Control,
	} {
		if err := dir.WriteFile(file, data, world.FileMode); err != nil {
			return err
		}
	}
	return nil
}

// territoriesSpec is the territory run's container: a preview container with
// its own command and environment, and the same limits (SPEC 14.3).
func (c *Controller) territoriesSpec(image, dir string, req PreviewRequest) docker.ContainerSpec {
	env := []string{
		gameenv.PreviewKind + "=" + gameenv.KindTerritories,
		gameenv.TerritoryRadius + "=" + strconv.Itoa(req.size()/(2*territory.ChunkTiles)),
		gameenv.Seed + "=" + strconv.FormatUint(uint64(req.Seed), 10),
	}
	if req.Preset != "" {
		env = append(env, gameenv.Preset+"="+req.Preset)
	}
	return docker.ContainerSpec{
		Image:      image,
		Labels:     map[string]string{gameenv.LabelPreview: c.hostData},
		Env:        env,
		Entrypoint: []string{"/bin/sh"},
		Cmd:        []string{"-c", territoriesScript},
		User:       c.user,
		HostConfig: c.oneOffHostConfig(dir),
	}
}

// readTerritories reads the run's JSON from the work folder, at most one byte
// more than territory.MaxJSON, and parses it for a picture of size pixels.
// The game's script is untrusted (SPEC 14.1), so Parse does the checking.
func readTerritories(base *os.Root, dir string, size int) (territory.Result, error) {
	root, err := base.OpenRoot(dir)
	if err != nil {
		return territory.Result{}, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(gameenv.TerritoryFile)
	if err != nil {
		return territory.Result{}, fmt.Errorf("the game made no territory file: %w", err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, territory.MaxJSON+1))
	if err != nil {
		return territory.Result{}, err
	}
	return territory.Parse(b, size)
}
