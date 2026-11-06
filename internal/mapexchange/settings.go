package mapexchange

// These types mirror the files the game reads with --map-gen-settings and
// --map-settings (its map-gen-settings.example.json and
// map-settings.example.json). Fields that exist only on some game lines are
// pointers, left out of the JSON when the line has no such field.

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type FrequencySizeRichness struct {
	Frequency F32 `json:"frequency"`
	Size      F32 `json:"size"`
	Richness  F32 `json:"richness"`
}

type AutoplaceSetting struct {
	TreatMissingAsDefault bool                             `json:"treat_missing_as_default"`
	Settings              map[string]FrequencySizeRichness `json:"settings"`
}

type CliffSettings struct {
	Name                   string  `json:"name"`
	Control                *string `json:"control,omitempty"`
	CliffElevation0        F32     `json:"cliff_elevation_0"`
	CliffElevationInterval F32     `json:"cliff_elevation_interval"`
	Richness               F32     `json:"richness"`
	CliffSmoothing         *F32    `json:"cliff_smoothing,omitempty"`
}

type TerritorySettings struct {
	Units                        []string `json:"units"`
	TerritoryIndexExpression     string   `json:"territory_index_expression"`
	TerritoryVariationExpression string   `json:"territory_variation_expression"`
	MinimumTerritorySize         uint32   `json:"minimum_territory_size"`
}

type MapGenSettings struct {
	TerrainSegmentation               *F32                             `json:"terrain_segmentation,omitempty"`
	Water                             *F32                             `json:"water,omitempty"`
	AutoplaceControls                 map[string]FrequencySizeRichness `json:"autoplace_controls"`
	AutoplaceSettings                 map[string]AutoplaceSetting      `json:"autoplace_settings"`
	DefaultEnableAllAutoplaceControls bool                             `json:"default_enable_all_autoplace_controls"`
	Seed                              uint32                           `json:"seed"`
	Width                             uint32                           `json:"width"`
	Height                            uint32                           `json:"height"`
	StartingArea                      F32                              `json:"starting_area"`
	PeacefulMode                      bool                             `json:"peaceful_mode"`
	NoEnemiesMode                     *bool                            `json:"no_enemies_mode,omitempty"`
	StartingPoints                    []Position                       `json:"starting_points"`
	PropertyExpressionNames           map[string]string                `json:"property_expression_names"`
	CliffSettings                     CliffSettings                    `json:"cliff_settings"`
	TerritorySettings                 *TerritorySettings               `json:"territory_settings,omitempty"`
}

type Pollution struct {
	Enabled                                 *bool    `json:"enabled,omitempty"`
	DiffusionRatio                          *float64 `json:"diffusion_ratio,omitempty"`
	MinToDiffuse                            *float64 `json:"min_to_diffuse,omitempty"`
	Ageing                                  *float64 `json:"ageing,omitempty"`
	ExpectedMaxPerChunk                     *float64 `json:"expected_max_per_chunk,omitempty"`
	MinToShowPerChunk                       *float64 `json:"min_to_show_per_chunk,omitempty"`
	MinPollutionToDamageTrees               *float64 `json:"min_pollution_to_damage_trees,omitempty"`
	PollutionWithMaxForestDamage            *float64 `json:"pollution_with_max_forest_damage,omitempty"`
	PollutionPerTreeDamage                  *float64 `json:"pollution_per_tree_damage,omitempty"`
	PollutionRestoredPerTreeDamage          *float64 `json:"pollution_restored_per_tree_damage,omitempty"`
	MaxPollutionToRestoreTrees              *float64 `json:"max_pollution_to_restore_trees,omitempty"`
	EnemyAttackPollutionConsumptionModifier *float64 `json:"enemy_attack_pollution_consumption_modifier,omitempty"`
}

type SteeringValues struct {
	Radius                     *float64 `json:"radius,omitempty"`
	SeparationFactor           *float64 `json:"separation_factor,omitempty"`
	SeparationForce            *float64 `json:"separation_force,omitempty"`
	ForceUnitFuzzyGotoBehavior *bool    `json:"force_unit_fuzzy_goto_behavior,omitempty"`
}

type Steering struct {
	Default SteeringValues `json:"default"`
	Moving  SteeringValues `json:"moving"`
}

