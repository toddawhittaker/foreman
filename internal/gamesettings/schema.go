package gamesettings

import "math"

// File names Validate knows the keys of.
const (
	FileServerSettings = "server-settings.json"
	FileMapGen         = "map-gen-settings.json"
	FileMapSettings    = "map-settings.json"
)

// Game lines Validate has key tables for.
const (
	Line11 = "1.1"
	Line20 = "2.0"
	Line21 = "2.1"
)

const maxUint32 = 4294967295

// spec says what one value may be. The tables below come from the example
// files shipped in the factoriotools images 1.1.110, 2.0.77 and 2.1.24.
type spec struct {
	types   uint8 // bit set of 1<<kind
	integer bool
	ranged  bool
	lo, hi  float64
	enum    []string
	fields  map[string]*spec // known keys of an object
	elem    *spec            // every array element, or every member of an object without fields
}

func typeBit(k kind) uint8 { return 1 << k }

var (
	boolean = &spec{types: typeBit(kBool)}
	text    = &spec{types: typeBit(kString)}
	// scalar is the value of a property expression: the game takes text or a number.
	scalar = &spec{types: typeBit(kString) | typeBit(kNumber) | typeBit(kBool)}
)

func whole(lo, hi float64) *spec {
	return &spec{types: typeBit(kNumber), integer: true, ranged: true, lo: lo, hi: hi}
}

// uint32s is a whole number the game stores in 32 bits.
func uint32s() *spec { return whole(0, maxUint32) }

// atLeast0 is any non-negative number.
func atLeast0() *spec {
	return &spec{types: typeBit(kNumber), ranged: true, lo: 0, hi: math.Inf(1)}
}

func number() *spec { return &spec{types: typeBit(kNumber)} }

func between(lo, hi float64) *spec {
	return &spec{types: typeBit(kNumber), ranged: true, lo: lo, hi: hi}
}

func list(elem *spec) *spec { return &spec{types: typeBit(kArray), elem: elem} }

func object(fields map[string]*spec) *spec {
	return &spec{types: typeBit(kObject), fields: fields}
}

// mapOf is an object whose members all share one spec.
func mapOf(elem *spec) *spec { return &spec{types: typeBit(kObject), elem: elem} }

// withKeys sets every name to the same spec.
func withKeys(fields map[string]*spec, s *spec, names ...string) map[string]*spec {
	for _, n := range names {
		fields[n] = s
	}
	return fields
}

// schemaFor returns the table for a file on a game line. ok is false for a
// file or line without one; such input gets only the checks that need no table.
func schemaFor(file, line string) (*spec, bool) {
	switch line {
	case Line11, Line20, Line21:
	default:
		return nil, false
	}
	switch file {
	case FileServerSettings:
		return serverSettings(line), true
	case FileMapGen:
		return mapGenSettings(line), true
	case FileMapSettings:
		return mapSettings(line), true
	}
	return nil, false
}

func serverSettings(line string) *spec {
	f := map[string]*spec{
		"visibility":                object(map[string]*spec{"public": boolean, "lan": boolean}),
		"tags":                      list(text),
		"max_heartbeats_per_second": whole(6, 240),
		// The example file shows text; the game also documents true and false.
		"allow_commands": {
			types: typeBit(kString) | typeBit(kBool),
			enum:  []string{"true", "false", "admins-only"},
		},
	}
	withKeys(f, text, "name", "description", "username", "password", "token", "game_password")
	withKeys(f, boolean,
		"require_user_verification", "ignore_player_limit_for_returning_players",
		"auto_pause", "only_admins_can_pause_the_game", "autosave_only_on_server", "non_blocking_saving")
	withKeys(f, uint32s(),
		"max_players", "max_upload_in_kilobytes_per_second", "max_upload_slots",
		"minimum_latency_in_ticks", "autosave_interval", "autosave_slots", "afk_autokick_interval",
		"minimum_segment_size", "minimum_segment_size_peer_count",
		"maximum_segment_size", "maximum_segment_size_peer_count")
	if line == Line21 {
		f["auto_pause_when_players_connect"] = boolean
	}
	return object(f)
}

func mapGenSettings(line string) *spec {
	control := object(withKeys(map[string]*spec{}, atLeast0(), "frequency", "size", "richness"))
	f := map[string]*spec{
		"width":                     uint32s(),
		"height":                    uint32s(),
		"starting_area":             atLeast0(),
		"peaceful_mode":             boolean,
		"autoplace_controls":        mapOf(control),
		"property_expression_names": mapOf(scalar),
		"starting_points":           list(object(withKeys(map[string]*spec{}, number(), "x", "y"))),
		// null asks for a random seed.
		"seed": {types: typeBit(kNull) | typeBit(kNumber), integer: true, ranged: true, lo: 0, hi: maxUint32},
		"cliff_settings": object(map[string]*spec{
			"name":                     text,
			"cliff_elevation_0":        number(),
			"cliff_elevation_interval": atLeast0(),
			"richness":                 between(0, 10),
		}),
	}
	if line == Line11 {
		f["terrain_segmentation"] = atLeast0()
		f["water"] = atLeast0()
	}
	return object(f)
}

