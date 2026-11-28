package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/cmd/fakeidp/idp"
	"github.com/toddawhittaker/foreman/internal/auth"
)

// Trust-boundary tests for single sign-on (SPEC 12.7, 12.11, 14.1, 14.8,
// 14.14) that the sign-in tests in oidc_test.go leave out.

// SPEC 12.7: the binding cookie ties a callback to the browser that started
// it, so a callback from someone else's sign-in, loaded in a browser that
// started its own, signs nobody in. The browser's own sign-in survives the
// attempt, so a hostile page cannot cancel it.
func TestOIDCCallbackFromAnotherSignInIsRefused(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	victimAuthorize, victimCookie := o.begin("")
	attackerCallback := o.approve(func() string { a, _ := o.begin(""); return a }())
	calls := o.tr.calls.Load()

	rec := o.get(attackerCallback, withCookie(victimCookie))
	if rec.Code != http.StatusBadRequest || sessionCookie(rec) != nil {
		t.Fatalf("someone else's callback: %d (session %v)", rec.Code, sessionCookie(rec) != nil)
	}
	if n := o.tr.calls.Load() - calls; n != 0 {
		t.Errorf("a callback bound to another browser made %d calls to the provider", n)
	}
	if has, _ := o.store.HasUsers(context.Background()); has {
		t.Error("a refused callback created a user")
	}
	o.wantRedirect(o.get(o.approve(victimAuthorize), withCookie(victimCookie)), PathWorlds)
}

// SPEC 12.7: a sign-in that failed is used up too, so its callback cannot be
// sent again, and the provider is not asked a second time.
func TestOIDCFailedCallbackCannotBeReplayed(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	c := idp.DefaultClaims()
	c.Nonce = "not-the-nonce"
	o.idp.SetNext(c)
	authorize, bind := o.begin("")
	cb := o.approve(authorize)
	if rec := o.get(cb, withCookie(bind)); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong nonce: %d", rec.Code)
	}
	o.idp.SetNext(idp.DefaultClaims())
	calls := o.tr.calls.Load()
	if rec := o.get(cb, withCookie(bind)); rec.Code != http.StatusBadRequest || sessionCookie(rec) != nil {
		t.Errorf("replayed failed callback: %d", rec.Code)
	}
	if n := o.tr.calls.Load() - calls; n != 0 {
		t.Errorf("the replay made %d calls to the provider", n)
	}
}

// SPEC 14.8, 14.14 and the plan's T9 gate ("no claim, code or secret is in
// logs or pages"): the authorization code, the state, the nonce and the
// client secret never reach a log line or a page, on success or failure, and
// words the provider sends back are not shown. The browser's address bar
// never carries the client secret either.
//
// DEFECT, failing on purpose: a failed callback's error page puts the whole
// callback address, code and state included, in the layout's hidden
// "return" fields (language and theme forms), and so repeats the provider's
// error_description too.
func TestOIDCNoCodeStateOrSecretInLogsOrPages(t *testing.T) {
	o := newOIDCHarness(t, "local,oidc", nil)
	var secrets []string
	var pages []string
	collect := func(authorize, callback string) {
		a, _ := url.Parse(authorize)
		cb, _ := url.Parse(callback)
		secrets = append(secrets, a.Query().Get("state"), a.Query().Get("nonce"), cb.Query().Get("code"))
	}

	// A start never puts the client secret in the address the browser sees.
	authorize, bind := o.begin("/worlds")
	if strings.Contains(authorize, idp.ClientSecret) || strings.Contains(authorize, url.QueryEscape(idp.ClientSecret)) {
		t.Errorf("the authorize address holds the client secret: %s", authorize)
	}
	cb := o.approve(authorize)
	collect(authorize, cb)
	ok := o.get(cb, withCookie(bind))
	o.wantRedirect(ok, "/worlds")
	pages = append(pages, ok.Body.String(), ok.Header().Get("Location"))
	pages = append(pages, o.get(PathWorlds, withCookie(sessionCookie(ok))).Body.String())

	// Failures: a wrong audience, a missing group, and a provider error
	// whose description a hostile provider or link could fill.
	for _, edit := range []func(*idp.Claims){
		func(c *idp.Claims) { c.Audience = "another-client" },
		func(c *idp.Claims) { c.Groups = []string{"other"} },
	} {
		c := idp.DefaultClaims()
		edit(&c)
		o.idp.SetNext(c)
		authorize, bind := o.begin("")
		cb := o.approve(authorize)
		collect(authorize, cb)
		rec := o.get(cb, withCookie(bind))
		if rec.Code < 400 {
			t.Fatalf("a bad token was accepted: %d", rec.Code)
		}
		pages = append(pages, rec.Body.String())
	}
	o.idp.SetNext(idp.DefaultClaims())
	authorize, bind = o.begin("")
	a, _ := url.Parse(authorize)
	state := a.Query().Get("state")
	secrets = append(secrets, state, a.Query().Get("nonce"))
	const injected = "INJECTED-provider-words"
	rec := o.get(PathOIDCCallback+"?"+url.Values{"state": {state}, "error": {"access_denied"}, "error_description": {injected}, "code": {"leaked-code-value"}}.Encode(), withCookie(bind))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("provider error: %d", rec.Code)
	}
	pages = append(pages, rec.Body.String())
	secrets = append(secrets, "leaked-code-value")

	logs := o.logs.String()
	for _, s := range secrets {
		if s == "" {
			t.Fatal("a value to look for is empty")
		}
		if strings.Contains(logs, s) {
			t.Errorf("the log holds %q", s)
		}
		for i, p := range pages {
			if strings.Contains(p, s) {
				t.Errorf("page %d holds %q", i, s)
			}
		}
	}
	for i, p := range append(pages, logs) {
		if strings.Contains(p, injected) {
			t.Errorf("text %d repeats the provider's error description", i)
		}
		if strings.Contains(p, idp.ClientSecret) {
			t.Errorf("text %d holds the client secret", i)
		}
	}
	o.noSecretsLogged()
}

