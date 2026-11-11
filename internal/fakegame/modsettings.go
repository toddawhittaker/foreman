package fakegame

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// modSettingsDefaults is the mod-settings.dat the real game wrote for the
// test mod fmtest (factoriotools/factorio 2.1.24), so a run's defaults are
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
}

// cannedModSettingDefs is what the test mod fmtest declares: for each of the
// three setting types a bool, an int, a double, a string with allowed values
// and a color, a hidden startup bool and a startup int with one allowed
// value. One more setting is of a type Foreman does not know, for the
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

// writeModSettings is a mod settings definitions run: it leaves
// gameenv.ModSettingsDefsFile in the work folder and the game's defaults
// gameenv.ModSettingsDefaultsFile in the work folder's mods folder, where the
// game writes mod-settings.dat. With broken the definitions are not JSON.
func writeModSettings(c dockertest.Container, broken bool) error {
	dir := mountSource(c, gameenv.PreviewMount)
	if dir == "" {
		return fmt.Errorf("the fake game's mod settings container has no folder bound at %s", gameenv.PreviewMount)
	}
	// The definitions are plain strings and numbers: marshalling cannot fail.
	b, _ := json.Marshal(struct {
		Settings []modSettingDef `json:"settings"`
	}{cannedModSettingDefs()})
	if broken {
		b = []byte(`{"settings": [{"name": "fm-sta-int", "mod": `)
	}
	mods := filepath.Join(dir, world.ModsDir)
	if err := os.MkdirAll(mods, world.DirMode); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mods, gameenv.ModSettingsDefaultsFile), modSettingsDefaults, world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		return err
	}
	return os.WriteFile(filepath.Join(dir, gameenv.ModSettingsDefsFile), b, world.FileMode) //nolint:gosec // the fake's own test folder
}
