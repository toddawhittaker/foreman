package web

import (
	"context"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
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
	// A Ready world: Switch is the one primary button, Change setup a plain link.
	sum.Unfinished = true
	page = render(WorldPage{World: sum, CanSwitch: true, Ready: true, SetupPath: newMapPath("half")})
	if !strings.Contains(page, `class="button button--primary" id="act-switch"`) || strings.Count(page, "button--primary") != 1 {
		t.Errorf("Switch is not the one primary button of a Ready world:\n%s", page)
	}
	if !strings.Contains(page, `<a href="`+newMapPath("half")+`" id="act-change-setup">Change setup</a>`) {
		t.Errorf("Change setup is not a plain link to step 4:\n%s", page)
	}
}

// SPEC 5.6: a world waiting for its first map is Ready once step 4 was
// saved and Unfinished before; a world with a save is neither.
func TestSetupReady(t *testing.T) {
	for _, c := range []struct {
		name       string
		unfinished bool
		saved      bool
		want       bool
	}{
		{"step 4 saved", true, true, true},
		{"step 4 not saved", true, false, false},
		{"has a save", false, true, false},
		{"has a save, step 4 never saved", false, false, false},
	} {
		if got := setupReady(world.Summary{Unfinished: c.unfinished}, c.saved); got != c.want {
			t.Errorf("%s: ready %t, want %t", c.name, got, c.want)
		}
	}
}

// SPEC 5.6: once step 4 is saved the world shows Ready on the list and its
// page, with no Continue; its page says the first start makes the map and
// links Change setup to step 4.
func TestReadyWorldAfterStepFourSave(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm(), c))
	hn.h.(*Handler).srv.audit(context.Background(), game.Actor{Username: "admin"}, AuditNewMap, id, "")
	list := hn.get(PathWorlds, withCookie(c)).Body.String()
	if strings.Contains(list, `id="act-continue-`+id+`"`) {
		t.Error("a Ready world's row still offers Continue")
	}
	if strings.Contains(list, `<span class="badge">Unfinished</span>`) || strings.Count(list, `<span class="badge">Ready</span>`) != 1 {
		t.Errorf("the list does not mark the one world Ready:\n%s", list)
	}
	page := hn.get(worldPath(id), withCookie(c)).Body.String()
	for _, want := range []string{
		`<span class="badge">Ready</span>`,
		`id="world-ready"`,
		"Its map is made the first time it starts.",
		`<a href="` + newMapPath(id) + `" id="act-change-setup">Change setup</a>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the world page lacks %q", want)
		}
	}
	for _, gone := range []string{`id="world-unfinished"`, `id="act-continue"`, ">Unfinished<"} {
		if strings.Contains(page, gone) {
			t.Errorf("the world page still has %q", gone)
		}
	}
}
