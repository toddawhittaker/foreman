package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	ssoStart    = `action="` + PathOIDCStart + `"`
	ssoLabel    = "Sign in with single sign-on"
	passwordFld = `name="password"`
)

// has reports which of the sign-in page's parts a body holds.
func has(body, part string) bool { return strings.Contains(body, part) }

// SPEC 12.7: with oidc alone, the sign-in page is one button that starts
// single sign-on and carries the page asked for. It has no password form
// and no setup link.
func TestSignInPageWithOIDCAloneIsOneButton(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	asked := PathWorlds + "?x=1"
	rec := o.get(PathSignIn + "?next=" + url.QueryEscape(asked))
	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in page: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{ssoStart, ssoLabel, `name="next" value="` + asked + `"`, `method="get"`} {
		if !has(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	for _, not := range []string{passwordFld, `name="username"`, PathSetup, "<h2"} {
		if has(body, not) {
			t.Errorf("the page holds %q", not)
		}
	}
}

// SPEC 12.3, 12.7: with local and oidc both on, the page has both forms,
// each under its own heading, and no setup link once a user exists.
func TestSignInPageWithLocalAndOIDCHasBoth(t *testing.T) {
	o := newOIDCHarness(t, "local,oidc", nil)
	o.addUser("alice", testPassword)
	body := o.get(PathSignIn).Body.String()
	for _, want := range []string{ssoStart, ssoLabel, passwordFld, `name="username"`, "<h2>Single sign-on</h2>", "<h2>Foreman account</h2>"} {
		if !has(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if sso, local := strings.Index(body, ssoStart), strings.Index(body, passwordFld); sso > local {
		t.Error("the single sign-on button comes after the password form")
	}
	if n := strings.Count(body, "button--primary"); n != 1 {
		t.Errorf("%d primary buttons, want 1", n)
	}
}

// A failed password sign-in with both methods still offers single sign-on,
// so the next step is a control (DESIGN 9).
func TestFailedPasswordSignInStillOffersSingleSignOn(t *testing.T) {
	o := newOIDCHarness(t, "local,oidc", nil)
	o.addUser("alice", testPassword)
	rec := o.post(PathSignIn, signInForm("alice", "not the right password"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("failed sign-in: %d", rec.Code)
	}
	if !has(rec.Body.String(), ssoStart) {
		t.Error("the failed sign-in page has no single sign-on button")
	}
}

// Without oidc the page is what it was: the password form and no button.
func TestSignInPageWithoutOIDCHasNoButton(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	body := hn.get(PathSignIn).Body.String()
	if has(body, PathOIDCStart) || has(body, ssoLabel) || has(body, "<h2") {
		t.Errorf("the local-only page mentions single sign-on or has headings:\n%s", body)
	}
	if !has(body, passwordFld) || !has(body, "button--primary") {
		t.Error("the local-only page lost its password form or primary button")
	}
}

// SPEC 12.7: the signed-out page offers the same methods as the sign-in
// page, and with oidc alone it goes straight to single sign-on.
func TestSignedOutPageOffersTheSignInMethods(t *testing.T) {
	for _, c := range []struct {
		methods    string
		sso, again bool
	}{
		{"oidc", true, false},
		{"local,oidc", true, true},
	} {
		o := newOIDCHarness(t, c.methods, nil)
		body := o.get(PathSignedOut).Body.String()
		if got := has(body, `href="`+PathOIDCStart+`"`) && has(body, ssoLabel); got != c.sso {
			t.Errorf("%s: single sign-on button is %v, want %v", c.methods, got, c.sso)
		}
		if got := has(body, "Sign in again"); got != c.again {
			t.Errorf("%s: 'Sign in again' is %v, want %v", c.methods, got, c.again)
		}
	}
	hn := newHarness(t, nil)
	body := hn.get(PathSignedOut).Body.String()
	if has(body, PathOIDCStart) || !has(body, "Sign in again") {
		t.Errorf("the local-only signed-out page changed:\n%s", body)
	}
}
