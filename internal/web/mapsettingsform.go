package web

import (
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// The Enemy and Advanced settings Generate offers from map-settings.json
// (SPEC 5.6; DESIGN 8.7). Each field shows its value in the units the game's
// own map generator shows (minutes, percent, "40" for an evolution time
// factor of 0.000004). Fields are named "ms." and the dotted path of the
// value in the file.

const msPrefix = "ms."

// What a map-settings value holds.
type msKind int

const (
	msNumber msKind = iota // a number of 0 or more
	msWhole                // a whole number of 0 or more
	msToggle               // true or false, as a checkbox
	msChoice               // one of a fixed list of numbers or words, as a select
)

// msOption is one choice of an msChoice field: the value the file stores (an
// int or a string, which stays the kind it is) and its words.
type msOption struct {
	stored any
	label  i18n.Msg
}

// shown is the option's value as the form posts it.
func (o msOption) shown() string {
	if n, ok := o.stored.(int); ok {
		return strconv.Itoa(n)
	}
	return o.stored.(string)
}

var (
	msNormalExpensive = []msOption{
		{0, i18n.M("mapgen.ms.choice.normal")},
		{1, i18n.M("mapgen.ms.choice.expensive")},
	}
	msResearchQueue = []msOption{
		{"after-victory", i18n.M("mapgen.ms.choice.after_victory")},
		{"always", i18n.M("mapgen.ms.choice.always")},
		{"never", i18n.M("mapgen.ms.choice.never")},
	}
)

// msDef is one field: the value's path, its words, and how it is shown.
type msDef struct {
	path  string
	label i18n.Msg
	kind  msKind
	// scale converts the file's value to the shown one (shown = stored × scale).
	scale float64
	unit  i18n.Msg // the unit after the field, or none
	// lo and hi bound the shown value when hi is not 0 (the game's own limits).
	lo, hi float64
	// choices lists the options of an msChoice field.
	choices []msOption
	// replacedBy is a path whose presence means the game no longer shows this
	// one: 2.1's group size factor replaced the group sizes.
	replacedBy string
}

// msGroup is a fieldset of the Enemy or Advanced tab.
type msGroup struct {
	tab    string
	legend i18n.Msg
	defs   []msDef
}

// Tabs of the map settings.
const (
	tabResources = "resources"
	tabTerrain   = "terrain"
	tabEnemy     = "enemy"
	tabAdvanced  = "advanced"
)

// msGroups lists every field any line has, in the game's order; a version
// shows those its own file holds.
var msGroups = []msGroup{
	{tabEnemy, i18n.M("mapgen.ms.expansion"), []msDef{
		{path: "enemy_expansion.enabled", label: i18n.M("mapgen.ms.enabled"), kind: msToggle},
		{path: "enemy_expansion.min_expansion_distance", label: i18n.M("mapgen.ms.min_distance"), kind: msWhole, scale: 1},
		{path: "enemy_expansion.max_expansion_distance", label: i18n.M("mapgen.ms.max_distance"), kind: msWhole, scale: 1},
		{path: "enemy_expansion.settler_group_min_size", label: i18n.M("mapgen.ms.min_group"), kind: msWhole, scale: 1, replacedBy: "enemy_expansion.evolution_group_size_factor"},
		{path: "enemy_expansion.settler_group_max_size", label: i18n.M("mapgen.ms.max_group"), kind: msWhole, scale: 1, replacedBy: "enemy_expansion.evolution_group_size_factor"},
		{path: "enemy_expansion.evolution_group_size_factor", label: i18n.M("mapgen.ms.group_factor"), scale: 1},
		{path: "enemy_expansion.min_expansion_cooldown", label: i18n.M("mapgen.ms.min_cooldown"), scale: 1.0 / 3600, unit: i18n.M("mapgen.unit.minutes")},
		{path: "enemy_expansion.max_expansion_cooldown", label: i18n.M("mapgen.ms.max_cooldown"), scale: 1.0 / 3600, unit: i18n.M("mapgen.unit.minutes")},
	}},
	{tabEnemy, i18n.M("mapgen.ms.evolution"), []msDef{
		{path: "enemy_evolution.enabled", label: i18n.M("mapgen.ms.enabled"), kind: msToggle},
		{path: "enemy_evolution.time_factor", label: i18n.M("mapgen.ms.time_factor"), scale: 1e7, unit: i18n.M("mapgen.unit.e7")},
		{path: "enemy_evolution.destroy_factor", label: i18n.M("mapgen.ms.destroy_factor"), scale: 1e5, unit: i18n.M("mapgen.unit.e5")},
		{path: "enemy_evolution.pollution_factor", label: i18n.M("mapgen.ms.pollution_factor"), scale: 1e7, unit: i18n.M("mapgen.unit.e7")},
	}},
	{tabAdvanced, i18n.M("mapgen.ms.technology"), []msDef{
		// Only 1.1's file holds these three.
		{path: "difficulty_settings.recipe_difficulty", label: i18n.M("mapgen.ms.recipe_difficulty"), kind: msChoice, choices: msNormalExpensive},
		{path: "difficulty_settings.technology_difficulty", label: i18n.M("mapgen.ms.technology_difficulty"), kind: msChoice, choices: msNormalExpensive},
		{path: "difficulty_settings.research_queue_setting", label: i18n.M("mapgen.ms.research_queue"), kind: msChoice, choices: msResearchQueue},
		// The runtime API's range for the multiplier.
		{path: "difficulty_settings.technology_price_multiplier", label: i18n.M("mapgen.ms.price"), scale: 1, lo: 0.001, hi: 100000},
	}},
	{tabAdvanced, i18n.M("mapgen.ms.pollution"), []msDef{
		{path: "pollution.enabled", label: i18n.M("mapgen.ms.enabled"), kind: msToggle},
		{path: "pollution.diffusion_ratio", label: i18n.M("mapgen.ms.diffusion"), scale: 100, unit: i18n.M("mapgen.unit.percent")},
		{path: "pollution.ageing", label: i18n.M("mapgen.ms.absorption"), scale: 100, unit: i18n.M("mapgen.unit.percent")},
		{path: "pollution.enemy_attack_pollution_consumption_modifier", label: i18n.M("mapgen.ms.attack_cost"), scale: 100, unit: i18n.M("mapgen.unit.percent")},
		{path: "pollution.min_pollution_to_damage_trees", label: i18n.M("mapgen.ms.tree_damage"), scale: 1},
		{path: "pollution.pollution_per_tree_damage", label: i18n.M("mapgen.ms.per_tree"), scale: 1},
	}},
	{tabAdvanced, i18n.M("mapgen.ms.asteroids"), []msDef{
		{path: "asteroids.spawning_rate", label: i18n.M("mapgen.ms.spawning"), scale: 100, unit: i18n.M("mapgen.unit.percent")},
	}},
	{tabAdvanced, i18n.M("mapgen.ms.spoiling"), []msDef{
		// The runtime API's range: 0.01 to 100.
		{path: "difficulty_settings.spoil_time_modifier", label: i18n.M("mapgen.ms.spoil_time"), scale: 100, unit: i18n.M("mapgen.unit.percent"), lo: 1, hi: 10000},
	}},
}

// AchWarn is how the browser tells a value crosses an achievement line, in
// the field's shown units (DESIGN 8.7).
type AchWarn struct {
	// Test is "true", "false", "below" or "above"; Limit is in shown units.
	Test, Limit string
	// Reason is the message key of the game's reason.
	Reason string
}

// MapSettingsChoice is one option of a select field.
type MapSettingsChoice struct {
	Value    string
	Label    i18n.Msg
	Selected bool
}

// MapSettingsField is one field of the Enemy or Advanced tab.
type MapSettingsField struct {
	ID, Name string
	Label    i18n.Msg
	Unit     i18n.Msg
	Toggle   bool
	// Value is the shown value; Checked a toggle's state. Preset is the
	// preset's shown value ("on" or "" for a toggle), for "[modified]".
	Value   string
	Checked bool
	Preset  string
	// Choices is set for a select; Value is then the chosen option's value.
	Choices []MapSettingsChoice
	Error   i18n.Msg
	Ach     *AchWarn
	Blocked bool
}

// MapSettingsGroup is one fieldset.
type MapSettingsGroup struct {
	Tab    string
	Legend i18n.Msg
	Fields []MapSettingsField
}

// MapSettingsForm is the map-settings part of Generate's form.
type MapSettingsForm struct {
	// Untested is true on a version whose map-settings layout no tested game
	// image has: Foreman does not know its keys, so it offers no fields and
	// writes no file (SPEC 5.6).
	Untested bool
	Groups   []MapSettingsGroup
}

// InTab lists the groups of one tab.
func (f MapSettingsForm) InTab(tab string) []MapSettingsGroup {
	var out []MapSettingsGroup
	for _, g := range f.Groups {
		if g.Tab == tab {
			out = append(out, g)
		}
	}
	return out
}

// Modified reports whether any field differs from the preset.
func (f MapSettingsForm) Modified() bool {
	for _, g := range f.Groups {
		for _, fl := range g.Fields {
			if fl.Toggle && fl.Checked != (fl.Preset == "on") {
				return true
			}
			if !fl.Toggle && !sameShown(fl.Value, fl.Preset) {
				return true
			}
		}
	}
	return false
}

// sameShown compares two shown numbers by value, so "4" and "4.0" match.
func sameShown(a, b string) bool {
	x, errA := strconv.ParseFloat(strings.TrimSpace(a), 64)
	y, errB := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if errA != nil || errB != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return math.Abs(x-y) <= 1e-9*math.Max(1, math.Abs(y))
}

// shown writes a stored number in shown units, without float noise.
func shown(v, scale float64) string {
	x := v * scale
	if x != 0 {
		// Nine significant digits: enough for any value the game shows.
		p := math.Pow(10, 8-math.Floor(math.Log10(math.Abs(x))))
		x = math.Round(x*p) / p
	}
	return strconv.FormatFloat(x, 'f', -1, 64)
}

func msFieldID(path string) string {
	return "ms-" + strings.NewReplacer(".", "-", "_", "-").Replace(path)
}

// presetMapSettings is the preset's file as dotted paths, or ErrUntestedLayout.
func presetMapSettings(version, preset string) (map[string]any, error) {
	b, err := mapgen.MapSettings(version, preset, nil)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return flatPaths(m, ""), nil
}

func flatPaths(m map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			for p, w := range flatPaths(sub, prefix+k+".") {
				out[p] = w
			}
			continue
		}
		out[prefix+k] = v
	}
	return out
}

