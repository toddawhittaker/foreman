package factorymap

import "image/color"

// categoryByType is the one table of SPEC 5.16: the engine's prototype type
// of an entity (not its name, so a modded entity of a known type lands in
// its category too). A type missing here is CategoryOther.
var categoryByType = map[string]Category{
	// Belts and inserters.
	"transport-belt":   CategoryBelts,
	"underground-belt": CategoryBelts,
	"splitter":         CategoryBelts,
	"lane-splitter":    CategoryBelts,
	"loader":           CategoryBelts,
	"loader-1x1":       CategoryBelts,
	"linked-belt":      CategoryBelts,
	"inserter":         CategoryBelts,

	// Production: machines that make, mine or research.
	"assembling-machine": CategoryProduction,
	"furnace":            CategoryProduction,
	"mining-drill":       CategoryProduction,
	"lab":                CategoryProduction,
	"beacon":             CategoryProduction,
	"rocket-silo":        CategoryProduction,
	"agricultural-tower": CategoryProduction,

	// Power: making, storing and carrying electricity and heat.
	"electric-pole":             CategoryPower,
	"power-switch":              CategoryPower,
	"generator":                 CategoryPower,
	"burner-generator":          CategoryPower,
	"solar-panel":               CategoryPower,
	"accumulator":               CategoryPower,
	"boiler":                    CategoryPower,
	"reactor":                   CategoryPower,
	"fusion-reactor":            CategoryPower,
	"fusion-generator":          CategoryPower,
	"heat-pipe":                 CategoryPower,
	"heat-interface":            CategoryPower,
	"electric-energy-interface": CategoryPower,
	"lightning-attractor":       CategoryPower,
	"heating-tower":             CategoryPower,

	// Fluids.
	"pipe":           CategoryFluids,
	"pipe-to-ground": CategoryFluids,
	"pump":           CategoryFluids,
	"storage-tank":   CategoryFluids,
	"offshore-pump":  CategoryFluids,
	"valve":          CategoryFluids,
	"infinity-pipe":  CategoryFluids,

	// Rail: track, signals, stops and trains.
	"straight-rail":               CategoryRail,
	"curved-rail-a":               CategoryRail,
	"curved-rail-b":               CategoryRail,
	"half-diagonal-rail":          CategoryRail,
	"legacy-straight-rail":        CategoryRail,
	"legacy-curved-rail":          CategoryRail,
	"elevated-straight-rail":      CategoryRail,
	"elevated-curved-rail-a":      CategoryRail,
	"elevated-curved-rail-b":      CategoryRail,
	"elevated-half-diagonal-rail": CategoryRail,
	"rail-ramp":                   CategoryRail,
	"rail-support":                CategoryRail,
	"rail-signal":                 CategoryRail,
	"rail-chain-signal":           CategoryRail,
	"train-stop":                  CategoryRail,
	"locomotive":                  CategoryRail,
	"cargo-wagon":                 CategoryRail,
	"fluid-wagon":                 CategoryRail,
	"artillery-wagon":             CategoryRail,
	"infinity-cargo-wagon":        CategoryRail,

	// Defence.
	"wall":             CategoryDefence,
	"gate":             CategoryDefence,
	"turret":           CategoryDefence,
	"ammo-turret":      CategoryDefence,
	"electric-turret":  CategoryDefence,
	"fluid-turret":     CategoryDefence,
	"artillery-turret": CategoryDefence,
	"land-mine":        CategoryDefence,
	"radar":            CategoryDefence,

	// Logistic network and storage.
	"container":          CategoryLogistics,
	"logistic-container": CategoryLogistics,
	"infinity-container": CategoryLogistics,
	"linked-container":   CategoryLogistics,
	"roboport":           CategoryLogistics,
	"construction-robot": CategoryLogistics,
	"logistic-robot":     CategoryLogistics,
	"cargo-landing-pad":  CategoryLogistics,
	"space-platform-hub": CategoryLogistics,
	"cargo-bay":          CategoryLogistics,
	"asteroid-collector": CategoryLogistics,
}

// CategoryOf is the category of an entity from the engine's prototype type,
// so a modded entity lands in one too (SPEC 5.16). A type it does not know is
// CategoryOther.
func CategoryOf(entityType string) Category {
	if c, ok := categoryByType[entityType]; ok {
		return c
	}
	return CategoryOther
}

// categoryColors are the layer colours: eight hues that stay apart for the
// common kinds of colour blindness and read on both the light and dark chart
// (the chart is mostly dark, so none is dark).
var categoryColors = map[Category]color.NRGBA{
	CategoryBelts:      {R: 0xFF, G: 0xD1, B: 0x1A, A: 0xFF}, // yellow
	CategoryProduction: {R: 0x3D, G: 0xA5, B: 0xFF, A: 0xFF}, // blue
	CategoryPower:      {R: 0xFF, G: 0x6B, B: 0x35, A: 0xFF}, // orange
	CategoryFluids:     {R: 0x2E, G: 0xE6, B: 0xD6, A: 0xFF}, // cyan
	CategoryRail:       {R: 0xFF, G: 0x5C, B: 0xA8, A: 0xFF}, // pink
	CategoryDefence:    {R: 0xFF, G: 0x3B, B: 0x3B, A: 0xFF}, // red
	CategoryLogistics:  {R: 0x8C, G: 0xE8, B: 0x4A, A: 0xFF}, // green
	CategoryOther:      {R: 0xC8, G: 0xB4, B: 0xFF, A: 0xFF}, // lilac
}

// CategoryColor is the colour a category's layer is drawn in, for a legend
// that matches the picture. An unknown category gets the colour of "other".
func CategoryColor(c Category) color.NRGBA {
	if col, ok := categoryColors[c]; ok {
		return col
	}
	return categoryColors[CategoryOther]
}
