package web

import (
	"context"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

// unfinished20 makes an Unfinished 2.0 world: new_map and no save (SPEC 5.6).
func (g *modsRig) unfinished20(name string) string {
	g.t.Helper()
	id, err := g.worlds.Generate(context.Background(), world.NewWorld{Name: name, GameVersion: v20})
	if err != nil {
		g.t.Fatal(err)
	}
	return id
}

// SPEC 5.6, 17.10: an Unfinished world's Mods tab has the step bar with step
// 2 current, and a way to skip to step 3.
func TestModsTabOfAnUnfinishedWorldShowsTheStepBar(t *testing.T) {
	g := newModsRig(t, true)
	g.world20("Active")
	id := g.unfinished20("Fresh")
	body := g.get(modsPath(id))
	wantIn(t, "mods tab", body, `id="flow-steps"`, `href="`+modsPath(id)+`" aria-current="step"`, `href="`+newMapPath(id)+`"`,
		`id="mods-skip"`)
	wantIn(t, "skip text", plain(body), "Skip to map settings")
	if strings.Count(body, `aria-current="step"`) != 1 {
		t.Error("not exactly one current step")
	}
	if strings.Contains(body, `id="act-next-map"`) {
		t.Error("Next is offered before any install")
	}
}

// SPEC 5.6: a world with a save has no step bar and no skip link.
func TestModsTabOfAFinishedWorldHasNoStepBar(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Made")
	body := g.get(modsPath(id))
	for _, no := range []string{`id="flow-steps"`, `id="mods-skip"`, `id="act-next-map"`, newMapPath(id)} {
		if strings.Contains(body, no) {
			t.Errorf("a finished world's Mods tab has %s", no)
		}
	}
}

// SPEC 5.6, 17.9: the review shows the step bar and a link to step 3; the
// result after the install has "Next: map settings" as a control.
func TestReviewAndResultOfAnUnfinishedWorldOfferTheNextStep(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.world20("Active")
	id := g.unfinished20("Fresh")
	_, body := g.getStatus(kindPlanPath(id, "install", "alpha"))
	wantIn(t, "review", body, `id="flow-steps"`, `href="`+modsPath(id)+`" aria-current="step"`, `id="plan-skip"`, `href="`+newMapPath(id)+`"`)
	wantIn(t, "review text", plain(body), "Skip to map settings")

	g.wantRedirectTo(modsPath(id)+ownResultQuery, g.post(modsPath(id)+"/plan", planValues(t, body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	tab := g.get(modsPath(id) + ownResultQuery)
	wantIn(t, "result", tab, `id="world-result"`, `id="act-next-map"`, `href="`+newMapPath(id)+`"`)
	wantIn(t, "result text", plain(tab), "Next: map settings")
	if strings.Contains(tab, `id="mods-skip"`) {
		t.Error("the result offers Skip as well as Next")
	}
}

// SPEC 5.6: a review that changes nothing offers Next, not Skip.
func TestReviewWithNothingToChangeOffersNext(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.world20("Active")
	id := g.unfinished20("Fresh")
	g.writeMod(id, "alpha", "1.0.0")
	_, body := g.getStatus(kindPlanPath(id, "update", ""))
	wantIn(t, "review", body, `id="plan-next"`, `href="`+newMapPath(id)+`"`)
	wantIn(t, "text", plain(body), "Next: map settings")
}

// SPEC 5.6: a world with a save has no step bar on its review.
func TestReviewOfAFinishedWorldHasNoStepBar(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Made")
	_, body := g.getStatus(kindPlanPath(id, "install", "alpha"))
	for _, no := range []string{`id="flow-steps"`, `id="plan-skip"`, `id="plan-next"`} {
		if strings.Contains(body, no) {
			t.Errorf("a finished world's review has %s", no)
		}
	}
}

// SPEC 5.6, 9.14: Continue opens step 2 while the world has mod picks not
// yet installed, otherwise step 3.
func TestContinuePath(t *testing.T) {
	g := newModsRig(t, true)
	id := g.unfinished20("Fresh")
	sum, err := g.worlds.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.h.(*Handler).srv.continuePath(context.Background(), sum); got != newMapPath(id) {
		t.Errorf("no picks: %s, want step 3", got)
	}
	g.pick(id, "alpha")
	if got := g.h.(*Handler).srv.continuePath(context.Background(), sum); got != modsPath(id) {
		t.Errorf("a pick: %s, want step 2", got)
	}
}
