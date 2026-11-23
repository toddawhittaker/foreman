package web

import (
	"context"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 5.6: a world whose map is not made yet is marked Unfinished on the
// list and its page, with a Continue link to where work remains: step 3
// when no mod pick waits, step 2 while one does.
func TestUnfinishedWorldsOfferContinue(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm(), c))
	list := hn.get(PathWorlds, withCookie(c)).Body.String()
	if !strings.Contains(list, `href="`+newMapPath(id)+`" class="button button--primary" id="act-continue-`+id+`"`) {
		t.Errorf("the list lacks Continue:\n%s", list)
	}
	// The active world has a save, so only the new one is Unfinished.
	if n := strings.Count(list, `<span class="badge">Unfinished</span>`); n != 1 {
		t.Errorf("%d Unfinished badges, want 1", n)
	}
	page := hn.get(worldPath(id), withCookie(c)).Body.String()
	if !strings.Contains(page, `id="world-unfinished"`) || !strings.Contains(page, `href="`+newMapPath(id)+`" class="button button--primary" id="act-continue"`) {
		t.Errorf("the world page lacks Continue:\n%s", page)
	}
	if err := hn.store.AddModPlanName(context.Background(), id, "some-mod", "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if page := hn.get(worldPath(id), withCookie(c)).Body.String(); !strings.Contains(page, `href="`+modsPath(id)+`" class="button button--primary" id="act-continue"`) {
		t.Error("Continue does not open step 2 while a pick waits")
	}
}

// Continue is an Unfinished world's one primary action; Switch steps back
// to a plain button beside it, and is primary again once the map is made
// (DESIGN 7.1).
func TestContinueTakesThePrimaryButtonFromSwitch(t *testing.T) {
	render := func(p WorldPage) string {
		t.Helper()
		tpl, err := newTestRenderer(t).template(i18n.English, p)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		if err := tpl.ExecuteTemplate(&b, "content", view{Page: p}); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	sum := world.Summary{ID: "half", Name: "Half", GameVersion: "2.1.24", Unfinished: true}
	page := render(WorldPage{World: sum, CanSwitch: true, ContinuePath: newMapPath("half")})
	if !strings.Contains(page, `class="button button--primary" id="act-continue"`) || !strings.Contains(page, `class="button" id="act-switch"`) {
		t.Errorf("Continue is not the one primary button:\n%s", page)
	}
	sum.Unfinished = false
	if page := render(WorldPage{World: sum, CanSwitch: true}); !strings.Contains(page, `class="button button--primary" id="act-switch"`) {
		t.Error("Switch is not primary on a finished world")
	}
}
