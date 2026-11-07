package mapgen

import (
	"encoding/json"
	"slices"
	"testing"
)

// The map types are the game's elevation expressions, read from each image.
func TestMapTypesPerLine(t *testing.T) {
	want := map[string][]string{
		"1.1": {"elevation", "0_17-island"},
		"2.0": {"elevation", "elevation_lakes", "elevation_island"},
		"2.1": {"elevation", "elevation_lakes", "elevation_island"},
	}
	for line, w := range want {
		if got := MapTypes(line); !slices.Equal(got, w) {
			t.Errorf("MapTypes(%s) = %v, want %v", line, got, w)
		}
	}
	if MapTypes("3.0") != nil {
		t.Error("an unknown line has map types")
	}
}

// Each preset that sets a map type sets one the line offers.
func TestPresetMapTypesAreOffered(t *testing.T) {
	for _, line := range Lines {
		for _, p := range Presets(line) {
			var d struct {
				Names map[string]string `json:"property_expression_names"`
			}
			if err := json.Unmarshal(PresetMapGen(line, p), &d); err != nil {
				t.Fatal(err)
			}
			if e, ok := d.Names[ElevationKey]; ok && !slices.Contains(MapTypes(line), e) {
				t.Errorf("%s preset %s sets map type %q, not offered", line, p, e)
			}
		}
	}
}

// The terrain keys are the ones each line's own example file names, at
// their defaults.
func TestTerrainKeysAreInTheExampleFile(t *testing.T) {
	for _, line := range Lines {
		k, ok := TerrainKeysOf(line)
		if !ok {
			t.Fatalf("no keys for %s", line)
		}
		names := exampleNames(t, line)
		for key, want := range map[string]string{k.MoistureScale: "1", k.AuxScale: "1", k.MoistureBias: "0", k.AuxBias: "0"} {
			if names[key] != want {
				t.Errorf("%s example file has %s = %q, want %q", line, key, names[key], want)
			}
		}
	}
	if _, ok := TerrainKeysOf("3.0"); ok {
		t.Error("an unknown line has terrain keys")
	}
}

func exampleNames(t *testing.T, line string) map[string]string {
	t.Helper()
	var d struct {
		Names map[string]string `json:"property_expression_names"`
	}
	if err := json.Unmarshal(DefaultMapGen(line), &d); err != nil {
		t.Fatal(err)
	}
	return d.Names
}

// Every resource can be switched off; on 2.x with Space Age the game says
// enemy bases cannot.
func TestCanBeDisabled(t *testing.T) {
	for _, line := range Lines {
		for _, p := range Planets(line, true) {
			for _, c := range Controls(line, p) {
				if c.Category == "resource" && !c.CanBeDisabled {
					t.Errorf("%s %s: resource %s cannot be switched off", line, p, c.Name)
				}
			}
		}
	}
	for _, line := range []string{"2.0", "2.1"} {
		for _, c := range Controls(line, "nauvis") {
			if c.Name == "enemy-base" && c.CanBeDisabled {
				t.Errorf("%s: enemy-base can be switched off", line)
			}
		}
	}
}
