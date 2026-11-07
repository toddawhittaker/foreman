package web

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

var choicePaths = []string{
	"ms.difficulty_settings.recipe_difficulty",
	"ms.difficulty_settings.technology_difficulty",
	"ms.difficulty_settings.research_queue_setting",
}

func choiceField(form MapSettingsForm, name string) (MapSettingsField, bool) {
	for _, g := range form.Groups {
		for _, fl := range g.Fields {
			if fl.Name == name {
				return fl, true
			}
		}
	}
	return MapSettingsField{}, false
}

// postedFor is what a browser sends for a preset's untouched fields, with the
// given values changed.
func postedFor(version, preset string, changes map[string]string) url.Values {
	form, _, _ := readMapSettings(version, preset, nil)
	v := url.Values{}
	for _, g := range form.Groups {
		for _, fl := range g.Fields {
			switch {
			case fl.Toggle && fl.Checked:
				v.Set(fl.Name, "on")
			case !fl.Toggle:
				v.Set(fl.Name, fl.Value)
			}
		}
	}
	for k, w := range changes {
		v.Set(k, w)
	}
	return v
}

// SPEC 5.6: the choices appear where the version's file holds the keys,
// which is 1.1 only.
func TestChoiceFieldsAreOn11Only(t *testing.T) {
	for version, want := range map[string]bool{"1.1.110": true, "2.0.77": false, "2.1.24": false} {
		form, _, _ := readMapSettings(version, "default", nil)
		for _, name := range choicePaths {
			if _, ok := choiceField(form, name); ok != want {
				t.Errorf("%s: field %s shown = %v, want %v", version, name, ok, want)
			}
		}
	}
}

func TestChoiceFieldsOfThePreset(t *testing.T) {
	form, overrides, problems := readMapSettings("1.1.110", "default", nil)
	if len(problems) != 0 || len(overrides) != 0 || form.Modified() {
		t.Fatalf("an untouched default: problems %v, overrides %v, modified %v", problems, overrides, form.Modified())
	}
	selected := func(name string) string {
		fl, _ := choiceField(form, name)
		for _, c := range fl.Choices {
			if c.Selected {
				return c.Value
			}
		}
		return ""
	}
	if got := selected(choicePaths[0]); got != "0" {
		t.Errorf("recipe difficulty %q, want 0", got)
	}
	if got := selected(choicePaths[2]); got != "always" {
		t.Errorf("research queue %q, want always (the default preset's)", got)
	}
	if fl, _ := choiceField(form, choicePaths[2]); len(fl.Choices) != 3 || fl.Choices[0].Value != "after-victory" || fl.Choices[2].Value != "never" {
		t.Errorf("research queue options %+v", fl.Choices)
	}
}

func TestMarathonShowsExpensiveAndChangeMarksModified(t *testing.T) {
	form, _, _ := readMapSettings("1.1.110", "marathon", nil)
	for _, name := range choicePaths[:2] {
		fl, _ := choiceField(form, name)
		if fl.Value != "1" || fl.Preset != "1" || !fl.Choices[1].Selected {
			t.Errorf("%s: value %q preset %q, want Expensive", name, fl.Value, fl.Preset)
		}
	}
	if form.Modified() {
		t.Error("marathon as it comes is marked modified")
	}

	posted := postedFor("1.1.110", "marathon", map[string]string{choicePaths[0]: "0"})
	form, overrides, problems := readMapSettings("1.1.110", "marathon", posted)
	if len(problems) != 0 || !form.Modified() {
		t.Fatalf("problems %v, modified %v", problems, form.Modified())
	}
	b, err := mapgen.MapSettings("1.1.110", "marathon", overrides)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		D map[string]json.RawMessage `json:"difficulty_settings"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	// Numbers stay numbers and words stay words in the written file.
	want := map[string]string{"recipe_difficulty": "0", "technology_difficulty": "1", "research_queue_setting": `"always"`, "technology_price_multiplier": "4"}
	for k, w := range want {
		if string(file.D[k]) != w {
			t.Errorf("%s written as %s, want %s", k, file.D[k], w)
		}
	}
}

func TestChoiceWordsAreWrittenAsWords(t *testing.T) {
	posted := postedFor("1.1.110", "default", map[string]string{choicePaths[2]: "never"})
	_, overrides, problems := readMapSettings("1.1.110", "default", posted)
	if len(problems) != 0 || overrides["difficulty_settings.research_queue_setting"] != "never" || len(overrides) != 1 {
		t.Fatalf("overrides %v, problems %v", overrides, problems)
	}
	b, err := mapgen.MapSettings("1.1.110", "default", overrides)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		D map[string]any `json:"difficulty_settings"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	if got := file.D["research_queue_setting"]; got != "never" {
		t.Errorf("written as %v", got)
	}
}

