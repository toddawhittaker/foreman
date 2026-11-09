package resources

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// The game loads the mod only when factorio_version is its own line. It
// depends on Space Age only when the map has it, so 1.1 and a 2.x map
// without Space Age load it too.
func TestInfoJSON(t *testing.T) {
	for _, tc := range []struct {
		line     string
		spaceAge bool
		want     []string
	}{
		{"1.1", false, []string{"base"}},
		{"2.0", false, []string{"base"}},
		{"2.0", true, []string{"base", "space-age"}},
		{"2.1", true, []string{"base", "space-age"}},
	} {
		var info struct {
			Name            string   `json:"name"`
			Version         string   `json:"version"`
			FactorioVersion string   `json:"factorio_version"`
			Dependencies    []string `json:"dependencies"`
		}
		b := InfoJSON(tc.line, tc.spaceAge)
		if err := json.Unmarshal(b, &info); err != nil {
			t.Fatal(err)
		}
		if info.FactorioVersion != tc.line || info.Name != ModName || info.Version != ModVersion ||
			!slices.Equal(info.Dependencies, tc.want) {
			t.Errorf("%s, Space Age %v: info.json is %s", tc.line, tc.spaceAge, b)
		}
	}
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
