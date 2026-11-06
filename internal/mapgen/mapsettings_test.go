package mapgen

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testedImages are the images whose example map-settings file is embedded,
// one per map-settings layout (SPEC 5.6), with their golden fixtures in
// internal/mapexchange/testdata.
var testedImages = []string{"1.1.110", "2.0.77", "2.1.9", "2.1.24"}

func flatJSON(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	for k, v := range leaves(m, "") {
		if !strings.HasPrefix(k[strings.LastIndex(k, ".")+1:], "_") {
			out[k] = v
		}
	}
	return out
}

func mustMapSettings(t *testing.T, version, preset string, o map[string]any) map[string]any {
	t.Helper()
	b, err := MapSettings(version, preset, o)
	if err != nil {
		t.Fatalf("MapSettings(%s, %s): %v", version, preset, err)
	}
	return flatJSON(t, b)
}

// sameValue allows float32 rounding: the game parses some values as float.
func sameValue(a, b any) bool {
	x, ok1 := a.(float64)
	y, ok2 := b.(float64)
	if ok1 && ok2 {
		return math.Abs(x-y) <= 1e-6*math.Abs(y)
	}
	return reflect.DeepEqual(a, b)
}

func exampleFile(t *testing.T, version string) map[string]any {
	t.Helper()
	line, _ := Line(version)
	b, err := files.ReadFile("data/" + line + "/map-settings-" + version + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return flatJSON(t, b)
}

// The file must hold every key the version knows (F3: the game refuses one
// missing any), and each preset must come out as the game itself makes it
// with only --preset: its golden parse, for every key the example file has.
func TestMapSettingsIsTheGamesOwnPresetInFull(t *testing.T) {
	for _, version := range testedImages {
		line, _ := Line(version)
		example := exampleFile(t, version)
		for _, preset := range Presets(line) {
			got := mustMapSettings(t, version, preset, nil)
			for k := range example {
				if _, ok := got[k]; !ok {
					t.Errorf("%s %s: key %s missing", version, preset, k)
				}
			}
			if len(got) != len(example) {
				t.Errorf("%s %s: %d keys, the example has %d", version, preset, len(got), len(example))
			}
			names := []string{preset}
			if line != "1.1" {
				names = append(names, preset+"_space-age")
			}
			for _, name := range names {
				b, err := os.ReadFile(filepath.Join("..", "mapexchange", "testdata", version, name+".json")) //nolint:gosec // a fixed test fixture
				if err != nil {
					t.Fatal(err)
				}
				var g struct {
					MapSettings json.RawMessage `json:"map_settings"`
				}
				if err := json.Unmarshal(b, &g); err != nil {
					t.Fatal(err)
				}
				for k, want := range flatJSON(t, g.MapSettings) {
					if _, inFile := example[k]; !inFile {
						continue
					}
					if !sameValue(got[k], want) {
						t.Errorf("%s %s: %s = %v, the game makes %v", version, name, k, got[k], want)
					}
				}
			}
		}
	}
}

// Values read back from a live game made with --create and these presets
// (a probe mod logging game.map_settings on 1.1.110, 2.0.77, 2.1.9 and
// 2.1.24). They show where the game differs from its own example file.
func TestMapSettingsMatchesTheLiveGame(t *testing.T) {
	cases := []struct {
		version, preset, key string
		want                 any
	}{
		{"2.1.24", "default", "enemy_expansion.min_expansion_cooldown", 36000.0},
		{"2.1.9", "default", "enemy_expansion.min_expansion_cooldown", 36000.0},
		{"2.0.77", "default", "enemy_expansion.min_expansion_cooldown", 14400.0},
		{"1.1.110", "default", "difficulty_settings.research_queue_setting", "always"},
		{"1.1.110", "default", "path_finder.max_steps_worked_per_tick", 1000.0},
		{"2.1.24", "death-world", "enemy_evolution.time_factor", 2e-05},
		{"2.1.24", "death-world", "enemy_expansion.build_base_unit_dispatch_cooldown", 600.0},
		{"2.1.24", "default", "enemy_expansion.build_base_unit_dispatch_cooldown", 1800.0},
		{"2.0.77", "death-world", "pollution.ageing", 0.5},
		{"1.1.110", "death-world", "pollution.enemy_attack_pollution_consumption_modifier", 0.5},
		{"1.1.110", "marathon", "difficulty_settings.technology_price_multiplier", 4.0},
		{"2.0.77", "marathon", "difficulty_settings.technology_price_multiplier", 4.0},
		{"2.1.24", "rail-world", "enemy_expansion.enabled", false},
		{"2.1.24", "rail-world", "enemy_evolution.time_factor", 2e-06},
	}
	for _, c := range cases {
		if got := mustMapSettings(t, c.version, c.preset, nil)[c.key]; !sameValue(got, c.want) {
			t.Errorf("%s %s: %s = %v, the game has %v", c.version, c.preset, c.key, got, c.want)
		}
	}
}

func TestMapSettingsFollowsTheLayout(t *testing.T) {
	for _, v := range []string{"2.1.13", "2.1.15", "2.1.17"} {
		if _, err := MapSettings(v, "default", nil); !errors.Is(err, ErrUntestedLayout) {
			t.Errorf("%s: want ErrUntestedLayout, got %v", v, err)
		}
	}
	for _, v := range []string{"1.1", "2.1", "3.0.1", "2.1.x", "", "2.1.07"} {
		if _, err := MapSettings(v, "default", nil); err == nil || errors.Is(err, ErrUntestedLayout) {
			t.Errorf("%q: want a refusal of the version, got %v", v, err)
		}
	}
	// Before 2.1.13 the file has max_pollution_to_restore_trees and no
	// dispatch cooldown; from 2.1.18 it is the other way round.
	old := mustMapSettings(t, "2.1.12", "default", nil)
	now := mustMapSettings(t, "2.1.18", "default", nil)
	if _, ok := old["pollution.max_pollution_to_restore_trees"]; !ok {
		t.Error("2.1.12 needs pollution.max_pollution_to_restore_trees")
	}
	if _, ok := old["enemy_expansion.build_base_unit_dispatch_cooldown"]; ok {
		t.Error("2.1.12 has no dispatch cooldown")
	}
	if _, ok := now["pollution.max_pollution_to_restore_trees"]; ok {
		t.Error("2.1.18 has no max_pollution_to_restore_trees")
	}
	if _, ok := now["enemy_expansion.build_base_unit_dispatch_cooldown"]; !ok {
		t.Error("2.1.18 needs the dispatch cooldown")
	}
	if _, ok := mustMapSettings(t, "2.0.0", "default", nil)["asteroids.spawning_rate"]; !ok {
		t.Error("2.0 needs asteroids")
	}
	if _, ok := mustMapSettings(t, "1.1.0", "default", nil)["asteroids.spawning_rate"]; ok {
		t.Error("1.1 has no asteroids")
	}
}

func TestMapSettingsLayoutNames(t *testing.T) {
	cases := map[string]string{
		"1.1.110": "1.1.0", "2.0.77": "2.0.0", "2.1.0": "2.1.0", "2.1.12": "2.1.0",
		"2.1.13": "2.1.13", "2.1.17": "2.1.13", "2.1.18": "2.1.18", "2.1.24": "2.1.18",
	}
	for v, want := range cases {
		if got, ok := MapSettingsLayout(v); !ok || got != want {
			t.Errorf("%s: got %q %v, want %q", v, got, ok, want)
		}
	}
	for _, v := range []string{"3.0.0", "2.1", "x"} {
		if _, ok := MapSettingsLayout(v); ok {
			t.Errorf("%s: want false", v)
		}
	}
}

func TestMapSettingsOverrides(t *testing.T) {
	got := mustMapSettings(t, "2.1.24", "death-world", map[string]any{
		"enemy_evolution.time_factor":                     1e-05,
		"enemy_expansion.enabled":                         false,
		"max_failed_behavior_count":                       5,
		"pollution.diffusion_ratio":                       json.Number("0.05"),
		"difficulty_settings.technology_price_multiplier": 2.5,
	})
	want := map[string]any{
		"enemy_evolution.time_factor":                     1e-05, // the override beats the preset
		"enemy_expansion.enabled":                         false,
		"max_failed_behavior_count":                       5.0,
		"pollution.diffusion_ratio":                       0.05,
		"pollution.ageing":                                0.5, // the preset's, untouched
		"difficulty_settings.technology_price_multiplier": 2.5,
	}
	for k, w := range want {
		if !sameValue(got[k], w) {
			t.Errorf("%s = %v, want %v", k, got[k], w)
		}
	}

	refused := []map[string]any{
		{"pollution.made_up_key": 1.0},
		{"made_up_section.x": 1.0},
		{"pollution": 1.0},
		{"pollution.enabled.x": true},
		{"pollution._comment": "x"},
		{"pollution.enabled": 1.0},
		{"pollution.diffusion_ratio": "0.05"},
		{"pollution.diffusion_ratio": math.NaN()},
		{"pollution.diffusion_ratio": json.Number("abc")},
		{"enemy_expansion.min_base_spacing": 3.0}, // a 1.1 key
	}
	for _, o := range refused {
		if _, err := MapSettings("2.1.24", "default", o); err == nil {
			t.Errorf("override %v was accepted", o)
		}
	}
	if _, err := MapSettings("2.1.24", "no-such-preset", nil); err == nil {
		t.Error("an unknown preset was accepted")
	}
	if _, err := MapSettings("1.1.110", "lakes", nil); err == nil {
		t.Error("lakes is not a 1.1 preset")
	}
}