// SPEC 12.7: a header user is linked to the first subject that
// signs in with their name, once. A second subject with the same name is
// refused and does not move the link.
func TestOIDCHeaderUserLinkCannotMove(t *testing.T) {
	env := map[string]string{"FOREMAN_AUTH_REQUIRED_GROUP": testGroup}
	for k, v := range headerEnv {
		env[k] = v
	}
	o := newOIDCHarness(t, "headers,oidc", env)
	if rec := o.get(PathWorlds, asUser(idp.Username)...); rec.Code != http.StatusOK {
		t.Fatalf("header sign-in: %d", rec.Code)
	}
	header, _, _ := o.store.UserByUsername(context.Background(), idp.Username)
	o.wantRedirect(o.signIn(""), PathWorlds)

	c := idp.DefaultClaims()
	c.Subject = "a-second-subject"
	o.idp.SetNext(c)
	rec := o.signIn("")
	if rec.Code != http.StatusForbidden || sessionCookie(rec) != nil {
		t.Fatalf("second subject with the header user's name: %d", rec.Code)
	}
	if _, found, _ := o.store.UserByOIDC(context.Background(), o.idp.URL, "a-second-subject"); found {
		t.Error("the second subject was linked")
	}
	linked, found, _ := o.store.UserByOIDC(context.Background(), o.idp.URL, idp.Subject)
	if !found || linked.ID != header.ID {
		t.Errorf("the first link moved: %+v found=%v", linked, found)
	}
	if all, _ := o.store.ListUsers(context.Background()); len(all) != 1 {
		t.Errorf("%d users, want 1", len(all))
	}
}

// SPEC 12.11, 14.1: the start limit counts the real peer. A client outside
// the trusted proxy ranges cannot get past it by sending X-Forwarded-For; a
// trusted proxy's header names the client, so two clients behind it are
// counted apart.
func TestOIDCStartLimitIgnoresUntrustedForwardedFor(t *testing.T) {
	o := newOIDCHarness(t, "oidc", map[string]string{"FOREMAN_TRUSTED_PROXIES": "10.0.0.0/8"})
	for range auth.OIDCStartsPerAddress {
		o.begin("")
	}
	for _, xff := range []string{"198.51.100.1", "198.51.100.2, 10.0.0.9", "10.0.0.5"} {
		if rec := o.get(PathOIDCStart, withHeader("X-Forwarded-For", xff)); rec.Code != http.StatusTooManyRequests {
			t.Errorf("untrusted peer claiming %q: %d, want 429", xff, rec.Code)
		}
	}

	// Behind the trusted proxy, one client's starts do not use up another's.
	first := []func(*http.Request){withPeer(peerProxy), withHeader("X-Forwarded-For", "203.0.113.1")}
	for range auth.OIDCStartsPerAddress {
		if rec := o.get(PathOIDCStart, first...); rec.Code != http.StatusSeeOther {
			t.Fatalf("first client behind the proxy: %d", rec.Code)
		}
	}
	if rec := o.get(PathOIDCStart, first...); rec.Code != http.StatusTooManyRequests {
		t.Errorf("first client over its limit: %d", rec.Code)
	}
	if rec := o.get(PathOIDCStart, withPeer(peerProxy), withHeader("X-Forwarded-For", "203.0.113.2")); rec.Code != http.StatusSeeOther {
		t.Errorf("second client behind the proxy: %d", rec.Code)
	}
}
