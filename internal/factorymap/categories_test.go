package factorymap

import "testing"

func TestCategoryOf(t *testing.T) {
	cases := map[string]Category{
		"transport-belt":              CategoryBelts,
		"underground-belt":            CategoryBelts,
		"splitter":                    CategoryBelts,
		"loader-1x1":                  CategoryBelts,
		"linked-belt":                 CategoryBelts,
		"inserter":                    CategoryBelts,
		"assembling-machine":          CategoryProduction,
		"furnace":                     CategoryProduction,
		"mining-drill":                CategoryProduction,
		"lab":                         CategoryProduction,
		"beacon":                      CategoryProduction,
		"rocket-silo":                 CategoryProduction,
		"agricultural-tower":          CategoryProduction,
		"electric-pole":               CategoryPower,
		"solar-panel":                 CategoryPower,
		"boiler":                      CategoryPower,
		"generator":                   CategoryPower,
		"reactor":                     CategoryPower,
		"fusion-reactor":              CategoryPower,
		"accumulator":                 CategoryPower,
		"heat-pipe":                   CategoryPower,
		"heating-tower":               CategoryPower,
		"pipe":                        CategoryFluids,
		"pipe-to-ground":              CategoryFluids,
		"pump":                        CategoryFluids,
		"storage-tank":                CategoryFluids,
		"offshore-pump":               CategoryFluids,
		"straight-rail":               CategoryRail,
		"curved-rail-a":               CategoryRail,
		"elevated-half-diagonal-rail": CategoryRail,
		"rail-signal":                 CategoryRail,
		"train-stop":                  CategoryRail,
		"locomotive":                  CategoryRail,
		"cargo-wagon":                 CategoryRail,
		"wall":                        CategoryDefence,
		"gate":                        CategoryDefence,
		"ammo-turret":                 CategoryDefence,
		"electric-turret":             CategoryDefence,
		"artillery-turret":            CategoryDefence,
		"land-mine":                   CategoryDefence,
		"container":                   CategoryLogistics,
		"logistic-container":          CategoryLogistics,
		"roboport":                    CategoryLogistics,
		"logistic-robot":              CategoryLogistics,
		"space-platform-hub":          CategoryLogistics,
		"lamp":                        CategoryOther,
		"constant-combinator":         CategoryOther,
		"car":                         CategoryOther,
	}
	for typ, want := range cases {
		if got := CategoryOf(typ); got != want {
			t.Errorf("CategoryOf(%q) = %q, want %q", typ, got, want)
		}
	}
}

// A type nobody listed, from a mod or a newer game, is "other" and never
// an error (SPEC 5.16).
func TestCategoryOfUnknownIsOther(t *testing.T) {
	for _, typ := range []string{"", "no-such-type", "TRANSPORT-BELT", "transport-belt ", "mod-thing-9"} {
		if got := CategoryOf(typ); got != CategoryOther {
			t.Errorf("CategoryOf(%q) = %q, want other", typ, got)
		}
	}
}

// Every table entry names a real category, and every category except
// "other" has at least one type, so a checkbox never draws nothing by design.
func TestCategoryTableIsConsistent(t *testing.T) {
	valid := map[Category]bool{}
	for _, c := range Categories {
		valid[c] = true
	}
	used := map[Category]bool{}
	for typ, c := range categoryByType {
		if !valid[c] {
			t.Errorf("type %q maps to unknown category %q", typ, c)
		}
		used[c] = true
	}
	for _, c := range Categories {
		if c != CategoryOther && !used[c] {
			t.Errorf("category %q has no type", c)
		}
	}
}

func TestCategoryColorsAreDistinctAndOpaque(t *testing.T) {
	seen := map[[3]uint8]Category{}
	for _, c := range Categories {
		col := CategoryColor(c)
		if col.A != 255 {
			t.Errorf("%s: colour is not opaque", c)
		}
		k := [3]uint8{col.R, col.G, col.B}
		if prev, ok := seen[k]; ok {
			t.Errorf("%s and %s share a colour", prev, c)
		}
		seen[k] = c
	}
}