func mapSettings(line string) *spec {
	old := line == Line11
	pre21 := line != Line21

	difficulty := withKeys(map[string]*spec{}, atLeast0(), "technology_price_multiplier")
	if old {
		difficulty["recipe_difficulty"] = uint32s()
		difficulty["technology_difficulty"] = uint32s()
		difficulty["research_queue_setting"] = text
	} else {
		difficulty["spoil_time_modifier"] = atLeast0()
	}

	pollution := withKeys(map[string]*spec{"enabled": boolean}, atLeast0(),
		"diffusion_ratio", "min_to_diffuse", "ageing", "expected_max_per_chunk",
		"min_to_show_per_chunk", "min_pollution_to_damage_trees", "pollution_with_max_forest_damage",
		"pollution_per_tree_damage", "pollution_restored_per_tree_damage",
		"enemy_attack_pollution_consumption_modifier")
	if pre21 {
		pollution["max_pollution_to_restore_trees"] = atLeast0()
	}

	evolution := withKeys(map[string]*spec{"enabled": boolean}, atLeast0(),
		"time_factor", "destroy_factor", "pollution_factor")

	expansion := withKeys(map[string]*spec{"enabled": boolean}, atLeast0(),
		"max_expansion_distance", "friendly_base_influence_radius", "enemy_building_influence_radius",
		"building_coefficient", "other_base_coefficient", "neighbouring_chunk_coefficient",
		"neighbouring_base_chunk_coefficient", "max_colliding_tiles_coefficient",
		"settler_group_min_size", "settler_group_max_size", "min_expansion_cooldown", "max_expansion_cooldown")
	if old {
		expansion["min_base_spacing"] = atLeast0()
	}
	if line == Line21 {
		withKeys(expansion, atLeast0(),
			"min_expansion_distance", "evolution_group_size_factor", "build_base_unit_dispatch_cooldown")
	}

	unitGroup := withKeys(map[string]*spec{}, atLeast0(),
		"min_group_gathering_time", "max_group_gathering_time", "max_wait_time_for_late_members",
		"max_group_radius", "min_group_radius", "max_member_speedup_when_behind",
		"max_member_slowdown_when_ahead", "max_group_slowdown_factor", "max_group_member_fallback_factor",
		"member_disown_distance", "tick_tolerance_when_member_arrives", "max_gathering_unit_groups",
		"max_unit_group_size")

	pathFinder := withKeys(map[string]*spec{"use_path_cache": boolean}, atLeast0(),
		"fwd2bwd_ratio", "goal_pressure_ratio", "max_steps_worked_per_tick", "max_work_done_per_tick",
		"short_cache_size", "long_cache_size", "short_cache_min_cacheable_distance",
		"short_cache_min_algo_steps_to_cache", "long_cache_min_cacheable_distance",
		"cache_max_connect_to_cache_steps_multiplier", "cache_accept_path_start_distance_ratio",
		"cache_accept_path_end_distance_ratio", "negative_cache_accept_path_start_distance_ratio",
		"negative_cache_accept_path_end_distance_ratio", "cache_path_start_distance_rating_multiplier",
		"cache_path_end_distance_rating_multiplier", "stale_enemy_with_same_destination_collision_penalty",
		"ignore_moving_enemy_collision_distance", "enemy_with_different_destination_collision_penalty",
		"general_entity_collision_penalty", "general_entity_subsequent_collision_penalty",
		"extended_collision_penalty", "max_clients_to_accept_any_new_request",
		"max_clients_to_accept_short_new_request", "direct_distance_to_consider_short_request",
		"short_request_max_steps", "short_request_ratio", "min_steps_to_check_path_find_termination",
		"start_to_goal_cost_multiplier_to_terminate_path_find", "negative_path_cache_delay_interval")
	pathFinder["overload_levels"] = list(atLeast0())
	pathFinder["overload_multipliers"] = list(atLeast0())

	f := map[string]*spec{
		"difficulty_settings":       object(difficulty),
		"pollution":                 object(pollution),
		"enemy_evolution":           object(evolution),
		"enemy_expansion":           object(expansion),
		"unit_group":                object(unitGroup),
		"path_finder":               object(pathFinder),
		"max_failed_behavior_count": atLeast0(),
	}
	if pre21 {
		steer := object(map[string]*spec{
			"radius": atLeast0(), "separation_force": atLeast0(), "separation_factor": atLeast0(),
			"force_unit_fuzzy_goto_behavior": boolean,
		})
		f["steering"] = object(map[string]*spec{"default": steer, "moving": steer})
	}
	if !old {
		f["asteroids"] = object(withKeys(map[string]*spec{}, atLeast0(),
			"spawning_rate", "max_ray_portals_expanded_per_tick"))
	}
	return object(f)
}
