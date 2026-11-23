package mods

import (
	"reflect"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

func inst(name string, deps ...string) world.InstalledMod {
	return world.InstalledMod{Name: name, Enabled: true, Dependencies: deps}
}

// recorded marks the mods as a world with a record of chosen mods, the
// chosen ones being those named.
func recorded(ms []world.InstalledMod, chosen ...string) []world.InstalledMod {
	for i := range ms {
		ms[i].Recorded = true
		ms[i].Chosen = slices.Contains(chosen, ms[i].Name)
	}
	return ms
}

func TestOriginsKinds(t *testing.T) {
	// A hidden optional dependency is not offered to the player, so it names nothing.
	got := Origins(recorded([]world.InstalledMod{
		inst("planet", "base >= 2.0", "planetslib", "~ ordered", "+ nice", "? extra", "(?) hidden", "! clash"),
		inst("planetslib"), inst("ordered"), inst("nice"), inst("extra"), inst("hidden"), inst("clash"),
	}, "planet"))
	want := map[string]Origin{
		"planetslib": {RequiredBy, []string{"planet"}},
		"ordered":    {RequiredBy, []string{"planet"}},
		"nice":       {RecommendedBy, []string{"planet"}},
		"extra":      {OfferedBy, []string{"planet"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Origins = %+v, want %+v", got, want)
	}
}

func TestOriginsSeveralParentsSorted(t *testing.T) {
	got := Origins([]world.InstalledMod{inst("zeta", "lib"), inst("alpha", "lib"), inst("lib")})
	if want := []string{"alpha", "zeta"}; !reflect.DeepEqual(got["lib"].By, want) {
		t.Errorf("By = %v, want %v", got["lib"].By, want)
	}
}

func TestOriginsPrecedence(t *testing.T) {
	// Required beats recommended beats offered; only the winning kind's parents are named.
	got := Origins([]world.InstalledMod{
		inst("a", "? lib"), inst("b", "+ lib"), inst("c", "lib"), inst("lib"),
		inst("d", "? other"), inst("e", "+ other"), inst("other"),
	})
	if o := got["lib"]; o.Kind != RequiredBy || !reflect.DeepEqual(o.By, []string{"c"}) {
		t.Errorf("lib = %+v, want required by c", o)
	}
	if o := got["other"]; o.Kind != RecommendedBy || !reflect.DeepEqual(o.By, []string{"e"}) {
		t.Errorf("other = %+v, want recommended by e", o)
	}
}

func TestOriginsCycle(t *testing.T) {
	got := Origins([]world.InstalledMod{inst("a", "b"), inst("b", "a"), inst("self", "self")})
	if o := got["a"]; o.Kind != RequiredBy || !reflect.DeepEqual(o.By, []string{"b"}) {
		t.Errorf("a = %+v", o)
	}
	if o := got["b"]; o.Kind != RequiredBy || !reflect.DeepEqual(o.By, []string{"a"}) {
		t.Errorf("b = %+v", o)
	}
	if _, ok := got["self"]; ok {
		t.Errorf("a mod is not its own origin: %+v", got["self"])
	}
}

func TestOriginsIgnoresBaseAndExpansions(t *testing.T) {
	space := inst("space-age", "base", "quality", "elevated-rails")
	space.BuiltIn = true
	got := Origins([]world.InstalledMod{
		inst("base"), inst("quality", "base"), inst("elevated-rails", "base"), space,
		inst("planet", "base", "space-age", "lib"), inst("lib"),
	})
	if len(got) != 1 || got["lib"].Kind != RequiredBy {
		t.Errorf("Origins = %+v, want only lib", got)
	}
	// An expansion never names a mod either.
	got = Origins([]world.InstalledMod{inst("space-age", "lib"), inst("lib")})
	if len(got) != 0 {
		t.Errorf("an expansion is not a parent: %+v", got)
	}
}

func TestOriginsSkipsDisabledMissingAndMalformed(t *testing.T) {
	off := inst("off", "lib")
	off.Enabled = false
	got := Origins(recorded([]world.InstalledMod{off, inst("lib"), inst("a", "absent", "bad/name", "? lib2"), inst("lib2")}, "a"))
	if o := got["lib2"]; o.Kind != OfferedBy {
		t.Errorf("lib2 = %+v", o)
	}
	if _, ok := got["lib"]; ok {
		t.Errorf("a disabled mod pulls nothing in: %+v", got["lib"])
	}
	if _, ok := got["absent"]; ok {
		t.Errorf("a mod that is not installed has no origin")
	}
}

func TestOriginsChosenModHasNone(t *testing.T) {
	// Moshine was chosen; Muluna offers it. Maraxsis was chosen; three mods
	// name it, two as hidden optional. Lib was chosen and is also required,
	// and Nice was chosen and is also recommended.
	got := Origins(recorded([]world.InstalledMod{
		inst("muluna", "? moshine", "(?) maraxsis", "lib", "+ nice"),
		inst("cerys", "? maraxsis"),
		inst("moshine", "(?) maraxsis"),
		inst("maraxsis"), inst("lib"), inst("nice"), inst("dep"),
		inst("other", "dep"),
	}, "muluna", "cerys", "moshine", "maraxsis", "lib", "nice", "other"))
	want := map[string]Origin{"dep": {RequiredBy, []string{"other"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Origins = %+v, want %+v", got, want)
	}
}

func TestChosenDerivedWithoutRecord(t *testing.T) {
	// No record: a mod nobody requires or recommends counts as chosen, an
	// offered one included; built-in mods are never in the set.
	base := inst("base")
	base.BuiltIn = true
	got := Chosen([]world.InstalledMod{base, inst("muluna", "lib", "+ nice", "? moshine"), inst("lib"), inst("nice"), inst("moshine")})
	want := map[string]bool{"muluna": true, "moshine": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Chosen = %v, want %v", got, want)
	}
	if o := Origins([]world.InstalledMod{inst("muluna", "? moshine"), inst("moshine")}); len(o) != 0 {
		t.Errorf("an offered mod with no record is taken as chosen: %+v", o)
	}
}

func TestChosenEmptyRecordIsNotDerived(t *testing.T) {
	ms := recorded([]world.InstalledMod{inst("kept")})
	if got := Chosen(ms); len(got) != 0 {
		t.Errorf("Chosen = %v, want none", got)
	}
}
