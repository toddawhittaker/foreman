package fakegame

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
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
// gameenv.ModSettingsDefsFile and gameenv.PlanetsFile in the work folder and
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
	if err := os.WriteFile(filepath.Join(mods, world.ModSettingsFile), modSettingsDefaults, world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, gameenv.ModSettingsDefsFile), b, world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		return err
	}
	v := imageVersion(c.Spec.Image)
	// Plain strings, numbers and maps: marshalling cannot fail.
	planets, _ := json.Marshal(cannedPlanets(fmt.Sprintf("%d.%d", v[0], v[1]), enabled))
	return os.WriteFile(filepath.Join(dir, gameenv.PlanetsFile), planets, world.FileMode) //nolint:gosec // the fake's own test folder
}
