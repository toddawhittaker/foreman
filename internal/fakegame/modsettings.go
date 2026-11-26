package fakegame

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// modSettingsDefaults is the mod-settings.dat the real game wrote for the
// test mod fmtest (factoriotools/factorio 2.0.77), so a run's defaults are
// bytes the game itself made.
//
//go:embed modsettings_defaults.dat
var modSettingsDefaults []byte

// modSettingDef is one definition in the shape the game's script writes. A
// field is absent when the game has no value for it.
type modSettingDef struct {
	Name          string `json:"name"`
	Mod           string `json:"mod"`
	SettingType   string `json:"setting_type"`
	Type          string `json:"type"`
	Order         string `json:"order,omitempty"`
	DefaultValue  any    `json:"default_value,omitempty"`
	MinimumValue  any    `json:"minimum_value,omitempty"`
	MaximumValue  any    `json:"maximum_value,omitempty"`
	AllowedValues []any  `json:"allowed_values,omitempty"`
	Hidden        bool   `json:"hidden"`
	AllowBlank    bool   `json:"allow_blank"`
	AutoTrim      bool   `json:"auto_trim"`
	// LocalisedName and LocalisedDescription are localised strings, as
	// the game writes them.
	LocalisedName        any `json:"localised_name,omitempty"`
	LocalisedDescription any `json:"localised_description,omitempty"`
}

// cannedModSettingDefs is what the test mod fmtest declares: for each of the
// three setting types a bool, an int, a double, a string with allowed values
// and a color (the global bool named by its own localised strings), a
// hidden startup bool and a startup int with one allowed value. One more setting is of a type Foreman does not know, for the
// page's "cannot be edited here" case; the game never writes it to
// mod-settings.dat. Every other default equals the value in the embedded
// defaults file.
func cannedModSettingDefs() []modSettingDef {
	var defs []modSettingDef
	for _, st := range []struct{ typ, prefix string }{
		{"startup", "fm-sta-"}, {"runtime-global", "fm-glo-"}, {"runtime-per-user", "fm-usr-"},
	} {
		def := func(name, typ, order string) modSettingDef {
			return modSettingDef{Name: st.prefix + name, Mod: "fmtest", SettingType: st.typ, Type: typ, Order: order}
		}
		b := def("bool", "bool-setting", "a")
		b.DefaultValue = true
		if st.typ == "runtime-global" {
			// Named the Squeak Through way: its text is in the mod's own
			// sections, with a parameter.
			b.LocalisedName = []any{"fm-setting-name.disable-setting", []any{"fm-setting-name.chests"}}
			b.LocalisedDescription = []any{"fm-setting-description.chests"}
		}
		i := def("int", "int-setting", "b")
		i.DefaultValue, i.MinimumValue, i.MaximumValue = 5, 1, 10
		if st.typ == "startup" {
			// The test mod's settings-updates stage raises this default, and
			// the embedded defaults file holds the raised one.
			i.DefaultValue = 7
		}
		d := def("double", "double-setting", "c")
		d.DefaultValue, d.MinimumValue, d.MaximumValue = 1.5, 0.5, 9.5
		s := def("string", "string-setting", "d")
		s.DefaultValue, s.AllowedValues = "red", []any{"red", "green", "blue"}
		c := def("color", "color-setting", "e")
		c.DefaultValue = map[string]float64{"r": 1, "g": 0.5, "b": 0, "a": 1}
		defs = append(defs, b, i, d, s, c)
	}
	hidden := modSettingDef{Name: "fm-sta-hidden", Mod: "fmtest", SettingType: "startup", Type: "bool-setting", Hidden: true}
	hidden.DefaultValue = false
	forced := modSettingDef{Name: "fm-sta-forced", Mod: "fmtest", SettingType: "startup", Type: "int-setting"}
	forced.DefaultValue, forced.AllowedValues = 3, []any{3}
	weird := modSettingDef{Name: "fm-sta-weird", Mod: "fmtest", SettingType: "startup", Type: "weird-setting", Order: "g"}
	weird.DefaultValue = "x"
	return append(defs, hidden, forced, weird)
}