// readMapSettings builds the map-settings fields for a version and preset.
// With posted nil the fields hold the preset's values; otherwise they hold
// what was posted, checked. overrides holds, by path, each value that differs
// from the preset, ready for mapgen.MapSettings.
func readMapSettings(version, preset string, posted url.Values) (form MapSettingsForm, overrides map[string]any, problems []Problem) {
	base, err := presetMapSettings(version, preset)
	if errors.Is(err, mapgen.ErrUntestedLayout) {
		return MapSettingsForm{Untested: true}, nil, nil
	}
	if err != nil {
		// An unknown version or preset: the form refuses it elsewhere.
		return MapSettingsForm{}, nil, nil
	}
	rules := map[string]mapgen.AchievementRule{}
	if rs, err := mapgen.AchievementRules(version); err == nil {
		for _, r := range rs {
			if r.MapSettings {
				rules[r.Field] = r
			}
		}
	}
	overrides = map[string]any{}
	for _, g := range msGroups {
		group := MapSettingsGroup{Tab: g.tab, Legend: g.legend}
		for _, d := range g.defs {
			pv, ok := base[d.path]
			if _, replaced := base[d.replacedBy]; !ok || replaced {
				continue
			}
			fl := MapSettingsField{ID: msFieldID(d.path), Name: msPrefix + d.path, Label: d.label, Toggle: d.kind == msToggle}
			fl.Unit = d.unit
			var stored any
			if fl.Toggle {
				on, _ := pv.(bool)
				if on {
					fl.Preset = "on"
				}
				fl.Checked = on
				if posted != nil {
					fl.Checked = posted.Get(fl.Name) != ""
				}
				stored = fl.Checked
				if fl.Checked != on {
					overrides[d.path] = fl.Checked
				}
			} else if d.kind == msChoice {
				fl.Preset = presetChoice(pv)
				fl.Value = fl.Preset
				if vs, ok := posted[fl.Name]; ok && len(vs) > 0 {
					fl.Value = vs[0]
				}
				var chosen *msOption
				for i, o := range d.choices {
					fl.Choices = append(fl.Choices, MapSettingsChoice{Value: o.shown(), Label: o.label, Selected: o.shown() == fl.Value})
					if o.shown() == fl.Value {
						chosen = &d.choices[i]
					}
				}
				switch {
				case chosen == nil:
					fl.Error = i18n.M("mapgen.err.step", "field", d.label)
					problems = append(problems, Problem{FieldID: fl.ID, Message: fl.Error})
				case fl.Value != fl.Preset:
					overrides[d.path] = chosen.stored
				}
			} else {
				n, _ := pv.(float64)
				fl.Preset = shown(n, d.scale)
				fl.Value = fl.Preset
				// A field the post lacks, as when the line changed under the page, keeps the preset's value.
				if vs, ok := posted[fl.Name]; ok && len(vs) > 0 {
					fl.Value = strings.TrimSpace(vs[0])
				}
				v, msg := parseShown(fl.Value, d)
				if !msg.IsZero() {
					fl.Error = msg
					problems = append(problems, Problem{FieldID: fl.ID, Message: msg})
				} else {
					if !sameShown(fl.Value, fl.Preset) {
						overrides[d.path] = storedValue(v, n, d)
						stored = overrides[d.path]
					} else {
						stored = n
					}
				}
			}
			if r, ok := rules[d.path]; ok {
				fl.Ach = achWarn(r, d.scale)
				if f, isNum := stored.(int); isNum {
					stored = float64(f)
				}
				fl.Blocked = stored != nil && r.Blocks(stored)
			}
			group.Fields = append(group.Fields, fl)
		}
		if len(group.Fields) > 0 {
			form.Groups = append(form.Groups, group)
		}
	}
	if len(problems) == 0 && len(overrides) > 0 {
		problems = checkMapSettingsFile(&form, version, preset, overrides)
	}
	return form, overrides, problems
}

