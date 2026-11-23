package mods

import (
	"reflect"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

func inst(name string, deps ...string) world.InstalledMod {
	return world.InstalledMod{Name: name, Enabled: true, Dependencies: deps}
}

func TestOriginsKinds(t *testing.T) {
	got := Origins([]world.InstalledMod{
		inst("planet", "base >= 2.0", "planetslib", "~ ordered", "+ nice", "? extra", "(?) hidden", "! clash"),
		inst("planetslib"), inst("ordered"), inst("nice"), inst("extra"), inst("hidden"), inst("clash"),
	})
	want := map[string]Origin{
		"planetslib": {RequiredBy, []string{"planet"}},
		"ordered":    {RequiredBy, []string{"planet"}},
		"nice":       {RecommendedBy, []string{"planet"}},
		"extra":      {OfferedBy, []string{"planet"}},
		"hidden":     {OfferedBy, []string{"planet"}},
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
	got := Origins([]world.InstalledMod{off, inst("lib"), inst("a", "absent", "bad/name", "? lib2"), inst("lib2")})
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