// A number past what the game stores is a field error, not a wrapped value in
// the written file.
func TestHugeMapSettingNumbersAreRefused(t *testing.T) {
	for _, c := range []struct{ version, name, value string }{
		{"2.1.24", "ms.enemy_expansion.max_expansion_distance", "1e300"},
		{"2.0.77", "ms.enemy_expansion.settler_group_max_size", "1e300"},
		{"2.0.77", "ms.enemy_expansion.settler_group_max_size", "4294967296"},
		{"2.1.24", "ms.pollution.min_pollution_to_damage_trees", "1e300"},
		{"2.1.24", "ms.enemy_expansion.min_expansion_cooldown", "1e15"},
	} {
		form, overrides, problems := readMapSettings(c.version, "default", postedFor(c.version, "default", map[string]string{c.name: c.value}))
		fl, _ := choiceField(form, c.name)
		if len(problems) != 1 || fl.Error.IsZero() || len(overrides) != 0 {
			t.Errorf("%s=%s: problems %v, overrides %v", c.name, c.value, problems, overrides)
		}
	}
}

// Every number field has an upper limit that fits the game's 32-bit numbers,
// so no stored value can wrap.
func TestEveryNumberFieldHasALimit(t *testing.T) {
	for _, g := range msGroups {
		for _, d := range g.defs {
			if d.kind == msToggle || d.kind == msChoice {
				continue
			}
			stored := d.hi / d.scale
			if d.inverse {
				stored = 100 / d.lo
			}
			if d.hi == 0 || stored > mgMaxNumber {
				t.Errorf("%s: limit %v", d.path, d.hi)
			}
		}
	}
}

// The game shows the spoil time modifier as a rate: 10% is stored as 10 and
// 1000% as 0.1, so the shown value is 100 divided by the stored one.
func TestSpoilingRateIsShownInverted(t *testing.T) {
	const name = "ms.difficulty_settings.spoil_time_modifier"
	const path = "difficulty_settings.spoil_time_modifier"
	for _, c := range []struct {
		shown  string
		stored float64
	}{{"10", 10}, {"1000", 0.1}, {"200", 0.5}, {"15", 100.0 / 15}, {"333", 100.0 / 333}} {
		_, overrides, problems := readMapSettings("2.1.24", "default", postedFor("2.1.24", "default", map[string]string{name: c.shown}))
		if len(problems) != 0 || overrides[path] != c.stored {
			t.Errorf("shown %s: problems %v, stored %v, want %v", c.shown, problems, overrides[path], c.stored)
		}
	}
	for _, bad := range []string{"5", "1010", "0", "x", "333.3"} {
		_, _, problems := readMapSettings("2.1.24", "default", postedFor("2.1.24", "default", map[string]string{name: bad}))
		if len(problems) != 1 {
			t.Errorf("shown %s: problems %v, want one", bad, problems)
		}
	}

	form, overrides, _ := readMapSettings("2.1.24", "default", postedFor("2.1.24", "default", map[string]string{name: "200"}))
	if fl, _ := choiceField(form, name); fl.Value != "200" || fl.Preset != "100" || !form.Modified() {
		t.Errorf("value %q preset %q modified %v", fl.Value, fl.Preset, form.Modified())
	}
	b, err := mapgen.MapSettings("2.1.24", "default", overrides)
	if err != nil || !strings.Contains(string(b), `"spoil_time_modifier": 0.5`) {
		t.Errorf("written file: %v\n%s", err, b)
	}

	for _, version := range []string{"2.0.77", "2.1.24"} {
		line, _ := mapgen.Line(version)
		for _, preset := range mapgen.Presets(line) {
			form, overrides, problems := readMapSettings(version, preset, nil)
			fl, ok := choiceField(form, name)
			if !ok || fl.Value != "100" || fl.Preset != "100" || len(problems) != 0 || len(overrides) != 0 {
				t.Errorf("%s %s: shown %q (found %v), problems %v", version, preset, fl.Value, ok, problems)
			}
		}
	}
}