// FakePlanetMod is the e2e test mod whose world gets one more planet,
// FakePlanet, with one more resource control, FakeOre, and Nauvis's stone
// shared, from the fake game's planet run. On 1.1 the control is on the one surface.
const (
	FakePlanetMod = "fake-planet"
	FakePlanet    = "fake-planet"
	FakeOre       = "fake-ore"
	// FakeOreRichness is FakePlanet's own default richness for FakeOre.
	FakeOreRichness = 150
	// FakeMoon is the planet a run adds when the world's mod-settings.dat
	// sets the startup setting FakeMoonSetting above its default of 7, as a
	// planet mod's startup setting can (SPEC 5.6, 9.6).
	FakeMoon        = "fake-moon"
	FakeMoonSetting = "fm-sta-int"
	// FakeBare is a planet with no map controls at all, as Igrys places its
	// resources from its map generator alone; a run adds it, with FakeMoon,
	// when FakeMoonSetting is FakeBareAt, its largest value (SPEC 5.6).
	FakeBare   = "fake-bare"
	FakeBareAt = 10
)

// The planet file's shape, as internal/mapgen/catalogue.go documents it.
type (
	fakePlanetFile struct {
		Planets   []fakePlanetEntry  `json:"planets"`
		Controls  []fakeControlEntry `json:"autoplace_controls"`
		Resources []fakeResource     `json:"resources"`
	}
	fakePlanetEntry struct {
		Name      string                        `json:"name"`
		LocName   []string                      `json:"localised_name"`
		Controls  map[string]map[string]float64 `json:"autoplace_controls"`
		Resources []string                      `json:"resources"`
	}
	fakeControlEntry struct {
		Name          string   `json:"name"`
		Category      string   `json:"category"`
		Richness      bool     `json:"richness"`
		CanBeDisabled bool     `json:"can_be_disabled"`
		Order         string   `json:"order"`
		LocName       []string `json:"localised_name"`
	}
	fakeResource struct {
		Name    string   `json:"name"`
		Control string   `json:"control"`
		LocName []string `json:"localised_name"`
	}
)

// cannedPlanets is the planet file for a game line and the enabled mods: the
// built-in catalogue, so a world without the fake mod looks as before, plus
// FakePlanet and FakeOre when FakePlanetMod is enabled. The built-in data has
// no order strings, so each control's order is where it first appears,
// planet by planet; a test checks every planet keeps its built-in order.
func cannedPlanets(line string, mods []string) fakePlanetFile {
	cat := mapgen.Builtin(line, slices.Contains(mods, world.ModSpaceAge))
	f := fakePlanetFile{Planets: []fakePlanetEntry{}, Controls: []fakeControlEntry{}, Resources: []fakeResource{}}
	defaults := func() map[string]float64 { return map[string]float64{"frequency": 1, "size": 1, "richness": 1} }
	seen := map[string]bool{}
	for _, p := range cat.Planets {
		entry := fakePlanetEntry{Name: p.Name, LocName: []string{"space-location-name." + p.Name}, Controls: map[string]map[string]float64{}, Resources: []string{}}
		for _, n := range p.Controls {
			entry.Controls[n] = defaults()
			if seen[n] {
				continue
			}
			seen[n] = true
			c := cat.Controls[n].Control
			f.Controls = append(f.Controls, fakeControlEntry{
				Name: c.Name, Category: c.Category, Richness: c.Richness, CanBeDisabled: c.CanBeDisabled,
				Order: fmt.Sprintf("a%03d", len(f.Controls)), LocName: []string{"autoplace-control-names." + n},
			})
		}
		f.Planets = append(f.Planets, entry)
	}
	if slices.Contains(mods, FakePlanetMod) {
		f.Controls = append(f.Controls, fakeControlEntry{
			Name: FakeOre, Category: "resource", Richness: true, CanBeDisabled: true,
			Order: "z", LocName: []string{"autoplace-control-names." + FakeOre},
		})
		f.Resources = append(f.Resources, fakeResource{Name: FakeOre, Control: FakeOre, LocName: []string{"entity-name." + FakeOre}})
		f.Planets = append(f.Planets, fakePlanetEntry{
			Name: FakePlanet, LocName: []string{"space-location-name." + FakePlanet},
			// Its own richness default, as Moshine's steam_geyser has, and
			// Nauvis's stone, shared (SPEC 5.6).
			Controls:  map[string]map[string]float64{FakeOre: {"frequency": 1, "size": 1, "richness": FakeOreRichness}, "stone": defaults()},
			Resources: []string{FakeOre},
		})
	}
	if cat.NoPlanets {
		// 1.1 has no planets: every control is on the one surface.
		f.Planets = []fakePlanetEntry{}
	}
	return f
}

