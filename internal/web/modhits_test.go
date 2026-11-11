package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

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

func TestSearchPagesAndTable(t *testing.T) {
	g, id := pagerRig(t)
	code, body := g.getStatus(modsPath(id) + "/search?q=pager")
	if code != http.StatusOK {
		t.Fatalf("search %d", code)
	}
	main := between(body, "<main", "</main>")
	// The world is the page's heading, the matches a section under it.
	wantIn(t, "search", plain(body), "<h1>Base <span",
		`<span class="mod-hits__title">Pager 00</span> <code translate="no">pager-00</code>`,
		`<summary>Details<span class="visually-hidden"> <span translate="no">pager-00</span></span></summary><p>Pages through pager-00</p>`,
		`<td class="num" role="cell" data-label="Downloads:">1,000</td>`, `<h2 id="results-heading" tabindex="-1">Mods matching &#34;pager&#34;</h2>`,
		"<caption>25 mods for Factorio 2.0 match. Showing 1 to 10.</caption>",
		`<th scope="col" role="columnheader">Mod</th>`, `<th scope="col" role="columnheader">Latest version</th>`,
		`<th scope="col" class="num" role="columnheader">Downloads</th>`,
		`<input type="checkbox" name="pick" value="pager-00" id="mod-pager-00-pick">`,
		`data-label="Latest version:"`, `data-label="Downloads:"`,
		`<nav class="pager" aria-label="Result pages">`, "Page 1 of 3",
		`<button type="submit" class="button button--quiet" name="page" value="2">Next page</button>`,
		`<button type="submit" class="button" name="picked" value="1">Add ticked mods to plan</button>`)
	if n := strings.Count(main, `<tr role="row">`); n != 11 {
		t.Errorf("%d rows, want a header and 10 mods", n)
	}
	if strings.Contains(main, "Previous page") || strings.Contains(main, "Game version") || strings.Contains(main, "autofocus") {
		t.Error("the first page offers a previous page, repeats the game version, or moves focus")
	}
	// The installed mod has a badge and no box.
	wantIn(t, "installed", plain(main), `<p class="mod-hits__name">`, `<span class="badge">Installed</span>`)
	if strings.Contains(main, `value="pager-03"`) {
		t.Error("an installed mod can be ticked")
	}

	// The last page: five rows, focus on the results, and the ticks kept.
	_, last := g.getStatus(modsPath(id) + "/search?q=pager&page=3&pick=pager-21&pick=pager-01")
	wantIn(t, "last page", plain(last), "Showing 21 to 25.", "Page 3 of 3", `id="results-heading" tabindex="-1" autofocus`,
		`value="pager-21" id="mod-pager-21-pick" checked>`,
		`<legend>Also ticked in other searches or on other pages</legend>`,
		`<input type="checkbox" name="pick" value="pager-01" checked>`,
		`<input type="hidden" name="at" value="3">`, `name="page" value="2">Previous page</button>`)
	if strings.Contains(between(last, "<main", "</main>"), "Next page") {
		t.Error("the last page offers a next page")
	}
}

// Ticks from earlier searches stay ticked through a new search that finds
// nothing and through a search left empty, and Add ticked mods adds them from
// either, so mods found by several searches go into one plan (SPEC 9.1).
func TestSearchKeepsTicksAcrossSearches(t *testing.T) {
	g, id := pagerRig(t)
	_, body := g.getStatus(modsPath(id) + "/search?q=pager-05&pick=pager-21&pick=pager-22")
	main := between(body, "<main", "</main>")
	wantIn(t, "new search", plain(main), `<input type="checkbox" name="pick" value="pager-21" checked>`,
		`<input type="checkbox" name="pick" value="pager-22" checked>`, `>Add ticked mods to plan</button>`,
		`<input type="hidden" name="shown" value="pager-05">`)
	// One form holds the search box and the boxes, so a new search sends the ticks.
	if n := strings.Count(main, "<form"); n != 1 {
		t.Errorf("%d forms, want one", n)
	}
	for _, q := range []string{"", "%20%20", strings.Repeat("a", maxSearchQuery+1)} {
		code, bad := g.getStatus(modsPath(id) + "/search?q=" + q + "&pick=pager-01")
		if code != http.StatusBadRequest {
			t.Errorf("search %q: %d", q, code)
		}
		wantIn(t, "bad search", plain(bad), `<input type="checkbox" name="pick" value="pager-01" checked>`, `>Add ticked mods to plan</button>`)
		if strings.Contains(bad, "results-heading") {
			t.Errorf("search %q shows results", q)
		}
	}
	for path, names := range map[string][]string{
		"/search?q=&pick=pager-01&picked=1":                    {"pager-01"},
		"/search?q=other&pick=pager-01&pick=pager-02&picked=1": {"pager-01", "pager-02"},
	} {
		rec := g.harness.get(modsPath(id)+path, withCookie(g.cookie))
		loc, _ := url.Parse(rec.Header().Get("Location"))
		if rec.Code != http.StatusSeeOther || !slices.Equal(loc.Query()["name"], names) {
			t.Errorf("%s: %d to %q, want a plan naming %v", path, rec.Code, loc, names)
		}
	}
}

