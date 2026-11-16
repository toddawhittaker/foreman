package web

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 9.2, 9.13, 17.10: a plan of two picked mods lists both, and one
// confirm installs them and what they need with one snapshot.
func TestReviewOfTwoModsOneSnapshot(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("aai-industry", "1.0.0")
	g.portal.AddZip("aai-containers", "1.0.0", "fp-lib >= 1.0.0")
	g.portal.AddZip("fp-lib", "1.0.0")
	id := g.world20("Base", "Krastorio2Assets_1.0.0")
	plan := "/worlds/" + id + "/mods/plan"

	code, one := g.getStatus(plan + "?kind=install&name=aai-industry")
	if code != http.StatusOK {
		t.Fatalf("plan %d\n%s", code, one)
	}
	wantIn(t, "plan of one", plain(one), "Install aai-industry in Base?", `<p class="plan-label" id="plan-changes" tabindex="-1">Changes:</p>`,
		// A stopped world's install is the primary action (DESIGN 7.1).
		`class="button button--primary" id="act-apply">Install 1 mod</button>`)
	wantIn(t, "plan of one rows", planRows(one), "aai-industry 1.0.0")
	if strings.Contains(one, "from the plan</span>") {
		t.Error("the only mod in a plan offers to take it out; Cancel does that")
	}

	// A taken-out mod redraws the plan and puts focus on the list it left.
	rec := g.harness.get(plan+"?kind=install&name=aai-industry&removed=aai-containers", withCookie(g.cookie), withHeader("HX-Request", "true"))
	wantIn(t, "removed", rec.Body.String(), `Removed aai-containers. The plan has 1 mod.`, `<p class="plan-label" id="plan-changes" tabindex="-1" autofocus>Changes:</p>`)

	// One confirm: both mods and the library, one snapshot.
	_, both := g.getStatus(plan + "?kind=install&name=aai-industry&name=aai-containers")
	wantIn(t, "plan of two", both, `hx-params="none"`, `id="mod-aai-industry-remove"`, `id="mod-aai-containers-remove"`, "Install 3 mods")
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

// SPEC 9.2: adding happens only on the Mods tab, so the review page has no
// search, and its Cancel and a conflict's way back lead to the tab.
func TestReviewPageHasNoAddSearch(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("fp-conf", "1.0.0", "! beta")
	g.portal.AddZip("fp-main", "1.0.0", "? fp-conf")
	g.portal.AddZip("aai-industry", "1.0.0")
	id := g.world20("Base", "beta_1.0.0")
	tab := `href="` + modsPath(id) + `"`

	_, ok := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"aai-industry"}}))
	for _, gone := range []string{"Add another mod", `id="add-search"`, `id="add-heading"`, `name="add"`, `role="search"`, `name="q"`, `class="mod-hits"`} {
		if strings.Contains(ok, gone) {
			t.Errorf("the review page still has %q", gone)
		}
	}
	wantIn(t, "cancel", plain(ok), tab+`>Cancel, change nothing</a>`)

	code, conflict := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"fp-main"}, "optional": {"fp-conf"}}))
	if code != http.StatusConflict {
		t.Fatalf("conflict plan: status %d", code)
	}
	wantIn(t, "conflict", plain(conflict), `id="plan-conflicts"`, tab+`>Back to the mods</a>`)
	if strings.Contains(conflict, "Back to search") {
		t.Error("a conflict sends the admin back to a search page")
	}
}

// SPEC 9.14: the saved picks are cleared when "Install N mods" is accepted,
// and only then.
func TestAcceptedInstallClearsSavedPicks(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("aai-industry", "1.0.0")
	g.portal.AddZip("aai-containers", "1.0.0")
	id := g.world20("Base")
	other := g.world20("Other")
	g.pick(id, "aai-industry", "aai-containers")
	g.pick(other, "aai-industry")
	plan := modsPath(id) + "/plan"

	_, body := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"aai-industry", "aai-containers"}}))
	v := planValues(t, body)

	// A plan that is no longer the one shown applies nothing and keeps the picks.
	stale := url.Values{}
	for k, vals := range v {
		stale[k] = slices.Clone(vals)
	}
	stale.Set("hash", "not-the-plan")
	if code := g.post(plan, stale).StatusCode; code != http.StatusConflict {
		t.Fatalf("a changed plan: status %d, want 409", code)
	}
	if got := g.picks(id); len(got) != 2 {
		t.Errorf("a refused install left picks %v, want both kept", got)
	}

	g.wantRedirectHome(g.post(plan, v))
	if got := g.picks(id); len(got) != 0 {
		t.Errorf("an accepted install left picks %v, want none", got)
	}
	if got := g.picks(other); len(got) != 1 {
		t.Errorf("another world's picks are %v, want kept", got)
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
}

