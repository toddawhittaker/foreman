package web

import (
	"encoding/json"
	"net/url"
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
		{"2.1.24", "ms.enemy_expansion.max_expansion_distance", "4294967296"},
		{"2.1.24", "ms.enemy_expansion.min_expansion_cooldown", "1e15"},
		{"2.0.77", "ms.enemy_expansion.settler_group_max_size", "1e300"},
		{"2.1.24", "ms.enemy_evolution.time_factor", "1e300"},
	} {
		form, overrides, problems := readMapSettings(c.version, "default", postedFor(c.version, "default", map[string]string{c.name: c.value}))
		fl, _ := choiceField(form, c.name)
		if len(problems) != 1 || fl.Error.IsZero() || len(overrides) != 0 {
			t.Errorf("%s=%s: problems %v, overrides %v", c.name, c.value, problems, overrides)
		}
	}
	// The largest value the game stores is still accepted and written whole.
	name := "ms.enemy_expansion.max_expansion_distance"
	_, overrides, problems := readMapSettings("2.1.24", "default", postedFor("2.1.24", "default", map[string]string{name: "4294967295"}))
	if len(problems) != 0 || overrides["enemy_expansion.max_expansion_distance"] != 4294967295 {
		t.Errorf("largest value: problems %v, overrides %v", problems, overrides)
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
