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
