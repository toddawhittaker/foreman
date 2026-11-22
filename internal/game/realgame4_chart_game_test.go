//go:build game

package game

// The real-game picture (SPEC 5.6, 14.3, 14.6) against the real 2.1.24
// image and one planet mod set, Cerys with its three dependencies: the
// picture shows Cerys's own terrain, not the flat colour the fast preview
// gives a planet it has only a prototype of, and it comes within the run's
// 90 seconds and 1 GiB. A world of 2.0.77, which has no --test-save, is
// refused before any container starts, and no container or work folder is
// left. The zips are read from the folder FOREMAN_TEST_PLANET_MODS names;
// the test skips when it is unset, and nothing is downloaded. Run by
// `make test-game`, never by `make verify`.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// chartModGlobs are the zips of Cerys and its three dependencies, and
// their mod names for the mod list, in load order.
var chartModGlobs = []struct{ glob, name string }{
	{"bplib_*.zip", "bplib"},
	{"Cerys-Moon-of-Fulgora_*.zip", "Cerys-Moon-of-Fulgora"},
	{"Flare Stack_*.zip", "Flare Stack"},
	{"PlanetsLib_*.zip", "PlanetsLib"},
}

// chartPlanets is every planet the world's request may name.
var chartPlanets = []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo", "cerys"}

