package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal"
)

// SPEC 9.1: search results are a table shown a page at a time;
// the count says how many match in all, so a cut is never hidden.
func TestPageOfHits(t *testing.T) {
	all := make([]modportal.Hit, 25)
	for _, tc := range []struct {
		page, want, rows, first, last int
	}{
		{1, 1, 10, 1, 10},
		{3, 3, 5, 21, 25},
		{0, 1, 10, 1, 10},  // before the first page
		{99, 3, 5, 21, 25}, // past the last
	} {
		h := pageOfHits(all, tc.page)
		if h.Page != tc.want || len(h.Rows) != tc.rows || h.First() != tc.first || h.Last() != tc.last || h.Total != 25 || h.Pages != 3 {
			t.Errorf("page %d: got page %d of %d, %d rows, %d to %d", tc.page, h.Page, h.Pages, len(h.Rows), h.First(), h.Last())
		}
	}
	if h := pageOfHits(nil, 2); h.Page != 1 || h.Pages != 1 || len(h.Rows) != 0 {
		t.Errorf("no hits: %+v", h)
	}
}

// pagerPortal answers a search for "pager" with 25 mods, which the
// recorded list does not have; the portal serves each one's releases.
type pagerPortal struct{ ModPortal }

func (p pagerPortal) Search(ctx context.Context, line, query string) ([]modportal.Hit, error) {
	if query != "pager" {
		return p.ModPortal.Search(ctx, line, query)
	}
	hits := make([]modportal.Hit, 25)
	for i := range hits {
		name := fmt.Sprintf("pager-%02d", i)
		hits[i] = modportal.Hit{Name: name, Title: "Pager " + name[6:], Summary: "Pages through " + name, LatestVersion: "1.0.0", Downloads: 1000 - i}
	}
	return hits, nil
}

// pagerRig is a stopped 2.0 world, Base, with pager-03 installed.
func pagerRig(t *testing.T) (*modsRig, string) {
	g := newModsRigWrapped(t, true, func(p ModPortal) ModPortal { return pagerPortal{p} })
	for i := range 25 {
		g.portal.AddZip(fmt.Sprintf("pager-%02d", i), "1.0.0")
	}
	return g, g.world20("Base", "pager-03_1.0.0")
}

