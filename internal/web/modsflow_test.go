package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/mods"
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
// 2 current, and a way to skip to step 3. A world with only built-in mods
// has no step 3, so the bar marks it (none) as plain text.
func TestModsTabOfAnUnfinishedWorldShowsTheStepBar(t *testing.T) {
	g := newModsRig(t, true)
	g.world20("Active")
	id := g.unfinished20("Fresh")
	body := g.get(modsPath(id))
	wantIn(t, "mods tab", body, `aria-label="Steps to make the world"`, `href="`+modsPath(id)+`" aria-current="step"`, `href="`+newMapPath(id)+`"`,
		`id="mods-skip"`, `<a href="`+flowModSettingsPath(id)+`" id="mods-skip">`)
	// Step 3 would pass on, so the link names where it lands.
	wantIn(t, "skip text", plain(body), "Skip to map settings", `3. </span>Mod settings (none)`)
	if strings.Contains(body, `<a href="`+flowModSettingsPath(id)+`">`) {
		t.Error("the skipped step 3 is a link in the bar")
	}
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
	for _, no := range []string{`aria-label="Steps to make the world"`, `id="mods-skip"`, `id="act-next-map"`, newMapPath(id)} {
		if strings.Contains(body, no) {
			t.Errorf("a finished world's Mods tab has %s", no)
		}
	}
}

// SPEC 5.6: the review of step 2's plan shows the step bar and ends with
// the step's footer: Back to the mods, the skip, and Next, which installs
// the plan as Install does. Once the install ends well the wizard goes on to
// step 3, not back to the Mods tab.
func TestReviewOfAnUnfinishedWorldInstallsAndGoesOn(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.world20("Active")
	id := g.unfinished20("Fresh")
	_, body := g.getStatus(kindPlanPath(id, "install", "alpha"))
	wantIn(t, "review", body, `aria-label="Steps to make the world"`, `href="`+modsPath(id)+`" aria-current="step"`,
		`<div class="actions flow-foot">`, `<a href="`+modsPath(id)+`" class="flow-back" id="flow-back">Back to the mods</a>`,
		`href="`+flowModSettingsPath(id)+`" id="plan-skip"`)
	wantIn(t, "review text", plain(body), "Skip to mod settings", `id="act-apply">Next: Mod settings</button>`)
	// The footer is the page's one set of actions: no Cancel, no Install.
	for _, no := range []string{"Cancel, change nothing", "Install 1 mod"} {
		if strings.Contains(plain(body), no) {
			t.Errorf("the review in the flow has %q", no)
		}
	}
	// Back leads the footer; the skip is secondary, before the main button.
	if back, skip, apply := strings.Index(body, `id="flow-back"`), strings.Index(body, `id="plan-skip"`), strings.Index(body, `id="act-apply"`); back < 0 || skip < back || apply < skip {
		t.Errorf("footer order: Back %d, Skip %d, Next %d", back, skip, apply)
	}

	g.wantRedirectTo(modsPath(id)+flowResultQuery, g.post(modsPath(id)+"/plan", planValues(t, body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if list, err := g.worlds.Mods(id); err != nil || !slices.ContainsFunc(list, func(m world.InstalledMod) bool { return m.Name == "alpha" }) {
		t.Errorf("alpha is not installed: %v", err)
	}
	g.wantRedirectTo(flowModSettingsPath(id), g.do(http.MethodGet, modsPath(id)+flowResultQuery, nil, http.Header{"Cookie": {g.cookie.String()}}))
	// The Mods tab reached any other way still shows the result, with Next.
	tab := g.get(modsPath(id) + ownResultQuery)
	wantIn(t, "result", tab, `id="world-result"`, `id="act-next-map"`, `href="`+flowModSettingsPath(id)+`"`)
	if strings.Contains(tab, `id="mods-skip"`) {
		t.Error("the result offers Skip as well as Next")
	}
}

// SPEC 5.6: a change to one installed mod during step 2, such as an update,
// keeps the review's own actions and comes back to the Mods tab.
func TestReviewOfAnotherKindInTheFlowKeepsItsActions(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.portal.AddZip("alpha", "1.1.0")
	g.world20("Active")
	id := g.unfinished20("Fresh")
	g.writeMod(id, "alpha", "1.0.0")
	_, body := g.getStatus(kindPlanPath(id, "update", ""))
	if strings.Contains(body, "flow-foot") {
		t.Error("an update's review has step 2's footer")
	}
	wantIn(t, "update", plain(body), ">Cancel, change nothing</a>", `id="act-apply">Update 1 mod`)
	g.wantRedirectTo(modsPath(id)+ownResultQuery, g.post(modsPath(id)+"/plan", planValues(t, body)))
}

// SPEC 5.6: a review that changes nothing offers Next, not Skip.
func TestReviewWithNothingToChangeOffersNext(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.world20("Active")
	id := g.unfinished20("Fresh")
	g.writeMod(id, "alpha", "1.0.0")
	_, body := g.getStatus(kindPlanPath(id, "update", ""))
	wantIn(t, "review", body, `id="plan-next"`, `href="`+flowModSettingsPath(id)+`"`)
	wantIn(t, "text", plain(body), "Next: Mod settings")
}

// SPEC 5.6: a world with a save has no step bar on its review.
func TestReviewOfAFinishedWorldHasNoStepBar(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Made")
	_, body := g.getStatus(kindPlanPath(id, "install", "alpha"))
	for _, no := range []string{`aria-label="Steps to make the world"`, `id="plan-skip"`, `id="plan-next"`} {
		if strings.Contains(body, no) {
			t.Errorf("a finished world's review has %s", no)
		}
	}
}

// SPEC 5.6, 9.14: Continue opens step 2 while the world has mod picks not
// yet installed; a world with only built-in mods has no step 3, so step 4.
func TestContinuePath(t *testing.T) {
	g := newModsRig(t, true)
	id := g.unfinished20("Fresh")
	sum, err := g.worlds.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := g.h.(*Handler).srv.continuePath(context.Background(), sum); got != newMapPath(id) {
		t.Errorf("no picks: %s, want step 4", got)
	}
	g.pick(id, "alpha")
	if got := g.h.(*Handler).srv.continuePath(context.Background(), sum); got != modsPath(id) {
		t.Errorf("a pick: %s, want step 2", got)
	}
}

// SPEC 5.6, 9.14: a failed install installs nothing, so its picks stay and
// Continue still opens step 2; a successful one takes them out.
func TestContinueAfterAFailedInstallOpensStepTwo(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.SetMode(g.portal.AddZip("alpha", "1.0.0"), modportaltest.WrongSHA1)
	g.world20("Active")
	id := g.unfinished20("Fresh")
	g.pick(id, "alpha")
	sum, err := g.worlds.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	_, body := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"alpha"}}))
	g.wantRedirectTo(modsPath(id)+flowResultQuery, g.post(modsPath(id)+"/plan", planValues(t, body)))
	if err := g.waitResult(id, game.AuditMods); !errors.Is(err, modportal.ErrChecksum) {
		t.Fatalf("result %v, want a checksum failure", err)
	}
	// A failed install does not go on: step 2 shows the failure.
	if rep := g.do(http.MethodGet, modsPath(id)+flowResultQuery, nil, http.Header{"Cookie": {g.cookie.String()}}); rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, `id="world-result"`) {
		t.Errorf("after a failed install: %d, want step 2 with the failure", rep.StatusCode)
	}
	if got := g.picks(id); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("picks %v after a failed install, want alpha kept", got)
	}
	if got := g.h.(*Handler).srv.continuePath(context.Background(), sum); got != modsPath(id) {
		t.Errorf("after a failed install: %s, want step 2", got)
	}
}

