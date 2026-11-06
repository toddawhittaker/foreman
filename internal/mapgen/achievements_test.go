package mapgen

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const (
	keyPeaceful  = "mapgen.achievement.not_in_peaceful_mode"
	keyNoEnemies = "mapgen.achievement.not_in_no_enemies_mode"
	keyLower     = "mapgen.achievement.not_with_lower_enemies"
	keyTrees     = "mapgen.achievement.not_with_extra_trees"
	keyStart     = "mapgen.achievement.not_with_bigger_starting_area"
	keyPollution = "mapgen.achievement.not_with_easier_pollution_settings"
	keyExpansion = "mapgen.achievement.not_with_easier_enemy_expansion_settings"
	keyEvolution = "mapgen.achievement.not_with_easier_enemy_evolution_settings"
)

func versionOf(line string) string { return parsed[line].GameVersion }

func blockersFor(t *testing.T, line string, mapGen map[string]any, settings map[string]any, preset string) []Blocker {
	t.Helper()
	var mg []byte
	if mapGen != nil {
		var err error
		if mg, err = json.Marshal(mapGen); err != nil {
			t.Fatal(err)
		}
	}
	ms, err := MapSettings(versionOf(line), preset, settings)
	if err != nil {
		t.Fatal(err)
	}
	got, err := AchievementBlockers(versionOf(line), mg, ms)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The game's own new map keeps achievements on every line, with or without
// the map-gen file.
func TestDefaultsBlockNothing(t *testing.T) {
	for _, line := range Lines {
		ms, err := MapSettings(versionOf(line), "default", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, mg := range [][]byte{nil, DefaultMapGen(line)} {
			got, err := AchievementBlockers(versionOf(line), mg, ms)
			if err != nil || len(got) != 0 {
				t.Errorf("%s: defaults give %v, %v", line, got, err)
			}
		}
		if got, err := AchievementBlockers(versionOf(line), nil, nil); err != nil || len(got) != 0 {
			t.Errorf("%s: no files give %v, %v", line, got, err)
		}
	}
}

// Presets: rail world turns expansion off and slows evolution (the wiki
// names it as one that disables achievements); ribbon world's starting area
// is 3 on 2.x, where a larger area counts; death world, marathon and the
// others only make the game harder or leave the checked values alone.
func TestPresetBlockers(t *testing.T) {
	for _, line := range Lines {
		for _, preset := range Presets(line) {
			var mg map[string]any
			if err := json.Unmarshal(PresetMapGen(line, preset), &mg); err != nil {
				t.Fatal(err)
			}
			got := blockersFor(t, line, mg, nil, preset)
			var want []Blocker
			if preset == "rail-world" && line != "1.1" {
				want = []Blocker{
					{"enemy_evolution.time_factor", keyEvolution},
					{"enemy_expansion.enabled", keyExpansion},
				}
			}
			if preset == "ribbon-world" && line != "1.1" {
				want = []Blocker{{"starting_area", keyStart}}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s %s: got %v, want %v", line, preset, got, want)
			}
		}
	}
}

func TestMapGenBlockers(t *testing.T) {
	enemy := func(name string, f, s float64) map[string]any {
		return map[string]any{"autoplace_controls": map[string]any{name: map[string]any{"frequency": f, "size": s, "richness": 1}}}
	}
	cases := []struct {
		line string
		mg   map[string]any
		want []Blocker
	}{
		{"1.1", enemy("enemy-base", 0.5, 1), []Blocker{{"autoplace_controls.enemy-base.frequency", keyLower}}},
		{"1.1", enemy("enemy-base", 1, 0.99), []Blocker{{"autoplace_controls.enemy-base.size", keyLower}}},
		{"1.1", enemy("enemy-base", 3, 3), nil},
		{"1.1", map[string]any{"peaceful_mode": true}, []Blocker{{"peaceful_mode", keyPeaceful}}},
		// 1.1 names no starting area, trees or no-enemies rule.
		{"1.1", map[string]any{"starting_area": 3, "no_enemies_mode": true, "autoplace_controls": map[string]any{"trees": map[string]any{"frequency": 3, "size": 3}}}, nil},
		{"2.0", map[string]any{"starting_area": 1.5}, []Blocker{{"starting_area", keyStart}}},
		{"2.1", map[string]any{"starting_area": 0.5}, nil},
		{"2.1", map[string]any{"no_enemies_mode": true, "peaceful_mode": false}, []Blocker{{"no_enemies_mode", keyNoEnemies}}},
		{"2.1", map[string]any{"autoplace_controls": map[string]any{"trees": map[string]any{"size": 2}}}, []Blocker{{"autoplace_controls.trees.size", keyTrees}}},
		{"2.0", map[string]any{"autoplace_controls": map[string]any{"trees": map[string]any{"frequency": 2}}}, []Blocker{{"autoplace_controls.trees.frequency", keyTrees}}},
		{"2.1", map[string]any{"autoplace_controls": map[string]any{"trees": map[string]any{"size": 0.5}}}, nil},
		// Gleba's enemies count like Nauvis's (related_to_fight_achievements).
		{"2.1", enemy("gleba_enemy_base", 1, 0.5), []Blocker{{"autoplace_controls.gleba_enemy_base.size", keyLower}}},
		{"2.0", enemy("gleba_enemy_base", 0.5, 1), []Blocker{{"autoplace_controls.gleba_enemy_base.frequency", keyLower}}},
	}
	for _, c := range cases {
		if got := blockersFor(t, c.line, c.mg, nil, "default"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %v: got %v, want %v", c.line, c.mg, got, c.want)
		}
	}
}

func TestMapSettingsBlockers(t *testing.T) {
	cases := []struct {
		line string
		ms   map[string]any
		want []Blocker
	}{
		// 1.1 checks no map setting at all.
		{"1.1", map[string]any{"pollution.enabled": false, "enemy_expansion.enabled": false, "enemy_evolution.time_factor": 0.0}, nil},
		{"2.0", map[string]any{"pollution.enabled": false}, []Blocker{{"pollution.enabled", keyPollution}}},
		{"2.1", map[string]any{"pollution.diffusion_ratio": 0.01}, []Blocker{{"pollution.diffusion_ratio", keyPollution}}},
		{"2.1", map[string]any{"pollution.diffusion_ratio": 0.05}, nil},
		{"2.1", map[string]any{"pollution.ageing": 1.5}, []Blocker{{"pollution.ageing", keyPollution}}},
		{"2.0", map[string]any{"pollution.enemy_attack_pollution_consumption_modifier": 2}, []Blocker{{"pollution.enemy_attack_pollution_consumption_modifier", keyPollution}}},
		{"2.1", map[string]any{"pollution.min_pollution_to_damage_trees": 100}, []Blocker{{"pollution.min_pollution_to_damage_trees", keyPollution}}},
		{"2.1", map[string]any{"pollution.pollution_per_tree_damage": 100}, []Blocker{{"pollution.pollution_per_tree_damage", keyPollution}}},
		{"2.1", map[string]any{"enemy_evolution.enabled": false}, []Blocker{{"enemy_evolution.enabled", keyEvolution}}},
		{"2.0", map[string]any{"enemy_evolution.destroy_factor": 0.001}, []Blocker{{"enemy_evolution.destroy_factor", keyEvolution}}},
		{"2.1", map[string]any{"enemy_evolution.pollution_factor": 1e-07}, []Blocker{{"enemy_evolution.pollution_factor", keyEvolution}}},
		{"2.1", map[string]any{"enemy_evolution.time_factor": 8e-06}, nil},
		{"2.1", map[string]any{"enemy_expansion.max_expansion_distance": 2}, []Blocker{{"enemy_expansion.max_expansion_distance", keyExpansion}}},
		{"2.0", map[string]any{"enemy_expansion.max_expansion_cooldown": 300000}, []Blocker{{"enemy_expansion.max_expansion_cooldown", keyExpansion}}},
		// 2.1's own default is 36000, above its example file's 14400.
		{"2.1", map[string]any{"enemy_expansion.min_expansion_cooldown": 36000}, nil},
		{"2.1", map[string]any{"enemy_expansion.min_expansion_cooldown": 40000}, []Blocker{{"enemy_expansion.min_expansion_cooldown", keyExpansion}}},
		{"2.0", map[string]any{"enemy_expansion.min_expansion_cooldown": 20000}, []Blocker{{"enemy_expansion.min_expansion_cooldown", keyExpansion}}},
		// The 2.0 and 2.1 expansion differences.
		{"2.0", map[string]any{"enemy_expansion.settler_group_min_size": 2}, []Blocker{{"enemy_expansion.settler_group_min_size", keyExpansion}}},
		{"2.0", map[string]any{"enemy_expansion.settler_group_max_size": 5}, []Blocker{{"enemy_expansion.settler_group_max_size", keyExpansion}}},
		{"2.1", map[string]any{"enemy_expansion.settler_group_max_size": 5}, nil},
		{"2.1", map[string]any{"enemy_expansion.min_expansion_distance": 4}, []Blocker{{"enemy_expansion.min_expansion_distance", keyExpansion}}},
		{"2.1", map[string]any{"enemy_expansion.evolution_group_size_factor": 4}, []Blocker{{"enemy_expansion.evolution_group_size_factor", keyExpansion}}},
	}
	for _, c := range cases {
		if got := blockersFor(t, c.line, nil, c.ms, "default"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %v: got %v, want %v", c.line, c.ms, got, c.want)
		}
	}
}

// Thresholds are the exact version's defaults: 2.1.9's evolution group size
// factor is 4, 2.1.24's is 8.
func TestThresholdsFollowTheVersion(t *testing.T) {
	o := map[string]any{"enemy_expansion.evolution_group_size_factor": 6}
	for version, want := range map[string]int{"2.1.9": 0, "2.1.24": 1} {
		ms, err := MapSettings(version, "default", o)
		if err != nil {
			t.Fatal(err)
		}
		got, err := AchievementBlockers(version, nil, ms)
		if err != nil || len(got) != want {
			t.Errorf("%s: got %v, %v; want %d blockers", version, got, err, want)
		}
	}
	// No tested layout: map-gen still answers, map settings are refused.
	got, err := AchievementBlockers("2.1.15", []byte(`{"peaceful_mode":true}`), nil)
	if err != nil || !reflect.DeepEqual(got, []Blocker{{"peaceful_mode", keyPeaceful}}) {
		t.Errorf("2.1.15 map-gen: got %v, %v", got, err)
	}
	if _, err := AchievementBlockers("2.1.15", nil, []byte(`{}`)); !errors.Is(err, ErrUntestedLayout) {
		t.Errorf("2.1.15 map settings: want ErrUntestedLayout, got %v", err)
	}
}

// Every map-settings rule names a field the line's file really has, so a
// renamed key cannot silently stop a warning.
func TestRulesNameRealFields(t *testing.T) {
	for _, line := range Lines {
		ms := mustMapSettings(t, versionOf(line), "default", nil)
		for _, r := range achievementRules(line) {
			if r.file == inMapSettings {
				if _, ok := ms[r.field]; !ok {
					t.Errorf("%s: no map setting %s", line, r.field)
				}
			}
		}
	}
}

func TestAchievementBlockersRefusesBadInput(t *testing.T) {
	if _, err := AchievementBlockers("2.1", nil, nil); err == nil {
		t.Error("a line instead of a version was accepted")
	}
	if _, err := AchievementBlockers("2.1.24", []byte("{"), nil); err == nil {
		t.Error("bad map-gen JSON was accepted")
	}
	if _, err := AchievementBlockers("2.1.24", nil, []byte("[]")); err == nil {
		t.Error("map settings that are not an object were accepted")
	}
}

// The rules a form warns with give the same answer as AchievementBlockers for
// the value each rule names, on every line.
func TestAchievementRulesMatchBlockers(t *testing.T) {
	for _, line := range Lines {
		rules, err := AchievementRules(versionOf(line))
		if err != nil || len(rules) == 0 {
			t.Fatalf("%s: %v, %v", line, rules, err)
		}
		for _, r := range rules {
			probes := []any{r.Limit, r.Limit * 0.5, r.Limit * 2}
			if r.Test == "true" || r.Test == "false" {
				probes = []any{true, false}
			}
			for _, p := range probes {
				mg, ms := map[string]any{}, map[string]any{}
				if r.MapSettings {
					ms[r.Field] = p
				} else {
					put(mg, r.Field, p)
				}
				has := false
				for _, b := range blockersFor(t, line, mg, ms, "default") {
					has = has || (b.Field == r.Field && b.Message == r.Message)
				}
				if has != r.Blocks(p) {
					t.Errorf("%s %s=%v: the rule says %t, the blockers %t", line, r.Field, p, r.Blocks(p), has)
				}
			}
		}
	}
	// An untested layout lists only the map-gen rules.
	rules, err := AchievementRules("2.1.15")
	if err != nil || len(rules) == 0 {
		t.Fatalf("2.1.15: %v, %v", rules, err)
	}
	for _, r := range rules {
		if r.MapSettings {
			t.Errorf("2.1.15 lists a map-settings rule: %+v", r)
		}
	}
}

// put sets a dotted path in a nested map.
func put(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = v
}
