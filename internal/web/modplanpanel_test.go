package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Tests for the plan panel beside the search results (SPEC 9.1, 9.2, 9.14):
// only the picked names are saved, and the tree under them is worked out
// each time.

// samplePanel is a panel with a tree, a shared dependency, an update and a
// warning, for the every-page render test.
func samplePanel(w world.Summary) PlanPanel {
	lib := PlanNode{Name: "lib", Title: "Library", Version: "1.0.0", Of: "Alpha"}
	return PlanPanel{World: w, Picks: []string{"alpha", longName}, Started: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), Count: 4, Return: modsPath(w.ID),
		Roots: []PlanNode{
			{Name: "alpha", Title: "Alpha", Version: "1.0.0", Children: []PlanNode{lib,
				{Name: "old", Title: "old", Version: "1.0.0", Of: "Alpha", Note: i18n.M("mods.plan_panel.update", "version", "2.0.0")}}},
			{Name: longName, Title: longName, Version: "0.1.0", Children: []PlanNode{{Name: "lib", Title: "Library", Version: "1.0.0", Of: longName},
				{Name: "gone", Title: "gone", Of: longName, Warnings: []i18n.Msg{i18n.M("mods.plan_panel.warn.not_on_portal")}}}},
		},
		neededBy: map[string]string{"lib": "Alpha"}}
}

func (g *modsRig) pick(id string, names ...string) {
	g.t.Helper()
	for _, n := range names {
		if err := g.store.AddModPlanName(context.Background(), id, n, "todd", time.Now()); err != nil {
			g.t.Fatal(err)
		}
	}
}

func (g *modsRig) picks(id string) []string {
	g.t.Helper()
	list, err := g.store.ModPlan(context.Background(), id)
	if err != nil {
		g.t.Fatal(err)
	}
	var out []string
	for _, m := range list {
		out = append(out, m.Name)
	}
	return out
}

func (g *modsRig) postPlan(path string, form url.Values, extra ...func(*http.Request)) *httpResult {
	g.t.Helper()
	rec := g.harness.post(path, form, append([]func(*http.Request){withCookie(g.cookie)}, extra...)...)
	return &httpResult{code: rec.Code, loc: rec.Header().Get("Location"), body: rec.Body.String()}
}

type httpResult struct {
	code      int
	loc, body string
}

func planModsPath(id string) string  { return modsPath(id) + "/plan/mods" }
func planClearPath(id string) string { return modsPath(id) + "/plan/clear" }

// Without script "+" and "−" save the pick and go back to the row they came
// from, keeping the search and its page; the cross and Clear plan go back to
// the panel (SPEC 9.1, 9.14).
func TestPlanPanelAddRemoveClear(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.portal.AddZip("beta", "1.0.0")
	id := g.world20("Base")
	back := modsPath(id) + "/search?q=al&page=2"
	r := g.postPlan(planModsPath(id), url.Values{"name": {"alpha"}, "op": {"add"}, "return": {back}})
	if r.code != http.StatusSeeOther || r.loc != modsPath(id)+"/search?page=2&q=al#mod-alpha-hit" {
		t.Fatalf("add: %d to %q", r.code, r.loc)
	}
	g.postPlan(planModsPath(id), url.Values{"name": {"beta"}, "op": {"add"}})
	g.postPlan(planModsPath(id), url.Values{"name": {"alpha"}, "op": {"add"}})
	if got := g.picks(id); !slices.Equal(got, []string{"alpha", "beta"}) {
		t.Errorf("picks %v, want alpha and beta once each", got)
	}

	_, tab := g.getStatus(modsPath(id))
	panel := plain(between(tab, `id="plan-panel"`, "</section>"))
	wantIn(t, "tab", panel, `<span class="plan-panel__sum">2 mods in the plan</span> <span class="plan-panel__names">alpha, beta</span>`,
		`<span class="plan-tree__name" translate="no">alpha</span> <span class="mod-ver" translate="no">1.0.0</span>`,
		`id="mod-alpha-unpick"><span class="visually-hidden">Remove alpha from the plan</span>`,
		`>Clear plan</button>`, "Started <time",
		`<a href="/worlds/`+id+`/mods/plan?kind=install&amp;name=alpha&amp;name=beta" class="button button--primary" id="act-review-plan">Review and install 2 mods</a>`)

	r = g.postPlan(planModsPath(id), url.Values{"name": {"alpha"}, "op": {"remove"}, "from": {"panel"}, "return": {modsPath(id)}})
	if r.code != http.StatusSeeOther || r.loc != modsPath(id)+"#plan-panel" {
		t.Errorf("remove: %d to %q", r.code, r.loc)
	}
	if got := g.picks(id); !slices.Equal(got, []string{"beta"}) {
		t.Errorf("after remove: %v", got)
	}
	r = g.postPlan(planClearPath(id), url.Values{"return": {modsPath(id)}})
	if r.code != http.StatusSeeOther || r.loc != modsPath(id)+"#plan-panel" || len(g.picks(id)) != 0 {
		t.Errorf("clear: %d to %q, picks %v", r.code, r.loc, g.picks(id))
	}
	_, empty := g.getStatus(modsPath(id))
	wantIn(t, "empty", plain(empty), `<section class="panel plan-panel plan-panel--empty" id="plan-panel"`,
		"No mods in the plan yet. Choose &#43; beside a mod to add it, then search again for the next.")
	if strings.Contains(empty, "act-review-plan") {
		t.Error("an empty plan offers Review")
	}
}

