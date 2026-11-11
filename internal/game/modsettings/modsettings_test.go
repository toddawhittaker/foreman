package modsettings

import (
	"bytes"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// The mod folder is named after the mod and its version.
func TestModDir(t *testing.T) {
	if ModDir != "foreman-modsettings_1.0.0" {
		t.Errorf("ModDir %q", ModDir)
	}
}

// The script runs on 1.1, 2.0 and 2.1: it reaches the game only inside an
// event, picks the prototype table and the writer that exist, reads each
// field under pcall, and writes the file the container copies out.
func TestControl(t *testing.T) {
	for _, want := range []string{
		`script.on_init`,
		`local h = helpers or game`,
		`prototypes.mod_setting`,
		`game.mod_setting_prototypes`,
		`pcall(`,
		`h.write_file("` + gameenv.ModSettingsDefsFile + `"`,
		`h.table_to_json(`,
		`"default_value"`, `"minimum_value"`, `"maximum_value"`, `"allowed_values"`,
		`"hidden"`, `"allow_blank"`, `"auto_trim"`,
		`"mod"`, `"setting_type"`, `"order"`,
	} {
		if !bytes.Contains(Control, []byte(want)) {
			t.Errorf("control.lua lacks %s", want)
		}
	}
	if bytes.Contains(Control, []byte("helpers.")) {
		t.Error("control.lua calls helpers directly, which 1.1 lacks")
	}
}
