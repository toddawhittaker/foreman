package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 17.9: the "made" box after step 1 says so once. A reload or a way
// back through history after a mod is picked or installed does not bring
// it back; the built-in mods every world has do not count.
func TestJustMadeSaysItOnlyBeforeAnyMod(t *testing.T) {
	builtIn := []world.InstalledMod{{Name: "base", BuiltIn: true}, {Name: "space-age", BuiltIn: true}}
	cases := []struct {
		name  string
		mods  []world.InstalledMod
		picks []store.ModPlanMod
		want  bool
	}{
		{"fresh", builtIn, nil, true},
		{"a pick saved", builtIn, []store.ModPlanMod{{Name: "alpha"}}, false},
		{"a mod installed", append(builtIn, world.InstalledMod{Name: "alpha"}), nil, false},
	}
	for _, tc := range cases {
		if got := justMade(tc.mods, tc.picks); got != tc.want {
			t.Errorf("%s: %t, want %t", tc.name, got, tc.want)
		}
	}
}