type EnemyEvolution struct {
	Enabled         *bool    `json:"enabled,omitempty"`
	TimeFactor      *float64 `json:"time_factor,omitempty"`
	DestroyFactor   *float64 `json:"destroy_factor,omitempty"`
	PollutionFactor *float64 `json:"pollution_factor,omitempty"`
}

type EnemyExpansion struct {
	Enabled                          *bool    `json:"enabled,omitempty"`
	MaxExpansionDistance             *uint32  `json:"max_expansion_distance,omitempty"`
	MinExpansionDistance             *uint32  `json:"min_expansion_distance,omitempty"`
	FriendlyBaseInfluenceRadius      *uint32  `json:"friendly_base_influence_radius,omitempty"`
	EnemyBuildingInfluenceRadius     *uint32  `json:"enemy_building_influence_radius,omitempty"`
	BuildingCoefficient              *float64 `json:"building_coefficient,omitempty"`
	OtherBaseCoefficient             *float64 `json:"other_base_coefficient,omitempty"`
	NeighbouringChunkCoefficient     *float64 `json:"neighbouring_chunk_coefficient,omitempty"`
	NeighbouringBaseChunkCoefficient *float64 `json:"neighbouring_base_chunk_coefficient,omitempty"`
	MaxCollidingTilesCoefficient     *float64 `json:"max_colliding_tiles_coefficient,omitempty"`
	SettlerGroupMinSize              *uint32  `json:"settler_group_min_size,omitempty"`
	SettlerGroupMaxSize              *uint32  `json:"settler_group_max_size,omitempty"`
	EvolutionGroupSizeFactor         *float64 `json:"evolution_group_size_factor,omitempty"`
	MinExpansionCooldown             *uint32  `json:"min_expansion_cooldown,omitempty"`
	MaxExpansionCooldown             *uint32  `json:"max_expansion_cooldown,omitempty"`
	BuildBaseUnitDispatchCooldown    *uint32  `json:"build_base_unit_dispatch_cooldown,omitempty"`
}

type UnitGroup struct {
	MinGroupGatheringTime          *uint32  `json:"min_group_gathering_time,omitempty"`
	MaxGroupGatheringTime          *uint32  `json:"max_group_gathering_time,omitempty"`
	MaxWaitTimeForLateMembers      *uint32  `json:"max_wait_time_for_late_members,omitempty"`
	MaxGroupRadius                 *float64 `json:"max_group_radius,omitempty"`
	MinGroupRadius                 *float64 `json:"min_group_radius,omitempty"`
	MaxMemberSpeedupWhenBehind     *float64 `json:"max_member_speedup_when_behind,omitempty"`
	MaxMemberSlowdownWhenAhead     *float64 `json:"max_member_slowdown_when_ahead,omitempty"`
	MaxGroupSlowdownFactor         *float64 `json:"max_group_slowdown_factor,omitempty"`
	MaxGroupMemberFallbackFactor   *float64 `json:"max_group_member_fallback_factor,omitempty"`
	MemberDisownDistance           *float64 `json:"member_disown_distance,omitempty"`
	TickToleranceWhenMemberArrives *uint32  `json:"tick_tolerance_when_member_arrives,omitempty"`
	MaxGatheringUnitGroups         *uint32  `json:"max_gathering_unit_groups,omitempty"`
	MaxUnitGroupSize               *uint32  `json:"max_unit_group_size,omitempty"`
}

