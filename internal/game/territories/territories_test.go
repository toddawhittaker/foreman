package territories

import (
	"bytes"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// The mod folder is named after the mod and its version.
func TestModDir(t *testing.T) {
	if ModDir != "foreman-territories_1.0.0" {
		t.Errorf("ModDir %q", ModDir)
	}
}

// The script reads its radius from RadiusFile and writes the file the
// container copies out.
func TestControl(t *testing.T) {
	for _, want := range []string{
		`require("radius")`,
		`script.on_init`,
		`helpers.write_file("` + gameenv.TerritoryFile + `"`,
	} {
		if !bytes.Contains(Control, []byte(want)) {
			t.Errorf("control.lua lacks %s", want)
		}
	}
	if RadiusFile != "radius.lua" {
		t.Errorf("RadiusFile %q does not match require(\"radius\")", RadiusFile)
	}
}
