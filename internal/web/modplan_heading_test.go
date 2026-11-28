package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// After a conflict fix, focus moves to the conflicts or the changes, so the
// announcement is one short sentence that does not repeat them.
func TestAnnounceAfterAConflictFix(t *testing.T) {
	p := ModPlanPage{Redraw: true, Form: planForm{Kind: mods.KindInstall, Fixed: "b"},
		Plan: mods.Plan{Conflicts: []mods.Conflict{{}}}}
	if got := english(p.Announce()); got != "Plan updated. 1 conflict left." {
		t.Errorf("conflict left: %q", got)
	}
	p.Plan.Conflicts = nil
	p.Plan.Changes = []mods.Change{{Name: "a", Action: mods.ActInstall}, {Name: "b", Action: mods.ActRemove}}
	if got := english(p.Announce()); got != "Plan updated. It changes 2 mods." {
		t.Errorf("no conflict left: %q", got)
	}
}

// A plan back at the installed version announces the sentence the page
// shows, which says to choose another version.
func TestAnnounceAtTheInstalledVersion(t *testing.T) {
	p := ModPlanPage{Redraw: true, Form: planForm{Kind: mods.KindUpdate, Names: []string{"a"}},
		Plan: mods.Plan{Changes: []mods.Change{{Name: "a", Action: mods.ActKeep, Why: mods.Reason{Code: mods.WhyAsked}, From: "1.0.0"}},
			Releases: map[string][]string{"a": {"1.0.0", "1.1.0"}}}}
	if got := p.Announce().Key; got != "mods.version.same" {
		t.Errorf("with a version list: %q", got)
	}
	p.Plan.Releases = nil
	if got := p.Announce().Key; got != "mods.plan.nothing" {
		t.Errorf("without a version list: %q", got)
	}
}

// The heading and the button agree on when a plan is a mix of changes: a
// remove plan that also removes stays a remove plan.
func TestPlanHeadingAgreesWithButton(t *testing.T) {
	p := ModPlanPage{World: world.Summary{ID: "base", Name: "Base"}, Form: planForm{Kind: mods.KindRemove, Names: []string{"a"}, AlsoRemove: []string{"b"}}}
	if got := p.Heading().Key; got != "mods.plan.heading.remove" {
		t.Errorf("remove plan with a removal fix: heading %q", got)
	}
	p.Form.AlsoDisable = []string{"c"}
	if got := p.Heading().Key; got != "mods.plan.heading.change" {
		t.Errorf("remove plan that also disables: heading %q", got)
	}
}

// SPEC 9.14: the Install button counts the recommended mods apart from the
// others, and says nothing of them when none is ticked.
func TestPlanInstallButtonNamesTheRecommendedCount(t *testing.T) {
	change := func(name string, why mods.ReasonCode) mods.Change {
		return mods.Change{Name: name, To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: why}}
	}
	p := ModPlanPage{World: world.Summary{ID: "base", Name: "Base"}, Form: planForm{Kind: mods.KindInstall, Names: []string{"a"}}}
	p.Plan.Changes = []mods.Change{change("a", mods.WhyAsked), change("b", mods.WhyRequiredBy), change("c", mods.WhyRecommendedBy), change("d", mods.WhyRecommendedBy)}
	if got := p.Button(); got.Key != "mods.plan.button.install" || got.Args["count"] != 2 {
		t.Errorf("button = %+v, want the install label counting the 2 others", got)
	}
	if got := p.ButtonRecommended(); got.Key != "mods.plan.button.recommended" || got.Args["count"] != 2 {
		t.Errorf("recommended = %+v, want the recommended label counting 2", got)
	}
	p.Plan.Changes = p.Plan.Changes[:2]
	if got := p.ButtonRecommended(); !got.IsZero() {
		t.Errorf("with none recommended the button says nothing of them, got %+v", got)
	}
	if got := p.Button(); got.Args["count"] != 2 {
		t.Errorf("button count without recommended = %v, want 2", got.Args["count"])
	}
}

// DESIGN 6, "Controls stay put": while the plan offers a mod to tick, the
// button holds the room of its longest words, so ticking never widens it
// and moves Cancel; with nothing to tick it holds none.
func TestPlanButtonRoomHoldsItsLongestWords(t *testing.T) {
	p := ModPlanPage{World: world.Summary{ID: "base", Name: "Base"}, Form: planForm{Kind: mods.KindInstall, Names: []string{"a"}}}
	p.Plan.Changes = []mods.Change{{Name: "a", To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyAsked}}}
	if got := p.ButtonRoom(); got != nil {
		t.Errorf("nothing offered: room = %+v, want none", got)
	}
	p.Plan.Optional = []mods.Offer{{Name: "b", Of: "a"}}
	room := p.ButtonRoom()
	if len(room) != 1 || english(room[0]) != "Install 88 mods" {
		t.Errorf("optional offered: room = %+v, want the install label at 88", room)
	}
	if english(p.Button()) != "Install 1 mod" {
		t.Errorf("the room leaves the label alone: %q", english(p.Button()))
	}
	p.Plan.Optional = append(p.Plan.Optional, mods.Offer{Name: "c", Of: "a", Recommended: true})
	if room := p.ButtonRoom(); len(room) != 2 || room[1].Key != "mods.plan.button.recommended" || room[1].Args["count"] != 88 {
		t.Errorf("recommended offered: room = %+v, want the recommended words too", room)
	}
}
