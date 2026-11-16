package mapexchange

// The write order mirrors fields.go, which holds the game's binary layout.

import "slices"

// areaToGenerateAtStart is the bounding box every game string carries
// (-224,-224 to 224,224, then an orientation the game does not vary); the
// settings files have no such key, so it is written as the game writes it.
var areaToGenerateAtStart = []Position{{X: -224, Y: -224}, {X: 224, Y: 224}}

const areaOrientation int16 = -32767

func writeFSR(w *writer, v FrequencySizeRichness) {
	w.f32(v.Frequency)
	w.f32(v.Size)
	w.f32(v.Richness)
}

func writeFSRDict(w *writer, m map[string]FrequencySizeRichness) {
	w.count(len(m))
	for _, k := range sortedKeys(m) {
		w.str(k)
		writeFSR(w, m[k])
	}
}

func writeMapGenSettings(w *writer, s *MapGenSettings, line gameLine) {
	if line == line11 {
		w.f32(deref(s.TerrainSegmentation, 1))
		w.f32(deref(s.Water, 1))
	}
	writeFSRDict(w, s.AutoplaceControls)

	w.count(len(s.AutoplaceSettings))
	for _, k := range sortedKeys(s.AutoplaceSettings) {
		w.str(k)
		w.boolean(s.AutoplaceSettings[k].TreatMissingAsDefault)
		writeFSRDict(w, s.AutoplaceSettings[k].Settings)
	}

	w.boolean(s.DefaultEnableAllAutoplaceControls)
	w.u32(s.Seed)
	w.u32(s.Width)
	w.u32(s.Height)
	w.position(areaToGenerateAtStart[0])
	w.position(areaToGenerateAtStart[1])
	w.i16(0)
	w.i16(areaOrientation)
	w.f32(s.StartingArea)
	w.boolean(s.PeacefulMode)
	if line >= line20 {
		w.boolean(deref(s.NoEnemiesMode, false))
	}

	w.count(len(s.StartingPoints))
	for _, p := range s.StartingPoints {
		w.position(p)
	}

	w.count(len(s.PropertyExpressionNames))
	for _, k := range sortedKeys(s.PropertyExpressionNames) {
		w.str(k)
		w.str(s.PropertyExpressionNames[k])
	}

	c := &s.CliffSettings
	w.str(c.Name)
	if line >= line20 {
		w.str(deref(c.Control, ""))
	}
	w.f32(c.CliffElevation0)
	w.f32(c.CliffElevationInterval)
	if line >= line20 {
		w.f32(deref(c.CliffSmoothing, 1))
	}
	w.f32(c.Richness)

	if line >= line20 {
		t := s.TerritorySettings
		w.boolean(t != nil)
		if t != nil {
			w.count(len(t.Units))
			for _, u := range t.Units {
				w.str(u)
			}
			w.str(t.TerritoryIndexExpression)
			w.str(t.TerritoryVariationExpression)
			w.u32(t.MinimumTerritorySize)
		}
	}
}

func writeSteeringValues(w *writer, v SteeringValues) {
	optF64W(w, v.Radius)
	optF64W(w, v.SeparationFactor)
	optF64W(w, v.SeparationForce)
	optBoolW(w, v.ForceUnitFuzzyGotoBehavior)
}

