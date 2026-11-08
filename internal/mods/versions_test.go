package mods

import (
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 9.2, 9.3: an install or update may hold an asked mod to one release
// on the world's line; its dependencies follow that release.
func TestChosenVersion(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0", "b >= 1.0.0"), rel("2.0.0", "2.0", "b >= 2.0.0"), rel("3.0.0", "1.1")},
		"b": {rel("1.0.0", "2.0"), rel("2.0.0", "2.0")},
	}

	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a"}, Versions: map[string]string{"a": "1.0.0"}})
	wantChange(t, plan, "a", ActInstall, "1.0.0", asked())
	// b is not held: the newest that fits the chosen a.
	wantChange(t, plan, "b", ActInstall, "2.0.0", by("a"))
	if c, _ := change(plan, "a"); c.Release == nil || c.Release.Version != "1.0.0" {
		t.Errorf("a's release = %+v; want the portal's 1.0.0", c.Release)
	}
	// Only the world's line, newest first.
	if got := plan.Releases["a"]; !slices.Equal(got, []string{"2.0.0", "1.0.0"}) {
		t.Errorf("releases of a = %v", got)
	}

	// Going back after an update: a downgrade, with the older release.
	w := World{Mods: []world.InstalledMod{installed("a", "2.0.0", true, "b >= 2.0.0"), installed("b", "2.0.0", true)}}
	plan = resolve(t, p, w, Request{Kind: KindUpdate, Names: []string{"a"}, Versions: map[string]string{"a": "1.0.0"}})
	wantChange(t, plan, "a", ActDowngrade, "1.0.0", asked())
	if c, ok := change(plan, "b"); ok && c.Action != ActKeep {
		t.Errorf("b changed: %+v", c)
	}

	// The installed version is kept with no portal release, even one the
	// portal no longer has.
	w = World{Mods: []world.InstalledMod{installed("a", "1.5.0", true), installed("b", "2.0.0", true)}}
	plan = resolve(t, p, w, Request{Kind: KindUpdate, Names: []string{"a"}, Versions: map[string]string{"a": "1.5.0"}})
	if len(plan.Conflicts) > 0 {
		t.Fatalf("conflicts: %+v", plan.Conflicts)
	}
	if c, ok := change(plan, "a"); !ok || c.Action != ActKeep || c.Release != nil {
		t.Errorf("a = %+v; want kept", c)
	}

	// A version the portal has not got on this line is refused, never fetched.
	for _, v := range []string{"3.0.0", "9.9.9"} {
		plan = resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a"}, Versions: map[string]string{"a": v}})
		if !hasConflict(plan, ConflictNoSuchRelease, "a") {
			t.Errorf("%s: conflicts = %+v; want no-such-release", v, plan.Conflicts)
		}
		for _, c := range plan.Changes {
			if c.Release != nil && c.Release.Version == v {
				t.Errorf("%s: plan downloads it: %+v", v, c)
			}
		}
	}
}

// A chosen version that another mod's limit refuses is a conflict, not
// silently changed.
func TestChosenVersionMeetsNeeds(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0"), rel("2.0.0", "2.0")},
		"c": {rel("1.0.0", "2.0", "a >= 2.0.0")},
	}
	w := World{Mods: []world.InstalledMod{installed("a", "2.0.0", true), installed("c", "1.0.0", true, "a >= 2.0.0")}}
	plan := resolve(t, p, w, Request{Kind: KindUpdate, Names: []string{"a"}, Versions: map[string]string{"a": "1.0.0"}})
	if !hasConflict(plan, ConflictVersion, "a") {
		t.Errorf("conflicts = %+v; want version", plan.Conflicts)
	}
}

// The release list is not part of the plan's hash: a new release on the
// portal does not change what the admin confirmed.
func TestReleasesNotHashed(t *testing.T) {
	a := Plan{Releases: map[string][]string{"a": {"1.0.0"}}}
	b := Plan{Releases: map[string][]string{"a": {"2.0.0", "1.0.0"}}}
	if HashPlan(a, nil) != HashPlan(b, nil) {
		t.Error("hash depends on the release list")
	}
}
