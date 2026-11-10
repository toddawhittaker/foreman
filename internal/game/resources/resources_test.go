package resources

import (
	"bytes"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// The mod folder is named after the mod and its version.
func TestModDir(t *testing.T) {
	if ModDir != "foreman-resources_1.0.0" {
		t.Errorf("ModDir %q", ModDir)
	}
}

// The script reads its parameters from ParamsFile, writes on 1.1 and 2.x
// alike, and writes the file the container copies out.
func TestControl(t *testing.T) {
	for _, want := range []string{
		`require("params")`,
		`script.on_init`,
		`local h = helpers or game`,
		`h.write_file("` + gameenv.ResourceFile + `"`,
		`h.table_to_json(`,
	} {
		if !bytes.Contains(Control, []byte(want)) {
			t.Errorf("control.lua lacks %s", want)
		}
	}
	if bytes.Contains(Control, []byte("helpers.")) {
		t.Error("control.lua calls helpers directly, which 1.1 lacks")
	}
	if ParamsFile != "params.lua" {
		t.Errorf("ParamsFile %q does not match require(\"params\")", ParamsFile)
	}
}