// SPEC 9.14: only an install plan has picks to clear; an update from the
// Mods tab leaves them alone.
func TestAcceptedUpdateKeepsSavedPicks(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("beta", "2.0.0")
	id := g.world20("Base", "beta_1.0.0")
	g.pick(id, "aai-industry")

	_, body := g.getStatus(planPath(id, url.Values{"kind": {"update"}, "name": {"beta"}}))
	g.wantRedirectHome(g.post(modsPath(id)+"/plan", planValues(t, body)))
	if got := g.picks(id); len(got) != 1 {
		t.Errorf("an accepted update left picks %v, want kept", got)
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
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

	// Two gone mods that both have updates: a button that posts, so the address names no mods.
	g3 := newModsRig(t, true)
	g3.portal.AddZip("beta", "2.0.0")
	g3.portal.AddZip("gamma", "2.0.0")
	id3 := g3.world20("Base", "beta_1.0.0", "gamma_1.0.0")
	_, frag = g3.getStatus(modsPath(id3) + "/notice")
	wantIn(t, "two updates", frag, `<form method="post" action="/worlds/`+id3+`/mods/plan/gone"><button type="submit" class="button" id="act-mods-fix">Update 2 mods</button></form>`)

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
	p := ModPlanPage{World: world.Summary{ID: "base"}, Form: planForm{Kind: mods.KindInstall, Names: []string{"a", "b"}}, Draft: "tok"}
	back := draftPath("base", "tok")
	if got, want := p.AccountPath(), PathAccount+"?return="+url.QueryEscape(back); got != want {
		t.Fatalf("account link %q, want %q", got, want)
	}
	form := accountForm(acctUser, acctToken)
	form.Set("return", back)
	ah.wantRedirect(ah.post(PathAccount, form, withCookie(c)), back)
}

// SPEC 9.14: an accepted install takes out of the saved picks only the mods
// it installed; the other picks stay for a later install.
func TestAcceptedInstallKeepsOtherSavedPicks(t *testing.T) {
	g := newModsRig(t, true)
	for _, n := range []string{"mod-a", "mod-b", "mod-c"} {
		g.portal.AddZip(n, "1.0.0")
	}
	id := g.world20("Base")
	g.pick(id, "mod-a", "mod-b")
	plan := modsPath(id) + "/plan"

	// Installing a mod that was never picked leaves both picks.
	_, body := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"mod-c"}}))
	g.wantRedirectHome(g.post(plan, planValues(t, body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if got := g.picks(id); len(got) != 2 {
		t.Errorf("installing another mod left picks %v, want both kept", got)
	}

	// Installing one pick takes out that one only.
	_, body = g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"mod-a"}}))
	g.wantRedirectHome(g.post(plan, planValues(t, body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if got := g.picks(id); len(got) != 1 || got[0] != "mod-b" {
		t.Errorf("picks %v, want only mod-b", got)
	}
}

// SPEC 5.12: the review page after Generate from a favorite says the world
// was made and takes focus; an ordinary review page has no such note.
func TestReviewAfterGenerateSaysTheWorldWasMade(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("aai-industry", "1.0.0")
	id := g.world20("Club world")

	q := url.Values{"kind": {"install"}, "name": {"aai-industry"}}
	_, plain0 := g.getStatus(planPath(id, q))
	if strings.Contains(plain0, `id="plan-generated"`) {
		t.Error("an ordinary review page has the generated note")
	}
	q.Set(generatedQuery, "1")
	_, body := g.getStatus(planPath(id, q))
	wantIn(t, "note", body, `id="plan-generated" tabindex="-1" autofocus`)
	wantIn(t, "note text", plain(body), "Generated Club world from the favorite.")
	if strings.Index(body, `id="plan-generated"`) > strings.Index(body, `id="mod-plan"`) {
		t.Error("the note is not above the plan")
	}
}