// A bad request is refused; the return address is only ever one of this
// world's mods pages.
func TestPlanPanelRefusesBadRequests(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	for _, form := range []url.Values{
		{"op": {"add"}},
		{"name": {"alpha"}, "op": {"tick"}},
		{"name": {strings.Repeat("a", maxPickName+1)}, "op": {"add"}},
	} {
		if r := g.postPlan(planModsPath(id), form); r.code != http.StatusBadRequest {
			t.Errorf("%v: %d", form, r.code)
		}
	}
	// A name no mod can have is refused before it is saved, with a reason.
	for _, bad := range []string{"../alpha", ".hidden", "a/b", `a\b`, "a\x01b"} {
		r := g.postPlan(planModsPath(id), url.Values{"name": {bad}, "op": {"add"}})
		if r.code != http.StatusBadRequest || !strings.Contains(r.body, "That is not a mod name Foreman can add.") {
			t.Errorf("add %q: %d", bad, r.code)
		}
		rec := g.harness.post(planModsPath(id), url.Values{"name": {bad}, "op": {"add"}}, withCookie(g.cookie), withHeader("HX-Request", "true"))
		wantIn(t, "bad name with script", rec.Body.String(), `id="plan-error"`, `data-focus="plan-heading"`)
		if rec.Code != http.StatusBadRequest || strings.Contains(rec.Body.String(), "<main") {
			t.Errorf("add %q with script: %d", bad, rec.Code)
		}
	}
	if len(g.picks(id)) != 0 {
		t.Error("a refused request saved a pick")
	}
	// Taking a name out needs no check: it can only remove.
	if r := g.postPlan(planModsPath(id), url.Values{"name": {"../alpha"}, "op": {"remove"}}); r.code != http.StatusSeeOther {
		t.Errorf("remove an odd name: %d", r.code)
	}
	for ret, want := range map[string]string{
		"https://example.com/":        modsPath(id),
		"//example.com/x":             modsPath(id),
		"/worlds":                     modsPath(id),
		"/worlds/other/mods":          modsPath(id),
		modsPath(id) + "/search?q=ab": modsPath(id) + "/search?q=ab",
	} {
		r := g.postPlan(planClearPath(id), url.Values{"return": {ret}})
		if r.loc != want+"#plan-panel" {
			t.Errorf("return %q: went to %q", ret, r.loc)
		}
	}
	if r := g.postPlan(modsPath("nope")+"/plan/mods", url.Values{"name": {"a"}, "op": {"add"}}); r.code != http.StatusNotFound {
		t.Errorf("unknown world: %d", r.code)
	}
}

