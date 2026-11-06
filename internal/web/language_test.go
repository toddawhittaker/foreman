package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// requestIn is a GET request that withLocale has given language loc.
func requestIn(loc i18n.Locale) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	return r.WithContext(context.WithValue(r.Context(), localeKey{}, loc))
}

// The page's language: the cookie first, then Accept-Language, else
// English (SPEC 22.2).
func TestPageLanguage(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	cases := []struct {
		name, cookie, header, lang, text string
	}{
		{"no preference", "", "", "en", "Sign in"},
		{"header", "", "de-DE,de;q=0.9", "de", "Anmelden"},
		{"cookie beats header", "fr", "de", "fr", "Connexion"},
		{"browser Chinese", "", "zh-CN,zh;q=0.9", "zh-Hans", "登录"},
		{"unknown header", "", "ja", "en", "Sign in"},
		{"bad cookie ignored", "../../etc", "ru", "ru", "Вход"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var mods []func(*http.Request)
			if c.cookie != "" {
				mods = append(mods, withCookie(&http.Cookie{Name: LangCookie, Value: c.cookie})) //nolint:gosec // a request cookie in a test
			}
			if c.header != "" {
				mods = append(mods, withHeader("Accept-Language", c.header))
			}
			rec := hn.get(PathSignIn, mods...)
			body := rec.Body.String()
			if !strings.Contains(body, `<html lang="`+c.lang+`">`) {
				t.Errorf("want lang %s in %.200s", c.lang, body)
			}
			if !strings.Contains(body, "<h1>"+c.text+"</h1>") {
				t.Errorf("want heading %q", c.text)
			}
		})
	}
}

// POST /language sets the cookie and returns to the page (SPEC 22.2, 14.17).
func TestLanguageHandler(t *testing.T) {
	hn := newHarness(t, nil)
	rec := hn.post(PathLanguage, url.Values{"lang": {"de"}, "return": {"/worlds?x=1"}})
	hn.wantRedirect(rec, "/worlds?x=1")
	var got *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == LangCookie {
			got = c
		}
	}
	if got == nil || got.Value != "de" || got.Path != "/" || !got.HttpOnly || got.SameSite != http.SameSiteLaxMode || got.MaxAge != 365*24*60*60 {
		t.Fatalf("cookie %+v", got)
	}
	if got.Secure != hn.cfg.SecureCookies() {
		t.Errorf("Secure %v, want %v as the session cookie", got.Secure, hn.cfg.SecureCookies())
	}

	for _, bad := range []string{"", "xx", "de-DE", "EN", "zh", "de;q=1"} {
		rec := hn.post(PathLanguage, url.Values{"lang": {bad}, "return": {"/"}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("lang %q: status %d, want 400", bad, rec.Code)
		}
		for _, c := range rec.Result().Cookies() {
			if c.Name == LangCookie {
				t.Errorf("lang %q set the cookie", bad)
			}
		}
	}

	returns := map[string]string{
		"":                              "/",
		"//evil.example/":               "/",
		"https://evil.example/":         "/",
		"/\\evil.example":               "/",
		"/signin":                       "/signin",
		"/signin?next=%2Fworlds":        "/signin?next=%2Fworlds",
		"/signin?next=%2F%2Fevil":       "/signin",
		"//evil.example/signin":         "/signin",
		"/setup":                        "/setup",
		"/signed-out":                   "/signed-out",
		"/snapshots?select=automatic":   "/snapshots?select=automatic",
		"javascript:alert(1)":           "/",
		"/worlds\r\nLocation: http://x": "/",
	}
	for in, want := range returns {
		rec := hn.post(PathLanguage, url.Values{"lang": {"pl"}, "return": {in}})
		if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || loc != want {
			t.Errorf("return %q: %d to %q, want %q", in, rec.Code, loc, want)
		}
	}
}

// The menu works signed out, and a post from another site is refused by
// the same cross-origin check as every other form.
func TestLanguageSignedOutAndCrossOrigin(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	rec := hn.post(PathLanguage, url.Values{"lang": {"cs"}, "return": {"/signin"}})
	hn.wantRedirect(rec, "/signin")

	rec = hn.post(PathLanguage, url.Values{"lang": {"cs"}, "return": {"/"}}, withHeader("Sec-Fetch-Site", "cross-site"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("cross-site post: %d, want 403", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == LangCookie {
			t.Error("cross-site post set the cookie")
		}
	}
}

// The menu names each language in that language, marks the current one,
// and carries the page's own address back.
func TestLanguageMenu(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	body := hn.get(PathSignIn+"?next=%2Fworlds", withHeader("Accept-Language", "de")).Body.String()
	for _, want := range []string{
		`<form method="post" action="/language" class="language">`,
		`<label for="language">Sprache</label>`,
		`<option value="de" lang="de" selected>Deutsch</option>`,
		`<option value="zh-Hans" lang="zh-Hans">简体中文</option>`,
		`<input type="hidden" name="return" value="/signin?next=%2Fworlds">`,
		`<script type="application/json" id="i18n">{`,
		`"common.label.note":"Hinweis:"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
}
