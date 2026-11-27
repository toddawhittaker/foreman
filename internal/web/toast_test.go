package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// DESIGN 6, "Toasts": a refused sign-in is summed up in a toast last in main,
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

// Every page has the empty region last in main, after the page's own parts;
// a page with nothing to say has no toast.
func TestEveryPageHasTheToastRegion(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, page: SignInPage{}})
	main := body[strings.Index(body, "<main"):strings.Index(body, "</main>")]
	if !strings.HasSuffix(strings.TrimSpace(main), `<div id="toasts" class="toasts"></div>`) {
		t.Errorf("main does not end with an empty toast region: …%s", main[max(0, len(main)-120):])
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
	// With the public page on, so the path reaches the unknown-path answer
	// rather than the closed page's.
	if err := hn.store.SetAppSetting(context.Background(), SettingPublicMods, PublicModsOn, "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if body := hn.get(PathPublic+"nope", withCookie(c)).Body.String(); strings.Contains(body, "alice") {
		t.Error("an unknown public path read the session")
	}
}

// A stray request for a missing file, such as an icon a browser guesses at, is not the user
// at work, so it never keeps an idle session alive (SPEC 12.8).
func TestUnknownPathLeavesTheSessionIdle(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hash := sha256.Sum256([]byte(c.Value))
	token := hex.EncodeToString(hash[:])
	_, before, found, err := hn.store.SessionUser(context.Background(), token)
	if err != nil || !found {
		t.Fatalf("the signed-in session: found=%v err=%v", found, err)
	}
	// Longer than the session's refresh interval, so a lookup would refresh it.
	hn.clock.Advance(10 * time.Minute)
	if rec := hn.get("/no-such-icon.ico", withCookie(c)); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "alice") {
		t.Fatalf("the signed-in 404: %d", rec.Code)
	}
	_, after, _, err := hn.store.SessionUser(context.Background(), token)
	if err != nil || !after.Equal(before) {
		t.Errorf("the 404 refreshed the session: last seen %v then %v, err=%v", before, after, err)
	}
}

// DESIGN 6, "Toasts": a toast in an htmx answer is the toast partial inside an
// out-of-band wrapper. htmx inserts the wrapper's children, so the whole toast
// lands in #toasts, newest first.
func TestToastOutOfBand(t *testing.T) {
	r := newTestRenderer(t)
	var b bytes.Buffer
	if err := r.pages[i18n.English]["error"].ExecuteTemplate(&b, "toast-oob", Toast{Error: true, Text: i18n.M("accounts.signin.busy")}); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := b.String()
	want := `<div hx-swap-oob="afterbegin:#toasts"><div class="toast toast--error" data-toast>`
	if !strings.HasPrefix(got, want) || !strings.HasSuffix(got, "</div></div>") {
		t.Errorf("toast-oob = %q, want it to open with %q", got, want)
	}
	if !strings.Contains(got, "Foreman is busy. Wait a moment and try again.") || !strings.Contains(got, "data-toast-close") {
		t.Errorf("toast-oob lacks the toast's words or close button: %q", got)
	}
}
