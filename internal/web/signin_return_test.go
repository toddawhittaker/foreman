package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// signInAfter is where a signed-out visit to path is sent.
func signInAfter(path string) string {
	return PathSignIn + "?next=" + url.QueryEscape(path)
}

// SPEC 14.17: a signed-out visit goes to sign-in with the page asked for,
// and signing in returns there.
func TestSignInReturnsToTheRequestedPage(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	asked := PathWorlds + "?x=1"
	rec := hn.get(asked)
	loc := rec.Header().Get("Location")
	hn.wantRedirect(rec, PathSignIn+"?next="+url.QueryEscape(asked))

	form := hn.get(loc)
	if !strings.Contains(form.Body.String(), `name="next" value="`+asked+`"`) {
		t.Fatalf("sign-in form does not carry the page:\n%s", form.Body)
	}
	f := signInForm("alice", testPassword)
	f.Set("next", asked)
	hn.wantRedirect(hn.post(PathSignIn, f), asked)

	// A failed attempt keeps the page for the next try.
	f.Set("password", "not the right password")
	if bad := hn.post(PathSignIn, f); !strings.Contains(bad.Body.String(), `name="next" value="`+asked+`"`) {
		t.Errorf("failed sign-in lost the page:\n%s", bad.Body)
	}
}

// SPEC 14.17: anything but a local path sends the admin home, from the
// link and from a forged form field alike.
func TestSignInIgnoresOffSiteReturnPaths(t *testing.T) {
	for _, next := range []string{"//evil.example/", "/\\evil.example", "https://evil.example/", "/\t/evil.example", PathSignIn} {
		hn := newHarness(t, nil)
		hn.addUser("alice", testPassword)
		form := hn.get(PathSignIn + "?next=" + url.QueryEscape(next))
		if strings.Contains(form.Body.String(), `name="next"`) {
			t.Errorf("%q: the form carries a refused path", next)
		}
		f := signInForm("alice", testPassword)
		f.Set("next", next)
		hn.wantRedirect(hn.post(PathSignIn, f), PathWorlds)
	}
}

// SPEC 12.3: setup lists every problem at once.
func TestSetupReportsEveryProblem(t *testing.T) {
	hn := newHarness(t, nil)
	rec := hn.post(PathSetup, url.Values{"username": {"  "}, "password": {"short"}, "confirm": {"other"}})
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
	for _, want := range []string{"Fix 3 problems", "Enter a username", "Use at least 12 characters", "Enter the same password in both fields"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q:\n%s", want, body)
		}
	}
}
