package modsettings

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/modsettings"
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
		`h.write_file("` + gameenv.PlanetsFile + `"`,
		`h.write_file("` + gameenv.IconsFile + `"`,
		`h.write_file("` + gameenv.StarMapFile + `"`,
		`"mod_data"`, `"foreman-icon-export"`, `script.active_mods`,
		`"space_connection"`, `"label_orientation"`, `"magnitude"`, `"parameter"`,
		`local max_locations = 256`, `local max_routes = 1024`,
		`prototypes.autoplace_control`, `game.autoplace_control_prototypes`,
		`"space_location"`, `"map_gen_settings"`,
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

// Each extra file is written under its own pcall, after the settings, so a
// failure in one loses only that file (SPEC 9.6). The header no longer claims
// the mod adds no prototypes.
func TestControlIsolatesExtraFiles(t *testing.T) {
	text := string(Control)
	settings := strings.Index(text, `h.write_file("`+gameenv.ModSettingsDefsFile+`"`)
	for _, file := range []string{gameenv.PlanetsFile, gameenv.IconsFile, gameenv.StarMapFile} {
		i := strings.Index(text, `h.write_file("`+file+`"`)
		if i < settings {
			t.Errorf("%s is written before the settings", file)
		}
		if open := strings.LastIndex(text[:i], "pcall(function()"); open < 0 || strings.Contains(text[open:i], "end)") {
			t.Errorf("%s is not written inside its own pcall", file)
		}
	}
	if strings.Contains(text, "adds no\n-- prototypes") || strings.Contains(text, "adds no prototypes") {
		t.Error("the header still says the mod adds no prototypes")
	}
}

// The data stage copies plain icon paths of every object kind, and each
// space location's starmap icon and PlanetsLib parent, into one mod-data
// prototype that control.lua reads by the same name. It runs only when the
// game has mod-data, under pcall, drops the game's own mods' paths, and
// keeps within the icon index's limits (SPEC 14.6, ADR 0036).
func TestDataFinalFixes(t *testing.T) {
	text := string(DataFinalFixes)
	for _, want := range []string{
		`defines.prototypes["mod-data"]`, `pcall(function()`, `type = "mod-data"`,
		`local export_name = "foreman-icon-export"`,
		`data.raw[t]`, `proto.starmap_icon,`, `proto.starmap_icons,`, `proto.starmap_icon_size`,
		`orbit.parent`, `layer.tint`, `layer.shift`, `layer.scale`,
		`local max_entries = ` + strconv.Itoa(modsettings.MaxIconIndexEntries),
		`local max_path = ` + strconv.Itoa(modsettings.MaxIconPathLen),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("data-final-fixes.lua lacks %s", want)
		}
	}
	for _, mod := range []string{"base", "core", "space-age", "quality", "elevated-rails", "recycler"} {
		if !strings.Contains(text, `["__`+mod+`__"] = true`) {
			t.Errorf("data-final-fixes.lua does not drop __%s__", mod)
		}
	}
	// Every kind the icon index takes, but starmap, which comes from space
	// locations.
	for _, kind := range []string{"item", "fluid", "recipe", "technology", "entity", "tile", "virtual-signal",
		"space-location", "quality", "achievement", "item-group", "equipment", "asteroid-chunk"} {
		if !modsettings.ValidIconKind(kind) || !strings.Contains(text, `"`+kind+`"`) {
			t.Errorf("kind %s is not exported or not known", kind)
		}
	}
	// The limit on bytes stays under the file limit the decoder applies.
	if !strings.Contains(text, "local max_bytes = 3 * 1024 * 1024") || 3<<20 >= modsettings.MaxIconIndexBytes {
		t.Error("the byte limit is not below the icon index's file limit")
	}
	if !strings.Contains(string(Control), `all["foreman-icon-export"]`) {
		t.Error("control.lua reads another mod-data name")
	}
}