type PathFinder struct {
	Fwd2BwdRatio                                  *int32    `json:"fwd2bwd_ratio,omitempty"`
	GoalPressureRatio                             *float64  `json:"goal_pressure_ratio,omitempty"`
	UsePathCache                                  *bool     `json:"use_path_cache,omitempty"`
	MaxStepsWorkedPerTick                         *float64  `json:"max_steps_worked_per_tick,omitempty"`
	MaxWorkDonePerTick                            *uint32   `json:"max_work_done_per_tick,omitempty"`
	ShortCacheSize                                *uint32   `json:"short_cache_size,omitempty"`
	LongCacheSize                                 *uint32   `json:"long_cache_size,omitempty"`
	ShortCacheMinCacheableDistance                *float64  `json:"short_cache_min_cacheable_distance,omitempty"`
	ShortCacheMinAlgoStepsToCache                 *uint32   `json:"short_cache_min_algo_steps_to_cache,omitempty"`
	LongCacheMinCacheableDistance                 *float64  `json:"long_cache_min_cacheable_distance,omitempty"`
	CacheMaxConnectToCacheStepsMultiplier         *uint32   `json:"cache_max_connect_to_cache_steps_multiplier,omitempty"`
	CacheAcceptPathStartDistanceRatio             *float64  `json:"cache_accept_path_start_distance_ratio,omitempty"`
	CacheAcceptPathEndDistanceRatio               *float64  `json:"cache_accept_path_end_distance_ratio,omitempty"`
	NegativeCacheAcceptPathStartDistanceRatio     *float64  `json:"negative_cache_accept_path_start_distance_ratio,omitempty"`
	NegativeCacheAcceptPathEndDistanceRatio       *float64  `json:"negative_cache_accept_path_end_distance_ratio,omitempty"`
	CachePathStartDistanceRatingMultiplier        *float64  `json:"cache_path_start_distance_rating_multiplier,omitempty"`
	CachePathEndDistanceRatingMultiplier          *float64  `json:"cache_path_end_distance_rating_multiplier,omitempty"`
	StaleEnemyWithSameDestinationCollisionPenalty *float64  `json:"stale_enemy_with_same_destination_collision_penalty,omitempty"`
	IgnoreMovingEnemyCollisionDistance            *float64  `json:"ignore_moving_enemy_collision_distance,omitempty"`
	EnemyWithDifferentDestinationCollisionPenalty *float64  `json:"enemy_with_different_destination_collision_penalty,omitempty"`
	GeneralEntityCollisionPenalty                 *float64  `json:"general_entity_collision_penalty,omitempty"`
	GeneralEntitySubsequentCollisionPenalty       *float64  `json:"general_entity_subsequent_collision_penalty,omitempty"`
	ExtendedCollisionPenalty                      *float64  `json:"extended_collision_penalty,omitempty"`
	MaxClientsToAcceptAnyNewRequest               *uint32   `json:"max_clients_to_accept_any_new_request,omitempty"`
	MaxClientsToAcceptShortNewRequest             *uint32   `json:"max_clients_to_accept_short_new_request,omitempty"`
	DirectDistanceToConsiderShortRequest          *uint32   `json:"direct_distance_to_consider_short_request,omitempty"`
	ShortRequestMaxSteps                          *uint32   `json:"short_request_max_steps,omitempty"`
	ShortRequestRatio                             *float64  `json:"short_request_ratio,omitempty"`
	MinStepsToCheckPathFindTermination            *uint32   `json:"min_steps_to_check_path_find_termination,omitempty"`
	StartToGoalCostMultiplierToTerminatePathFind  *float64  `json:"start_to_goal_cost_multiplier_to_terminate_path_find,omitempty"`
	OverloadLevels                                []uint32  `json:"overload_levels,omitempty"`
	OverloadMultipliers                           []float64 `json:"overload_multipliers,omitempty"`
	NegativePathCacheDelayInterval                *uint32   `json:"negative_path_cache_delay_interval,omitempty"`
}

type DifficultySettings struct {
	RecipeDifficulty          *uint8   `json:"recipe_difficulty,omitempty"`
	TechnologyDifficulty      *uint8   `json:"technology_difficulty,omitempty"`
	TechnologyPriceMultiplier float64  `json:"technology_price_multiplier"`
	ResearchQueueSetting      *string  `json:"research_queue_setting,omitempty"`
	SpoilTimeModifier         *float64 `json:"spoil_time_modifier,omitempty"`
}

type Asteroids struct {
	SpawningRate                 *float64 `json:"spawning_rate,omitempty"`
	MaxRayPortalsExpandedPerTick *uint32  `json:"max_ray_portals_expanded_per_tick,omitempty"`
}

type MapSettings struct {
	Pollution              Pollution          `json:"pollution"`
	Steering               *Steering          `json:"steering,omitempty"`
	EnemyEvolution         EnemyEvolution     `json:"enemy_evolution"`
	EnemyExpansion         EnemyExpansion     `json:"enemy_expansion"`
	UnitGroup              UnitGroup          `json:"unit_group"`
	PathFinder             PathFinder         `json:"path_finder"`
	MaxFailedBehaviorCount uint32             `json:"max_failed_behavior_count"`
	DifficultySettings     DifficultySettings `json:"difficulty_settings"`
	Asteroids              *Asteroids         `json:"asteroids,omitempty"`
}
