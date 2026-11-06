package web

import "github.com/toddawhittaker/foreman/internal/i18n"

// Names the game shows, in our language. The wording of each language is the
// game's own translation (docs/I18N.md, section 2), so a player sees the same
// word here and in the game.

// controlNames maps an autoplace control to its name. Space Age's copies of
// a resource carry the resource's name; the planet group above them says
// which planet it is.
var controlNames = map[string]i18n.Msg{
	"iron-ore":               i18n.M("mapgen.control.iron_ore"),
	"copper-ore":             i18n.M("mapgen.control.copper_ore"),
	"stone":                  i18n.M("mapgen.control.stone"),
	"coal":                   i18n.M("mapgen.control.coal"),
	"crude-oil":              i18n.M("mapgen.control.crude_oil"),
	"uranium-ore":            i18n.M("mapgen.control.uranium_ore"),
	"water":                  i18n.M("mapgen.control.water"),
	"trees":                  i18n.M("mapgen.control.trees"),
	"rocks":                  i18n.M("mapgen.control.rocks"),
	"enemy-base":             i18n.M("mapgen.control.enemy_base"),
	"nauvis_cliff":           i18n.M("mapgen.control.nauvis_cliff"),
	"starting_area_moisture": i18n.M("mapgen.control.starting_area_moisture"),
	"vulcanus_coal":          i18n.M("mapgen.control.coal"),
	"calcite":                i18n.M("mapgen.control.calcite"),
	"sulfuric_acid_geyser":   i18n.M("mapgen.control.sulfuric_acid_geyser"),
	"tungsten_ore":           i18n.M("mapgen.control.tungsten_ore"),
	"vulcanus_volcanism":     i18n.M("mapgen.control.vulcanus_volcanism"),
	"gleba_stone":            i18n.M("mapgen.control.stone"),
	"gleba_cliff":            i18n.M("mapgen.control.gleba_cliff"),
	"gleba_water":            i18n.M("mapgen.control.gleba_water"),
	"gleba_plants":           i18n.M("mapgen.control.gleba_plants"),
	"gleba_enemy_base":       i18n.M("mapgen.control.gleba_enemy_base"),
	"fulgora_cliff":          i18n.M("mapgen.control.fulgora_cliff"),
	"fulgora_islands":        i18n.M("mapgen.control.fulgora_islands"),
	"scrap":                  i18n.M("mapgen.control.scrap"),
	"aquilo_crude_oil":       i18n.M("mapgen.control.crude_oil"),
	"lithium_brine":          i18n.M("mapgen.control.lithium_brine"),
	"fluorine_vent":          i18n.M("mapgen.control.fluorine_vent"),
}

// controlMsg names a control. A control the table lacks (a game patch added
// one) shows its game name, so the field is never unlabelled.
func controlMsg(name string) i18n.Msg {
	if m, ok := controlNames[name]; ok {
		return m
	}
	return i18n.M("mapgen.control.other", "name", name)
}

var planetNames = map[string]i18n.Msg{
	"nauvis":   i18n.M("mapgen.planet.nauvis"),
	"vulcanus": i18n.M("mapgen.planet.vulcanus"),
	"gleba":    i18n.M("mapgen.planet.gleba"),
	"fulgora":  i18n.M("mapgen.planet.fulgora"),
	"aquilo":   i18n.M("mapgen.planet.aquilo"),
}

func planetMsg(name string) i18n.Msg {
	if m, ok := planetNames[name]; ok {
		return m
	}
	return i18n.M("mapgen.planet.other", "name", name)
}

var presetNames = map[string]i18n.Msg{
	"default":              i18n.M("mapgen.preset.default"),
	"rich-resources":       i18n.M("mapgen.preset.rich_resources"),
	"marathon":             i18n.M("mapgen.preset.marathon"),
	"death-world":          i18n.M("mapgen.preset.death_world"),
	"death-world-marathon": i18n.M("mapgen.preset.death_world_marathon"),
	"rail-world":           i18n.M("mapgen.preset.rail_world"),
	"ribbon-world":         i18n.M("mapgen.preset.ribbon_world"),
	"lakes":                i18n.M("mapgen.preset.lakes"),
	"island":               i18n.M("mapgen.preset.island"),
}

// PresetMsg is the game's name for a preset, for Generate's preset list. A
// preset the table lacks shows its game name.
func PresetMsg(name string) i18n.Msg {
	if m, ok := presetNames[name]; ok {
		return m
	}
	return i18n.M("mapgen.preset.other", "name", name)
}

// achievementMsgs words the game's reasons achievements are turned off, by
// the message key mapgen gives them; the wording is the game's own.
var achievementMsgs = map[string]i18n.Msg{
	"mapgen.achievement.not_in_peaceful_mode":                     i18n.M("mapgen.achievement.not_in_peaceful_mode"),
	"mapgen.achievement.not_in_no_enemies_mode":                   i18n.M("mapgen.achievement.not_in_no_enemies_mode"),
	"mapgen.achievement.not_with_lower_enemies":                   i18n.M("mapgen.achievement.not_with_lower_enemies"),
	"mapgen.achievement.not_with_extra_trees":                     i18n.M("mapgen.achievement.not_with_extra_trees"),
	"mapgen.achievement.not_with_bigger_starting_area":            i18n.M("mapgen.achievement.not_with_bigger_starting_area"),
	"mapgen.achievement.not_with_easier_pollution_settings":       i18n.M("mapgen.achievement.not_with_easier_pollution_settings"),
	"mapgen.achievement.not_with_easier_enemy_expansion_settings": i18n.M("mapgen.achievement.not_with_easier_enemy_expansion_settings"),
	"mapgen.achievement.not_with_easier_enemy_evolution_settings": i18n.M("mapgen.achievement.not_with_easier_enemy_evolution_settings"),
}
