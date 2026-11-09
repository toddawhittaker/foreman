package fakegame_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/resource"
)

func resourceRun(t *testing.T, r *rig, radius int, planet string) (resource.Result, error) {
	t.Helper()
	ctx := context.Background()
	spec := docker.ContainerSpec{
		Image:  image21,
		Labels: map[string]string{gameenv.LabelPreview: "/data"},
		Env: []string{
			gameenv.PreviewKind + "=" + gameenv.KindResources,
			gameenv.ResourceRadius + "=" + strconv.Itoa(radius),
			gameenv.ResourcePlanet + "=" + planet,
			gameenv.Seed + "=9",
		},
		HostConfig: docker.HostConfig{Binds: []string{r.world + ":" + gameenv.PreviewMount}, NetworkMode: "none"},
	}
	id, err := r.docker.CreateContainer(ctx, "foreman-resources-t", spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if c := r.inspect(id); c.State.Status != "exited" || c.State.ExitCode != 0 {
		t.Fatalf("state %+v, want exited with 0", c.State)
	}
	if matches, _ := filepath.Glob(filepath.Join(r.world, "*.png")); len(matches) != 0 {
		t.Errorf("a resource run drew pictures: %v", matches)
	}
	b, err := os.ReadFile(filepath.Join(r.world, gameenv.ResourceFile)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	return resource.Parse(b, radius*64, planet)
}

// A resource run writes JSON that resource.Parse takes at the smallest and
// at the largest picture, for every planet, with ores and an infinite
// resource and with labels close enough together to overlap.
func TestResourceRunWritesPatches(t *testing.T) {
	for _, planet := range []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo"} {
		for _, radius := range []int{3, 20, 48} {
			t.Run(planet+"/"+strconv.Itoa(radius), func(t *testing.T) {
				res, err := resourceRun(t, newRig(t, fakegame.Config{}), radius, planet)
				if err != nil {
					t.Fatal(err)
				}
				var ores, infinite int
				for _, r := range res.Resources {
					if r.Infinite {
						infinite++
					} else {
						ores++
					}
				}
				if ores < 2 || infinite < 1 {
					t.Errorf("%d ores and %d infinite resources, want at least 2 and 1", ores, infinite)
				}
				if len(res.Patches) < 5 {
					t.Fatalf("%d patches", len(res.Patches))
				}
				// A large patch, a small one, and a close pair of labels.
				first, last := res.Patches[0], res.Patches[len(res.Patches)-1]
				if area(first) < 10*area(last) {
					t.Errorf("largest box %d tiles, smallest %d: want a large and a small", area(first), area(last))
				}
				if !closePair(res.Patches) {
					t.Error("no two labels of the same resource lie within 8 tiles of each other")
				}
				var yields int
				for _, p := range res.Patches {
					if _, ok := resource.Yield(p); ok {
						yields++
					}
				}
				if yields == 0 {
					t.Error("no patch has a yield")
				}
			})
		}
	}
}

func area(p resource.Patch) int {
	return (p.Box[2] - p.Box[0] + 1) * (p.Box[3] - p.Box[1] + 1)
}

func closePair(ps []resource.Patch) bool {
	for i, a := range ps {
		for _, b := range ps[i+1:] {
			if a.Name == b.Name && abs(a.X-b.X) <= 8 && abs(a.Y-b.Y) <= 8 {
				return true
			}
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
