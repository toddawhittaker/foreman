package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The page's theme comes from the cookie; with none, or one Foreman did not
// write, the page follows the system setting (SPEC 17.3).
func TestPageTheme(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	cases := []struct {
		cookie, html, scheme string
	}{
		{"", `<html lang="en">`, `content="dark light"`},
		{"auto", `<html lang="en">`, `content="dark light"`},
		{"light", `<html lang="en" data-theme="light">`, `content="light"`},
		{"dark", `<html lang="en" data-theme="dark">`, `content="dark"`},
		{"Dark", `<html lang="en">`, `content="dark light"`},
		{`dark" onload="x`, `<html lang="en">`, `content="dark light"`},
		{"sepia", `<html lang="en">`, `content="dark light"`},
	}
	for _, c := range cases {
		t.Run(c.cookie, func(t *testing.T) {
			var mods []func(*http.Request)
			if c.cookie != "" {
				mods = append(mods, withCookie(&http.Cookie{Name: ThemeCookie, Value: c.cookie})) //nolint:gosec // a request cookie in a test
			}
			body := hn.get(PathSignIn, mods...).Body.String()
			if !strings.Contains(body, c.html) {
				t.Errorf("want %s in %.200s", c.html, body)
			}
			if want := `<meta name="color-scheme" ` + c.scheme + `>`; !strings.Contains(body, want) {
				t.Errorf("want %s", want)
			}
		})
	}
}

// POST /theme sets the cookie and returns to the page, by the same rules as
// the language menu (SPEC 17.3, 14.17).
func TestThemeHandler(t *testing.T) {
	hn := newHarness(t, nil)
	for _, v := range []string{"auto", "light", "dark"} {
		rec := hn.post(PathTheme, url.Values{"theme": {v}, "return": {"/worlds?x=1"}})
		hn.wantRedirect(rec, "/worlds?x=1")
		got := responseCookie(rec, ThemeCookie)
		if got == nil || got.Value != v || got.Path != "/" || !got.HttpOnly || got.SameSite != http.SameSiteLaxMode || got.MaxAge != 365*24*60*60 {
			t.Fatalf("theme %s: cookie %+v", v, got)
		}
		if got.Secure != hn.cfg.SecureCookies() {
			t.Errorf("Secure %v, want %v as the session cookie", got.Secure, hn.cfg.SecureCookies())
		}
	}

	for _, bad := range []string{"", "Dark", "sepia", "dark;", "light dark"} {
		rec := hn.post(PathTheme, url.Values{"theme": {bad}, "return": {"/"}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("theme %q: status %d, want 400", bad, rec.Code)
		}
		if responseCookie(rec, ThemeCookie) != nil {
			t.Errorf("theme %q set the cookie", bad)
		}
	}

	returns := map[string]string{
		"":                              "/",
		"//evil.example/":               "/",
		"https://evil.example/":         "/",
		"/\\evil.example":               "/",
		"javascript:alert(1)":           "/",
		"/worlds\r\nLocation: http://x": "/",
		"/signin?next=%2Fworlds":        "/signin?next=%2Fworlds",
		"/signed-out":                   "/signed-out",
		"/snapshots?select=automatic":   "/snapshots?select=automatic",
	}
	for in, want := range returns {
		rec := hn.post(PathTheme, url.Values{"theme": {"dark"}, "return": {in}})
		if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || loc != want {
			t.Errorf("return %q: %d to %q, want %q", in, rec.Code, loc, want)
		}
	}
}

// The switch works signed out, and a post from another site is refused by
// the same cross-origin check as every other form.
func TestThemeSignedOutAndCrossOrigin(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	hn.wantRedirect(hn.post(PathTheme, url.Values{"theme": {"light"}, "return": {"/signin"}}), "/signin")

	rec := hn.post(PathTheme, url.Values{"theme": {"dark"}, "return": {"/"}}, withHeader("Sec-Fetch-Site", "cross-site"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site post: %d, want 403", rec.Code)
	}
	if responseCookie(rec, ThemeCookie) != nil {
		t.Error("cross-site post set the cookie")
	}
}

// The menu offers the three choices as buttons, marks the one in use, and
// carries the page's own address back.
func TestThemeMenu(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	body := hn.get(PathSignIn+"?next=%2Fworlds", withCookie(&http.Cookie{Name: ThemeCookie, Value: "light"})).Body.String() //nolint:gosec // a request cookie in a test
	for _, want := range []string{
		`<form method="post" action="/theme">`,
		`<legend>Theme</legend>`,
		`<button type="submit" name="theme" value="auto" class="menu__item">Auto</button>`,
		`<button type="submit" name="theme" value="light" class="menu__item" aria-current="true">Light</button>`,
		`<button type="submit" name="theme" value="dark" class="menu__item">Dark</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	form := body[strings.Index(body, `action="/theme"`):]
	form = form[:strings.Index(form, "</form>")]
	if want := `<input type="hidden" name="return" value="/signin?next=%2Fworlds">`; !strings.Contains(form, want) {
		t.Errorf("theme form lacks %s", want)
	}

	if auto := hn.get(PathSignIn).Body.String(); !strings.Contains(auto, `value="auto" class="menu__item" aria-current="true"`) {
		t.Error("with no cookie, Auto is not marked as the current choice")
	}
}

func responseCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
