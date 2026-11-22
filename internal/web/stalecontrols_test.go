package web

import (
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 7.4: stale map settings name the controls, and the fix is step 3 of
// Generate for the refused world.
func TestStaleControlsWordsAndFix(t *testing.T) {
	p := world.Problem{Kind: world.ProblemStaleControls, Names: []string{"moon-rock", "moon-ice"}}
	if got := english(problemMsg(p)); !strings.Contains(got, "moon-rock, moon-ice") || strings.Contains(got, "{") {
		t.Errorf("words %q", got)
	}
	o := precheckFix(world.PreCheck{Problems: []world.Problem{p}}, "w1")
	if o == nil || o.Path != newMapPath("w1") || o.Post {
		t.Fatalf("fix %+v, want a link to the world's map settings", o)
	}
}
