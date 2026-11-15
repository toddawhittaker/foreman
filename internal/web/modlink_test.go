package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// SPEC 9.1, 9.11: a pasted mod portal link finds exactly that mod, from the
// name alone.
func TestSearchPastedPortalLinkFindsThatMod(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	link := "www.mods.factorio.com/mod/Krastorio2/changelog?from=search"
	code, body := g.getStatus(modsPath(id) + "/search?q=" + url.QueryEscape(link))
	if code != http.StatusOK {
		t.Fatalf("status %d\n%s", code, body)
	}
	main := plain(between(body, "<main", "</main>"))
	wantIn(t, "link search", main, "<caption>1 mod for Factorio 2.0 matches.</caption>", `<code translate="no">Krastorio2</code>`)
	// Krastorio2Assets also contains the name, so a text search would list it.
	if strings.Contains(main, "Krastorio2Assets") {
		t.Errorf("the link listed more than the one mod:\n%s", main)
	}
	if g.portal.FullCalls("Krastorio2") != 0 {
		t.Error("a mod in the list needed a /full call")
	}
}

// A mod the cached list lacks is asked for by name; one with a release for the
// world's line is shown.
func TestSearchPastedPortalLinkFallsBackToTheModsOwnReply(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	_, body := g.getStatus(modsPath(id) + "/search?q=" + url.QueryEscape("https://mods.factorio.com/mod/chest-auto-sort"))
	wantIn(t, "full lookup", plain(body), `<code translate="no">chest-auto-sort</code>`)
	if g.portal.FullCalls("chest-auto-sort") != 1 {
		t.Errorf("the mod's /full reply was read %d times, want 1", g.portal.FullCalls("chest-auto-sort"))
	}
}

// The two misses read differently: no mod of that name, and a mod with no
// release for the world's game line.
func TestSearchPastedPortalLinkMisses(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZipFor("2.1", "only-new", "1.0.0")
	id := g.world20("Base")
	for _, tc := range []struct{ link, want string }{
		{"https://mods.factorio.com/mod/no-such-mod-anywhere", "No mod named no-such-mod-anywhere is on the mod portal."},
		{"https://mods.factorio.com/mod/only-new/downloads", "only-new has no release for Factorio 2.0."},
		{"https://mods.factorio.com/mod/Flow%20Contro", "No mod named Flow Contro is on the mod portal."},
	} {
		code, body := g.getStatus(modsPath(id) + "/search?q=" + url.QueryEscape(tc.link))
		if code != http.StatusOK {
			t.Fatalf("%s: status %d", tc.link, code)
		}
		main := plain(between(body, "<main", "</main>"))
		wantIn(t, tc.link, main, tc.want)
		if strings.Contains(main, "<table") {
			t.Errorf("%s: a miss lists a result", tc.link)
		}
	}
}

// Only a mod portal link is read as one; any other link is search text.
func TestSearchOtherLinkIsOrdinarySearchText(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	link := "https://example.com/mod/Krastorio2"
	code, body := g.getStatus(modsPath(id) + "/search?q=" + url.QueryEscape(link))
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	wantIn(t, "other link", plain(between(body, "<main", "</main>")), "No mod for Factorio 2.0 matches.")
	if g.portal.FullCalls("Krastorio2") != 0 {
		t.Error("a link to another site asked the portal for a mod")
	}
}

// A portal that cannot be reached is said so for a pasted link too.
func TestSearchPastedPortalLinkPortalDown(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	g.portal.FailAPI(http.StatusBadGateway)
	code, body := g.getStatus(modsPath(id) + "/search?q=" + url.QueryEscape("https://mods.factorio.com/mod/no-such-mod-anywhere"))
	if code != http.StatusBadGateway {
		t.Fatalf("status %d", code)
	}
	wantIn(t, "portal down", body, "could not reach the mod portal")
}

// A pasted link carries a sub-page and tracking query, so the 100-character
// limit applies to the mod name in it, not to the whole link; plain text keeps
// the limit.
func TestSearchLongPastedLinkIsLimitedByTheName(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	long := "https://mods.factorio.com/mod/Krastorio2/changelog?utm=" + strings.Repeat("x", 200)
	code, body := g.getStatus(modsPath(id) + "/search?q=" + url.QueryEscape(long))
	if code != http.StatusOK {
		t.Fatalf("a long link: status %d", code)
	}
	wantIn(t, "long link", plain(between(body, "<main", "</main>")), `<code translate="no">Krastorio2</code>`)

	longName := "https://mods.factorio.com/mod/" + strings.Repeat("a", maxSearchQuery+1)
	if code, _ := g.getStatus(modsPath(id) + "/search?q=" + url.QueryEscape(longName)); code != http.StatusBadRequest {
		t.Errorf("a link with a too long name: status %d, want 400", code)
	}
	if code, _ := g.getStatus(modsPath(id) + "/search?q=" + strings.Repeat("a", maxSearchQuery+1)); code != http.StatusBadRequest {
		t.Errorf("long plain text: status %d, want 400", code)
	}
}

// A ticked box rides along on a link search, and the one result stays.
func TestSearchPastedLinkKeepsTicks(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base")
	q := url.Values{"q": {"https://mods.factorio.com/mod/Krastorio2?from=search"}, "pick": {"jetpack"}}
	code, body := g.getStatus(modsPath(id) + "/search?" + q.Encode())
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	main := between(body, "<main", "</main>")
	wantIn(t, "link and tick", main, "<caption>1 mod for Factorio 2.0 matches.</caption>", `<code translate="no">Krastorio2</code>`,
		`name="pick" value="jetpack" checked`)
}

// SPEC 9.1, 9.11: the plan's "Add another mod" reads a pasted portal link the
// way the search page does, and says why a link found nothing.
func TestAddAnotherModReadsPastedPortalLink(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	g.portal.AddZip("alpha-extra", "1.0.0")
	g.portal.AddZipFor("2.1", "only-new", "1.0.0")
	id := g.world20("Base")
	plan := func(q string) string {
		_, body := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"alpha"}, "q": {q}}))
		return plain(between(body, `id="add-heading"`, "</main>"))
	}

	long := "https://mods.factorio.com/mod/alpha-extra/changelog?from=" + strings.Repeat("x", 150)
	found := plan(long)
	wantIn(t, "link", found, `id="mod-alpha-extra-add"`)
	if strings.Contains(found, `id="mod-alpha-add"`) {
		t.Errorf("the link listed more than the one mod:\n%s", found)
	}

	wantIn(t, "unknown", plan("https://mods.factorio.com/mod/no-such-mod-anywhere"), "No mod named no-such-mod-anywhere is on the mod portal.")
	wantIn(t, "no release", plan("https://mods.factorio.com/mod/only-new"), "only-new has no release for Factorio 2.0.")

	// A link whose mod name is too long, and long plain text, are not searched.
	for _, q := range []string{"https://mods.factorio.com/mod/" + strings.Repeat("n", 101), strings.Repeat("a", 101)} {
		if strings.Contains(plan(q), "<table") {
			t.Errorf("%.40s was searched", q)
		}
	}
}
