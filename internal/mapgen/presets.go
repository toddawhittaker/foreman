package mapgen

// What each preset sets in map-gen-settings (SPEC 5.6; DESIGN 8.7), so the
// custom form can open filled with a preset's values. The data is the game's
// own reading of each preset, as the map exchange golden fixtures hold it
// (internal/mapexchange/testdata), kept as the difference from the "default"
// preset. It holds only what map-gen-settings.json carries: the changes a
// preset makes to the map settings (evolution, pollution, price multiplier)
// are in MapSettings (mapsettings.go).

import "encoding/json"

// presetData is each line's presets.json: preset name to its map-gen
// differences from the default.
var presetData = func() map[string]map[string]json.RawMessage {
	m := map[string]map[string]json.RawMessage{}
	for _, l := range Lines {
		b, err := files.ReadFile("data/" + l + "/presets.json")
		if err != nil {
			panic("mapgen: " + err.Error())
		}
		var d map[string]json.RawMessage
		if err := json.Unmarshal(b, &d); err != nil {
			panic("mapgen: data/" + l + "/presets.json: " + err.Error())
		}
		m[l] = d
	}
	return m
}()

// PresetMapGen returns what a preset changes in map-gen-settings.json on a
// line, as a JSON object in the game's own keys and numbers: width, height,
// starting_area, peaceful_mode, autoplace_controls (only the controls it
// changes, each with frequency, size and richness), terrain_segmentation and
// water (1.1), cliff_settings (1.1) and property_expression_names. It
// returns "{}" for "default" and for a preset that changes none of these,
// and nil for an unknown line or preset.
func PresetMapGen(line, preset string) []byte {
	b, ok := presetData[line][preset]
	if !ok {
		return nil
	}
	return append([]byte(nil), b...)
}
