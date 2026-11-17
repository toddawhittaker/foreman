package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// renderPublic draws the public page for data, in English.
func renderPublic(t *testing.T, data PublicModsData) string {
	t.Helper()
	r, err := sharedRenderer()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, PathPublicMods, nil)
	if err := r.Render(rec, req, http.StatusOK, "", LiveState{}, nil, PublicModsPage{data}); err != nil {
		t.Fatal(err)
	}
	return rec.Body.String()
}

var attrURL = regexp.MustCompile(`(?:href|src|srcset|action)="([^"]*)"`)

// SPEC 9.16: the page names the world and version, links each zip by title
// and version, marks mods in words, and never links a folder mod.
func TestPublicModsPageShowsModsAndMarks(t *testing.T) {
	body := renderPublic(t, PublicModsData{Worlds: []PublicWorld{{
		ID: "w1", Name: "Family base", GameVersion: "2.0.77", AllPath: PublicModsAllPath("w1"),
		Mods: []PublicMod{
			{Name: "alpha", Title: "Alpha mod", Version: "1.2.0", DownloadPath: PublicModFilePath("w1", "alpha_1.2.0.zip")},
			{Name: "beta", Version: "2.0.0", DownloadPath: PublicModFilePath("w1", "beta_2.0.0.zip"), NotOnPortal: true, Hidden: true},
			{Name: "local", Title: "Local mod", Version: "3.0.0", Folder: true},
		},
	}}})
	for _, want := range []string{
		`<meta name="robots" content="noindex">`,
		`>Family base</h2>`, `<dd>2.0.77</dd>`,
		`href="/public/mods/w1/all.zip" download id="public-all">Download all</a>`,
		`href="/public/mods/w1/files/alpha_1.2.0.zip" download class="public-mods__name"><span translate="no">Alpha mod</span> <span class="public-mods__version">1.2.0</span></a>`,
		// A mod with no title shows its name.
		`<span translate="no">beta</span> <span class="public-mods__version">2.0.0</span></a>`,
		`Not on the mod portal`, `Hidden on the mod portal`, `Ask the server admin for this mod`,
		`action="/public/language"`, `%APPDATA%\Factorio\mods`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "files/local") {
		t.Error("a folder mod has a download link")
	}
	if strings.Contains(body, "public-unchecked") || strings.Contains(body, "public-none") {
		t.Error("the page says the portal was not checked or no game runs")
	}
	// Everything loads from /public/static/, and the only link into Foreman
	// is the product name.
	for _, m := range attrURL.FindAllStringSubmatch(body, -1) {
		for u := range strings.FieldsSeq(m[1]) {
			if u == "2x" || strings.HasPrefix(u, PathPublic) || u == "#main" {
				continue
			}
			if u != PathServer {
				t.Errorf("page refers to %q outside /public/", u)
			}
		}
	}
	if n := strings.Count(body, `href="/"`); n != 1 {
		t.Errorf("%d links to the home page, want 1 (the product name)", n)
	}
}

// SPEC 9.16: no game running lists nothing and says so; an unchecked portal
// is one line.
func TestPublicModsPageEmptyStates(t *testing.T) {
	body := renderPublic(t, PublicModsData{})
	if !strings.Contains(body, `id="public-none">No game is running now`) || strings.Contains(body, "public-mods__name") {
		t.Errorf("no game running: want the message and no mods:\n%s", body)
	}
	body = renderPublic(t, PublicModsData{PortalUnchecked: true, Worlds: []PublicWorld{{ID: "w", Name: "W", GameVersion: "2.0.77"}}})
	for _, want := range []string{`id="public-unchecked"`, `id="public-no-mods"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, "public-all") {
		t.Error("Download all shown for a world with no mods")
	}
}

// SPEC 9.16, 14.18: the live page needs no sign-in, sets no cookie, keeps
// no-cache, and its styles load from /public/static/.
func TestPublicModsPageLive(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	rep := p.anon(PathPublicMods)
	if rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, "alpha") {
		t.Fatalf("GET %s: %d\n%s", PathPublicMods, rep.StatusCode, rep.body)
	}
	if got := rep.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control %q, want no-cache", got)
	}
	if rep.Header.Get("Set-Cookie") != "" || rep.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("headers %v", rep.Header)
	}
	css := regexp.MustCompile(`href="(/public/static/foreman\.css\?v=[0-9a-f]+)"`).FindStringSubmatch(rep.body)
	if css == nil {
		t.Fatal("no style sheet under /public/static/")
	}
	st := p.anon(css[1])
	if st.StatusCode != http.StatusOK || !strings.HasPrefix(st.Header.Get("Content-Type"), "text/css") {
		t.Errorf("GET %s: %d %s", css[1], st.StatusCode, st.Header.Get("Content-Type"))
	}
	if st := p.anon(PathPublicStatic + "SHA256SUMS"); st.StatusCode != http.StatusNotFound {
		t.Errorf("an unlisted static file answered %d", st.StatusCode)
	}
}

// Each world's section is named by its own heading, so the ids stay unique
// when the page lists more than one world.
func TestPublicModsPageHeadingIDsAreUnique(t *testing.T) {
	body := renderPublic(t, PublicModsData{Worlds: []PublicWorld{
		{ID: "w1", Name: "One", GameVersion: "2.0.77"},
		{ID: "w2", Name: "Two", GameVersion: "2.0.77"},
	}})
	for _, want := range []string{
		`aria-labelledby="public-world-name-0"`, `<h2 id="public-world-name-0" translate="no">One</h2>`,
		`aria-labelledby="public-world-name-1"`, `<h2 id="public-world-name-1" translate="no">Two</h2>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %s", want)
		}
	}
}