// writeModSettings is a mod settings definitions run: it leaves
// gameenv.ModSettingsDefsFile and gameenv.PlanetsFile in the work folder, on
// 2.x gameenv.IconsFile and gameenv.StarMapFile too, and
// the game's defaults in the work folder's mods folder as
// world.ModSettingsFile, where the game writes it.
func writeModSettings(c dockertest.Container) error {
	dir := mountSource(c, gameenv.PreviewMount)
	if dir == "" {
		return fmt.Errorf("the fake game's mod settings container has no folder bound at %s", gameenv.PreviewMount)
	}
	enabled := enabledMods(dir)
	defs := cannedModSettingDefs()
	if slices.Contains(enabled, FakePlanetMod) {
		// A second mod with settings, so a page can show groups of two mods.
		planet := modSettingDef{Name: "fp-sta-bool", Mod: FakePlanetMod, SettingType: "startup", Type: "bool-setting", Order: "a"}
		planet.DefaultValue = true
		defs = append(defs, planet)
	}
	// The definitions are plain strings and numbers: marshalling cannot fail.
	b, _ := json.Marshal(struct {
		Settings []modSettingDef `json:"settings"`
	}{defs})
	mods := filepath.Join(dir, world.ModsDir)
	if err := os.MkdirAll(mods, world.DirMode); err != nil {
		return err
	}
	moonSet := startupMoonSetting(filepath.Join(mods, world.ModSettingsFile))
	moon := moonSet > 7
	if err := os.WriteFile(filepath.Join(mods, world.ModSettingsFile), modSettingsDefaults, world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, gameenv.ModSettingsDefsFile), b, world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		return err
	}
	v := imageVersion(c.Spec.Image)
	// Plain strings, numbers and maps: marshalling cannot fail.
	f := cannedPlanets(fmt.Sprintf("%d.%d", v[0], v[1]), enabled)
	if moon && len(f.Planets) > 0 {
		f.Planets = append(f.Planets, fakePlanetEntry{Name: FakeMoon, LocName: []string{"space-location-name." + FakeMoon},
			Controls: map[string]map[string]float64{"stone": {"frequency": 1, "size": 1, "richness": 1}}, Resources: []string{}})
	}
	if moonSet >= FakeBareAt && len(f.Planets) > 0 {
		f.Planets = append(f.Planets, fakePlanetEntry{Name: FakeBare, LocName: []string{"space-location-name." + FakeBare},
			Controls: map[string]map[string]float64{}, Resources: []string{}})
	}
	planets, _ := json.Marshal(f)
	if err := os.WriteFile(filepath.Join(dir, gameenv.PlanetsFile), planets, world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		return err
	}
	if v[0] < 2 {
		// 1.1 has no space locations and no mod-data, so the real run
		// writes neither file.
		return nil
	}
	// Plain strings, numbers and maps: marshalling cannot fail.
	icons, _ := json.Marshal(cannedIcons(v, enabled, modVersions(mods)))
	if err := os.WriteFile(filepath.Join(dir, gameenv.IconsFile), icons, world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		return err
	}
	starmap, _ := json.Marshal(cannedStarMap(enabled, moon))
	return os.WriteFile(filepath.Join(dir, gameenv.StarMapFile), starmap, world.FileMode) //nolint:gosec // the fake's own test folder
}

