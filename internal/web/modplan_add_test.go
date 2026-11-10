package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 9.1, 9.2, 17.10: a search result adds its mod to a plan; the plan
// lists the mods asked for, offers the same search to add another, and one
// confirm installs every mod with one snapshot.
func TestAddToPlanTwoModsOneSnapshot(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("aai-industry", "1.0.0")
	g.portal.AddZip("aai-containers", "1.0.0", "fp-lib >= 1.0.0")
	g.portal.AddZip("fp-lib", "1.0.0")
	id := g.world20("Base", "Krastorio2Assets_1.0.0")
	plan := "/worlds/" + id + "/mods/plan"

	_, search := g.getStatus(modsPath(id) + "/search?q=aai")
	wantIn(t, "search", search,
		`href="`+plan+`?kind=install&amp;name=aai-industry&amp;q=aai" class="button" id="mod-aai-industry-add">Add to plan<span class="visually-hidden"> <span translate="no">aai-industry</span></span></a>`,
		`href="https://mods.factorio.com/mod/aai-industry" class="portal-link" target="_blank" rel="noopener noreferrer"`)
	if strings.Contains(search, "Install aai-industry") {
		t.Error("a search result still installs at once")
	}

	code, one := g.getStatus(plan + "?kind=install&name=aai-industry&q=aai")
	if code != http.StatusOK {
		t.Fatalf("plan %d\n%s", code, one)
	}
	wantIn(t, "plan of one", plain(one), "Install aai-industry in Base?", `<p class="plan-label" id="plan-changes" tabindex="-1">Changes:</p>`, "aai-industry 1.0.0", "Add another mod",
		`<input type="hidden" name="q" value="aai">`, `id="add-search" name="q" type="search" value="aai"`,
		// The mod in the plan and the installed one say so; the others can be added.
		`<span class="badge" id="mod-aai-industry-add" tabindex="-1">In plan<span class="visually-hidden"> <span translate="no">aai-industry</span></span></span>`,
		`hx-get="`+plan+`?added=aai-containers&amp;kind=install&amp;name=aai-industry&amp;name=aai-containers&amp;q=aai"`,
		`hx-push-url="`+plan+`?kind=install&amp;name=aai-industry&amp;name=aai-containers&amp;q=aai"`,
		// A stopped world's install is the primary action (DESIGN 7.1).
		`class="button button--primary" id="act-apply">Install 1 mod</button>`)
	if strings.Contains(one, "Remove from plan") {
		t.Error("the only mod in a plan offers to take it out; Cancel does that")
	}
	if strings.Index(one, `id="act-apply"`) > strings.Index(one, `id="add-heading"`) {
		t.Error("Add another mod comes before the confirm button")
	}

	// A mod the world has already is not offered.
	_, other := g.getStatus(plan + "?kind=install&name=aai-industry&q=Krastorio2")
	wantIn(t, "installed", other, `<span class="badge">Installed</span>`, `id="mod-Krastorio2-add"`)

	// A click on Add to plan redraws the plan in place and says what changed.
	rec := g.harness.get(plan+"?added=aai-containers&kind=install&name=aai-industry&name=aai-containers&q=aai", withCookie(g.cookie), withHeader("HX-Request", "true"))
	two := rec.Body.String()
	wantIn(t, "redraw", plain(two), `<div hx-swap-oob="innerHTML:#announcer">Added aai-containers. The plan has 3 mods.</div>`,
		`<h1 id="confirm-heading" hx-swap-oob="true">Install mods in Base?</h1>`, "fp-lib 1.0.0 (aai-containers needs it)", "<dt>Install</dt>",
		`id="mod-aai-containers-add" tabindex="-1">In plan`, "Install 3 mods", "Remove from plan")
	if strings.Contains(between(two, `id="mod-plan"`, `</main>`), "autofocus") {
		t.Error("adding a mod moves focus")
	}

	// Taking one out again puts focus on the list it left.
	rec = g.harness.get(plan+"?kind=install&name=aai-industry&q=aai&removed=aai-containers", withCookie(g.cookie), withHeader("HX-Request", "true"))
	wantIn(t, "removed", rec.Body.String(), `Removed aai-containers. The plan has 1 mod.`, `<p class="plan-label" id="plan-changes" tabindex="-1" autofocus>Changes:</p>`)

	// One confirm: both mods and the library, one snapshot.
	_, both := g.getStatus(plan + "?kind=install&name=aai-industry&name=aai-containers&q=aai")
	wantIn(t, "plan of two", both, `hx-params="none"`, `id="mod-aai-industry-remove"`, `id="mod-aai-containers-remove"`)
	g.wantRedirectHome(g.post(plan, planValues(t, both)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"aai-industry_1.0.0.zip", "aai-containers_1.0.0.zip", "fp-lib_1.0.0.zip"} {
		if !g.hasFile(id, f) {
			t.Errorf("%s is not installed", f)
		}
	}
	if n := g.modSnapshots(id); n != 1 {
		t.Errorf("%d snapshots for one plan, want 1", n)
	}
}

// A plan the portal cannot work out offers nothing to add to.
func TestAddSearchNotOfferedWithoutAPlan(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.FailAPI(http.StatusInternalServerError)
	id := g.world20("Base")
	code, body := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"aai-industry"}, "q": {"fp"}}))
	if code != http.StatusBadGateway {
		t.Fatalf("status %d", code)
	}
	if strings.Contains(body, `id="add-heading"`) {
		t.Error("a plan that could not be made offers to add to it")
	}
}