// SPEC 9.1: the results are a list of ten rows a page, the count says how
// many match and which show, and the page controls sit above and below.
func TestSearchPagesAndList(t *testing.T) {
	g, id := pagerRig(t)
	code, body := g.getStatus(modsPath(id) + "/search?q=pager")
	if code != http.StatusOK {
		t.Fatalf("search %d", code)
	}
	main := plain(between(body, "<main", "</main>"))
	search := "/worlds/" + id + "/mods/search"
	wantIn(t, "search", main, "<h1>Base <span",
		`<section class="panel mod-found" id="results" aria-labelledby="results-heading">`,
		`<h2 id="results-heading" tabindex="-1">Mods matching &#34;pager&#34;</h2>`,
		`<p class="mod-found__count" id="results-count">25 mods for Factorio 2.0 match. Showing 1 to 10.</p>`,
		`<ul class="hits" role="list" aria-labelledby="results-count">`,
		`<h3 class="hit__title"><a href="https://mods.factorio.com/mod/pager-00" target="_blank" rel="noopener noreferrer">Pager 00<span class="visually-hidden"> on the mod portal, opens in a new tab</span></a> <code translate="no">pager-00</code></h3>`,
		`<p class="hit__summary">Pages through pager-00</p>`,
		`<div><dt>Latest</dt><dd translate="no">1.0.0</dd></div>`, `<div><dt>Downloads</dt><dd>1,000</dd></div>`,
		`<nav class="pager result-pager" aria-label="Result pages, above the list"`,
		`<nav class="pager result-pager" aria-label="Result pages, below the list"`,
		`<label for="page-top">Page</label>`, `id="page-top" name="page" type="text" inputmode="numeric"`, `value="1"`,
		`<span id="pages-top">of 3</span>`, `<span class="visually-hidden">Go to page</span>`,
		`<a href="`+search+`?page=2&amp;q=pager" class="button button--quiet result-pager__step" rel="next" hx-get="`+search+`?page=2&amp;q=pager">Next page</a>`,
		// The search page is the no-script page, so paging keeps its address.
		`hx-push-url="true"`)
	if n := strings.Count(main, `<li class="hit"`); n != 10 {
		t.Errorf("%d rows, want 10", n)
	}
	if strings.Contains(main, "Previous page") || strings.Contains(main, "<table") || strings.Contains(main, "autofocus") ||
		strings.Contains(main, "Updated") || strings.Contains(main, `name="pick"`) {
		t.Error("the first page offers a previous page, is a table, moves focus, dates a hit with no date, or has boxes to tick")
	}
	// Each row offers "+" named for its mod; the installed one says so instead.
	wantIn(t, "add", main, `<button type="submit" class="button plan-toggle" id="mod-pager-00-plan"><span class="visually-hidden">Add pager-00 to the plan</span></button>`,
		`<span class="hit__busy">Adding…</span>`)
	wantIn(t, "installed", between(main, `id="mod-pager-03-hit"`, "</li>"), `<span class="badge">Installed</span>`)
	if strings.Contains(between(main, `id="mod-pager-03-hit"`, "</li>"), "plan-toggle") {
		t.Error("an installed mod offers +")
	}

	// The last page: five rows, focus on the results, Previous and no Next.
	_, last := g.getStatus(modsPath(id) + "/search?q=pager&page=3")
	wantIn(t, "last page", plain(last), "Showing 21 to 25.", `<span id="pages-bottom">of 3</span>`, `id="results-heading" tabindex="-1" autofocus`,
		`rel="prev" hx-get="`+search+`?page=2&amp;q=pager">Previous page</a>`)
	if strings.Contains(between(last, "<main", "</main>"), "Next page") {
		t.Error("the last page offers a next page")
	}
	// A page past the end shows the last page; one that is not a number, the first.
	_, past := g.getStatus(modsPath(id) + "/search?q=pager&page=99")
	wantIn(t, "past", past, "Showing 21 to 25.")
	_, junk := g.getStatus(modsPath(id) + "/search?q=pager&page=x")
	wantIn(t, "junk", junk, "Showing 1 to 10.")
}

// SPEC 9.1: a hit with a release date shows it, with the date written by the
// server and put in the reader's time zone by script.
func TestSearchShowsTheUpdateDate(t *testing.T) {
	when := time.Date(2026, 7, 29, 14, 5, 0, 0, time.UTC)
	g := newModsRigWrapped(t, true, func(p ModPortal) ModPortal { return datedPortal{p, when} })
	id := g.world20("Base")
	_, body := g.getStatus(modsPath(id) + "/search?q=dated")
	wantIn(t, "dated", plain(body), `<div><dt>Updated</dt><dd><time datetime="2026-07-29T14:05:00Z" data-day>2026-07-29</time></dd></div>`)
}

type datedPortal struct {
	ModPortal
	when time.Time
}

func (p datedPortal) Search(context.Context, string, string) ([]modportal.Hit, error) {
	return []modportal.Hit{{Name: "dated", Title: "Dated", LatestVersion: "1.0.0", ReleasedAt: p.when}}, nil
}

// SPEC 9.1: the search page and the Mods tab's typing share this reply; a
// request with script under 2 characters asks the portal nothing and says
// what to type, and a search left empty without script is refused.
func TestSearchShortAndEmpty(t *testing.T) {
	g, id := pagerRig(t)
	calls := g.portal.ListCalls()
	rec := g.harness.get(modsPath(id)+"/search?q=p&in=tab", withCookie(g.cookie), withHeader("HX-Request", "true"))
	if rec.Code != http.StatusOK {
		t.Fatalf("short: %d", rec.Code)
	}
	wantIn(t, "short", rec.Body.String(), `<h2 id="results-heading" tabindex="-1">Mods matching</h2>`,
		`<p class="empty">Type at least 2 characters of a mod&#39;s name in Find a mod. Matching mods show here.</p>`)
	if g.portal.ListCalls() != calls {
		t.Error("a search under 2 characters asked the portal")
	}
	for _, q := range []string{"", "%20%20", strings.Repeat("a", maxSearchQuery+1)} {
		if code, _ := g.getStatus(modsPath(id) + "/search?q=" + q); code != http.StatusBadRequest {
			t.Errorf("search %q: %d", q, code)
		}
	}
}

