package web

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// SPEC 5.6, DESIGN 8.7: the custom form opens filled with the chosen preset.
// The reference is the game's own parse of each preset's exchange string
// (internal/mapexchange/testdata), for the three tested patches.

var goldenVersions = map[string]string{"1.1": "1.1.110", "2.0": "2.0.77", "2.1": "2.1.24"}

func goldenMapGen(t *testing.T, line, preset string, spaceAge bool) map[string]any {
	t.Helper()
	name := preset
	if spaceAge {
		name += "_space-age"
	}
	b, err := os.ReadFile(filepath.Join("..", "mapexchange", "testdata", goldenVersions[line], name+".json")) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		MapGen map[string]any `json:"map_gen_settings"`
	}
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	return g.MapGen
}

func near(a, b any) bool {
	x, ok1 := a.(float64)
	y, ok2 := b.(float64)
	return ok1 && ok2 && math.Abs(x-y) <= 1e-6*math.Max(1, math.Abs(y))
}

func obj(m map[string]any, k string) map[string]any {
	o, _ := m[k].(map[string]any)
	return o
}

func TestPresetPrefillParsesToWhatTheGameSets(t *testing.T) {
	for _, line := range mapgen.Lines {
		for _, spaceAge := range []bool{false, true} {
			if spaceAge && line == "1.1" {
				continue
			}
			for _, preset := range mapgen.Presets(line) {
				name := line + " " + preset
				if spaceAge {
					name += " with Space Age"
				}
				t.Run(name, func(t *testing.T) {
					form, out, problems := parseMapGenValues(presetValues(line, spaceAge, preset, "42"), line, spaceAge)
					if len(problems) > 0 {
						t.Fatalf("the prefilled form has problems: %v", problems)
					}
					if form.Seed.Value != "42" {
						t.Errorf("the seed was %q, want the one posted", form.Seed.Value)
					}
					var have map[string]any
					if err := json.Unmarshal(out, &have); err != nil {
						t.Fatal(err)
					}
					want := goldenMapGen(t, line, preset, spaceAge)
					for _, k := range []string{"width", "height", "starting_area"} {
						w := want[k]
						if w == float64(2000000) {
							w = float64(0) // the game's own reading of "no limit"; the file says 0
						}
						if !near(have[k], w) {
							t.Errorf("%s = %v, the game sets %v", k, have[k], w)
						}
					}
					if line == "1.1" {
						for _, k := range []string{"terrain_segmentation", "water"} {
							if !near(have[k], want[k]) {
								t.Errorf("%s = %v, the game sets %v", k, have[k], want[k])
							}
						}
						for _, k := range []string{"cliff_elevation_interval", "richness"} {
							if !near(obj(have, "cliff_settings")[k], obj(want, "cliff_settings")[k]) {
								t.Errorf("cliff_settings.%s = %v, the game sets %v", k, obj(have, "cliff_settings")[k], obj(want, "cliff_settings")[k])
							}
						}
					}
					if have["peaceful_mode"] != want["peaceful_mode"] {
						t.Errorf("peaceful_mode = %v, the game sets %v", have["peaceful_mode"], want["peaceful_mode"])
					}
					for ctl, gs := range obj(want, "autoplace_controls") {
						hs, ok := obj(have, "autoplace_controls")[ctl].(map[string]any)
						if !ok {
							continue // a control the form has no field for on this line
						}
						for _, k := range []string{"frequency", "size", "richness"} {
							if _, ok := hs[k]; ok && !near(hs[k], gs.(map[string]any)[k]) {
								t.Errorf("%s.%s = %v, the game sets %v", ctl, k, hs[k], gs.(map[string]any)[k])
							}
						}
					}
					// Every terrain-type key the preset sets is in the raw box's object.
					for k, v := range obj(want, "property_expression_names") {
						if got := obj(have, "property_expression_names")[k]; got != v {
							// the default prefill holds some keys at its own value; only a preset's own must match
							if _, own := presetOwnKeys(t, line, preset)[k]; own {
								t.Errorf("property_expression_names.%s = %v, the game sets %v", k, got, v)
							}
						}
					}
				})
			}
		}
	}
}

