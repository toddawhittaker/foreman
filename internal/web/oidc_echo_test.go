package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// SPEC 14.8, 14.14: a failed callback's page never carries the callback's
// query (code, state, the provider's words) into its language menu or links;
// the menu returns to the sign-in page.
func TestOIDCFailurePageDoesNotEchoTheCallback(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	authorize, bind := o.begin("")
	a, _ := url.Parse(authorize)
	state := a.Query().Get("state")
	q := url.Values{"state": {state}, "error": {"access_denied"}, "error_description": {"INJECTED-words"}, "code": {"leaked-code"}}
	rec := o.get(PathOIDCCallback+"?"+q.Encode(), withCookie(bind))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("provider error: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, secret := range []string{state, "INJECTED-words", "leaked-code", "access_denied"} {
		if strings.Contains(body, secret) {
			t.Errorf("the page holds %q", secret)
		}
	}
	if !strings.Contains(body, `name="return" value="`+PathSignIn+`"`) {
		t.Errorf("the language menu does not return to sign-in")
	}
}