// DESIGN 7.1, 8.11: danger is for what destroys something; a restart of a
// running world is said in words, not in red.
func TestPlanButtonStyle(t *testing.T) {
	cases := []struct {
		name   string
		page   ModPlanPage
		danger bool
	}{
		{"install, stopped", ModPlanPage{Plan: mods.Plan{Changes: []mods.Change{{Name: "a", Action: mods.ActInstall}}}}, false},
		{"update, stopped", ModPlanPage{Plan: mods.Plan{Changes: []mods.Change{{Name: "a", Action: mods.ActUpgrade}}}}, false},
		{"install, running", ModPlanPage{Running: true, Plan: mods.Plan{Changes: []mods.Change{{Name: "a", Action: mods.ActInstall}}}}, false},
		{"update, running", ModPlanPage{Running: true, Plan: mods.Plan{Changes: []mods.Change{{Name: "a", Action: mods.ActUpgrade}}}}, false},
		{"disable", ModPlanPage{Plan: mods.Plan{Changes: []mods.Change{{Name: "a", Action: mods.ActDisable}}}}, false},
		{"remove", ModPlanPage{Plan: mods.Plan{Changes: []mods.Change{{Name: "a", Action: mods.ActRemove}}}}, true},
		{"drops save content", ModPlanPage{Drops: []string{"a"}, Plan: mods.Plan{Changes: []mods.Change{{Name: "b", Action: mods.ActInstall}}}}, true},
		{"continue without", ModPlanPage{Plan: mods.Plan{Unresolved: []mods.Unresolved{{Name: "a", Choice: &mods.Choice{Kind: mods.ChoiceWithout}}}}}, true},
	}
	for _, c := range cases {
		if got := c.page.Danger(); got != c.danger {
			t.Errorf("%s: danger %v, want %v", c.name, got, c.danger)
		}
	}
}

// The plan page leaves out an empty "Changes" and explains Nearest only
// beside a Nearest choice.
func TestPlanPageOmitsWhatItDoesNotOffer(t *testing.T) {
	r := newTestRenderer(t)
	p := ModPlanPage{World: world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.77"}, Form: planForm{Kind: mods.KindSync, Save: "s.zip"}, Hash: "h",
		Plan: mods.Plan{Unresolved: []mods.Unresolved{{Name: "gone", Version: "1.0.0"}}}}
	_, body := renderIn(t, r, pageCase{"plan", "todd", 200, p}, "en")
	if strings.Contains(body, `id="plan-changes"`) {
		t.Error("an unresolved-only plan shows an empty change list")
	}
	if strings.Contains(body, "Nearest means") {
		t.Error("the hint explains Nearest without a Nearest choice")
	}
	p.Plan.Unresolved[0].Nearest = "1.0.1"
	if _, body := renderIn(t, r, pageCase{"plan", "todd", 200, p}, "en"); !strings.Contains(body, "Nearest means the closest release for Factorio") {
		t.Error("the hint does not explain the Nearest choice it offers")
	}
}

// SPEC 9.3: the Server page's notice names the mod and its fix; several mods
// are one review.
func TestServerModNoticeNamesTheFix(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base", "beta_1.0.0", "gamma_1.0.0")
	// The portal has beta and gamma, but no release of either for the world's game line.
	g.portal.AddZipFor("1.1", "beta", "0.5.0")
	g.portal.AddZipFor("1.1", "gamma", "0.5.0")
	_, frag := g.getStatus(modsPath(id) + "/notice")
	wantIn(t, "two errors", frag, "2 mods of Base can no longer be downloaded", ">Review 2 mods</a>", `href="/worlds/`+id+`/mods"`)

	// Two gone mods that both have updates: the button says what its plan does.
	g3 := newModsRig(t, true)
	g3.portal.AddZip("beta", "2.0.0")
	g3.portal.AddZip("gamma", "2.0.0")
	id3 := g3.world20("Base", "beta_1.0.0", "gamma_1.0.0")
	_, frag = g3.getStatus(modsPath(id3) + "/notice")
	wantIn(t, "two updates", frag, ">Update 2 mods</a>", `href="/worlds/`+id3+`/mods/plan?kind=update&amp;name=beta&amp;name=gamma"`)

	g2 := newModsRig(t, true)
	id2 := g2.world20("Base", "beta_1.0.0")
	g2.portal.AddZipFor("1.1", "beta", "0.5.0")
	_, frag = g2.getStatus(modsPath(id2) + "/notice")
	wantIn(t, "no update", plain(frag), "beta 1.0.0 is gone from the mod portal, so players cannot join.", ">Remove beta</a>",
		`href="/worlds/`+id2+`/mods/plan?kind=remove&amp;name=beta"`)
}

