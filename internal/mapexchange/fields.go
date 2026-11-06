package mapexchange

// The read order below is the game's binary layout and must not change;
// each line-specific field is read only on the lines that write it.

// The 2.1 patch numbers at which the map-settings layout changes. Compatible
// reads the same constants (layout.go), so the two cannot disagree.
const (
	dispatchCooldownAddedIn   = 13 // adds enemy_expansion.build_base_unit_dispatch_cooldown
	pollutionRestoreRemovedIn = 18 // removes pollution.max_pollution_to_restore_trees
)

func readFSR(r *reader, field string) FrequencySizeRichness {
	return FrequencySizeRichness{
		Frequency: r.f32(field),
		Size:      r.f32(field),
		Richness:  r.f32(field),
	}
}

func readFSRDict(r *reader, field string) map[string]FrequencySizeRichness {
	n := r.count()
	m := make(map[string]FrequencySizeRichness, n)
	for i := 0; i < n && r.err == nil; i++ {
		k := r.str(field)
		if _, dup := m[k]; dup {
			r.fail(errDuplicate)
		}
		m[k] = readFSR(r, field)
	}
	return m
}

func readMapGenSettings(r *reader, line gameLine) MapGenSettings {
	var s MapGenSettings
	if line == line11 {
		ts, w := r.f32("terrain_segmentation"), r.f32("water")
		s.TerrainSegmentation, s.Water = &ts, &w
	}
	s.AutoplaceControls = readFSRDict(r, "autoplace_controls")

	n := r.count()
	s.AutoplaceSettings = make(map[string]AutoplaceSetting, n)
	for i := 0; i < n && r.err == nil; i++ {
		k := r.str("autoplace_settings")
		if _, dup := s.AutoplaceSettings[k]; dup {
			r.fail(errDuplicate)
		}
		s.AutoplaceSettings[k] = AutoplaceSetting{
			TreatMissingAsDefault: r.boolean("autoplace_settings"),
			Settings:              readFSRDict(r, "autoplace_settings"),
		}
	}

	s.DefaultEnableAllAutoplaceControls = r.boolean("default_enable_all_autoplace_controls")
	s.Seed = r.u32()
	s.Width = r.u32()
	s.Height = r.u32()
	// area_to_generate_at_start: read to keep the layout in step, but not
	// written out; the example files have no such key and the game's
	// default is what every string carries.
	r.position()
	r.position()
	r.i16()
	r.i16()
	s.StartingArea = r.f32("starting_area")
	s.PeacefulMode = r.boolean("peaceful_mode")
	if line >= line20 {
		ne := r.boolean("no_enemies_mode")
		s.NoEnemiesMode = &ne
	}

	n = r.count()
	s.StartingPoints = make([]Position, 0, n)
	for i := 0; i < n && r.err == nil; i++ {
		s.StartingPoints = append(s.StartingPoints, r.position())
	}

	n = r.count()
	s.PropertyExpressionNames = make(map[string]string, n)
	for i := 0; i < n && r.err == nil; i++ {
		k := r.str("property_expression_names")
		if _, dup := s.PropertyExpressionNames[k]; dup {
			r.fail(errDuplicate)
		}
		s.PropertyExpressionNames[k] = r.str("property_expression_names")
	}

	c := &s.CliffSettings
	c.Name = r.str("cliff_settings")
	if line >= line20 {
		ctl := r.str("cliff_settings")
		c.Control = &ctl
	}
	c.CliffElevation0 = r.f32("cliff_settings")
	c.CliffElevationInterval = r.f32("cliff_settings")
	// From 2.0 cliff_smoothing comes before richness. A round trip through
	// the game's own --map-gen-settings shows this order.
	if line >= line20 {
		sm := r.f32("cliff_settings")
		c.CliffSmoothing = &sm
	}
	c.Richness = r.f32("cliff_settings")

	if line >= line20 && r.present("territory_settings") {
		t := &TerritorySettings{}
		n = r.count()
		t.Units = make([]string, 0, n)
		for i := 0; i < n && r.err == nil; i++ {
			t.Units = append(t.Units, r.str("territory_settings"))
		}
		t.TerritoryIndexExpression = r.str("territory_settings")
		t.TerritoryVariationExpression = r.str("territory_settings")
		t.MinimumTerritorySize = r.u32()
		s.TerritorySettings = t
	}
	return s
}