// SPEC 5.6, 9.14: a pick that is installed another way, such as an upload,
// is not work that remains, so Continue opens step 3, the mod settings, and
// step 4 once the world has saved mod settings.
func TestContinueIgnoresAPickThatIsInstalled(t *testing.T) {
	g := newModsRig(t, true)
	id := g.unfinished20("Fresh")
	g.pick(id, "alpha", "beta")
	g.writeMod(id, "alpha", "1.0.0")
	sum, err := g.worlds.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s := g.h.(*Handler).srv
	if got := s.continuePath(context.Background(), sum); got != modsPath(id) {
		t.Errorf("beta still not installed: %s, want step 2", got)
	}
	g.writeMod(id, "beta", "1.0.0")
	if got := s.continuePath(context.Background(), sum); got != flowModSettingsPath(id) {
		t.Errorf("both installed: %s, want step 3", got)
	}
	if err := os.WriteFile(filepath.Join(g.worlds.Dirs().Worlds, id, world.ModsDir, world.ModSettingsFile), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.continuePath(context.Background(), sum); got != newMapPath(id) {
		t.Errorf("settings saved: %s, want step 4", got)
	}
}

// SPEC 5.6: once step 4 has been saved, Continue opens step 4 even when
// step 3 was skipped and the world has no mod-settings.dat.
func TestContinueAfterSkippingStepThreeOpensStepFour(t *testing.T) {
	g := newModsRig(t, true)
	id := g.unfinished20("Fresh")
	g.writeMod(id, "alpha", "1.0.0")
	sum, err := g.worlds.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s := g.h.(*Handler).srv
	if got := s.continuePath(context.Background(), sum); got != flowModSettingsPath(id) {
		t.Fatalf("nothing saved yet: %s, want step 3", got)
	}
	s.audit(context.Background(), game.Actor{Username: "admin"}, AuditNewMap, id, "")
	if got := s.continuePath(context.Background(), sum); got != newMapPath(id) {
		t.Errorf("map settings saved: %s, want step 4", got)
	}
}

// SPEC 5.6: step 2's main button reviews a plan that has mods in it, and
// otherwise goes on to step 3, named as step 4 when step 3 has nothing.
func TestStepTwoNext(t *testing.T) {
	review := kindPlanPath("w", mods.KindInstall, "")
	for _, c := range []struct {
		name                       string
		picks, count               int
		unreachable, noModSettings bool
		path, label                string
		isReview                   bool
	}{
		{"no plan, step 3 has settings", 0, 0, false, false, flowModSettingsPath("w"), "Next: Mod settings", false},
		{"no plan, step 3 none", 0, 0, false, true, flowModSettingsPath("w"), "Next: Map settings", false},
		{"one mod", 1, 1, false, false, review, "Next: Review 1 mod", true},
		{"a pick and its dependencies", 1, 3, false, true, review, "Next: Review 3 mods", true},
		{"portal unreachable", 2, 0, true, false, review, "Next: Review the plan", true},
		{"picks already installed", 1, 0, false, false, review, "Next: Review the plan", true},
	} {
		got := stepTwoNext("w", c.picks, c.count, c.unreachable, c.noModSettings)
		if got.Path != c.path || got.Review != c.isReview || i18n.English.Format(got.Label) != c.label {
			t.Errorf("%s: %s %q review=%v, want %s %q review=%v", c.name, got.Path, i18n.English.Format(got.Label), got.Review, c.path, c.label, c.isReview)
		}
	}
}

// SPEC 5.6: the review's Next always names step 3. Whether step 3 has
// anything depends on the mods the install brings, so it is not known
// before; step 3 passes itself on when it has nothing.
func TestReviewNextNamesModSettings(t *testing.T) {
	if got := i18n.English.Format((ModPlanPage{}).NextLabel()); got != "Next: Mod settings" {
		t.Errorf("review Next %q", got)
	}
}

// SPEC 5.6: on step 2 the footer's main button reviews the plan while it
// has mods in it, and the plan panel has no Review of its own; a change to
// the plan swaps the button. Outside Generate the panel keeps its Review.
func TestStepTwoFooterFollowsThePlan(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.world20("Active")
	id := g.unfinished20("Fresh")
	body := g.get(modsPath(id))
	wantIn(t, "no plan", plain(body), `<a href="`+flowModSettingsPath(id)+`" class="button button--primary" id="flow-next">Next: Map settings</a>`)

	g.pick(id, "alpha")
	body = g.get(modsPath(id))
	wantIn(t, "plan", plain(body), `<a href="/worlds/`+id+`/mods/plan?kind=install" class="button button--primary" id="flow-next" data-review>Next: Review 1 mod</a>`,
		`id="plan-wait"`)
	if strings.Contains(body, `id="act-review-plan"`) {
		t.Error("step 2 offers the panel's Review as well as the footer's")
	}
	// The skip stays, and leaves the plan saved for Continue.
	wantIn(t, "skip", body, `id="mods-skip"`)

	rec := g.harness.post(planModsPath(id), url.Values{"name": {"alpha"}, "op": {"remove"}, "from": {"panel"}, "return": {modsPath(id)}},
		withCookie(g.cookie), withHeader("HX-Request", "true"))
	wantIn(t, "swap", plain(rec.Body.String()), `id="flow-next" hx-swap-oob="true">Next: Map settings</a>`)

	other := g.world20("Made")
	g.pick(other, "alpha")
	tab := g.get(modsPath(other))
	wantIn(t, "finished world", tab, `id="act-review-plan"`)
	if strings.Contains(tab, `id="flow-next"`) {
		t.Error("a finished world's Mods tab has step 2's footer")
	}
	// The search page has no footer, so its panel keeps Review.
	g.pick(id, "alpha")
	_, search := g.getStatus(modsPath(id) + "/search?q=alpha")
	wantIn(t, "search page", search, `id="act-review-plan"`)
}
