package fakegame_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// planetsRun runs a definitions container of image in the rig's work folder
// with the given mod list and parses the planet file it leaves.
func planetsRun(t *testing.T, image, modList string) mapgen.Catalogue {
	t.Helper()
	r := newRig(t, fakegame.Config{})
	if err := os.MkdirAll(filepath.Join(r.world, world.ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.world, world.ModsDir, world.ModListFile), []byte(modList), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, err := r.docker.CreateContainer(ctx, "foreman-modsettings-t", docker.ContainerSpec{
		Image:  image,
		Labels: map[string]string{gameenv.LabelPreview: "/data"},
		Env:    []string{gameenv.PreviewKind + "=" + gameenv.KindModSettings, gameenv.Seed + "=9"},
		HostConfig: docker.HostConfig{
			Binds: []string{r.world + ":" + gameenv.PreviewMount}, NetworkMode: "none",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.docker.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}
	if st := r.inspect(id).State; st.ExitCode != 0 {
		t.Fatalf("state %+v", st)
	}
	b, err := os.ReadFile(filepath.Join(r.world, gameenv.PlanetsFile)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	c, err := mapgen.ParseWorldPlanets(image[len("factoriotools/factorio:"):len("factoriotools/factorio:")+3], b)
	if err != nil {
		t.Fatalf("the planet file does not parse: %v\n%s", err, b)
	}
	if c.Dropped != 0 {
		t.Errorf("%d entries dropped", c.Dropped)
	}
	return c
}

const (
	spaceAgeList   = `{"mods":[{"name":"base","enabled":true},{"name":"space-age","enabled":true}]}`
	fakePlanetList = `{"mods":[{"name":"base","enabled":true},{"name":"space-age","enabled":true},{"name":"fake-planet","enabled":true}]}`
)

// A world without the fake mod gets the built-in planets and controls, in
// the built-in order, so pages look as they do without a planet file.
func TestPlanetFileIsTheBuiltinCatalogue(t *testing.T) {
	for _, tc := range []struct {
		image, line, list string
		spaceAge          bool
	}{
		{image21, "2.1", spaceAgeList, true},
		{image21, "2.1", `{"mods":[{"name":"base","enabled":true}]}`, false},
		{image11, "1.1", `{"mods":[{"name":"base","enabled":true}]}`, false},
	} {
		got := planetsRun(t, tc.image, tc.list)
		want := mapgen.Builtin(tc.line, tc.spaceAge)
		if got.NoPlanets != want.NoPlanets || !reflect.DeepEqual(got.PlanetNames(), want.PlanetNames()) {
			t.Errorf("%s: planets %v, want %v", tc.line, got.PlanetNames(), want.PlanetNames())
		}
		for _, p := range want.PlanetNames() {
			if g, w := got.PlanetControls(p), want.PlanetControls(p); !reflect.DeepEqual(g, w) {
				t.Errorf("%s %s: controls %v, want %v", tc.line, p, g, w)
			}
		}
	}
}

// With the fake mod enabled the world has one more planet with one more
// resource control, so e2e tests can see a modded planet.
func TestPlanetFileWithTheFakePlanetMod(t *testing.T) {
	got := planetsRun(t, image21, fakePlanetList)
	names := got.PlanetNames()
	if names[len(names)-1] != fakegame.FakePlanet || len(names) != len(mapgen.Builtin("2.1", true).Planets)+1 {
		t.Errorf("planets %v, want the built-in ones and %s", names, fakegame.FakePlanet)
	}
	ctl, ok := got.Controls[fakegame.FakeOre]
	if !ok || ctl.Category != "resource" || !ctl.Richness || !slices.Equal(ctl.Planets, []string{fakegame.FakePlanet}) {
		t.Errorf("control %+v", ctl)
	}
	if len(got.Resources) != 1 || got.Resources[0].Name != fakegame.FakeOre {
		t.Errorf("resources %+v", got.Resources)
	}

	// 1.1 has no planets: the control is on the one surface.
	old := planetsRun(t, image11, `{"mods":[{"name":"base","enabled":true},{"name":"fake-planet","enabled":true}]}`)
	if !old.NoPlanets || !slices.Contains(old.Planets[0].Controls, fakegame.FakeOre) {
		t.Errorf("1.1 catalogue %+v", old)
	}
}