func readSteeringValues(r *reader) SteeringValues {
	const f = "steering"
	return SteeringValues{
		Radius:                     optF64(r, f),
		SeparationFactor:           optF64(r, f),
		SeparationForce:            optF64(r, f),
		ForceUnitFuzzyGotoBehavior: optBool(r, f),
	}
}

// researchQueueSettings is the 1.1 enum, indexed by its stored byte.
var researchQueueSettings = []string{"always", "after-victory", "never"}

func readMapSettings(r *reader, line gameLine, v [4]uint16) MapSettings {
	var s MapSettings

	const p = "pollution"
	s.Pollution = Pollution{
		Enabled:                        optBool(r, p),
		DiffusionRatio:                 optF64(r, p),
		MinToDiffuse:                   optF64(r, p),
		Ageing:                         optF64(r, p),
		ExpectedMaxPerChunk:            optF64(r, p),
		MinToShowPerChunk:              optF64(r, p),
		MinPollutionToDamageTrees:      optF64(r, p),
		PollutionWithMaxForestDamage:   optF64(r, p),
		PollutionPerTreeDamage:         optF64(r, p),
		PollutionRestoredPerTreeDamage: optF64(r, p),
	}
	if line != line21 || v[2] < pollutionRestoreRemovedIn {
		s.Pollution.MaxPollutionToRestoreTrees = optF64(r, p)
	}
	s.Pollution.EnemyAttackPollutionConsumptionModifier = optF64(r, p)

	if line < line21 {
		s.Steering = &Steering{Default: readSteeringValues(r), Moving: readSteeringValues(r)}
	}

	const ev = "enemy_evolution"
	s.EnemyEvolution = EnemyEvolution{
		Enabled:         optBool(r, ev),
		TimeFactor:      optF64(r, ev),
		DestroyFactor:   optF64(r, ev),
		PollutionFactor: optF64(r, ev),
	}

	const ex = "enemy_expansion"
	e := &s.EnemyExpansion
	e.Enabled = optBool(r, ex)
	e.MaxExpansionDistance = optU32(r, ex)
	if line >= line21 {
		e.MinExpansionDistance = optU32(r, ex)
	}
	e.FriendlyBaseInfluenceRadius = optU32(r, ex)
	e.EnemyBuildingInfluenceRadius = optU32(r, ex)
	e.BuildingCoefficient = optF64(r, ex)
	e.OtherBaseCoefficient = optF64(r, ex)
	e.NeighbouringChunkCoefficient = optF64(r, ex)
	e.NeighbouringBaseChunkCoefficient = optF64(r, ex)
	e.MaxCollidingTilesCoefficient = optF64(r, ex)
	e.SettlerGroupMinSize = optU32(r, ex)
	e.SettlerGroupMaxSize = optU32(r, ex)
	if line >= line21 {
		e.EvolutionGroupSizeFactor = optF64(r, ex)
	}
	e.MinExpansionCooldown = optU32(r, ex)
	e.MaxExpansionCooldown = optU32(r, ex)
	if line >= line21 && v[2] >= dispatchCooldownAddedIn {
		e.BuildBaseUnitDispatchCooldown = optU32(r, ex)
	}

	const ug = "unit_group"
	s.UnitGroup = UnitGroup{
		MinGroupGatheringTime:          optU32(r, ug),
		MaxGroupGatheringTime:          optU32(r, ug),
		MaxWaitTimeForLateMembers:      optU32(r, ug),
		MaxGroupRadius:                 optF64(r, ug),
		MinGroupRadius:                 optF64(r, ug),
		MaxMemberSpeedupWhenBehind:     optF64(r, ug),
		MaxMemberSlowdownWhenAhead:     optF64(r, ug),
		MaxGroupSlowdownFactor:         optF64(r, ug),
		MaxGroupMemberFallbackFactor:   optF64(r, ug),
		MemberDisownDistance:           optF64(r, ug),
		TickToleranceWhenMemberArrives: optU32(r, ug),
		MaxGatheringUnitGroups:         optU32(r, ug),
		MaxUnitGroupSize:               optU32(r, ug),
	}

	const pf = "path_finder"
	f := &s.PathFinder
	f.Fwd2BwdRatio = optI32(r, pf)
	f.GoalPressureRatio = optF64(r, pf)
	f.UsePathCache = optBool(r, pf)
	f.MaxStepsWorkedPerTick = optF64(r, pf)
	f.MaxWorkDonePerTick = optU32(r, pf)
	f.ShortCacheSize = optU32(r, pf)
	f.LongCacheSize = optU32(r, pf)
	f.ShortCacheMinCacheableDistance = optF64(r, pf)
	f.ShortCacheMinAlgoStepsToCache = optU32(r, pf)
	f.LongCacheMinCacheableDistance = optF64(r, pf)
	f.CacheMaxConnectToCacheStepsMultiplier = optU32(r, pf)
	f.CacheAcceptPathStartDistanceRatio = optF64(r, pf)
	f.CacheAcceptPathEndDistanceRatio = optF64(r, pf)
	f.NegativeCacheAcceptPathStartDistanceRatio = optF64(r, pf)
	f.NegativeCacheAcceptPathEndDistanceRatio = optF64(r, pf)
	f.CachePathStartDistanceRatingMultiplier = optF64(r, pf)
	f.CachePathEndDistanceRatingMultiplier = optF64(r, pf)
	f.StaleEnemyWithSameDestinationCollisionPenalty = optF64(r, pf)
	f.IgnoreMovingEnemyCollisionDistance = optF64(r, pf)
	f.EnemyWithDifferentDestinationCollisionPenalty = optF64(r, pf)
	f.GeneralEntityCollisionPenalty = optF64(r, pf)
	f.GeneralEntitySubsequentCollisionPenalty = optF64(r, pf)
	f.ExtendedCollisionPenalty = optF64(r, pf)
	f.MaxClientsToAcceptAnyNewRequest = optU32(r, pf)
	f.MaxClientsToAcceptShortNewRequest = optU32(r, pf)
	f.DirectDistanceToConsiderShortRequest = optU32(r, pf)
	f.ShortRequestMaxSteps = optU32(r, pf)
	f.ShortRequestRatio = optF64(r, pf)
	f.MinStepsToCheckPathFindTermination = optU32(r, pf)
	f.StartToGoalCostMultiplierToTerminatePathFind = optF64(r, pf)
	if r.present(pf) {
		n := r.count()
		f.OverloadLevels = make([]uint32, 0, n)
		for i := 0; i < n && r.err == nil; i++ {
			f.OverloadLevels = append(f.OverloadLevels, r.u32())
		}
	}
	if r.present(pf) {
		n := r.count()
		f.OverloadMultipliers = make([]float64, 0, n)
		for i := 0; i < n && r.err == nil; i++ {
			f.OverloadMultipliers = append(f.OverloadMultipliers, r.f64(pf))
		}
	}
	f.NegativePathCacheDelayInterval = optU32(r, pf)

	s.MaxFailedBehaviorCount = r.u32()

	const ds = "difficulty_settings"
	d := &s.DifficultySettings
	if line == line11 {
		recipe, tech := r.u8(), r.u8()
		if recipe > 1 || tech > 1 {
			r.fail(rangeError(ds))
		}
		d.RecipeDifficulty, d.TechnologyDifficulty = &recipe, &tech
		d.TechnologyPriceMultiplier = r.f64(ds)
		q := int(r.u8())
		if q >= len(researchQueueSettings) {
			r.fail(rangeError(ds))
		} else {
			d.ResearchQueueSetting = &researchQueueSettings[q]
		}
	} else {
		d.TechnologyPriceMultiplier = r.f64(ds)
		spoil := r.f64(ds)
		d.SpoilTimeModifier = &spoil
		s.Asteroids = &Asteroids{
			SpawningRate:                 optF64(r, "asteroids"),
			MaxRayPortalsExpandedPerTick: optU32(r, "asteroids"),
		}
	}
	return s
}
