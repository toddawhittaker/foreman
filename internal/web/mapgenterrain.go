package web

import (
	"encoding/json"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// The Terrain tab's settings that the game keeps in property_expression_names
// (SPEC 5.6): Nauvis's map type, and the moisture and terrain type scale and
// bias.

// mapTypeGroup is Nauvis's map type: a select of the game's elevation
// expressions. Other planets keep their own elevation.
func mapTypeGroup(line string, posted func(string) (string, bool)) MapGenGroup {
	sel := MapGenSelect{ID: "mg-map-type", Name: mgMapType, Label: i18n.M("mapgen.map_type.label"),
		Value: mapgen.ElevationKey, Plain: true, def: mapgen.ElevationKey}
	for _, t := range mapgen.MapTypes(line) {
		sel.Options = append(sel.Options, MapGenOption{t, mapTypeMsg(line, t)})
	}
	if v, ok := posted(mgMapType); ok {
		sel.Value = v
	}
	return MapGenGroup{ID: "mg-map-type-group", Legend: i18n.M("mapgen.map_type.label"), Selects: []MapGenSelect{sel}}
}

// terrainGroup is moisture ("moisture") or terrain type ("aux"): its scale
// and its bias. The game stores the scale's inverse, as for water.
func terrainGroup(kind string, legend i18n.Msg, posted func(string) (string, bool)) MapGenGroup {
	scaleName, biasName := mgPrefix+kind+".scale", mgPrefix+kind+".bias"
	scale := MapGenSelect{ID: "mg-" + kind + "-scale", Name: scaleName, Label: i18n.M("mapgen.setting.scale"),
		Value: mgDefaultStep, Options: optionsTerrainScale, invert: true}
	bias := MapGenSelect{ID: "mg-" + kind + "-bias", Name: biasName, Label: i18n.M("mapgen.setting.bias"),
		Value: mgBiasDefault, Options: optionsBias, def: mgBiasDefault}
	if v, ok := posted(scaleName); ok {
		scale.Value = v
	}
	if v, ok := posted(biasName); ok {
		bias.Value = v
	}
	return MapGenGroup{ID: "mg-" + kind + "-group", Legend: legend, Selects: []MapGenSelect{scale, bias}}
}

// The moisture and terrain type sliders' steps, observed in the game's
// map generator by the owner, 2026-11-06; they are not in the game's data.
// Scale has its own steps, unlike water's (no 71% or 141%; 75%, 133% and
// 150% instead). Bias runs -0.5 to 0.5 by 0.05; a bias code is hundredths.
var optionsTerrainScale = func() []MapGenOption {
	var out []MapGenOption
	for _, code := range []string{"17", "25", "33", "50", "75", "100", "133", "150", "200", "300", "400", "600"} {
		n, _ := strconv.Atoi(code)
		label := i18n.M("mapgen.step.percent", "percent", n)
		if code == mgDefaultStep {
			label = i18n.M("mapgen.step.percent_default", "percent", n)
		}
		out = append(out, MapGenOption{code, label})
	}
	return out
}()

const mgBiasDefault = "0"

var optionsBias = func() []MapGenOption {
	var out []MapGenOption
	for n := -50; n <= 50; n += 5 {
		text := strconv.FormatFloat(float64(n)/100, 'f', 2, 64)
		label := i18n.M("mapgen.step.bias", "value", text)
		if n == 0 {
			label = i18n.M("mapgen.step.bias_default", "value", text)
		}
		out = append(out, MapGenOption{strconv.Itoa(n), label})
	}
	return out
}()

// biasValue is the bias a posted code stands for.
func biasValue(code string) float64 {
	n, _ := strconv.Atoi(code)
	return float64(n) / 100
}

// penCovered are the property_expression_names members the fields decide.
func penCovered(line, key string) bool {
	if key == mapgen.ElevationKey {
		return true
	}
	k, ok := mapgen.TerrainKeysOf(line)
	return ok && (key == k.MoistureScale || key == k.AuxScale || key == k.MoistureBias || key == k.AuxBias)
}

// withoutCoveredPen drops the members the fields decide from a
// property_expression_names object.
func withoutCoveredPen(line string, v json.RawMessage) json.RawMessage {
	inner, ok := orderedMembers(v)
	if !ok {
		return v
	}
	var kept []jsonMember
	for _, m := range inner {
		if !penCovered(line, m.key) {
			kept = append(kept, m)
		}
	}
	return writeMembers(kept)
}

// buildPen lays the map type, scale and bias fields over the raw box's
// property_expression_names. ok is false only if the raw object cannot be
// read.
func buildPen(f MapGenForm, rawPen json.RawMessage, chosen map[string]string) (json.RawMessage, bool) {
	pen := map[string]json.RawMessage{}
	if rawPen != nil {
		ms, ok := orderedMembers(rawPen)
		if !ok {
			return nil, false
		}
		pen = asMap(ms)
	}
	if t := chosen[mgMapType]; t == mapgen.ElevationKey {
		// The game's example: leave elevation out for the default map type.
		delete(pen, mapgen.ElevationKey)
	} else {
		pen[mapgen.ElevationKey] = jsonString(t)
	}
	keys, terrain := mapgen.TerrainKeysOf(f.line)
	if terrain {
		for key, field := range map[string]string{keys.MoistureScale: mgMoistureScale, keys.AuxScale: mgAuxScale} {
			v, _ := stepValue(chosen[field])
			pen[key] = jsonString(string(num(1 / v)))
		}
		for key, field := range map[string]string{keys.MoistureBias: mgMoistureBias, keys.AuxBias: mgAuxBias} {
			// As the game writes it: only a bias other than 0.
			delete(pen, key)
			if v := biasValue(chosen[field]); v != 0 {
				pen[key] = jsonString(strconv.FormatFloat(v, 'f', -1, 64))
			}
		}
	}
	return writeMembers(withPreferred(pen, mapgen.ElevationKey, keys.MoistureScale, keys.AuxScale)), true
}