// The Enemy tab's limits are the game screen's, in whole shown units (SPEC 5.6).
func TestEnemyFieldBounds(t *testing.T) {
	const x = "ms.enemy_expansion."
	const e = "ms.enemy_evolution."
	for _, c := range []struct {
		name           string
		accept, refuse []string
	}{
		{x + "min_expansion_distance", []string{"1", "4"}, []string{"0", "21", "2.5", "-1", "x"}},
		{x + "max_expansion_distance", []string{"6", "20"}, []string{"1", "21", "5.5"}},
		{x + "evolution_group_size_factor", []string{"1", "20"}, []string{"0", "21", "8.5"}},
		{x + "min_expansion_cooldown", []string{"1", "30"}, []string{"0", "61", "1.5"}},
		{x + "max_expansion_cooldown", []string{"60", "180"}, []string{"4", "181", "10.5"}},
		{e + "time_factor", []string{"0", "1000"}, []string{"1001", "40.5", "-1"}},
		{e + "destroy_factor", []string{"0", "1000"}, []string{"1001", "40.5"}},
		{e + "pollution_factor", []string{"0", "1000"}, []string{"1001", "9.5"}},
	} {
		for _, v := range c.accept {
			form, _, problems := readMapSettings("2.1.24", "default", postedFor("2.1.24", "default", map[string]string{c.name: v}))
			if fl, _ := choiceField(form, c.name); len(problems) != 0 || !fl.Error.IsZero() {
				t.Errorf("%s=%s refused: %v", c.name, v, problems)
			}
		}
		for _, v := range c.refuse {
			form, overrides, problems := readMapSettings("2.1.24", "default", postedFor("2.1.24", "default", map[string]string{c.name: v}))
			if fl, _ := choiceField(form, c.name); len(problems) != 1 || fl.Error.IsZero() || len(overrides) != 0 {
				t.Errorf("%s=%s accepted: problems %v, overrides %v", c.name, v, problems, overrides)
			}
		}
	}
}

// The Advanced tab's limits, also the game screen's (SPEC 5.6).
func TestAdvancedFieldBounds(t *testing.T) {
	for _, c := range []struct {
		name           string
		accept, refuse []string
	}{
		{"ms.difficulty_settings.technology_price_multiplier", []string{"1", "100000"}, []string{"0", "0.5", "100001", "2.5"}},
		{"ms.pollution.diffusion_ratio", []string{"0", "25"}, []string{"26", "2.5", "-1"}},
		{"ms.pollution.ageing", []string{"10", "400"}, []string{"9", "401", "10.5"}},
		{"ms.pollution.enemy_attack_pollution_consumption_modifier", []string{"10", "400"}, []string{"9", "401", "10.5"}},
		{"ms.pollution.min_pollution_to_damage_trees", []string{"0", "9999"}, []string{"10000", "1.5"}},
		{"ms.pollution.pollution_per_tree_damage", []string{"0", "9999"}, []string{"10000", "1.5"}},
		{"ms.asteroids.spawning_rate", []string{"10", "400"}, []string{"9", "401", "12.5"}},
	} {
		for _, v := range c.accept {
			form, _, problems := readMapSettings("2.1.24", "default", postedFor("2.1.24", "default", map[string]string{c.name: v}))
			if fl, _ := choiceField(form, c.name); len(problems) != 0 || !fl.Error.IsZero() {
				t.Errorf("%s=%s refused: %v", c.name, v, problems)
			}
		}
		for _, v := range c.refuse {
			form, overrides, problems := readMapSettings("2.1.24", "default", postedFor("2.1.24", "default", map[string]string{c.name: v}))
			if fl, _ := choiceField(form, c.name); len(problems) != 1 || fl.Error.IsZero() || len(overrides) != 0 {
				t.Errorf("%s=%s accepted: problems %v, overrides %v", c.name, v, problems, overrides)
			}
		}
	}
}

// 1.1 and 2.0 offer the group sizes that 2.1.18 and later replace with a factor.
func TestGroupSizeBounds(t *testing.T) {
	const min, max = "ms.enemy_expansion.settler_group_min_size", "ms.enemy_expansion.settler_group_max_size"
	for _, version := range []string{"1.1.110", "2.0.77"} {
		post := func(change map[string]string) (MapSettingsForm, []Problem) {
			form, _, problems := readMapSettings(version, "default", postedFor(version, "default", change))
			return form, problems
		}
		for _, c := range []struct {
			name   string
			accept []string
			refuse []string
		}{
			{min, []string{"1", "20"}, []string{"0", "21", "2.5"}},
			{max, []string{"40"}, []string{"0", "41", "5.5"}},
		} {
			for _, v := range c.accept {
				change := map[string]string{c.name: v}
				if c.name == min {
					change[max] = "40" // the minimum may not pass the maximum
				}
				if _, problems := post(change); len(problems) != 0 {
					t.Errorf("%s %s=%s refused: %v", version, c.name, v, problems)
				}
			}
			for _, v := range c.refuse {
				if form, problems := post(map[string]string{c.name: v}); len(problems) != 1 {
					fl, _ := choiceField(form, c.name)
					t.Errorf("%s %s=%s accepted (error %v): %v", version, c.name, v, fl.Error, problems)
				}
			}
		}
		// The maximum may equal the minimum but not fall below it.
		if _, problems := post(map[string]string{min: "8", max: "8"}); len(problems) != 0 {
			t.Errorf("%s: equal sizes refused: %v", version, problems)
		}
		form, problems := post(map[string]string{min: "9", max: "8"})
		fl, _ := choiceField(form, max)
		if len(problems) != 1 || problems[0].FieldID != fl.ID || fl.Error.Args["other"] == nil {
			t.Errorf("%s: maximum 8 below minimum 9: %v", version, problems)
		}
	}
}