func presetOwnKeys(t *testing.T, line, preset string) map[string]json.RawMessage {
	t.Helper()
	var p presetSettings
	if err := json.Unmarshal(mapgen.PresetMapGen(line, preset), &p); err != nil {
		t.Fatal(err)
	}
	return p.PropertyNames
}

// Every value a preset sets must land on an option the select shows, or the
// form would silently open at the default.
func TestPresetValuesAreAllOnSteps(t *testing.T) {
	for _, line := range mapgen.Lines {
		for _, preset := range mapgen.Presets(line) {
			vals := presetValues(line, line != "1.1", preset, "")
			form := buildMapGenForm(line, line != "1.1", func(n string) (string, bool) {
				v, ok := vals[n]
				if !ok || len(v) == 0 {
					return "", false
				}
				return v[0], true
			})
			for name, v := range form.chosen() {
				if !validStep(v, append([]MapGenOption{}, selectOptionsFor(form, name)...)) {
					t.Errorf("%s %s: %s holds %q, which is not one of its options", line, preset, name, v)
				}
			}
		}
	}
}

func selectOptionsFor(f MapGenForm, name string) []MapGenOption {
	if f.StartingArea.Name == name {
		return f.StartingArea.Options
	}
	groups := []MapGenGroup{f.Water, f.Cliffs}
	for _, p := range f.Planets {
		groups = append(groups, p.Groups...)
	}
	for _, g := range groups {
		for _, s := range g.Selects {
			if s.Name == name {
				return s.Options
			}
		}
	}
	return nil
}

func TestPresetPrefillShowsWhatThePresetChanged(t *testing.T) {
	rail := NewMapGenFormForPreset("2.1", true, "rail-world")
	iron := selectNamed(t, rail, fieldName("nauvis", "iron-ore", "size"))
	if iron.Value != "300" {
		t.Errorf("rail world iron size = %s, want 300", iron.Value)
	}
	freq := selectNamed(t, rail, fieldName("nauvis", "iron-ore", "frequency"))
	if freq.Value != "33" {
		t.Errorf("rail world iron frequency = %s, want 33", freq.Value)
	}
	water := selectNamed(t, rail, fieldName("nauvis", "water", "size"))
	if water.Value != "150" || !hasOption(water, "150") {
		t.Errorf("rail world water coverage = %s, options %v: want 150 offered and chosen", water.Value, water.Options)
	}
	if scale := selectNamed(t, rail, fieldName("nauvis", "water", "frequency")); scale.Value != "200" {
		t.Errorf("rail world water scale = %s, want 200 (the inverse of frequency 0.5)", scale.Value)
	}
	// An ordinary form does not offer the extra step.
	if hasOption(selectNamed(t, NewMapGenForm("2.1", true), fieldName("nauvis", "water", "size")), "150") {
		t.Error("150% is offered on a form that is not filled from a preset")
	}

	ribbon := NewMapGenFormForPreset("2.1", true, "ribbon-world")
	if ribbon.Height.Value != "128" || ribbon.StartingArea.Value != "300" {
		t.Errorf("ribbon world height %s and starting area %s, want 128 and 300", ribbon.Height.Value, ribbon.StartingArea.Value)
	}
	island := NewMapGenFormForPreset("2.1", true, "island")
	if !strings.Contains(island.Raw.Value, `"elevation": "elevation_island"`) {
		t.Errorf("island's terrain type is not in the raw box:\n%s", island.Raw.Value)
	}
	if !strings.Contains(NewMapGenFormForPreset("1.1", false, "island").Raw.Value, `"elevation": "0_17-island"`) {
		t.Error("1.1 island's terrain type is not in the raw box")
	}
	if death := NewMapGenFormForPreset("1.1", false, "death-world"); death.StartingArea.Value != "71" {
		t.Errorf("death world starting area = %s, want 71", death.StartingArea.Value)
	}
	def := NewMapGenFormForPreset("2.1", true, "default")
	if got, want := def.chosen(), NewMapGenForm("2.1", true).chosen(); len(got) != len(want) {
		t.Fatalf("default preset has %d selects, the plain form %d", len(got), len(want))
	} else {
		for k, v := range want {
			if got[k] != v {
				t.Errorf("default preset %s = %s, want %s", k, got[k], v)
			}
		}
	}
}

