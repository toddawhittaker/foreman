package web

import (
	"net/http"
	"strings"
	"testing"
)

// DESIGN 6, "Toasts": a refused sign-in is summed up in a toast first in main,
// which takes no focus; the form keeps no message of its own.
func TestSignInRefusalIsAToast(t *testing.T) {
	r := newTestRenderer(t)
	for _, c := range []struct {
		name string
		page SignInPage
		want string
	}{
		{"rate limited", SignInPage{RateLimited: true}, "Too many failed sign-ins."},
		{"busy", SignInPage{Busy: true}, "Foreman is busy. Wait a moment and try again."},
	} {
		_, body := render(t, r, pageCase{status: 429, page: c.page})
		region := between(body, `<div id="toasts" class="toasts">`, "</div></div>")
		if region == "" {
			t.Fatalf("%s: no toast region", c.name)
		}
		if !strings.Contains(region, `class="toast toast--error"`) || !strings.Contains(region, "<strong>Error:</strong>") || !strings.Contains(region, c.want) {
			t.Errorf("%s: toast region %q", c.name, region)
		}
		form := between(body, `<form method="post" action="`+PathSignIn+`"`, "</form>")
		if strings.Contains(form, c.want) || strings.Contains(form, "error-summary") {
			t.Errorf("%s: the form still carries the message", c.name)
		}
		if !strings.Contains(region, `aria-label="Close this message" data-toast-close`) {
			t.Errorf("%s: the close button has no name", c.name)
		}
		if strings.Contains(region, "autofocus") || strings.Contains(region, "tabindex") {
			t.Errorf("%s: the toast takes focus", c.name)
		}
		if strings.Contains(body, "aria-invalid") {
			t.Errorf("%s: a field is marked at fault", c.name)
		}
	}
}

// Every page has the empty region first in main; a page with nothing to say
// has no toast.
func TestEveryPageHasTheToastRegion(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, page: SignInPage{}})
	main := body[strings.Index(body, "<main"):]
	main = main[strings.Index(main, ">")+1:]
	if !strings.HasPrefix(strings.TrimSpace(main), `<div id="toasts" class="toasts"></div>`) {
		t.Errorf("main does not open with an empty toast region: %.120s", main)
	}
	// A page with nothing to say carries no message words.
	if strings.Contains(main, "Done:") || strings.Contains(main, "Error:") {
		t.Error("an empty page carries toast words")
	}
	if !strings.Contains(body, `toast.js`) {
		t.Error("toast.js is not loaded")
	}
}

// DESIGN 6, "The page header": the error and gate pages draw their title in
// the one header, with its slots in order, and main carries no width of its own.
func TestPageHeadPartial(t *testing.T) {
	r := newTestRenderer(t)
	for _, p := range []Page{ErrorPage{Status: http.StatusNotFound}, SetupPage{}, SignInPage{}, SignedOutPage{}} {
		_, body := render(t, r, pageCase{status: 200, page: p})
		head := between(body, `<header class="pagehead">`, `</header>`)
		if head == "" {
			t.Fatalf("%T: no page header", p)
		}
		order := []string{`<div class="pagehead__crumb">`, "<h1>", `<div class="pagehead__actions">`}
		at := 0
		for _, part := range order {
			i := strings.Index(head[at:], part)
			if i < 0 {
				t.Fatalf("%T: %q missing or out of order in %q", p, part, head)
			}
			at += i
		}
		if strings.Count(body, "<h1") != 1 {
			t.Errorf("%T: %d titles", p, strings.Count(body, "<h1"))
		}
		if strings.Contains(body, "data-width") {
			t.Errorf("%T: main still sets a width", p)
		}
	}
	for _, p := range []Page{SetupPage{}, SignInPage{}, SignedOutPage{}} {
		_, body := render(t, r, pageCase{status: 200, page: p})
		if !strings.Contains(body, `<img class="gate-logo"`) {
			t.Errorf("%T: no wordmark over the title", p)
		}
	}
}

// DESIGN 6: a signed-in visitor on an unknown path keeps the shell; nobody
// else is named, and nothing under the public page reads a session.
func TestUnknownPathKeepsTheShellWhenSignedIn(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := func(r *http.Request) { r.Method = method; r.AddCookie(c) }
		rec := hn.get("/no-such-page", req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: got %d", method, rec.Code)
		}
		if body := rec.Body.String(); !strings.Contains(body, `<nav aria-label="Main">`) || !strings.Contains(body, `<span class="menu__user">alice</span>`) {
			t.Errorf("%s: the signed-in 404 has no shell", method)
		}
	}
	if body := hn.get("/no-such-page").Body.String(); strings.Contains(body, `<nav aria-label="Main">`) || strings.Contains(body, "alice") {
		t.Error("a signed-out 404 shows the shell")
	}
	if body := hn.get(PathPublic+"nope", withCookie(c)).Body.String(); strings.Contains(body, "alice") {
		t.Error("an unknown public path read the session")
	}
}