// The maximum is an error on the maximum field, naming the minimum.
func TestMaximumsMustNotBeBelowMinimums(t *testing.T) {
	const x = "ms.enemy_expansion."
	for _, c := range []struct {
		change map[string]string
		field  string
		bad    bool
	}{
		{map[string]string{x + "max_expansion_distance": "3"}, x + "max_expansion_distance", true}, // equal to the minimum, which must be less
		{map[string]string{x + "max_expansion_distance": "2"}, x + "max_expansion_distance", true},
		{map[string]string{x + "min_expansion_distance": "5"}, x + "max_expansion_distance", true},
		{map[string]string{x + "min_expansion_distance": "4"}, "", false},
		{map[string]string{x + "min_expansion_cooldown": "30", x + "max_expansion_cooldown": "29"}, x + "max_expansion_cooldown", true},
		{map[string]string{x + "min_expansion_cooldown": "30", x + "max_expansion_cooldown": "30"}, "", false}, // equal is allowed
		{map[string]string{x + "min_expansion_cooldown": "60", x + "max_expansion_cooldown": "50"}, x + "max_expansion_cooldown", true},
	} {
		form, _, problems := readMapSettings("2.1.24", "default", postedFor("2.1.24", "default", c.change))
		if !c.bad {
			if len(problems) != 0 {
				t.Errorf("%v refused: %v", c.change, problems)
			}
			continue
		}
		fl, _ := choiceField(form, c.field)
		if len(problems) != 1 || problems[0].FieldID != fl.ID || fl.Error.IsZero() {
			t.Errorf("%v: problems %v", c.change, problems)
			continue
		}
		if _, ok := fl.Error.Args["other"]; !ok {
			t.Errorf("%v: the message does not name the other field: %+v", c.change, fl.Error)
		}
	}
	// 2.0 has no minimum distance, so its maximum is held to the screen's lower limit only.
	form, _, problems := readMapSettings("2.0.77", "default", postedFor("2.0.77", "default", map[string]string{x + "max_expansion_distance": "1"}))
	if fl, _ := choiceField(form, x+"max_expansion_distance"); len(problems) != 1 || fl.Error.IsZero() {
		t.Errorf("2.0 maximum distance 1 (below the screen's 2): %v", problems)
	}
}

// Every preset of every line reads back inside the limits.
func TestEveryPresetIsInsideTheEnemyLimits(t *testing.T) {
	for _, version := range []string{"1.1.110", "2.0.77", "2.1.9", "2.1.24"} {
		line, _ := mapgen.Line(version)
		for _, preset := range mapgen.Presets(line) {
			form, _, problems := readMapSettings(version, preset, nil)
			if len(problems) != 0 {
				t.Errorf("%s %s: %v", version, preset, problems)
			}
			if _, _, problems := readMapSettings(version, preset, postedFor(version, preset, nil)); len(problems) != 0 {
				t.Errorf("%s %s posted back: %v", version, preset, problems)
			}
			if _, ok := choiceField(form, "ms.enemy_evolution.time_factor"); !ok {
				t.Errorf("%s %s: no time factor", version, preset)
			}
		}
	}
}

func TestChoiceNotInTheListIsRefused(t *testing.T) {
	bad := map[string][]string{
		choicePaths[0]: {"2", "-1", "1.0", "", "expensive"},
		choicePaths[1]: {"2", "-1", "1.0", "", "expensive"},
		choicePaths[2]: {"0", "sometimes", "", "Always"},
	}
	for name, values := range bad {
		for _, v := range values {
			form, overrides, problems := readMapSettings("1.1.110", "default", postedFor("1.1.110", "default", map[string]string{name: v}))
			fl, _ := choiceField(form, name)
			if len(problems) != 1 || problems[0].FieldID != fl.ID || fl.Error.IsZero() || len(overrides) != 0 {
				t.Errorf("%s=%q: problems %v, overrides %v", name, v, problems, overrides)
			}
		}
	}
}