func selectNamed(t *testing.T, f MapGenForm, name string) MapGenSelect {
	t.Helper()
	for _, o := range [][]MapGenOption{selectOptionsFor(f, name)} {
		if o == nil {
			t.Fatalf("no select %s", name)
		}
	}
	return MapGenSelect{Name: name, Value: f.chosen()[name], Options: selectOptionsFor(f, name)}
}

func hasOption(s MapGenSelect, code string) bool {
	for _, o := range s.Options {
		if o.Value == code {
			return true
		}
	}
	return false
}

// Only a refresh that the preset select started refills the form; every
// other request keeps what the person typed (SPEC 5.6).
func TestOnlyThePresetSelectRefillsTheForm(t *testing.T) {
	post := func(trigger string) MapGenForm {
		v := url.Values{"preset": {"death-world"}, mgStart: {"200"}, mgSeed: {"7"}}
		r := httptest.NewRequest(http.MethodPost, "/generate/refresh", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if trigger != "" {
			r.Header.Set("HX-Trigger", trigger)
		}
		f, _, _ := ParseMapGenForm(r, "2.1", true)
		return f
	}
	if f := post(""); f.StartingArea.Value != "200" || !f.FromPreset.IsZero() {
		t.Errorf("a plain post changed the form: starting area %s, note %v", f.StartingArea.Value, f.FromPreset)
	}
	if f := post("generate-version-select"); f.StartingArea.Value != "200" {
		t.Errorf("another element's post changed the form: %s", f.StartingArea.Value)
	}
	f := post(PresetSelectID)
	if f.StartingArea.Value != "71" || f.Seed.Value != "7" || f.FromPreset.IsZero() {
		t.Errorf("the preset select's post: starting area %s, seed %q, note %v; want 71, 7 and a note", f.StartingArea.Value, f.Seed.Value, f.FromPreset)
	}
	// An unknown preset changes nothing.
	r := httptest.NewRequest(http.MethodPost, "/generate/refresh", strings.NewReader(url.Values{"preset": {"nope"}, mgStart: {"200"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("HX-Trigger", PresetSelectID)
	if f, _, _ := ParseMapGenForm(r, "2.1", true); f.StartingArea.Value != "200" {
		t.Errorf("an unknown preset changed the form: %s", f.StartingArea.Value)
	}
}

// DESIGN 8.7: the game words terrain controls as Scale and Coverage.
func TestTerrainControlsAreWordedAsTheGameWordsThem(t *testing.T) {
	f := NewMapGenForm("2.1", true)
	for _, p := range f.Planets {
		for _, g := range p.Groups {
			for _, s := range g.Selects {
				if strings.HasSuffix(s.Name, ".trees.frequency") && s.Label.Key != "mapgen.setting.scale" {
					t.Errorf("trees %s is labelled %s, want scale", s.Name, s.Label.Key)
				}
				if strings.HasSuffix(s.Name, ".trees.size") && s.Label.Key != "mapgen.setting.coverage" {
					t.Errorf("trees %s is labelled %s, want coverage", s.Name, s.Label.Key)
				}
				if strings.HasSuffix(s.Name, ".iron-ore.frequency") && s.Label.Key != "mapgen.setting.frequency" {
					t.Errorf("iron %s is labelled %s, want frequency", s.Name, s.Label.Key)
				}
			}
		}
	}
	// A scale of 200% is a stored frequency of 0.5, for trees as for water.
	v := url.Values{fieldName("nauvis", "trees", "frequency"): {"200"}}
	_, out, problems := parseMapGenValues(v, "2.1", false)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	var doc struct {
		Controls map[string]struct{ Frequency float64 } `json:"autoplace_controls"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Controls["trees"].Frequency != 0.5 {
		t.Errorf("trees scale 200%% stored as frequency %v, want 0.5", doc.Controls["trees"].Frequency)
	}
}
