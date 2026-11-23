package game

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/game/chart"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// ChartLimit is how long a real-game picture may take in all, the map's
// creation and the charting together, before its container is killed (SPEC
// 14.3). Measured with five planet mods: 13 to 39 seconds.
const ChartLimit = 90 * time.Second

// chartLimit is ChartLimit; tests shorten it.
var chartLimit = ChartLimit

// chartSave is the throwaway map the run makes. It goes to the work folder,
// not the tmpfs, which counts against the memory limit.
const chartSave = "chart.zip"

// ErrChartBadOutput means the real-game picture run left no chart Foreman
// accepts (SPEC 14.6). Nothing of the file is in the message.
var ErrChartBadOutput = errors.New("the game made no usable real-game picture")

// chartMod is the real-game picture's helper. Of controlMod it uses kind,
// name, dir, control and params; the run's own script makes the map and
// loads it, so save and output are not used.
var chartMod = controlMod{
	kind:    gameenv.KindChart,
	what:    "real-game picture",
	name:    chart.ModName,
	dir:     chart.ModDir,
	control: chart.Control,
	params: `printf 'return { radius = %d, planet = "%s" }\n' "$` + gameenv.ChartRadius + `" "$` + gameenv.ChartPlanet + `" > ` +
		modFile(chart.ModDir, chart.ParamsFile),
}

// chartScript makes the params file, creates a map from the request's seed,
// preset and settings files, loads it with --test-save, which gives it a
// player so the helper can chart, and moves the helper's folder to the work
// folder. Values come from the environment (gameenv), so no value of a
// request is built into it.
var chartScript = strings.Join(slices.Concat(
	[]string{
		chartMod.params + ` || exit 1`,
		`game="` + gameBinary + ` --config ` + gameenv.PreviewMount + `/config.ini --mod-directory ` + gameenv.PreviewMount + `/` + world.ModsDir + `"`,
	},
	createLines("", chartSave),
	[]string{
		`$game "$@" || exit 1`,
		`$game --test-save ` + gameenv.PreviewMount + `/` + chartSave + ` --test-ticks ` + strconv.Itoa(chart.Ticks) + ` || exit 1`,
		`mv ` + previewWrite + `/script-output/` + gameenv.ChartDir + ` ` + gameenv.PreviewMount + `/` + gameenv.ChartDir,
	},
), "\n")

// chartRunLimit is how long the run may take: the limit of a preview that
// loads a world's mods, or ChartLimit when that is larger (SPEC 14.3).
func (c *Controller) chartRunLimit() time.Duration {
	return max(chartLimit, c.previewWorldLimit)
}

// testSaveLine is the one game line with --test-save among the supported lines.
const testSaveLine = "2.1"

// ChartSupported reports whether a world of the game version can have a
// real-game picture: only 2.1 has --test-save among the supported lines
// (SPEC 5.6).
func ChartSupported(gameVersion string) bool {
	line, ok := mapgen.Line(gameVersion)
	return ok && line == testSaveLine
}

// Chart draws planet of the world req names from a real game (SPEC 5.6,
// 14.3): a seventh kind of one-off container, "foreman-chart-<folder>", in
// the preview slot with the preview's host settings and the memory of a
// run that loads a world's mods, that loads the world's mods, creates a
// throwaway map from req's settings, loads it with --test-save and charts
// the chart.Radius chunks around the origin, all within chartRunLimit. The chart is checked under SPEC 14.6 and drawn as the
// factory map is (SPEC 5.16). req must name a world on a line ChartSupported
// accepts, and planet must be in req.KnownPlanets; req.Planets and req.Size
// are not used. Errors are ErrPreviewInvalid, ErrPreviewBusy,
// ErrPreviewImageMissing, ErrPreviewTimeout, a *PreviewError (OOM set when
// the run ran out of memory), one wrapping ErrChartBadOutput, or what
// making the work folder returns (world.ErrBusy, world.ErrModsTooBig). A
// helper that gives up or a game that crashes is a *PreviewError, and a chart
// that is read but refused is ErrChartBadOutput; the UI shows the one "could
// not draw this planet" message for both. An ErrChartBadOutput may also match
// ErrSaveCopyBadOutput, since the chart is read with the factory map's
// readers, so a caller tests ErrChartBadOutput first.
func (c *Controller) Chart(ctx context.Context, req PreviewRequest, planet string) (PNG, error) {
	line, err := checkChart(req, planet)
	if err != nil {
		return PNG{}, err
	}
	var out PNG
	plan := previewPlan{
		gameVersion: req.GameVersion, kind: chartMod.kind, limit: c.chartRunLimit(),
		fill: func(base *os.Root) (string, error) { return c.makeWorldPreviewDir(base, req) },
	}
	err = c.previewRunIn(ctx, plan, func(base *os.Root, ref, name string, container func(docker.ContainerSpec) error) error {
		if err := addControlMod(base, name, chartMod, chartInfo(line)); err != nil {
			return err
		}
		spec := c.controlModSpec(ref, name, req, chartMod, chartEnv(planet))
		spec.Cmd = []string{"-c", chartScript}
		mounts, err := c.worldModMounts(base, name, req)
		if err != nil {
			return err
		}
		spec.HostConfig.Mounts = mounts
		if err := container(spec); err != nil {
			return err
		}
		data, err := readChart(base, name, planet)
		out = PNG{Planet: planet, Data: data}
		return err
	})
	if err != nil {
		return PNG{}, err
	}
	return out, nil
}