// SPEC 9.11: the token hint links to the factorio.com profile page, where
// the token is, in a new tab so the form keeps what was typed.
func TestAccountLinksToTheProfilePage(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := ah.signIn()
	body := ah.get(PathAccount, withCookie(c)).Body.String()
	want := `Your token is on <a href="https://factorio.com/profile" target="_blank" rel="noopener noreferrer">your factorio.com profile page<span class="visually-hidden"> (opens in a new tab)</span></a>.`
	if !strings.Contains(body, want) {
		t.Errorf("page lacks %q", want)
	}
}

// SPEC 14.17: a plan page's no-account link returns to the same plan, so
// saving the account goes back to it.
func TestPlanAccountLinkReturnsToThePlan(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := ah.signIn()
	p := ModPlanPage{World: world.Summary{ID: "base"}, Form: planForm{Kind: mods.KindInstall, Names: []string{"a", "b"}, Query: "x"}}
	back := planPath("base", url.Values{"kind": {"install"}, "name": {"a", "b"}, "q": {"x"}})
	if got, want := p.AccountPath(), PathAccount+"?return="+url.QueryEscape(back); got != want {
		t.Fatalf("account link %q, want %q", got, want)
	}
	form := accountForm(acctUser, acctToken)
	form.Set("return", back)
	ah.wantRedirect(ah.post(PathAccount, form, withCookie(c)), back)
}

// A search from "Add another mod" reloads the page with focus on its
// results, not on the conflict box above the plan; a plain load of the same
// plan still puts focus on the conflict.
func TestAddSearchKeepsFocusOnResults(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("fp-conf", "1.0.0", "! beta")
	g.portal.AddZip("fp-main", "1.0.0", "? fp-conf")
	id := g.world20("Base", "beta_1.0.0")
	q := url.Values{"kind": {"install"}, "name": {"fp-main"}, "optional": {"fp-conf"}, "q": {"fp"}}
	_, plainLoad := g.getStatus(planPath(id, q))
	wantIn(t, "plain load", plainLoad, `id="plan-conflicts" tabindex="-1" autofocus`, `<input type="hidden" name="add" value="1">`)
	if strings.Contains(plainLoad, `id="add-heading" tabindex="-1" autofocus`) {
		t.Error("a plain load puts focus on the search")
	}
	q.Set("add", "1")
	_, searched := g.getStatus(planPath(id, q))
	wantIn(t, "search", searched, `id="add-heading" tabindex="-1" autofocus`)
	if n := strings.Count(between(searched, `id="mod-plan"`, `</main>`), "autofocus"); n != 1 {
		t.Errorf("a search page has %d autofocus, want 1", n)
	}
	// The marker is the search form's and its page links' alone; the add and remove links do not carry it.
	if regexp.MustCompile(`(href|hx-get|hx-push-url)="[^"]*add=1`).MatchString(searched) {
		t.Error("a link carries the search marker")
	}
	// A redraw sends the whole page's fields, the marker too, and still
	// leaves focus where it is.
	rec := g.harness.get(planPath(id, q), withCookie(g.cookie), withHeader("HX-Request", "true"))
	if strings.Contains(between(rec.Body.String(), `id="mod-plan"`, `</main>`), "autofocus") {
		t.Error("a redraw with the search marker moves focus")
	}
}

// Only the plan page searches the portal for "Add another mod"; applying
// the plan, which carries the search along, does not search again.
func TestPlanApplyDoesNotSearch(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base")
	before := g.portal.ListCalls()
	_, body := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"alpha"}, "q": {"alp"}}))
	if g.portal.ListCalls() == before {
		t.Fatal("the plan page did not search")
	}
	v := planValues(t, body)
	if v.Get("q") != "alp" {
		t.Fatalf("the plan form does not carry the search: %v", v)
	}
	before = g.portal.ListCalls()
	g.wantRedirectHome(g.post(modsPath(id)+"/plan", v))
	if n := g.portal.ListCalls() - before; n != 0 {
		t.Errorf("applying the plan searched the portal %d times", n)
	}
}