// checkMapSettingsFile runs the file the overrides make through the settings
// validator, as the map-gen file is, so a value the game would refuse is
// never previewed or written. It marks the field at fault, or the first one.
func checkMapSettingsFile(form *MapSettingsForm, version, preset string, overrides map[string]any) []Problem {
	line, _ := mapgen.Line(version)
	b, err := mapgen.MapSettings(version, preset, overrides)
	var errs []gamesettings.FieldError
	if err == nil {
		errs = gamesettings.Validate(gamesettings.FileMapSettings, line, b)
	}
	if err == nil && len(errs) == 0 {
		return nil
	}
	// The form holds the first field, as an override came from one.
	at := &form.Groups[0].Fields[0]
	if len(errs) > 0 {
		for g := range form.Groups {
			for i := range form.Groups[g].Fields {
				if form.Groups[g].Fields[i].ID == msFieldID(errs[0].Key) {
					at = &form.Groups[g].Fields[i]
				}
			}
		}
	}
	at.Error = i18n.M("mapgen.err.unknown")
	return []Problem{{FieldID: at.ID, Message: at.Error}}
}

// presetChoice is a preset's number or word as a choice's value.
func presetChoice(v any) string {
	if n, ok := v.(float64); ok {
		return shown(n, 1)
	}
	s, _ := v.(string)
	return s
}