// SPEC 14.18: public static files are revalidated, never cached for a year,
// and carry the same headers as every public answer, an SVG's own policy
// included. The signed-in /static/ keeps its caching.
func TestPublicStaticCarriesThePublicHeaders(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	rep := p.anon(PathPublicMods)
	for _, name := range []string{"foreman.css", "flags/de.svg"} {
		m := regexp.MustCompile(`/public/static/` + regexp.QuoteMeta(name) + `\?v=[0-9a-f]+`).FindString(rep.body)
		if m == "" {
			t.Fatalf("the page does not load %s", name)
		}
		st := p.anon(m)
		if st.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", m, st.StatusCode)
		}
		h := st.Header
		if h.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: Cache-Control %q, want no-cache", m, h.Get("Cache-Control"))
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" ||
			h.Get("X-Frame-Options") != "DENY" || !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("%s: headers %v", m, h)
		}
	}

	signedIn := strings.Replace(regexp.MustCompile(`/public/static/foreman\.css\?v=[0-9a-f]+`).FindString(rep.body), "/public", "", 1)
	if got := p.anon(signedIn).Header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("/static/ Cache-Control %q, want the year-long caching kept", got)
	}
}

// SPEC 9.16: with no game running the page says so.
func TestPublicModsPageNoGame(t *testing.T) {
	g := newModsRig(t, false)
	g.world20("Base")
	p := &publicRig{modsRig: g}
	p.setting(true)
	rep := p.anon(PathPublicMods)
	if rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, `id="public-none"`) {
		t.Errorf("GET %s: %d\n%s", PathPublicMods, rep.StatusCode, rep.body)
	}
}

// SPEC 22.2, 14.17, 14.18: the public language menu sets only the language
// cookie and returns through the same check as the user menu.
func TestPublicLanguage(t *testing.T) {
	p := newPublicRig(t)
	post := func(form url.Values) reply {
		return p.do(http.MethodPost, PathPublicLanguage, form, http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
	}
	rep := post(url.Values{"lang": {"de"}, "return": {PathPublicMods}})
	if rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") != PathPublicMods {
		t.Fatalf("POST: %d to %q", rep.StatusCode, rep.Header.Get("Location"))
	}
	cookies := rep.Header.Values("Set-Cookie")
	if len(cookies) != 1 || !strings.HasPrefix(cookies[0], LangCookie+"=de;") {
		t.Errorf("cookies %q, want only the language", cookies)
	}
	if rep := post(url.Values{"lang": {"de"}, "return": {"//evil.example/x"}}); rep.Header.Get("Location") != "/" {
		t.Errorf("a foreign return went to %q, want home", rep.Header.Get("Location"))
	}
	if rep := post(url.Values{"lang": {"xx"}}); rep.StatusCode != http.StatusBadRequest {
		t.Errorf("an unknown language: %d, want 400", rep.StatusCode)
	}
	big := url.Values{"lang": {"de"}, "pad": {strings.Repeat("a", maxFormBytes)}}
	if rep := post(big); rep.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("a body over 64 KB: %d, want 413", rep.StatusCode)
	}
	de := p.do(http.MethodGet, PathPublicMods, nil, http.Header{"Cookie": {LangCookie + "=de"}})
	if !strings.Contains(de.body, `<html lang="de"`) {
		t.Error("the page is not in German after the cookie")
	}
}
