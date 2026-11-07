package mapgen

// Map type, moisture and terrain type: the Terrain tab's settings that the
// game keeps in property_expression_names (SPEC 5.6).

// ElevationKey is the property_expression_names key the map type sets.
const ElevationKey = "elevation"

// MapTypes returns the map types Nauvis offers on a line: the game's noise
// expressions meant for elevation, the default ("elevation") first. Other
// planets keep their own elevation. It returns nil for an unknown line.
func MapTypes(line string) []string {
	return append([]string(nil), parsed[line].MapTypes...)
}

// TerrainKeys are the property_expression_names keys of the moisture and
// terrain type sliders: the scales (stored as their inverse, a frequency)
// and the biases.
type TerrainKeys struct {
	MoistureScale, AuxScale, MoistureBias, AuxBias string
}

// TerrainKeysOf returns a line's keys, as its example file names them, and
// false for an unknown line. The sliders' steps are not in the game's data;
// they were observed in the game's map generator on every line by the owner,
// 2026-11-06.
func TerrainKeysOf(line string) (TerrainKeys, bool) {
	switch line {
	case "1.1":
		return TerrainKeys{"control-setting:moisture:frequency:multiplier", "control-setting:aux:frequency:multiplier",
			"control-setting:moisture:bias", "control-setting:aux:bias"}, true
	case "2.0", "2.1":
		return TerrainKeys{"control:moisture:frequency", "control:aux:frequency", "control:moisture:bias", "control:aux:bias"}, true
	}
	return TerrainKeys{}, false
}
