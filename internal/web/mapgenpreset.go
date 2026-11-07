package web

import (
	"encoding/json"
	"math"
	"net/url"
	"slices"
	"sort"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// The custom form opened on a preset's values (SPEC 5.6; DESIGN 8.7): "Custom"
// means a preset, and then changes.

// presetSettings is what mapgen.PresetMapGen reports, in the game's keys.
type presetSettings struct {
	Width        *float64 `json:"width"`
	Height       *float64 `json:"height"`
	StartingArea *float64 `json:"starting_area"`
	Peaceful     *bool    `json:"peaceful_mode"`
	Controls     map[string]struct {
		Frequency *float64 `json:"frequency"`
		Size      *float64 `json:"size"`
		Richness  *float64 `json:"richness"`
	} `json:"autoplace_controls"`
	Segmentation *float64 `json:"terrain_segmentation"`
	Water        *float64 `json:"water"`
	Cliff        struct {
		Interval *float64 `json:"cliff_elevation_interval"`
		Richness *float64 `json:"richness"`
	} `json:"cliff_settings"`
	PropertyNames map[string]json.RawMessage `json:"property_expression_names"`
}

// NewMapGenFormForPreset is the form filled with a preset's values for the
// line. Keys the fields do not cover, such as an island's terrain type, fill
// the raw box.
func NewMapGenFormForPreset(line string, spaceAge bool, preset string) MapGenForm {
	form, _, _ := parseMapGenValues(presetValues(line, spaceAge, preset, ""), line, spaceAge)
	return form
}

// presetValues are the post of the form opened on a preset: what the preset
// sets, and the game's defaults elsewhere. Nauvis holds every control a preset
// names. A value that is on no step is left at the default.
func presetValues(line string, spaceAge bool, preset, seed string) url.Values {
	vals := url.Values{mgSeed: {seed}, mgRaw: {rawPrefillFor(line, preset)}}
	var p presetSettings
	if json.Unmarshal(mapgen.PresetMapGen(line, preset), &p) != nil {
		return vals
	}
	whole := func(v *float64) string { return strconv.FormatUint(uint64(*v), 10) }
	if p.Width != nil {
		vals.Set(mgWidth, whole(p.Width))
	}
	if p.Height != nil {
		vals.Set(mgHeight, whole(p.Height))
	}
	if p.Peaceful != nil && *p.Peaceful {
		vals.Set(mgPeaceful, "on")
	}
	step := func(name string, v *float64, invert bool) {
		if v == nil || (invert && *v == 0) {
			return
		}
		x := *v
		if invert {
			x = 1 / x
		}
		if code, ok := stepCode(x); ok {
			vals.Set(name, code)
		}
	}
	step(mgStart, p.StartingArea, false)
	vals.Set(mgMapType, mapgen.ElevationKey)
	if t, ok := p.PropertyNames[mapgen.ElevationKey]; ok {
		var name string
		if json.Unmarshal(t, &name) == nil && slices.Contains(mapgen.MapTypes(line), name) {
			vals.Set(mgMapType, name)
		}
	}
	if keys, ok := mapgen.TerrainKeysOf(line); ok {
		for key, field := range map[string]string{keys.MoistureScale: mgMoistureScale, keys.AuxScale: mgAuxScale} {
			var text string
			if t, ok := p.PropertyNames[key]; ok && json.Unmarshal(t, &text) == nil {
				if v, err := strconv.ParseFloat(text, 64); err == nil {
					step(field, &v, true)
				}
			}
		}
		for key, field := range map[string]string{keys.MoistureBias: mgMoistureBias, keys.AuxBias: mgAuxBias} {
			var text string
			if t, ok := p.PropertyNames[key]; ok && json.Unmarshal(t, &text) == nil {
				if v, err := strconv.ParseFloat(text, 64); err == nil {
					code := strconv.Itoa(int(math.Round(v * 100)))
					if validStep(code, optionsBias) {
						vals.Set(field, code)
					}
				}
			}
		}
	}
	for _, pl := range mgPlanets(line, spaceAge) {
		if pl.name != "nauvis" {
			continue
		}
		for _, c := range pl.controls {
			if ctl, named := p.Controls[c.name]; c.toggle && named && ctl.Size != nil && *ctl.Size == 0 {
				vals.Set(fieldName(pl.name, c.name, mgOn), "off")
			}
			for _, s := range c.settings {
				name := fieldName(pl.name, c.name, s.key)
				switch {
				case c.topLevel && c.isWater() && s.key == "frequency":
					step(name, p.Segmentation, true)
				case c.topLevel && c.isWater():
					step(name, p.Water, false)
				case c.topLevel && s.key == "frequency":
					if p.Cliff.Interval != nil && *p.Cliff.Interval != 0 {
						v := 40 / *p.Cliff.Interval
						step(name, &v, false)
					}
				case c.topLevel:
					step(name, p.Cliff.Richness, false)
				default:
					if ctl, ok := p.Controls[c.name]; ok {
						if c.toggle && s.key == "size" && ctl.Size != nil && *ctl.Size == 0 {
							// Off is the box, not a step.
							continue
						}
						step(name, map[string]*float64{"frequency": ctl.Frequency, "size": ctl.Size, "richness": ctl.Richness}[s.key], s.invert)
					}
				}
			}
		}
	}
	return vals
}

// rawPrefillFor is the raw box opened on a preset: the usual prefill with the
// preset's own terrain-type keys laid over property_expression_names.
func rawPrefillFor(line, preset string) string {
	base := rawPrefill(line)
	var p presetSettings
	if json.Unmarshal(mapgen.PresetMapGen(line, preset), &p) != nil || len(p.PropertyNames) == 0 {
		return base
	}
	ms, ok := orderedMembers([]byte(base))
	if !ok {
		return base
	}
	keys := make([]string, 0, len(p.PropertyNames))
	for k := range p.PropertyNames {
		if !penCovered(line, k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	lay := func(inner []jsonMember) []jsonMember {
		for _, k := range keys {
			if at := slices.IndexFunc(inner, func(m jsonMember) bool { return m.key == k }); at >= 0 {
				inner[at].val = p.PropertyNames[k]
			} else {
				inner = append(inner, jsonMember{k, p.PropertyNames[k]})
			}
		}
		return inner
	}
	found := false
	for i := range ms {
		if ms[i].key == "property_expression_names" {
			inner, _ := orderedMembers(ms[i].val)
			ms[i].val = writeMembers(lay(inner))
			found = true
		}
	}
	if !found {
		ms = append(ms, jsonMember{"property_expression_names", writeMembers(lay(nil))})
	}
	return string(indent(writeMembers(ms))) + "\n"
}