// parseShown reads a shown value and returns it in shown units, or the
// field's error.
func parseShown(s string, d msDef) (float64, i18n.Msg) {
	field := d.label
	v, err := strconv.ParseFloat(s, 64)
	switch {
	case err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0:
		if d.kind == msWhole {
			return 0, i18n.M("mapgen.err.whole", "field", field)
		}
		return 0, i18n.M("mapgen.err.number", "field", field)
	case d.kind == msWhole && v != math.Trunc(v):
		return 0, i18n.M("mapgen.err.whole", "field", field)
	case d.hi != 0 && (v < d.lo || v > d.hi):
		return 0, i18n.M("mapgen.err.between", "field", field, "low", shown(d.lo, 1), "high", shown(d.hi, 1))
	case d.hi == 0 && math.Round(v/d.scale) > mgMaxNumber:
		// The game stores these as 32-bit numbers; a larger one would wrap.
		top := math.Floor(mgMaxNumber*d.scale*100) / 100
		return 0, i18n.M("mapgen.err.between", "field", field, "low", "0", "high", strconv.FormatFloat(top, 'f', -1, 64))
	}
	return v, i18n.Msg{}
}

// storedValue turns a shown value back into the file's units. A value the
// file holds as a whole number (a cooldown in ticks, a distance) stays whole.
func storedValue(v, preset float64, d msDef) any {
	x := v / d.scale
	if d.kind == msWhole || preset == math.Trunc(preset) && d.scale < 1 {
		return int(math.Round(x))
	}
	return x
}

// achWarn is a rule in a field's shown units.
func achWarn(r mapgen.AchievementRule, scale float64) *AchWarn {
	w := &AchWarn{Test: r.Test, Reason: r.Message}
	if r.Test == "below" || r.Test == "above" {
		w.Limit = shown(r.Limit, scale)
	}
	return w
}
