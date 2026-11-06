package mapgen

// Which new-map settings disable achievements (SPEC 5.6). The rules are the
// game's own text: each setting's tooltip in core/locale/en/core.cfg ("Note:
// ... will disable some achievements") and the reasons the game lists when it
// warns before a new game ([achievement-progress] not-in-..., not-with-...).
// The game does this check in its client, so it cannot be tested headless;
// the table follows the text.

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Blocker is one setting that disables achievements: the dotted path of the
// field in map-gen-settings.json or map-settings.json, and the message key of
// the game's reason.
type Blocker struct {
	Field   string
	Message string
}

// File a rule reads.
const (
	inMapGen      = "map-gen"
	inMapSettings = "map-settings"
)

// How a rule compares the value. "Default" is the game's own value for a new
// map on the exact version: DefaultMapGen, and MapSettings with the "default"
// preset.
const (
	isTrue       = "true"
	isFalse      = "false"
	belowDefault = "below-default"
	aboveDefault = "above-default"
	aboveOne     = "above-100%"
	belowOne     = "below-100%"
)

// achievementKey prefixes the game's reason id to make a message key.
const achievementKey = "mapgen.achievement."

type achievementRule struct {
	file   string
	field  string
	test   string
	reason string // the game's reason id
}

// rules11 is 1.1's only setting besides the enemy controls.
var rules11 = []achievementRule{
	{inMapGen, "peaceful_mode", isTrue, "not-in-peaceful-mode"},
}

// rules2x are the 2.0 and 2.1 settings both lines share.
var rules2x = []achievementRule{
	{inMapGen, "peaceful_mode", isTrue, "not-in-peaceful-mode"},
	{inMapGen, "no_enemies_mode", isTrue, "not-in-no-enemies-mode"},
	// starting-area-size-description: "Making the area larger".
	{inMapGen, "starting_area", aboveDefault, "not-with-bigger-starting-area"},
	// not-with-extra-trees: "when tree spawning is higher than default". The
	// game does not say which of the trees control's values it reads, so both
	// the form shows are checked.
	{inMapGen, "autoplace_controls.trees.frequency", aboveDefault, "not-with-extra-trees"},
	{inMapGen, "autoplace_controls.trees.size", aboveDefault, "not-with-extra-trees"},

	{inMapSettings, "pollution.enabled", isFalse, "not-with-easier-pollution-settings"},
	// The descriptions below say "lower (or higher) than __1__"; the game
	// fills __1__ in at run time, read here as the default value.
	{inMapSettings, "pollution.diffusion_ratio", belowDefault, "not-with-easier-pollution-settings"},
	// pollution-absorption-modifier-description: "higher than 100%". The
	// absorption modifier is pollution.ageing in the file.
	{inMapSettings, "pollution.ageing", aboveOne, "not-with-easier-pollution-settings"},
	{inMapSettings, "pollution.enemy_attack_pollution_consumption_modifier", aboveOne, "not-with-easier-pollution-settings"},
	{inMapSettings, "pollution.min_pollution_to_damage_trees", aboveDefault, "not-with-easier-pollution-settings"},
	{inMapSettings, "pollution.pollution_per_tree_damage", aboveDefault, "not-with-easier-pollution-settings"},

	{inMapSettings, "enemy_evolution.enabled", isFalse, "not-with-easier-enemy-evolution-settings"},
	{inMapSettings, "enemy_evolution.time_factor", belowDefault, "not-with-easier-enemy-evolution-settings"},
	{inMapSettings, "enemy_evolution.destroy_factor", belowDefault, "not-with-easier-enemy-evolution-settings"},
	{inMapSettings, "enemy_evolution.pollution_factor", belowDefault, "not-with-easier-enemy-evolution-settings"},

	{inMapSettings, "enemy_expansion.enabled", isFalse, "not-with-easier-enemy-expansion-settings"},
	{inMapSettings, "enemy_expansion.max_expansion_distance", belowDefault, "not-with-easier-enemy-expansion-settings"},
	{inMapSettings, "enemy_expansion.min_expansion_cooldown", aboveDefault, "not-with-easier-enemy-expansion-settings"},
	{inMapSettings, "enemy_expansion.max_expansion_cooldown", aboveDefault, "not-with-easier-enemy-expansion-settings"},
}

// Expansion settings that changed between 2.0 and 2.1: 2.1 replaced the group
// sizes with a minimum distance and an evolution group size factor.
var rules20 = []achievementRule{
	{inMapSettings, "enemy_expansion.settler_group_min_size", belowDefault, "not-with-easier-enemy-expansion-settings"},
	{inMapSettings, "enemy_expansion.settler_group_max_size", belowDefault, "not-with-easier-enemy-expansion-settings"},
}

var rules21 = []achievementRule{
	{inMapSettings, "enemy_expansion.min_expansion_distance", aboveDefault, "not-with-easier-enemy-expansion-settings"},
	{inMapSettings, "enemy_expansion.evolution_group_size_factor", belowDefault, "not-with-easier-enemy-expansion-settings"},
}

// enemyRules covers each enemy autoplace control on the line
// (enemy-frequency-description, enemy-size-description: "A value lower than
// 100% will disable some achievements"): enemy-base, and on 2.x Gleba's, which the game marks related_to_fight_achievements
// (autoplace-related-to-fight-tooltip: "Value lower than 100%").
func enemyRules(line string) []achievementRule {
	seen := map[string]bool{}
	var out []achievementRule
	for _, p := range parsed[line].Planets {
		for _, c := range p.Controls {
			if c.Category != "enemy" || seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			for _, v := range []string{"frequency", "size"} {
				out = append(out, achievementRule{inMapGen, "autoplace_controls." + c.Name + "." + v, belowOne, "not-with-lower-enemies"})
			}
		}
	}
	return out
}