func writeMapSettings(w *writer, s *MapSettings, line gameLine, patch uint16) error {
	p := &s.Pollution
	optBoolW(w, p.Enabled)
	optF64W(w, p.DiffusionRatio)
	optF64W(w, p.MinToDiffuse)
	optF64W(w, p.Ageing)
	optF64W(w, p.ExpectedMaxPerChunk)
	optF64W(w, p.MinToShowPerChunk)
	optF64W(w, p.MinPollutionToDamageTrees)
	optF64W(w, p.PollutionWithMaxForestDamage)
	optF64W(w, p.PollutionPerTreeDamage)
	optF64W(w, p.PollutionRestoredPerTreeDamage)
	if line != line21 || patch < pollutionRestoreRemovedIn {
		optF64W(w, p.MaxPollutionToRestoreTrees)
	}
	optF64W(w, p.EnemyAttackPollutionConsumptionModifier)

	if line < line21 {
		var st Steering
		if s.Steering != nil {
			st = *s.Steering
		}
		writeSteeringValues(w, st.Default)
		writeSteeringValues(w, st.Moving)
	}

	ev := &s.EnemyEvolution
	optBoolW(w, ev.Enabled)
	optF64W(w, ev.TimeFactor)
	optF64W(w, ev.DestroyFactor)
	optF64W(w, ev.PollutionFactor)

	e := &s.EnemyExpansion
	optBoolW(w, e.Enabled)
	optU32W(w, e.MaxExpansionDistance)
	if line >= line21 {
		optU32W(w, e.MinExpansionDistance)
	}
	optU32W(w, e.FriendlyBaseInfluenceRadius)
	optU32W(w, e.EnemyBuildingInfluenceRadius)
	optF64W(w, e.BuildingCoefficient)
	optF64W(w, e.OtherBaseCoefficient)
	optF64W(w, e.NeighbouringChunkCoefficient)
	optF64W(w, e.NeighbouringBaseChunkCoefficient)
	optF64W(w, e.MaxCollidingTilesCoefficient)
	optU32W(w, e.SettlerGroupMinSize)
	optU32W(w, e.SettlerGroupMaxSize)
	if line >= line21 {
		optF64W(w, e.EvolutionGroupSizeFactor)
	}
	optU32W(w, e.MinExpansionCooldown)
	optU32W(w, e.MaxExpansionCooldown)
	if line >= line21 && patch >= dispatchCooldownAddedIn {
		optU32W(w, e.BuildBaseUnitDispatchCooldown)
	}

	u := &s.UnitGroup
	optU32W(w, u.MinGroupGatheringTime)
	optU32W(w, u.MaxGroupGatheringTime)
	optU32W(w, u.MaxWaitTimeForLateMembers)
	optF64W(w, u.MaxGroupRadius)
	optF64W(w, u.MinGroupRadius)
	optF64W(w, u.MaxMemberSpeedupWhenBehind)
	optF64W(w, u.MaxMemberSlowdownWhenAhead)
	optF64W(w, u.MaxGroupSlowdownFactor)
	optF64W(w, u.MaxGroupMemberFallbackFactor)
	optF64W(w, u.MemberDisownDistance)
	optU32W(w, u.TickToleranceWhenMemberArrives)
	optU32W(w, u.MaxGatheringUnitGroups)
	optU32W(w, u.MaxUnitGroupSize)

	f := &s.PathFinder
	optI32W(w, f.Fwd2BwdRatio)
	optF64W(w, f.GoalPressureRatio)
	optBoolW(w, f.UsePathCache)
	optF64W(w, f.MaxStepsWorkedPerTick)
	optU32W(w, f.MaxWorkDonePerTick)
	optU32W(w, f.ShortCacheSize)
	optU32W(w, f.LongCacheSize)
	optF64W(w, f.ShortCacheMinCacheableDistance)
	optU32W(w, f.ShortCacheMinAlgoStepsToCache)
	optF64W(w, f.LongCacheMinCacheableDistance)
	optU32W(w, f.CacheMaxConnectToCacheStepsMultiplier)
	optF64W(w, f.CacheAcceptPathStartDistanceRatio)
	optF64W(w, f.CacheAcceptPathEndDistanceRatio)
	optF64W(w, f.NegativeCacheAcceptPathStartDistanceRatio)
	optF64W(w, f.NegativeCacheAcceptPathEndDistanceRatio)
	optF64W(w, f.CachePathStartDistanceRatingMultiplier)
	optF64W(w, f.CachePathEndDistanceRatingMultiplier)
	optF64W(w, f.StaleEnemyWithSameDestinationCollisionPenalty)
	optF64W(w, f.IgnoreMovingEnemyCollisionDistance)
	optF64W(w, f.EnemyWithDifferentDestinationCollisionPenalty)
	optF64W(w, f.GeneralEntityCollisionPenalty)
	optF64W(w, f.GeneralEntitySubsequentCollisionPenalty)
	optF64W(w, f.ExtendedCollisionPenalty)
	optU32W(w, f.MaxClientsToAcceptAnyNewRequest)
	optU32W(w, f.MaxClientsToAcceptShortNewRequest)
	optU32W(w, f.DirectDistanceToConsiderShortRequest)
	optU32W(w, f.ShortRequestMaxSteps)
	optF64W(w, f.ShortRequestRatio)
	optU32W(w, f.MinStepsToCheckPathFindTermination)
	optF64W(w, f.StartToGoalCostMultiplierToTerminatePathFind)
	w.boolean(f.OverloadLevels != nil)
	if f.OverloadLevels != nil {
		w.count(len(f.OverloadLevels))
		for _, v := range f.OverloadLevels {
			w.u32(v)
		}
	}
	w.boolean(f.OverloadMultipliers != nil)
	if f.OverloadMultipliers != nil {
		w.count(len(f.OverloadMultipliers))
		for _, v := range f.OverloadMultipliers {
			w.f64(v)
		}
	}
	optU32W(w, f.NegativePathCacheDelayInterval)

	w.u32(s.MaxFailedBehaviorCount)

	d := &s.DifficultySettings
	if line == line11 {
		recipe, tech := deref(d.RecipeDifficulty, 0), deref(d.TechnologyDifficulty, 0)
		if recipe > 1 || tech > 1 {
			return rangeError("difficulty_settings")
		}
		w.u8(recipe)
		w.u8(tech)
		w.f64(d.TechnologyPriceMultiplier)
		q := slices.Index(researchQueueSettings, deref(d.ResearchQueueSetting, "after-victory"))
		if q < 0 {
			return rangeError("difficulty_settings")
		}
		w.u8(uint8(q)) //nolint:gosec // an index into a three-item list
	} else {
		w.f64(d.TechnologyPriceMultiplier)
		w.f64(deref(d.SpoilTimeModifier, 1))
		var a Asteroids
		if s.Asteroids != nil {
			a = *s.Asteroids
		}
		optF64W(w, a.SpawningRate)
		optU32W(w, a.MaxRayPortalsExpandedPerTick)
	}
	return nil
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
