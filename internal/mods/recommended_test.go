package mods

import (
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

func recOf(m string) Reason { return Reason{Code: WhyRecommendedBy, Of: m} }

// A "+" dependency is on by default and offered ticked (SPEC 9.2).
func TestRecommendedChosenByDefault(t *testing.T) {
	p := portal{"k2": {rel("2.1.4", "2.0", "+ lane >= 1.3.0")}, "lane": {rel("1.3.1", "2.0")}}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"k2"}})
	if !plan.Applicable() {
		t.Fatalf("plan not applicable: %+v", plan.Conflicts)
	}
	wantChange(t, plan, "lane", ActInstall, "1.3.1", recOf("k2"))
	want := []Offer{{Name: "lane", Of: "k2", Chosen: true, Recommended: true}}
	if !slices.Equal(plan.Optional, want) {
		t.Errorf("offers = %+v, want %+v", plan.Optional, want)
	}
}

func TestRecommendedUnticked(t *testing.T) {
	p := portal{"k2": {rel("2.1.4", "2.0", "+ lane")}, "lane": {rel("1.3.1", "2.0")}}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"k2"}, Declined: []string{"lane"}})
	if _, ok := change(plan, "lane"); ok {
		t.Errorf("declined recommended mod is in the plan: %+v", plan.Changes)
	}
	want := []Offer{{Name: "lane", Of: "k2", Recommended: true}}
	if !slices.Equal(plan.Optional, want) {
		t.Errorf("offers = %+v, want %+v", plan.Optional, want)
	}
}

// Its version limit holds whenever it is on, like an optional's.
func TestRecommendedVersionLimit(t *testing.T) {
	p := portal{
		"k2":   {rel("2.1.4", "2.0", "+ lane >= 1.3.0")},
		"lane": {rel("1.2.0", "2.0"), rel("1.3.1", "2.0"), rel("1.4.0", "2.0")},
		"x":    {rel("1.0.0", "2.0", "lane <= 1.3.5")},
	}
	w := World{Mods: []world.InstalledMod{installed("lane", "1.2.0", true)}}
	plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"k2", "x"}})
	if !plan.Applicable() {
		t.Fatalf("plan not applicable: %+v", plan.Conflicts)
	}
	if c, _ := change(plan, "lane"); c.To != "1.3.1" {
		t.Errorf("lane = %+v, want 1.3.1", c)
	}
	// Already on, it is held to the limit even when unticked.
	plan = resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"k2"}, Declined: []string{"lane"}})
	if c, _ := change(plan, "lane"); c.Action != ActUpgrade || c.To != "1.4.0" {
		t.Errorf("lane = %+v, want an upgrade to 1.4.0", c)
	}
}

// One the portal lacks, or that cannot fit, is left out with a note.
func TestRecommendedUnavailableDoesNotBlock(t *testing.T) {
	p := portal{
		"k2":  {rel("2.1.4", "2.0", "+ gone", "+ old >= 2.0.0", "+ bad")},
		"old": {rel("1.0.0", "2.0")},
		"bad": {rel("1.0.0", "2.0", "! k2")},
	}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"k2"}})
	if !plan.Applicable() {
		t.Fatalf("plan not applicable: %+v", plan.Conflicts)
	}
	for _, n := range []string{"gone", "old", "bad"} {
		if _, ok := change(plan, n); ok {
			t.Errorf("%s is in the plan", n)
		}
	}
	want := []Offer{{Name: "bad", Of: "k2", Recommended: true}, {Name: "gone", Of: "k2", Recommended: true}, {Name: "old", Of: "k2", Recommended: true}}
	if !slices.Equal(plan.Unavailable, want) {
		t.Errorf("unavailable = %+v, want %+v", plan.Unavailable, want)
	}
	if len(plan.Optional) != 0 {
		t.Errorf("offers = %+v, want none", plan.Optional)
	}
}

// A recommended built-in mod follows the Space Age flag (SPEC 9.7).
func TestRecommendedBuiltInFollowsSpaceAge(t *testing.T) {
	p := portal{"k2": {rel("2.1.4", "2.0", "+ quality")}}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"k2"}})
	if !plan.Applicable() || len(plan.Optional) != 0 || len(plan.Unavailable) != 0 {
		t.Errorf("plan = %+v", plan)
	}
}

// A sync makes the world the save's mods, so nothing is added by default.
func TestRecommendedNotAddedBySync(t *testing.T) {
	p := portal{"k2": {rel("2.1.4", "2.0", "+ lane")}, "lane": {rel("1.3.1", "2.0")}}
	plan := resolve(t, p, World{}, Request{Kind: KindSync, SaveMods: []SaveMod{{Name: "k2", Version: "2.1.4"}}})
	if _, ok := change(plan, "lane"); ok {
		t.Errorf("sync added a recommended mod: %+v", plan.Changes)
	}
}