// AchievementBlockers returns, in the table's order, the settings in a
// map-gen-settings.json and a map-settings.json that disable achievements on
// an exact game version. It takes the version, not the line, because a
// default the rules compare with can change inside a line (2.1.9's
// evolution_group_size_factor is 4, 2.1.24's is 8). A field missing from
// either file is the game's default and blocks nothing; either file may be
// nil. Map settings on a version with no tested layout are refused with
// ErrUntestedLayout, as MapSettings refuses them.
func AchievementBlockers(version string, mapGen, mapSettings []byte) ([]Blocker, error) {
	line, ok := Line(version)
	if !ok {
		return nil, fmt.Errorf("mapgen: %q is not a supported game version", version)
	}
	have := map[string]map[string]any{}
	defs := map[string]map[string]any{}
	var err error
	if have[inMapGen], err = flatObject(mapGen); err != nil {
		return nil, fmt.Errorf("mapgen: map-gen settings: %w", err)
	}
	if defs[inMapGen], err = flatObject(DefaultMapGen(line)); err != nil {
		return nil, fmt.Errorf("mapgen: %w", err)
	}
	if len(mapSettings) > 0 {
		if have[inMapSettings], err = flatObject(mapSettings); err != nil {
			return nil, fmt.Errorf("mapgen: map settings: %w", err)
		}
		b, err := MapSettings(version, "default", nil)
		if err != nil {
			return nil, err
		}
		if defs[inMapSettings], err = flatObject(b); err != nil {
			return nil, fmt.Errorf("mapgen: %w", err)
		}
	}
	var out []Blocker
	for _, r := range achievementRules(line) {
		v, ok := have[r.file][r.field]
		if ok && blocks(r, v, defs[r.file]) {
			out = append(out, Blocker{Field: r.field, Message: achievementKey + strings.ReplaceAll(r.reason, "-", "_")})
		}
	}
	return out, nil
}

// flatObject reads a JSON object as its dotted paths; empty input is none.
func flatObject(b []byte) (map[string]any, error) {
	if len(b) == 0 {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return leaves(m, ""), nil
}

func blocks(r achievementRule, v any, defs map[string]any) bool {
	switch r.test {
	case isTrue:
		return v == true
	case isFalse:
		return v == false
	}
	n, ok := v.(float64)
	if !ok {
		return false
	}
	def := 1.0
	if r.test == belowDefault || r.test == aboveDefault {
		// A control value the default file leaves out is 1, as the game
		// reads a missing one.
		if d, ok := defs[r.field].(float64); ok {
			def = d
		}
	}
	// Relative, to allow float32 rounding: the game stores some as float.
	tol := 1e-6 * math.Abs(def)
	if r.test == belowDefault || r.test == belowOne {
		return n < def-tol
	}
	return n > def+tol
}

func achievementRules(line string) []achievementRule {
	out := enemyRules(line)
	switch line {
	case "1.1":
		out = append(out, rules11...)
	case "2.0":
		out = append(append(out, rules2x...), rules20...)
	case "2.1":
		out = append(append(out, rules2x...), rules21...)
	}
	return out
}

// AchievementRule is one setting's line, for a form that warns as soon as a
// value crosses it: the file and dotted path of the setting, how it is
// compared, the value it is compared with, and the reason's message key.
type AchievementRule struct {
	// MapSettings is true for a value in map-settings.json, false for one in
	// map-gen-settings.json.
	MapSettings bool
	Field       string
	// Test is "true" or "false" (the setting blocks when it is that), or
	// "below" or "above" (it blocks when it is beyond Limit by more than the
	// game's float rounding).
	Test    string
	Limit   float64
	Message string
}

// AchievementRules lists the rules AchievementBlockers applies on an exact
// game version, each with the value it compares with. On a version with no
// tested map-settings layout only the map-gen rules are listed.
func AchievementRules(version string) ([]AchievementRule, error) {
	line, ok := Line(version)
	if !ok {
		return nil, fmt.Errorf("mapgen: %q is not a supported game version", version)
	}
	defs := map[string]map[string]any{}
	var err error
	if defs[inMapGen], err = flatObject(DefaultMapGen(line)); err != nil {
		return nil, fmt.Errorf("mapgen: %w", err)
	}
	if b, err := MapSettings(version, "default", nil); err == nil {
		if defs[inMapSettings], err = flatObject(b); err != nil {
			return nil, fmt.Errorf("mapgen: %w", err)
		}
	}
	var out []AchievementRule
	for _, r := range achievementRules(line) {
		d, known := defs[r.file]
		if !known {
			continue
		}
		rule := AchievementRule{MapSettings: r.file == inMapSettings, Field: r.field, Limit: 1,
			Message: achievementKey + strings.ReplaceAll(r.reason, "-", "_")}
		switch r.test {
		case isTrue, isFalse:
			rule.Test = r.test
		case belowDefault, belowOne:
			rule.Test = "below"
		default:
			rule.Test = "above"
		}
		if r.test == belowDefault || r.test == aboveDefault {
			if v, ok := d[r.field].(float64); ok {
				rule.Limit = v
			}
		}
		out = append(out, rule)
	}
	return out, nil
}

// Blocks reports whether a value breaks the rule, with the tolerance
// AchievementBlockers uses.
func (r AchievementRule) Blocks(v any) bool {
	switch r.Test {
	case isTrue:
		return v == true
	case isFalse:
		return v == false
	}
	n, ok := v.(float64)
	if !ok {
		return false
	}
	tol := 1e-6 * math.Abs(r.Limit)
	if r.Test == "below" {
		return n < r.Limit-tol
	}
	return n > r.Limit+tol
}