// The fake planet mod's icons, as e2e/testdata/fake-planet_1.0.0.zip holds
// them: FakeWidget's item icon, 64 px with mipmaps beside it, and
// FakePlanet's starmap icon.
const (
	FakeWidget         = "fake-widget"
	FakeWidgetIcon     = "graphics/icons/fake-widget.png"
	FakePlanetStarmap  = "graphics/starmap-fake-planet.png"
	fakeIconSize       = 64
	fakeHiddenLocation = "space-location-unknown"
)

// The icon and star map files' shapes, as internal/modsettings/iconindex.go
// and internal/mapgen/starmap.go document them.
type (
	fakeIconFile struct {
		Mods  map[string]string `json:"mods"`
		Icons []fakeIcon        `json:"icons"`
	}
	fakeIcon struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
		Path string `json:"path"`
		Size int    `json:"size"`
	}
	fakeStarMap struct {
		Locations []fakeLocation `json:"locations"`
		Routes    []fakeRoute    `json:"routes"`
	}
	fakeLocation struct {
		Name             string   `json:"name"`
		LocName          []string `json:"localised_name"`
		Distance         float64  `json:"distance"`
		Orientation      float64  `json:"orientation"`
		Magnitude        float64  `json:"magnitude"`
		LabelOrientation float64  `json:"label_orientation"`
		Parent           string   `json:"parent,omitempty"`
		Position         *fakePos `json:"position,omitempty"`
		Hidden           bool     `json:"hidden"`
		StarmapIcon      bool     `json:"starmap_icon"`
	}
	fakePos struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	fakeRoute struct {
		From   string  `json:"from"`
		To     string  `json:"to"`
		Length float64 `json:"length"`
	}
)

// modVersions is the version of each mod zip in the mods folder, by name.
func modVersions(mods string) map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(mods)
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".zip")
		if i := strings.LastIndexByte(base, '_'); ok && i > 0 {
			out[base[:i]] = base[i+1:]
		}
	}
	return out
}

// cannedIcons is the icon file for the enabled mods: the active mods with
// their versions (built-in ones at the game's), and, when FakePlanetMod is
// on, its item and starmap icons. Base-game icons are never in it, as the
// real export drops them.
func cannedIcons(v [3]uint16, enabled []string, versions map[string]string) fakeIconFile {
	f := fakeIconFile{Mods: map[string]string{}, Icons: []fakeIcon{}}
	game := fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
	for _, m := range enabled {
		f.Mods[m] = cmp.Or(versions[m], game)
	}
	if !slices.Contains(enabled, FakePlanetMod) {
		return f
	}
	f.Icons = append(f.Icons,
		fakeIcon{Kind: "item", Name: FakeWidget, Path: "__" + FakePlanetMod + "__/" + FakeWidgetIcon, Size: fakeIconSize},
		fakeIcon{Kind: "starmap", Name: FakePlanet, Path: "__" + FakePlanetMod + "__/" + FakePlanetStarmap, Size: fakeIconSize},
	)
	return f
}

// spaceAgeLocations are Space Age's space locations and routes as the game
// defines them (2.1.24's data), in the game's order.
var (
	spaceAgeLocations = []fakeLocation{
		{Name: "nauvis", Distance: 15, Orientation: 0.275, Magnitude: 1, LabelOrientation: 0.25},
		{Name: "vulcanus", Distance: 10, Orientation: 0.1, Magnitude: 1.5, LabelOrientation: 0.25},
		{Name: "gleba", Distance: 20, Orientation: 0.175, Magnitude: 1.1, LabelOrientation: 0.15},
		{Name: "fulgora", Distance: 25, Orientation: 0.325, Magnitude: 0.9, LabelOrientation: 0.35},
		{Name: "aquilo", Distance: 35, Orientation: 0.225, Magnitude: 1, LabelOrientation: 0.15},
		{Name: "solar-system-edge", Distance: 50, Orientation: 0.25, Magnitude: 1, LabelOrientation: 0.15},
		{Name: "shattered-planet", Distance: 80, Orientation: 0.25, Magnitude: 1, LabelOrientation: 0.25},
	}
	spaceAgeRoutes = []fakeRoute{
		{"nauvis", "vulcanus", 15000}, {"nauvis", "gleba", 15000}, {"nauvis", "fulgora", 15000},
		{"vulcanus", "gleba", 15000}, {"gleba", "fulgora", 15000}, {"gleba", "aquilo", 30000},
		{"fulgora", "aquilo", 30000}, {"aquilo", "solar-system-edge", 100000},
		{"solar-system-edge", "shattered-planet", 4000000},
	}
)

