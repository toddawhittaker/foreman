package fakegame_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
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

// In the no-patches preview mode a resource run finds nothing, as on a
// planet whose resources are tiles, and the result still parses (SPEC 5.6).
func TestResourceRunNoPatchesMode(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	if err := r.game.SetPreviewMode(fakegame.PreviewNoPatches); err != nil {
		t.Fatal(err)
	}
	res, err := resourceRun(t, r, 3, "nauvis")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Patches) != 0 {
		t.Errorf("%d patches, want none", len(res.Patches))
	}
}

// In that mode a modded planet the canned count does not know finds nothing
// too, so its note can be shown (SPEC 5.6).
func TestResourceRunNoPatchesModdedPlanet(t *testing.T) {
	r := newRig(t, fakegame.Config{})
	if err := r.game.SetPreviewMode(fakegame.PreviewNoPatches); err != nil {
		t.Fatal(err)
	}
	res, err := resourceRun(t, r, 3, "fake-planet")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Patches) != 0 {
		t.Errorf("%d patches, want none", len(res.Patches))
	}
}

// The fake planet's count finds spots of a fluid tied to its resource
// control and no ore, and the base planets find no spots (SPEC 5.6).
func TestResourceRunFakePlanetSpots(t *testing.T) {
	res, err := resourceRun(t, newRig(t, fakegame.Config{}), 3, fakegame.FakePlanet)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := res.Fluids[fakegame.FakeGas]
	if len(res.Fluids) != 1 || !ok || len(f.Controls) != 1 || f.Controls[0] != fakegame.FakeOre {
		t.Fatalf("fluids %+v", res.Fluids)
	}
	var tiles []int
	for _, p := range res.Patches {
		if !p.Fluid || p.Name != fakegame.FakeGas || p.Amount != 0 {
			t.Errorf("patch %+v is not a spot of the fake gas", p)
		}
		tiles = append(tiles, p.Entities)
	}
	// A large spot, a spot beside it whose label is left off, and a spot of one tile.
	if !slices.Equal(tiles, []int{312, 48, 20, 1}) {
		t.Errorf("spot tiles %v", tiles)
	}
	if got := resource.HiddenLabels(res.Patches); !slices.Equal(got, []bool{false, false, true, false}) {
		t.Errorf("hidden labels %v", got)
	}
	base, err := resourceRun(t, newRig(t, fakegame.Config{}), 3, "nauvis")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range base.Patches {
		if p.Fluid {
			t.Errorf("nauvis has a spot: %+v", p)
		}
	}
	if len(base.Fluids) != 0 {
		t.Errorf("nauvis has fluids %+v", base.Fluids)
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
