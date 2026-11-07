package fakegame_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/territory"
)

// A territory run writes JSON that territory.Parse takes, with the canned
// small, medium and big territories inside the picture, and no pictures.
func TestTerritoryRunWritesTerritories(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	ctx := context.Background()
	spec := docker.ContainerSpec{
		Image:  image21,
		Labels: map[string]string{gameenv.LabelPreview: "/data"},
		Env: []string{
			gameenv.PreviewKind + "=" + gameenv.KindTerritories,
			gameenv.TerritoryRadius + "=10", gameenv.Seed + "=9",
		},
		HostConfig: docker.HostConfig{Binds: []string{r.world + ":" + gameenv.PreviewMount}, NetworkMode: "none"},
	}
	id, err := r.docker.CreateContainer(ctx, "foreman-preview-t", spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if c := r.inspect(id); c.State.Status != "exited" || c.State.ExitCode != 0 {
		t.Fatalf("state %+v, want exited with 0", c.State)
	}
	b, err := os.ReadFile(filepath.Join(r.world, gameenv.TerritoryFile)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	res, err := territory.Parse(b, 640)
	if err != nil {
		t.Fatal(err)
	}
	ov := territory.Geometry(res, 640)
	want := []territory.Count{{Size: territory.Small, N: 1}, {Size: territory.Medium, N: 1}, {Size: territory.Big, N: 1}}
	if len(ov.Counts) != len(want) {
		t.Fatalf("counts %v, want %v", ov.Counts, want)
	}
	for i := range want {
		if ov.Counts[i] != want[i] {
			t.Errorf("counts %v, want %v", ov.Counts, want)
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(r.world, "*.png")); len(matches) != 0 {
		t.Errorf("a territory run drew pictures: %v", matches)
	}
}