// Another site's page cannot change a signed-in person's plan.
func TestPlanPanelRefusesCrossSite(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	g.pick(id, "beta")
	for _, path := range []string{planModsPath(id), planClearPath(id)} {
		r := g.postPlan(path, url.Values{"name": {"alpha"}, "op": {"add"}}, withHeader("Sec-Fetch-Site", "cross-site"))
		if r.code != http.StatusForbidden {
			t.Errorf("%s from another site: %d", path, r.code)
		}
	}
	if got := g.picks(id); !slices.Equal(got, []string{"beta"}) {
		t.Errorf("picks %v after cross-site posts", got)
	}
}

// SPEC 9.14: the plan holds at most store.MaxModPlanNames picks; one more is
// refused with a message on the page the person was on.
func TestPlanPanelLimit(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	for i := range store.MaxModPlanNames {
		g.pick(id, fmt.Sprintf("m%02d", i))
	}
	// The portal is down, so the full panel draws from the names alone.
	g.portal.FailAPI(http.StatusInternalServerError)
	r := g.postPlan(planModsPath(id), url.Values{"name": {"extra"}, "op": {"add"}, "return": {modsPath(id)}})
	if r.code != http.StatusSeeOther || r.loc != modsPath(id)+"?full=1#plan-panel" {
		t.Fatalf("full: %d to %q", r.code, r.loc)
	}
	_, body := g.getStatus(strings.TrimSuffix(r.loc, "#plan-panel"))
	wantIn(t, "full", body, `<p class="error" id="plan-error"><strong>Error:</strong> The plan already holds 50 mods, the most one plan can.`)
	if slices.Contains(g.picks(id), "extra") {
		t.Error("a pick past the limit was saved")
	}
	rec := g.harness.post(planModsPath(id), url.Values{"name": {"extra"}, "op": {"add"}}, withCookie(g.cookie), withHeader("HX-Request", "true"))
	wantIn(t, "full with script", rec.Body.String(), `id="plan-error"`, `data-focus="plan-heading"`)
	// The next change drops the message from the address.
	r = g.postPlan(planModsPath(id), url.Values{"name": {"m00"}, "op": {"remove"}, "return": {modsPath(id) + "?full=1"}})
	if r.loc != modsPath(id)+"#mod-alpha-hit" && r.loc != modsPath(id)+"#mod-m00-hit" {
		t.Errorf("after remove: %q", r.loc)
	}
}

// SPEC 9.2 (rulings 2-4): each pick shows the dependencies that change under
// it, a shared one under each pick, nested as deep as they go; built-in mods
// and installed ones that fit are left out; an update says its version; a
// dependency that cannot be had shows a warning. The count is of distinct
// mods.
func TestPlanPanelTree(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0", "lib", "base", "fits")
	g.portal.AddZip("beta", "1.0.0", "lib >= 1.0.0", "old >= 2.0.0", "gone")
	g.portal.AddZip("lib", "1.0.0", "core")
	g.portal.AddZip("core", "1.0.0")
	g.portal.AddZip("fits", "1.0.0")
	g.portal.AddZip("old", "1.0.0")
	g.portal.AddZip("old", "2.0.0")
	id := g.world20("Base", "fits_1.0.0", "old_1.0.0")
	g.pick(id, "alpha", "beta")
	_, body := g.getStatus(modsPath(id))
	panel := squash(plain(between(body, `id="plan-panel"`, "</section>")))
	alpha := panel[strings.Index(panel, `<li class="plan-tree__pick">`):strings.LastIndex(panel, `<li class="plan-tree__pick">`)]
	beta := panel[strings.LastIndex(panel, `<li class="plan-tree__pick">`):]

	dep := func(name, version, of string) string {
		return `<span class="plan-tree__name" translate="no">` + name + `</span><span class="mod-ver" translate="no">` + version +
			`</span></div><p class="plan-tree__why">Needed by ` + of + `</p>`
	}
	wantIn(t, "alpha", alpha, dep("lib", "1.0.0", "alpha"), dep("core", "1.0.0", "lib"))
	wantIn(t, "beta", beta, dep("lib", "1.0.0", "beta"), dep("core", "1.0.0", "lib"),
		dep("old", "1.0.0", "beta")+`<p class="plan-tree__note">Will update to 2.0.0</p>`,
		`<span class="plan-tree__name" translate="no">gone</span></div><p class="plan-tree__why">Needed by beta</p><p class="plan-tree__warn"><strong class="visually-hidden">Warning:</strong> Not on the mod portal</p>`)
	// core hangs under lib, not beside it.
	if !strings.Contains(alpha, `<p class="plan-tree__why">Needed by alpha</p><ul class="plan-tree__deps" role="list"><li class="plan-tree__dep"><div class="plan-tree__row">`+
		`<span class="plan-tree__name" translate="no">core</span>`) {
		t.Error("core is not nested under lib")
	}
	for _, hidden := range []string{">base<", ">fits<"} {
		if strings.Contains(panel, hidden) {
			t.Errorf("the panel shows %s, which needs no change", hidden)
		}
	}
	// alpha, beta, lib, core and old change; gone cannot be had.
	wantIn(t, "count", panel, ">Review and install 5 mods</a>")
}

