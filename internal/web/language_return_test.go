package web

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"testing"
)

var menuReturnField = regexp.MustCompile(`<input type="hidden" name="return" value="([^"]*)">`)

// The language menu on a page shown in answer to a POST must return to an
// address that answers GET. The POST address itself is POST-only, so the
// menu uses the page the form was on, read from the Referer (SPEC 22.2).
func TestLanguageMenuAfterPostOnlyAddress(t *testing.T) {
	g := newGameRig(t, image21)
	g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	start := gamePath(b, "start")
	cookie := http.Header{"Cookie": {g.cookie.String()}}

	// A world that is not the active one refuses to start, with a page.
	post := func(referer string) string {
		t.Helper()
		h := cookie.Clone()
		if referer != "" {
			h.Set("Referer", referer)
		}
		rep := g.do(http.MethodPost, start, url.Values{}, h)
		if rep.StatusCode != http.StatusConflict {
			t.Fatalf("start: %d, want the 409 refusal page\n%s", rep.StatusCode, rep.body)
		}
		m := menuReturnField.FindStringSubmatch(rep.body)
		if m == nil {
			return ""
		}
		return html.UnescapeString(m[1])
	}

	page := worldPath(b) + "/stop"
	got := post(g.url + page)
	if got != page {
		t.Fatalf("menu returns to %q, want the Referer's page %q", got, page)
	}
	if rep := g.do(http.MethodGet, got, nil, cookie); rep.StatusCode != http.StatusOK {
		t.Errorf("GET %s: %d, want 200", got, rep.StatusCode)
	}

	for name, referer := range map[string]string{
		"no Referer":   "",
		"another site": "https://evil.example" + page,
		"not a URL":    "%zz",
		"unsafe path":  g.url + "//evil.example/",
	} {
		if got := post(referer); got != "" {
			t.Errorf("%s: menu returns to %q, want none (home)", name, got)
		}
	}
}