// Paging and Add ticked mods use the search shown, not text typed in the box
// since (SPEC 9.1).
func TestSearchPagesTheSearchShown(t *testing.T) {
	g, id := pagerRig(t)
	_, body := g.getStatus(modsPath(id) + "/search?q=typed&shown=pager&page=2")
	wantIn(t, "paged", plain(body), "Showing 11 to 20.", `<input type="hidden" name="shown" value="pager">`)
	rec := g.harness.get(modsPath(id)+"/search?q=typed&shown=pager&at=2&pick=pager-12&picked=1", withCookie(g.cookie))
	want := planPath(id, url.Values{"kind": {"install"}, "name": {"pager-12"}, "q": {"pager"}, "page": {"2"}})
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("went to %q, want %q", got, want)
	}
	// Enter in the box sends no page, so it runs the typed search.
	_, typed := g.getStatus(modsPath(id) + "/search?q=pager-05&shown=pager")
	wantIn(t, "typed", plain(typed), `<input type="hidden" name="shown" value="pager-05">`)
}

// Ticks stay on the page when the portal cannot be reached.
func TestSearchPortalDownKeepsTicks(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	g.portal.FailAPI(http.StatusInternalServerError)
	code, body := g.getStatus(modsPath(id) + "/search?q=alpha&pick=beta")
	if code != http.StatusBadGateway {
		t.Errorf("portal down: %d", code)
	}
	wantIn(t, "portal down", plain(body), "could not reach the mod portal",
		`<input type="checkbox" name="pick" value="beta" checked>`, `>Add ticked mods to plan</button>`)
}

// More ticks than one plan holds are refused with a message, not dropped.
func TestSearchRefusesTooManyTicks(t *testing.T) {
	g, id := pagerRig(t)
	v := url.Values{"q": {"pager"}, "picked": {"1"}}
	for i := range maxPlanNames + 1 {
		v.Add("pick", fmt.Sprintf("m%02d", i))
	}
	rec := g.harness.get(modsPath(id)+"/search?"+v.Encode(), withCookie(g.cookie))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("too many ticks: %d", rec.Code)
	}
	body := plain(rec.Body.String())
	wantIn(t, "too many", body, "Too many mods are ticked for one plan. The limit is 50.", `value="m50" checked>`)
}

// Add ticked mods starts one plan with every ticked mod, at the same page of
// the same search; with nothing ticked it says so on the search page.
func TestSearchAddTicked(t *testing.T) {
	g, id := pagerRig(t)
	rec := g.harness.get(modsPath(id)+"/search?q=pager&at=2&pick=pager-12&pick=pager-01&pick=pager-12&picked=1", withCookie(g.cookie))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("add ticked: %d", rec.Code)
	}
	want := planPath(id, url.Values{"kind": {"install"}, "name": {"pager-12", "pager-01"}, "q": {"pager"}, "page": {"2"}})
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("went to %q, want %q", got, want)
	}
	_, plan := g.getStatus(want)
	wantIn(t, "plan", plain(plan), "Install mods in Base?", "Install 2 mods", "Showing 11 to 20.",
		`<input type="hidden" name="page" value="2">`, `id="mod-pager-12-add" tabindex="-1">In plan`)

	code, none := g.getStatus(modsPath(id) + "/search?q=pager&at=2&picked=1")
	if code != http.StatusBadRequest {
		t.Errorf("nothing ticked: %d", code)
	}
	wantIn(t, "nothing ticked", plain(none), `id="error-summary"`, `<a href="#mod-pager-10-pick">Nothing is ticked.`,
		`<p class="error" id="pick-error">`, `aria-invalid="true" aria-describedby="pick-error"`, "Showing 11 to 20.")
}

// A ticked name longer than a mod name can be is refused, not echoed into the
// redirect to the plan page.
func TestSearchRefusesLongPick(t *testing.T) {
	g, id := pagerRig(t)
	long := strings.Repeat("a", maxPickName+1)
	rec := g.harness.get(modsPath(id)+"/search?q=pager&pick=pager-01&pick="+long+"&picked=1", withCookie(g.cookie))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long pick: %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Errorf("redirected to %q", loc)
	}
	wantIn(t, "long pick", plain(rec.Body.String()), "Enter at most 100 characters.")
	ok := g.harness.get(modsPath(id)+"/search?q=pager&pick="+strings.Repeat("a", maxPickName)+"&picked=1", withCookie(g.cookie))
	if ok.Code != http.StatusSeeOther {
		t.Errorf("a name at the limit: %d", ok.Code)
	}
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
