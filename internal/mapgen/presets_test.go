package mapgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// The preset data is the game's own parse of each preset (the golden
// fixtures of internal/mapexchange), as the difference from "default"
// (SPEC 5.6). This test derives that difference again and compares.

type goldenFile struct {
	MapGen map[string]any `json:"map_gen_settings"`
}

func readGolden(t *testing.T, version, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "mapexchange", "testdata", version, name+".json")) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	var g goldenFile
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g.MapGen
}

func sub(m map[string]any, key string) map[string]any {
	s, _ := m[key].(map[string]any)
	return s
}

// differences is what preset sets in the keys PresetMapGen reports.
func differences(line string, def, preset map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"width", "height", "starting_area", "peaceful_mode", "terrain_segmentation", "water"} {
		if v, ok := preset[k]; ok && !reflect.DeepEqual(def[k], v) {
			out[k] = v
		}
	}
	for _, key := range []string{"autoplace_controls", "property_expression_names"} {
		d := map[string]any{}
		for k, v := range sub(preset, key) {
			if !reflect.DeepEqual(sub(def, key)[k], v) {
				d[k] = v
			}
		}
		if len(d) > 0 {
			out[key] = d
		}
	}
	if line == "1.1" {
		d := map[string]any{}
		for k, v := range sub(preset, "cliff_settings") {
			if !reflect.DeepEqual(sub(def, "cliff_settings")[k], v) {
				d[k] = v
			}
		}
		if len(d) > 0 {
			out["cliff_settings"] = d
		}
	}
	return out
}

func TestPresetMapGenIsWhatTheGameParses(t *testing.T) {
	for _, line := range Lines {
		version := parsed[line].GameVersion
		def := readGolden(t, version, "default")
		for _, name := range Presets(line) {
			t.Run(line+" "+name, func(t *testing.T) {
				got := PresetMapGen(line, name)
				if got == nil {
					t.Fatalf("no data for preset %s", name)
				}
				var have map[string]any
				if err := json.Unmarshal(got, &have); err != nil {
					t.Fatal(err)
				}
				if want := differences(line, def, readGolden(t, version, name)); !reflect.DeepEqual(have, want) {
					t.Errorf("preset %s on %s\n have %v\n want %v (the game's parse)", name, version, have, want)
				}
			})
		}
	}
}

func TestPresetMapGenCoversEveryPresetAndOnlyThose(t *testing.T) {
	for _, line := range Lines {
		var have []string
		for name := range presetData[line] {
			have = append(have, name)
		}
		slices.Sort(have)
		want := Presets(line)
		slices.Sort(want)
		if !slices.Equal(have, want) {
			t.Errorf("%s: presets.json has %v, the game lists %v", line, have, want)
		}
	}
	if PresetMapGen("2.1", "no-such-preset") != nil || PresetMapGen("9.9", "default") != nil {
		t.Error("an unknown preset or line must return nil")
	}
	if string(PresetMapGen("2.1", "default")) != "{}" {
		t.Errorf("default changes nothing, got %s", PresetMapGen("2.1", "default"))
	}
}
