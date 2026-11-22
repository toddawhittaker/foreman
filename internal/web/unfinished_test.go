package web

import (
	"context"
	"strings"
	"testing"
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