// checkChart refuses a request that cannot have a real-game picture of
// planet, then checks it as a preview of that planet, and returns the line.
func checkChart(req PreviewRequest, planet string) (string, error) {
	line, ok := mapgen.Line(req.GameVersion)
	switch {
	case !ok || line != testSaveLine:
		return "", fmt.Errorf("%w: there is no real-game picture for Factorio %q", ErrPreviewInvalid, req.GameVersion)
	case req.World == "":
		return "", fmt.Errorf("%w: a real-game picture is of a world", ErrPreviewInvalid)
	case !slices.Contains(req.knownPlanets(line), planet):
		return "", fmt.Errorf("%w: planet %q", ErrPreviewInvalid, planet)
	}
	req.Planets = []string{planet}
	req.Size = 0
	return checkPreview(req)
}

// chartInfo is the helper's info.json. It needs the base game only.
func chartInfo(line string) []byte {
	return modInfo(chart.ModName, chart.ModVersion, "Foreman real-game picture",
		"Charts one planet for Foreman's real-game picture. Adds no prototypes.", line, "base")
}

// chartEnv is the run's own environment: the radius and the planet.
func chartEnv(planet string) []string {
	return []string{
		gameenv.ChartRadius + "=" + strconv.Itoa(chart.Radius),
		gameenv.ChartPlanet + "=" + planet,
	}
}

// readChart reads the helper's folder in the work folder dir and draws the
// planet's picture as a PNG; any error wraps ErrChartBadOutput. The output
// is untrusted (SPEC 14.1, 14.6): one planet surface named planet, whose
// bounds are the whole charted square, at most chart.MaxChunks chunks of 32
// by 32 pixels, the manifest under its own limit and the chart file at most
// chart.MaxFileBytes, each a regular file.
func readChart(base *os.Root, dir, planet string) ([]byte, error) {
	b, err := drawChartOutput(base, dir, planet)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrChartBadOutput, err)
	}
	return b, nil
}

func drawChartOutput(base *os.Root, dir, planet string) ([]byte, error) {
	root, err := base.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	out, err := root.OpenRoot(gameenv.ChartDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = out.Close() }()
	mb, err := readManifest(out, &outputBudget{left: factorymap.MaxManifestBytes})
	if err != nil {
		return nil, err
	}
	m, err := factorymap.ParseManifest(bytes.NewReader(mb))
	if err != nil {
		return nil, err
	}
	if len(m.Surfaces) != 1 {
		return nil, fmt.Errorf("the chart has %d surfaces, not one", len(m.Surfaces))
	}
	s := m.Surfaces[0]
	r := chart.Radius
	switch {
	case s.Name != planet || s.Kind != "planet":
		return nil, errors.New("the chart is not of the planet asked for")
	case s.MinX != -r || s.MinY != -r || s.MaxX != r-1 || s.MaxY != r-1:
		return nil, errors.New("the chart's bounds are not the charted square")
	case s.Chunks > chart.MaxChunks:
		return nil, fmt.Errorf("the chart has %d chunks, at most %d", s.Chunks, chart.MaxChunks)
	}
	f, err := openOutputFile(out, s.Chart, &outputBudget{left: chart.MaxFileBytes})
	if errors.Is(err, errFactoryMapTooLarge) {
		return nil, fmt.Errorf("the chart file is over %d bytes", chart.MaxFileBytes)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	img, _, err := factorymap.DrawChart(bytes.NewReader(mb), f, s.Index)
	if err != nil {
		return nil, err
	}
	return encodePNG(img)
}