// chartWorld makes a world of version with the Cerys set in its mods folder
// and every mod of the list enabled, and returns its id.
func (e *gameEnv) chartWorld(t *testing.T, version, dir string) string {
	t.Helper()
	id, err := e.worlds.Create(context.Background(), "chart "+version+" "+e.run, version, true)
	if err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(e.worlds.Dirs().Worlds, id, world.ModsDir)
	if err := os.MkdirAll(mods, 0o750); err != nil {
		t.Fatal(err)
	}
	list := []string{"base", "elevated-rails", "quality", "recycler", "space-age"}
	for _, m := range chartModGlobs {
		zips, err := filepath.Glob(filepath.Join(dir, m.glob))
		if err != nil || len(zips) != 1 {
			t.Fatalf("want one %s in %s, found %v (%v)", m.glob, dir, zips, err)
		}
		zip, err := os.ReadFile(zips[0]) //nolint:gosec // the tester names the folder
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mods, filepath.Base(zips[0])), zip, 0o640); err != nil { //nolint:gosec // the test's own world
			t.Fatal(err)
		}
		list = append(list, m.name)
	}
	var b strings.Builder
	for i, name := range list {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"name":%q,"enabled":true}`, name)
	}
	if err := os.WriteFile(filepath.Join(mods, world.ModListFile), []byte(`{"mods":[`+b.String()+`]}`), 0o640); err != nil { //nolint:gosec // the test's own world
		t.Fatal(err)
	}
	return id
}

// pixelStats is what a picture's pixels say about it: how many are not the
// black of ground no chunk was made for, how many colours there are, and the
// commonest colour that is not black.
type pixelStats struct {
	lit, colours int
	dominant     color.NRGBA
}

func chartPixels(t *testing.T, data []byte) pixelStats {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	b := img.Bounds()
	counts := map[color.NRGBA]int{}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			counts[color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)]++
		}
	}
	black := color.NRGBA{A: 255}
	var s pixelStats
	s.colours = len(counts)
	top := 0
	for c, n := range counts {
		if c == black {
			continue
		}
		s.lit += n
		if n > top || (n == top && lessColor(c, s.dominant)) {
			top, s.dominant = n, c
		}
	}
	return s
}

// lessColor orders colours so a tie for the commonest is settled the same way
// on every run.
func lessColor(a, b color.NRGBA) bool {
	return a.R < b.R || (a.R == b.R && (a.G < b.G || (a.G == b.G && a.B < b.B)))
}

// containerCreations lists the containers Docker made between two times that
// carry this run's preview label, so a refusal can be shown to have made none.
func (e *gameEnv) containerCreations(t *testing.T, from, until time.Time) string {
	t.Helper()
	unix := func(at time.Time) string { return fmt.Sprintf("%d.%09d", at.Unix(), at.Nanosecond()) }
	out, err := exec.CommandContext(context.Background(), "docker", "events", "--since", unix(from), "--until", unix(until), //nolint:gosec // fixed arguments, times of this test
		"--filter", "type=container", "--filter", "event=create",
		"--filter", "label="+gameenv.LabelPreview+"="+e.data, "--format", "{{.Actor.Attributes.name}}").CombinedOutput()
	if err != nil {
		t.Fatalf("docker events: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRealGameChartOfAModdedPlanet(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && only != "2.1" {
		t.Skip("runs on 2.1 only")
	}
	dir := os.Getenv("FOREMAN_TEST_PLANET_MODS")
	if dir == "" {
		t.Skip("FOREMAN_TEST_PLANET_MODS is not set")
	}
	const version = "2.1.24"
	e := newGameEnv(t)
	e.requireImage(t, ImageRef(version))
	c, _ := e.controller(t)
	id := e.chartWorld(t, version, dir)
	mods := filepath.Join(e.worlds.Dirs().Worlds, id, world.ModsDir)
	before := listFolder(t, mods)

	req := PreviewRequest{
		GameVersion: version, SpaceAge: true, Seed: 123456, Size: PreviewSizeMin, World: id,
		Planets: []string{"cerys"}, KnownPlanets: chartPlanets,
	}

	// The refusals come first: they must not start a container, and the 2.0
	// world needs no image because none is used.
	t.Run("a world of 2.0.77 is refused before any container", func(t *testing.T) {
		old := req
		old.GameVersion = "2.0.77"
		old.World = e.chartWorld(t, old.GameVersion, dir)
		from := time.Now()
		_, err := c.Chart(context.Background(), old, "cerys")
		until := time.Now().Add(time.Second)
		if !errors.Is(err, ErrPreviewInvalid) {
			t.Fatalf("error %v, want ErrPreviewInvalid", err)
		}
		if made := e.containerCreations(t, from, until); made != "" {
			t.Errorf("a refused picture made containers: %s", made)
		}
		e.previewLeftovers(t, "after the refusal")
	})
	t.Run("a planet the world does not know is refused before any container", func(t *testing.T) {
		from := time.Now()
		_, err := c.Chart(context.Background(), req, "not-a-planet")
		until := time.Now().Add(time.Second)
		if !errors.Is(err, ErrPreviewInvalid) {
			t.Fatalf("error %v, want ErrPreviewInvalid", err)
		}
		if made := e.containerCreations(t, from, until); made != "" {
			t.Errorf("a refused picture made containers: %s", made)
		}
		e.previewLeftovers(t, "after the refusal")
	})

	var chart pixelStats
	t.Run("cerys shows its own terrain within 90 s and 1 GiB", func(t *testing.T) {
		stop := e.peakMemory()
		begin := time.Now()
		pic, err := c.Chart(context.Background(), req, "cerys")
		took := time.Since(begin)
		peak := stop()
		t.Logf("TIMING real-game picture of cerys: %s, peak %d MiB", took.Round(100*time.Millisecond), peak>>20)
		if err != nil {
			t.Fatal(err)
		}
		if pic.Planet != "cerys" || len(pic.Data) == 0 {
			t.Fatalf("picture of %q, %d bytes", pic.Planet, len(pic.Data))
		}
		if took > ChartLimit {
			t.Errorf("took %s, more than %s", took, ChartLimit)
		}
		// A zero peak would mean the cgroup was not read, and the limit unproved.
		if peak == 0 || peak > previewMemory {
			t.Errorf("peak memory %d bytes, want over 0 and at most %d", peak, int64(previewMemory))
		}
		chart = chartPixels(t, pic.Data)
		t.Logf("chart: %d lit pixels, %d colours, dominant %v", chart.lit, chart.colours, chart.dominant)
		// A prototype's disc is one flat colour over a small area; real terrain
		// is a large area of land in many colours.
		if chart.lit < 30000 || chart.colours < 20 {
			t.Errorf("%d lit pixels in %d colours, want at least 30000 in 20", chart.lit, chart.colours)
		}
		e.previewLeftovers(t, "after the picture")
	})
	t.Run("the picture is not the fast preview's disc", func(t *testing.T) {
		if chart.lit == 0 {
			t.Skip("there is no real-game picture to compare")
		}
		pngs, err := c.Preview(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if len(pngs) != 1 || pngs[0].Planet != "cerys" {
			t.Fatalf("%d previews", len(pngs))
		}
		fast := chartPixels(t, pngs[0].Data)
		t.Logf("fast preview: %d lit pixels, %d colours, dominant %v", fast.lit, fast.colours, fast.dominant)
		if fast.lit == 0 {
			t.Fatal("the fast preview of cerys is empty")
		}
		if chart.dominant == fast.dominant {
			t.Errorf("the real-game picture's commonest colour %v is the fast preview's", chart.dominant)
		}
		e.previewLeftovers(t, "after the preview")
	})

	if after := listFolder(t, mods); !slices.Equal(after, before) {
		t.Errorf("the world's mods folder changed: %v, was %v", after, before)
	}
}
