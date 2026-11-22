package fakegame_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// A real-game picture run writes the factory map's format with one planet
// surface of that name, every chunk of the square charted, which DrawChart
// draws as a disc of land in water.
func TestChartRunWritesOnePlanet(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	ctx := context.Background()
	spec := docker.ContainerSpec{
		Image:  image21,
		Labels: map[string]string{gameenv.LabelPreview: "/data"},
		Env: []string{
			gameenv.PreviewKind + "=" + gameenv.KindChart,
			gameenv.ChartRadius + "=16",
			gameenv.ChartPlanet + "=cerys",
			gameenv.Seed + "=9",
		},
		HostConfig: docker.HostConfig{Binds: []string{r.world + ":" + gameenv.PreviewMount}, NetworkMode: "none"},
	}
	id, err := r.docker.CreateContainer(ctx, "foreman-chart-t", spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if c := r.inspect(id); c.State.Status != "exited" || c.State.ExitCode != 0 {
		t.Fatalf("state %+v, want exited with 0", c.State)
	}
	dir := filepath.Join(r.world, gameenv.ChartDir)
	mb, err := os.ReadFile(filepath.Join(dir, "manifest.json")) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	m, err := factorymap.ParseManifest(bytes.NewReader(mb))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Surfaces) != 1 {
		t.Fatalf("%d surfaces", len(m.Surfaces))
	}
	s := m.Surfaces[0]
	if s.Name != "cerys" || s.Kind != "planet" || s.Chunks != 1024 || s.MinX != -16 || s.MaxY != 15 {
		t.Errorf("surface %+v", s)
	}
	f, err := os.Open(filepath.Join(dir, s.Chart)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, _, err := factorymap.DrawChart(bytes.NewReader(mb), f, s.Index)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 1024 {
		t.Fatalf("picture %v", b)
	}
	centre, corner := img.NRGBAAt(516, 516), img.NRGBAAt(4, 4)
	if centre.G <= centre.B || corner.B <= corner.G {
		t.Errorf("centre %v and corner %v, want land and water", centre, corner)
	}
}