// A pick that is installed already says so, and counts nothing.
func TestPlanPanelInstalledPick(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base", "alpha_1.0.0")
	g.pick(id, "alpha")
	_, body := g.getStatus(modsPath(id))
	wantIn(t, "installed pick", plain(between(body, `id="plan-panel"`, "</section>")),
		`<p class="plan-tree__note">Already installed</p>`, ">Review the plan</a>")
}

// SPEC 9.2: a portal that cannot be reached still lists the picks by name,
// says the dependencies cannot be checked now, and offers to check again.
func TestPlanPanelPortalDown(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	g.pick(id, "alpha", "beta")
	g.portal.FailAPI(http.StatusInternalServerError)
	code, body := g.getStatus(modsPath(id))
	if code != http.StatusOK {
		t.Fatalf("tab: %d", code)
	}
	panel := plain(between(body, `id="plan-panel"`, "</section>"))
	wantIn(t, "down", panel, `<span class="plan-tree__name" translate="no">alpha</span>`, `<span class="plan-tree__name" translate="no">beta</span>`,
		"Foreman could not reach the mod portal, so it cannot check what these mods need now.",
		`<a href="/worlds/`+id+`/mods" class="button">Check again</a>`, ">Review the plan</a>")
}

// With script a change answers with the panel, each result row on the page
// redrawn out of band, the words to announce and where focus goes.
func TestPlanPanelHtmxReply(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0", "lib")
	g.portal.AddZip("lib", "1.0.0")
	g.portal.AddZip("other", "1.0.0")
	id := g.world20("Base", "inst_1.0.0")
	rows := url.Values{"name": {"alpha"}, "op": {"add"}, "row": {"alpha", "lib", "other", "inst"}}
	rec := g.harness.post(planModsPath(id), rows, withCookie(g.cookie), withHeader("HX-Request", "true"))
	if rec.Code != http.StatusOK {
		t.Fatalf("add: %d", rec.Code)
	}
	body := squash(plain(rec.Body.String()))
	wantIn(t, "add", body, `<section class="panel plan-panel" id="plan-panel"`,
		`<p class="visually-hidden" data-said data-focus="mod-alpha-plan">alpha added to the plan. The plan has 1 mod.</p>`,
		`<div class="hit__act" id="mod-alpha-act" hx-swap-oob="true">`, `<span class="badge badge--chosen">In plan</span>`,
		`<div class="hit__act" id="mod-lib-act" hx-swap-oob="true"><span class="badge badge--needed" id="mod-lib-plan" tabindex="-1">Needed by alpha</span>`,
		`<div class="hit__act" id="mod-other-act" hx-swap-oob="true">`,
		`<div class="hit__act" id="mod-inst-act" hx-swap-oob="true"><span class="badge">Installed</span>`)
	if strings.Contains(body, "<main") {
		t.Error("the reply is a whole page")
	}

	rec = g.harness.post(planModsPath(id), url.Values{"name": {"alpha"}, "op": {"remove"}, "from": {"panel"}, "row": {"lib"}},
		withCookie(g.cookie), withHeader("HX-Request", "true"))
	wantIn(t, "remove", plain(rec.Body.String()), `data-focus="plan-heading">alpha removed from the plan. The plan has 0 mods.</p>`,
		`id="mod-lib-act" hx-swap-oob="true">`, `<input type="hidden" name="op" value="add">`)

	// More rows than a page has are not redrawn.
	many := url.Values{"name": {"other"}, "op": {"add"}}
	for i := range maxPanelRows + 5 {
		many.Add("row", fmt.Sprintf("r%02d", i))
	}
	rec = g.harness.post(planModsPath(id), many, withCookie(g.cookie), withHeader("HX-Request", "true"))
	if n := strings.Count(rec.Body.String(), `hx-swap-oob="true"`); n != maxPanelRows {
		t.Errorf("%d rows redrawn, want %d", n, maxPanelRows)
	}
	rec = g.harness.post(planClearPath(id), url.Values{}, withCookie(g.cookie), withHeader("HX-Request", "true"))
	wantIn(t, "clear", plain(rec.Body.String()), `data-focus="plan-heading">The plan is empty.</p>`, "No mods in the plan yet.")
}

