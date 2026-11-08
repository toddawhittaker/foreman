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
