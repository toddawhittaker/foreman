package territories

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// The game loads the mod only when factorio_version is its own line, and
// only with Space Age, which has Vulcanus.
func TestInfoJSON(t *testing.T) {
	for _, line := range []string{"2.0", "2.1"} {
		var info struct {
			Name            string   `json:"name"`
			Version         string   `json:"version"`
			FactorioVersion string   `json:"factorio_version"`
			Dependencies    []string `json:"dependencies"`
		}
		b := InfoJSON(line)
		if err := json.Unmarshal(b, &info); err != nil {
			t.Fatal(err)
		}
		if info.FactorioVersion != line || info.Name != ModName || info.Version != ModVersion ||
			!slices.Contains(info.Dependencies, "space-age") {
			t.Errorf("%s: info.json is %s", line, b)
		}
	}
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