// The Mods tab's own requests keep its address: their page controls ask for
// the tab's results and push no history.
func TestSearchForTheTabKeepsItsAddress(t *testing.T) {
	g, id := pagerRig(t)
	rec := g.harness.get(modsPath(id)+"/search?q=pager&in=tab", withCookie(g.cookie), withHeader("HX-Request", "true"))
	body := between(rec.Body.String(), `id="results"`, "</section>")
	wantIn(t, "tab", body, `hx-get="/worlds/`+id+`/mods/search?in=tab&amp;page=2&amp;q=pager"`, `<input type="hidden" name="in" value="tab">`,
		// Without script a "+" goes back to the tab.
		`<input type="hidden" name="return" value="/worlds/`+id+`/mods">`)
	if strings.Contains(body, "hx-push-url") {
		t.Error("paging on the Mods tab changes the address")
	}
}

// SPEC 9.1: a result's state follows the plan: "In plan" with "−" for a
// pick, "Needed by" with no button for a dependency, "+" otherwise.
func TestSearchRowsFollowThePlan(t *testing.T) {
	g, id := pagerRig(t)
	g.portal.AddZip("pager-01", "1.1.0", "pager-02")
	if err := g.store.AddModPlanName(context.Background(), id, "pager-01", "todd", time.Now()); err != nil {
		t.Fatal(err)
	}
	_, body := g.getStatus(modsPath(id) + "/search?q=pager")
	main := plain(between(body, "<main", "</main>"))
	picked := between(main, `id="mod-pager-01-hit"`, "</li>")
	wantIn(t, "picked", picked, `<span class="badge badge--chosen">In plan</span>`, `<input type="hidden" name="op" value="remove">`,
		`id="mod-pager-01-plan"><span class="visually-hidden">Remove pager-01 from the plan</span>`)
	needed := between(main, `id="mod-pager-02-hit"`, "</li>")
	wantIn(t, "needed", needed, `<span class="badge badge--needed" id="mod-pager-02-plan" tabindex="-1">Needed by pager-01</span>`)
	if strings.Contains(needed, "<button") {
		t.Error("a dependency has a button of its own")
	}
	wantIn(t, "other", between(main, `id="mod-pager-04-hit"`, "</li>"), `<input type="hidden" name="op" value="add">`)
}

// The plan page's "Add another mod" is the same table with links to its
// pages, and an empty search says what to try.
func TestAddAnotherModPages(t *testing.T) {
	g, id := pagerRig(t)
	q := url.Values{"kind": {"install"}, "name": {"pager-00"}, "q": {"pager"}}
	_, body := g.getStatus(planPath(id, q))
	next := url.Values{"add": {"1"}, "kind": {"install"}, "name": {"pager-00"}, "q": {"pager"}, "page": {"2"}}
	wantIn(t, "plan", plain(body), "<caption>25 mods for Factorio 2.0 match. Showing 1 to 10.</caption>",
		`<a href="`+strings.ReplaceAll(planPath(id, next), "&", "&amp;")+`#add-heading" class="button button--quiet">Next page</a>`,
		`<a href="#confirm-heading">Back to the plan</a>`)
	if strings.Contains(body, `name="pick"`) {
		t.Error("the plan page has boxes to tick; each Add adds at once")
	}
	// An add from page 2 keeps page 2.
	q.Set("page", "2")
	_, two := g.getStatus(planPath(id, q))
	wantIn(t, "page 2", two, `hx-get="/worlds/`+id+`/mods/plan?added=pager-10&amp;kind=install&amp;name=pager-00&amp;name=pager-10&amp;page=2&amp;q=pager"`)

	q.Set("q", "no such mod anywhere")
	_, none := g.getStatus(planPath(id, q))
	wantIn(t, "none", none, "No mod for Factorio 2.0 matches. Try a shorter part of the name.")
}
