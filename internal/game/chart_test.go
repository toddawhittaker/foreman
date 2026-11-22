package game

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game/chart"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 5.6, 14.3: only 2.1 has --test-save.
func TestChartSupported(t *testing.T) {
	for v, want := range map[string]bool{"2.1.24": true, "2.1.0": true, "2.0.77": false, "1.1.110": false, "2.1": false, "": false} {
		if got := ChartSupported(v); got != want {
			t.Errorf("ChartSupported(%q) = %v", v, got)
		}
	}
}

// SPEC 5.6, 14.3: a real-game picture is one planet's chart, 1024 by 1024
// tiles, drawn from the game's own chart: the fake's disc of land in water.
// The container and the work folder are gone after.
func TestChartDrawsThePlanet(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	got, err := r.c.Chart(context.Background(), worldRequest(id), "cerys")
	if err != nil {
		t.Fatal(err)
	}
	if got.Planet != "cerys" {
		t.Errorf("planet %q", got.Planet)
	}
	img, err := png.Decode(bytes.NewReader(got.Data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 1024 {
		t.Fatalf("picture %v, want 1024 by 1024", b)
	}
	// The fake's land is green and its water blue.
	_, g, b, _ := img.At(512+4, 512+4).RGBA()
	if g <= b {
		t.Error("the centre is not land")
	}
	_, g, b, _ = img.At(4, 4).RGBA()
	if b <= g {
		t.Error("the corner is not water")
	}
	if left := r.previewContainers(); len(left) != 0 {
		t.Errorf("%d containers left", len(left))
	}
	if left := r.previewFolders(); len(left) != 0 {
		t.Errorf("work folders left: %v", left)
	}
}

// SPEC 14.3: the seventh kind of container, with the preview's host
// settings and 1 GiB, the world's mods and the helper mod in its work
// folder, and a script that creates the map and then loads it with
// --test-save.
func TestChartContainer(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	q := worldRequest(id)
	q.Preset = "island"
	q.MapGenSettings = []byte(`{"width": 500}`)
	got := capturePreview(t, r, "chart", func(ctx context.Context) error {
		_, err := r.c.Chart(ctx, q, "cerys")
		return err
	})
	if got.Name != "foreman-chart-FOLDER" {
		t.Errorf("name %q", got.Name)
	}
	h := got.Spec.HostConfig
	if h.Memory != 1<<30 || h.MemorySwap != 1<<30 || h.NetworkMode != "none" || !h.ReadonlyRootfs ||
		!slices.Equal(h.CapDrop, []string{"ALL"}) || len(h.Binds) != 1 || h.Binds[0] != "DATA/previews/FOLDER:/out" {
		t.Errorf("host config %+v", h)
	}
	if len(h.Mounts) != 1 || h.Mounts[0].Target != "/out/mods/planet_1.0.0.zip" || !h.Mounts[0].ReadOnly {
		t.Errorf("mounts %+v", h.Mounts)
	}
	for _, e := range []string{
		gameenv.PreviewKind + "=" + gameenv.KindChart,
		gameenv.ChartPlanet + "=cerys",
		gameenv.ChartRadius + "=16",
		gameenv.Seed + "=7",
		gameenv.Preset + "=island",
	} {
		if !slices.Contains(got.Spec.Env, e) {
			t.Errorf("env %v lacks %s", got.Spec.Env, e)
		}
	}
	script := strings.Join(got.Spec.Cmd, " ")
	create := strings.Index(script, "--create /out/chart.zip")
	test := strings.Index(script, "--test-save /out/chart.zip")
	if create < 0 || test < create || !strings.Contains(script, "--map-gen-settings") {
		t.Errorf("script %s", script)
	}
	if got.Files[world.MapGenSettingsFile] != `{"width": 500}` {
		t.Errorf("map-gen-settings %q", got.Files[world.MapGenSettingsFile])
	}
	if got.Files["mods/"+chart.ModDir+"/control.lua"] != string(chart.Control) {
		t.Error("the helper mod is not in the work folder")
	}
	var list struct{ Mods []modEntry }
	if err := json.Unmarshal([]byte(got.Files["mods/"+world.ModListFile]), &list); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(list.Mods, modEntry{"planet", true}) || !slices.Contains(list.Mods, modEntry{chart.ModName, true}) {
		t.Errorf("mod list %+v", list.Mods)
	}
}

// SPEC 5.6, 14.3: 2.0 and 1.1 have no real-game picture, and a request with
// no world or a planet the world lacks is refused, before any container or
// folder is made.
func TestChartRefusals(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	old, err := r.worlds.Create(context.Background(), "Old", "2.0.77", true)
	if err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]PreviewRequest{
		"2.0":      {GameVersion: "2.0.77", SpaceAge: true, Seed: 7, World: old, KnownPlanets: []string{"nauvis"}},
		"1.1":      {GameVersion: "1.1.110", Seed: 7, World: old, KnownPlanets: []string{"nauvis"}},
		"no world": {GameVersion: "2.1.24", SpaceAge: true, Seed: 7, Planets: []string{"nauvis"}},
		"a planet the world lacks": func() PreviewRequest {
			q := worldRequest(id)
			q.KnownPlanets = []string{"nauvis"}
			return q
		}(),
	} {
		planet := "nauvis"
		if name == "a planet the world lacks" {
			planet = "cerys"
		}
		if _, err := r.c.Chart(context.Background(), q, planet); !errors.Is(err, ErrPreviewInvalid) {
			t.Errorf("%s: error %v, want ErrPreviewInvalid", name, err)
		}
	}
	if n := len(r.calls("POST /containers/create")); n != 0 || len(r.previewFolders()) != 0 {
		t.Errorf("%d containers made, folders %v", n, r.previewFolders())
	}
}

// SPEC 14.3: a world mod with the helper's name refuses the run.
func TestChartRefusesAWorldModNamedLikeTheHelper(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	r.writeFile(id, filepath.Join(world.ModsDir, chart.ModDir+".zip"), []byte("PK"))
	if _, err := r.c.Chart(context.Background(), worldRequest(id), "nauvis"); !errors.Is(err, ErrControlModTaken) {
		t.Fatalf("error %v, want ErrControlModTaken", err)
	}
	if n := len(r.calls("POST /containers/create")); n != 0 {
		t.Errorf("%d containers made", n)
	}
}

// SPEC 14.3: one time limit covers the whole run, not one for each planet.
func TestChartHasOneLimit(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	defer func(d time.Duration) { chartLimit = d }(chartLimit)
	chartLimit = 300 * time.Millisecond
	r.c.previewLimit = time.Hour
	if err := r.game.SetPreviewMode(fakegame.PreviewHangs); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	_, err := r.c.Chart(context.Background(), worldRequest(id), "cerys")
	if !errors.Is(err, ErrPreviewTimeout) {
		t.Fatalf("error %v, want ErrPreviewTimeout", err)
	}
	if took := time.Since(began); took > 10*time.Second {
		t.Errorf("stopped after %v", took)
	}
	if left := r.previewContainers(); len(left) != 0 {
		t.Errorf("%d containers left", len(left))
	}
}

// SPEC 14.3: a run that runs out of memory says so.
func TestChartOutOfMemory(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := newModdedWorld(t, r)
	if err := r.game.SetPreviewMode(fakegame.PreviewOOM); err != nil {
		t.Fatal(err)
	}
	_, err := r.c.Chart(context.Background(), worldRequest(id), "cerys")
	var pe *PreviewError
	if !errors.As(err, &pe) || !pe.OOM {
		t.Fatalf("error %v, want an out-of-memory PreviewError", err)
	}
}

// chartLine is one chunk line of a chart file, its data a chunk of n bytes.
func chartLine(x, y, n int) string {
	var b bytes.Buffer
	w := zlib.NewWriter(&b)
	_, _ = w.Write(make([]byte, n))
	_ = w.Close()
	return fmt.Sprintf(`{"x":%d,"y":%d,"data":%q}`+"\n", x, y, base64.StdEncoding.EncodeToString(b.Bytes()))
}

// chartOutput is a helper folder of one surface, which change may alter
// before it is written.
type chartOutput struct {
	manifest factorymap.Manifest
	chart    string
}

// fullChart is every chunk of the square, made once since compressing
// 1,024 chunks for each case is slow.
var fullChart = sync.OnceValue(func() string {
	var lines strings.Builder
	for y := -16; y < 16; y++ {
		for x := -16; x < 16; x++ {
			lines.WriteString(chartLine(x, y, 2048))
		}
	}
	return lines.String()
})

func validChartOutput() chartOutput {
	return chartOutput{
		manifest: factorymap.Manifest{Format: 1, GameVersion: "2.1.24", Tick: 221, Surfaces: []factorymap.ManifestSurface{{
			Index: 3, Name: "cerys", Kind: "planet", Chunks: 1024, MinX: -16, MinY: -16, MaxX: 15, MaxY: 15,
			Chart: factorymap.ChartFile(3), Entities: factorymap.EntitiesFile(3),
		}}},
		chart: fullChart(),
	}
}

// writeChartOutput writes o as the work folder "w" of a new base folder.
func writeChartOutput(t *testing.T, o chartOutput) *os.Root {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "w", gameenv.ChartDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	m, err := json.Marshal(o.manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0o600); err != nil {
		t.Fatal(err)
	}
	if len(o.manifest.Surfaces) > 0 && o.chart != "" {
		if err := os.WriteFile(filepath.Join(dir, o.manifest.Surfaces[0].Chart), []byte(o.chart), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

// SPEC 14.6: the chart is untrusted game output. One planet surface named as
// asked, whose bounds are the whole square, at most 1,024 chunks of exactly
// 32 by 32 pixels, the file at most 4 MiB and a regular file; anything else
// refuses the picture.
func TestReadChartLimits(t *testing.T) {
	if _, err := readChart(writeChartOutput(t, validChartOutput()), "w", "cerys"); err != nil {
		t.Fatalf("a valid chart: %v", err)
	}
	surface := func(f func(*factorymap.ManifestSurface)) func(*chartOutput) {
		return func(o *chartOutput) { f(&o.manifest.Surfaces[0]) }
	}
	for name, change := range map[string]func(*chartOutput){
		"no surface": func(o *chartOutput) { o.manifest.Surfaces = nil },
		"two surfaces": func(o *chartOutput) {
			s := o.manifest.Surfaces[0]
			s.Index, s.Name, s.Chart, s.Entities = 4, "nauvis", factorymap.ChartFile(4), factorymap.EntitiesFile(4)
			o.manifest.Surfaces = append(o.manifest.Surfaces, s)
		},
		"another planet":    surface(func(s *factorymap.ManifestSurface) { s.Name = "nauvis" }),
		"not a planet":      surface(func(s *factorymap.ManifestSurface) { s.Kind = "platform" }),
		"a larger square":   surface(func(s *factorymap.ManifestSurface) { s.MinX, s.MaxX = -17, 16 }),
		"a smaller square":  surface(func(s *factorymap.ManifestSurface) { s.MaxY = 14 }),
		"more chunks":       surface(func(s *factorymap.ManifestSurface) { s.Chunks = 1025 }),
		"fewer than listed": surface(func(s *factorymap.ManifestSurface) { s.Chunks = 1000 }),
		"a chunk of the wrong size": func(o *chartOutput) {
			o.chart = chartLine(0, 0, 2047)
			o.manifest.Surfaces[0].Chunks = 1
		},
		"a chunk outside the square": func(o *chartOutput) {
			o.chart = chartLine(16, 0, 2048)
			o.manifest.Surfaces[0].Chunks = 1
		},
		"a file over 4 MiB": func(o *chartOutput) {
			o.chart = chartLine(0, 0, 2048) + strings.Repeat(" ", chart.MaxFileBytes)
			o.manifest.Surfaces[0].Chunks = 1
		},
		"no chart file": func(o *chartOutput) { o.chart = "" },
		"a bad format":  func(o *chartOutput) { o.manifest.Format = 2 },
	} {
		o := validChartOutput()
		change(&o)
		if _, err := readChart(writeChartOutput(t, o), "w", "cerys"); !errors.Is(err, ErrChartBadOutput) {
			t.Errorf("%s: error %v, want ErrChartBadOutput", name, err)
		}
	}
}

// SPEC 14.6: a chart file that is a link, even to a valid chart, is refused.
func TestReadChartRefusesALink(t *testing.T) {
	o := validChartOutput()
	base := writeChartOutput(t, o)
	dir := filepath.Join(base.Name(), "w", gameenv.ChartDir)
	chartPath := filepath.Join(dir, o.manifest.Surfaces[0].Chart)
	if err := os.Rename(chartPath, filepath.Join(dir, "real.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.jsonl", chartPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readChart(base, "w", "cerys"); !errors.Is(err, ErrChartBadOutput) {
		t.Errorf("a linked chart file: error %v", err)
	}
}

// A chart is read with the factory map's readers, so a refused chart can
// match ErrSaveCopyBadOutput too; it must always match ErrChartBadOutput,
// which callers test first (SPEC 14.6).
func TestReadChartErrorMatchesChartBeforeSaveCopy(t *testing.T) {
	base := writeChartOutput(t, validChartOutput())
	if err := os.Remove(filepath.Join(base.Name(), "w", gameenv.ChartDir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	_, err := readChart(base, "w", "cerys")
	if !errors.Is(err, ErrChartBadOutput) {
		t.Fatalf("error %v, want ErrChartBadOutput", err)
	}
	if !errors.Is(err, ErrSaveCopyBadOutput) {
		t.Errorf("error %v no longer matches ErrSaveCopyBadOutput: update Chart's doc comment", err)
	}
}

// SPEC 14.6: a bad chart is ErrChartBadOutput, and the error carries
// nothing the file says.
func TestReadChartErrors(t *testing.T) {
	o := validChartOutput()
	o.manifest.Surfaces[0].Name = "elsewhere"
	_, err := readChart(writeChartOutput(t, o), "w", "cerys")
	if !errors.Is(err, ErrChartBadOutput) || strings.Contains(err.Error(), "elsewhere") {
		t.Errorf("error %v", err)
	}
}