// FakePlanet's place on the star map.
const fakePlanetDistance, fakePlanetOrientation = 30, 0.4

// cannedStarMap is the star map file for the enabled mods on 2.x: the
// game's hidden placeholder location, which every 2.x game has; Nauvis, or
// Space Age's locations and routes when it is on; and FakePlanet with a
// route from Nauvis when FakePlanetMod is on; and FakeMoon, orbiting
// FakePlanet, when the moon setting is on, with a route from FakePlanet
// when FakePlanetMod is on too. As 2.1 gives them, the moon's distance and
// orientation are about its planet, and every location has the game's own
// position on the map.
func cannedStarMap(enabled []string, moon bool) fakeStarMap {
	locName := func(n string) []string { return []string{"space-location-name." + n} }
	m := fakeStarMap{Routes: []fakeRoute{}}
	m.Locations = append(m.Locations, fakeLocation{Name: fakeHiddenLocation, LocName: locName(fakeHiddenLocation),
		Magnitude: 1, LabelOrientation: 0.25, Hidden: true})
	if slices.Contains(enabled, world.ModSpaceAge) {
		m.Locations = append(m.Locations, spaceAgeLocations...)
		m.Routes = append(m.Routes, spaceAgeRoutes...)
	} else {
		m.Locations = append(m.Locations, spaceAgeLocations[0])
	}
	if slices.Contains(enabled, FakePlanetMod) {
		m.Locations = append(m.Locations, fakeLocation{Name: FakePlanet, Distance: fakePlanetDistance, Orientation: fakePlanetOrientation,
			Magnitude: 0.8, LabelOrientation: 0.25, StarmapIcon: true})
		m.Routes = append(m.Routes, fakeRoute{"nauvis", FakePlanet, 20000})
	}
	if moon {
		// As the planet file adds it, with or without FakePlanetMod; a
		// parent the map does not list is cleared when it is read.
		m.Locations = append(m.Locations, fakeLocation{Name: FakeMoon, Distance: 1.5, Orientation: 0.75,
			Magnitude: 0.4, LabelOrientation: 0.25, Parent: FakePlanet})
		if slices.Contains(enabled, FakePlanetMod) {
			m.Routes = append(m.Routes, fakeRoute{FakePlanet, FakeMoon, 500})
		}
	}
	for i, l := range m.Locations {
		m.Locations[i].LocName = locName(l.Name)
		a := 2 * math.Pi * l.Orientation
		pos := fakePos{l.Distance * math.Sin(a), -l.Distance * math.Cos(a)}
		if l.Name == FakeMoon {
			// The moon orbits the planet's place, listed or not.
			p := 2 * math.Pi * fakePlanetOrientation
			pos.X += fakePlanetDistance * math.Sin(p)
			pos.Y -= fakePlanetDistance * math.Cos(p)
		}
		m.Locations[i].Position = &pos
	}
	return m
}

// startupMoonSetting is FakeMoonSetting in the run's mod-settings.dat, the
// world's own when the run reads it; above its default of 7 it adds FakeMoon.
// An unreadable file gives 0.
func startupMoonSetting(name string) int64 {
	b, err := os.ReadFile(name) //nolint:gosec // the fake's own test folder
	if err != nil {
		return 0
	}
	f, err := modsettings.Decode(b)
	if err != nil {
		return 0
	}
	v, ok, err := f.Get(modsettings.Startup, FakeMoonSetting)
	if err != nil || !ok {
		return 0
	}
	return v.Int
}
