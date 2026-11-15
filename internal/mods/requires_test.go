package mods

import (
	"slices"
	"testing"
)

// SPEC 9.2: the plan says which mod needs which, so a shared dependency can
// be shown under each mod that needs it. Optional and incompatible
// dependencies are not requirements.
func TestPlanRequires(t *testing.T) {
	p := portal{
		"a":    {rel("1.0.0", "2.0", "lib", "? opt", "! bad")},
		"b":    {rel("1.0.0", "2.0", "lib >= 1.0.0", "~ base")},
		"lib":  {rel("1.0.0", "2.0", "core")},
		"core": {rel("1.0.0", "2.0")},
		"opt":  {rel("1.0.0", "2.0")},
	}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a", "b"}})
	for name, want := range map[string][]string{"a": {"lib"}, "b": {"lib", "base"}, "lib": {"core"}} {
		if got := plan.Requires[name]; !slices.Equal(got, want) {
			t.Errorf("%s requires %v, want %v", name, got, want)
		}
	}
	if _, ok := plan.Requires["core"]; ok {
		t.Error("a mod with no requirements has an entry")
	}
	// The plan's digest ignores it, so a confirmation made before still binds.
	q := plan
	q.Requires = nil
	if HashPlan(plan, nil) != HashPlan(q, nil) {
		t.Error("Requires changes the plan's digest")
	}
}
