package fakegame_test

import (
	"archive/zip"
	"bytes"
	"context"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// planetsRun runs a definitions container of image in the rig's work folder
// with the given mod list and parses the planet file it leaves.
func planetsRun(t *testing.T, image, modList string, settings ...[]byte) mapgen.Catalogue {
	t.Helper()
	dir := defsRun(t, image, modList, settings...)
	b, err := os.ReadFile(filepath.Join(dir, gameenv.PlanetsFile)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	f, err := mapgen.DecodeWorldPlanets(image[len("factoriotools/factorio:"):len("factoriotools/factorio:")+3], b)
	if err != nil {
		t.Fatalf("the planet file does not parse: %v\n%s", err, b)
	}
	c := f.Labelled()
	if c.Dropped != 0 {
		t.Errorf("%d entries dropped", c.Dropped)
	}
	return c
}

// defsRun runs a definitions container of image in a rig's work folder with
// the given mod list, and the fake planet mod's zip in its mods folder, and
// returns the folder.
func defsRun(t *testing.T, image, modList string, settings ...[]byte) string {
	t.Helper()
	r := newRig(t, fakegame.Config{})
	if err := os.MkdirAll(filepath.Join(r.world, world.ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, b := range settings {
		if err := os.WriteFile(filepath.Join(r.world, world.ModsDir, world.ModSettingsFile), b, 0o600); err != nil { //nolint:gosec // a test's own temp folder
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(r.world, world.ModsDir, world.ModListFile), []byte(modList), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.world, world.ModsDir, fakePlanetZip), []byte("PK"), 0o600); err != nil {
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
	return r.world
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

// A startup setting above its default adds FakeMoon, so e2e tests can see
// the planets follow the world's mod settings (SPEC 5.6, 9.6).
func TestPlanetFileFollowsAStartupSetting(t *testing.T) {
	b, err := os.ReadFile("modsettings_defaults.dat")
	if err != nil {
		t.Fatal(err)
	}
	f, err := modsettings.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := planetsRun(t, image21, spaceAgeList, b).PlanetNames(); slices.Contains(got, fakegame.FakeMoon) {
		t.Errorf("the defaults added %s: %v", fakegame.FakeMoon, got)
	}
	if err := f.Set(modsettings.Startup, fakegame.FakeMoonSetting, modsettings.Value{Type: modsettings.TypeInt, Int: 8}, nil); err != nil {
		t.Fatal(err)
	}
	set, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if got := planetsRun(t, image21, spaceAgeList, set).PlanetNames(); got[len(got)-1] != fakegame.FakeMoon {
		t.Errorf("planets %v, want %s last", got, fakegame.FakeMoon)
	}
}

// fakePlanetZip is the fake planet mod's zip, as e2e tests install it.
const fakePlanetZip = "fake-planet_1.0.0.zip"

// moonSettings is the test mod's defaults with the moon setting raised.
func moonSettings(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("modsettings_defaults.dat")
	if err != nil {
		t.Fatal(err)
	}
	f, err := modsettings.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Set(modsettings.Startup, fakegame.FakeMoonSetting, modsettings.Value{Type: modsettings.TypeInt, Int: 8}, nil); err != nil {
		t.Fatal(err)
	}
	if b, err = f.Encode(); err != nil {
		t.Fatal(err)
	}
	return b
}

// exportRun is defsRun that decodes the icon and star map files.
func exportRun(t *testing.T, image, modList string, settings ...[]byte) (modsettings.IconIndex, mapgen.StarMap) {
	t.Helper()
	dir := defsRun(t, image, modList, settings...)
	b, err := os.ReadFile(filepath.Join(dir, gameenv.IconsFile)) //nolint:gosec // a test's own temp folder
	if err != nil {
		t.Fatal(err)
	}
	icons, err := modsettings.DecodeIconIndex(b)
	if err != nil || icons.Dropped != 0 {
		t.Fatalf("the icon file: %v, %d dropped\n%s", err, icons.Dropped, b)
	}
	if b, err = os.ReadFile(filepath.Join(dir, gameenv.StarMapFile)); err != nil { //nolint:gosec // a test's own temp folder
		t.Fatal(err)
	}
	m, err := mapgen.DecodeStarMap(b)
	if err != nil {
		t.Fatalf("the star map file: %v\n%s", err, b)
	}
	return icons, m
}

// With Space Age the star map is the game's own: Vulcanus at distance 10 and
// orientation 0.1, Nauvis to Vulcanus 15,000 km, the hidden placeholder left
// out. The fake planet mod adds FakePlanet with its starmap icon, a route to
// it, and FakeMoon orbiting it when the moon setting is on; its icons are
// in the index at the zip's version (SPEC 5.6, 14.6).
func TestStarMapAndIconsWithTheFakePlanetMod(t *testing.T) {
	icons, m := exportRun(t, image21, fakePlanetList, moonSettings(t))
	var names []string
	for _, l := range m.Locations {
		names = append(names, l.Name)
	}
	want := []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo", "solar-system-edge", "shattered-planet",
		fakegame.FakePlanet, fakegame.FakeMoon}
	if !reflect.DeepEqual(names, want) || m.Dropped != 1 {
		t.Errorf("locations %v (%d dropped), want %v and the hidden one dropped", names, m.Dropped, want)
	}
	if v, _ := m.Location("vulcanus"); v.Distance != 10 || v.Orientation != 0.1 || v.Magnitude != 1.5 {
		t.Errorf("vulcanus %+v", v)
	}
	if a, _ := m.Location("aquilo"); a.Distance != 35 || a.Orientation != 0.225 {
		t.Errorf("aquilo %+v", a)
	}
	if moon, _ := m.Location(fakegame.FakeMoon); moon.Parent != fakegame.FakePlanet {
		t.Errorf("moon %+v", moon)
	}
	if p, _ := m.Location(fakegame.FakePlanet); !p.HasIcon {
		t.Errorf("fake planet %+v has no icon", p)
	}
	if len(m.Routes) != 10 || m.Routes[0] != (mapgen.Route{From: "nauvis", To: "vulcanus", Length: 15000}) {
		t.Errorf("routes %+v", m.Routes)
	}
	for kind, path := range map[string]string{"item": fakegame.FakeWidgetIcon, "starmap": fakegame.FakePlanetStarmap} {
		name := fakegame.FakeWidget
		if kind == "starmap" {
			name = fakegame.FakePlanet
		}
		if ref, ok := icons.Lookup(kind, name); !ok || ref.Mod != fakegame.FakePlanetMod || ref.Version != "1.0.0" || ref.Path != path || ref.Size != 64 {
			t.Errorf("%s %s: %+v %t", kind, name, ref, ok)
		}
	}
}

// Without the fake mod, the icon index is empty and the star map is the
// game's: Space Age's, or Nauvis alone without it. 1.1 writes neither file.
func TestStarMapAndIconsWithoutTheFakeMod(t *testing.T) {
	icons, m := exportRun(t, image21, spaceAgeList)
	if icons.Len() != 0 || len(m.Locations) != 7 || len(m.Routes) != 9 {
		t.Errorf("%d icons, %d locations, %d routes; want 0, 7 and 9", icons.Len(), len(m.Locations), len(m.Routes))
	}
	icons, m = exportRun(t, image21, `{"mods":[{"name":"base","enabled":true}]}`)
	if icons.Len() != 0 || len(m.Locations) != 1 || m.Locations[0].Name != "nauvis" || len(m.Routes) != 0 {
		t.Errorf("base only: %d icons, star map %+v", icons.Len(), m)
	}
	dir := defsRun(t, image11, `{"mods":[{"name":"base","enabled":true},{"name":"fake-planet","enabled":true}]}`)
	for _, f := range []string{gameenv.IconsFile, gameenv.StarMapFile} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			t.Errorf("1.1 wrote %s", f)
		}
	}
}

// The icon paths the fake game reports are in the e2e fake planet zip: a 64
// px item icon with its mipmaps beside it, a starmap icon, and a setting
// description that names the item.
func TestFakePlanetZipHoldsTheIcons(t *testing.T) {
	z, err := zip.OpenReader(filepath.Join("..", "..", "e2e", "testdata", fakePlanetZip))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Close() }()
	read := func(name string) []byte {
		f, err := z.Open("fake-planet_1.0.0/" + name)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()
		b, err := io.ReadAll(f)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for name, want := range map[string][2]int{fakegame.FakeWidgetIcon: {120, 64}, fakegame.FakePlanetStarmap: {64, 64}} {
		cfg, err := png.DecodeConfig(bytes.NewReader(read(name)))
		if err != nil || cfg.Width != want[0] || cfg.Height != want[1] {
			t.Errorf("%s: %dx%d %v, want %dx%d", name, cfg.Width, cfg.Height, err, want[0], want[1])
		}
	}
	if cfg := string(read("locale/en/fake-planet.cfg")); !strings.Contains(cfg, "[item="+fakegame.FakeWidget+"]") {
		t.Errorf("no setting description names the widget:\n%s", cfg)
	}
}