// SPEC 3.4: deleting a world deletes its unfinished plan.
func TestWorldDeleteClearsThePlan(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	_, id := hn.seedTwo()
	if err := hn.store.AddModPlanName(context.Background(), id, "alpha", "todd", time.Now()); err != nil {
		t.Fatal(err)
	}
	if rec := hn.post(deletePath(id), url.Values{"confirm_name": {"Second"}}, withCookie(c)); rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", rec.Code)
	}
	if list, err := hn.store.ModPlan(context.Background(), id); err != nil || len(list) != 0 {
		t.Errorf("plan after delete: %v, %v", list, err)
	}
}

// squash drops the white space between tags, so a test can read nested markup as one line.
func squash(s string) string { return tagGap.ReplaceAllString(s, "><") }

var tagGap = regexp.MustCompile(`>\s+<`)

// The panel's tree is built from the plan alone: shared dependencies under
// each pick, picks never under another pick, and a cycle ends.
func TestPlanTreeBuild(t *testing.T) {
	plan := mods.Plan{
		Changes: []mods.Change{
			{Name: "a", To: "1", Action: mods.ActInstall}, {Name: "b", To: "1", Action: mods.ActInstall},
			{Name: "lib", To: "1", Action: mods.ActInstall}, {Name: "loop", To: "1", Action: mods.ActInstall},
			{Name: "kept", From: "1", To: "1", Action: mods.ActKeep}, {Name: "deep", To: "2", Action: mods.ActInstall},
		},
		Requires: map[string][]string{"a": {"lib", "b", "kept"}, "b": {"lib"}, "lib": {"loop"}, "loop": {"lib"}, "kept": {"deep"}},
	}
	tr := planTree{plan: plan, title: func(n string) string { return strings.ToUpper(n) }, titles: map[string]string{}, changes: map[string]mods.Change{}}
	roots, count, needed := tr.build([]string{"a", "b"})
	names := func(ns []PlanNode) (out []string) {
		for _, n := range ns {
			out = append(out, n.Name+"<"+n.Of)
		}
		return out
	}
	// kept is hidden, so deep is lifted into its place under A.
	if got := names(roots[0].Children); !slices.Equal(got, []string{"lib<A", "deep<A"}) {
		t.Errorf("a's children %v", got)
	}
	if got := names(roots[0].Children[0].Children); !slices.Equal(got, []string{"loop<LIB"}) || len(roots[0].Children[0].Children[0].Children) != 0 {
		t.Errorf("lib's children %v", got)
	}
	if got := names(roots[1].Children); !slices.Equal(got, []string{"lib<B"}) {
		t.Errorf("b's children %v", got)
	}
	if count != 5 || needed["lib"] != "A" || needed["deep"] != "A" || needed["b"] != "" {
		t.Errorf("count %d, needed %v", count, needed)
	}
}
