package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/cmd/fakeidp/idp"
	"github.com/toddawhittaker/foreman/internal/auth"
)

const oidcPublicURL = "https://foreman.example.com"

// countingTransport counts the calls to the identity provider and can make
// them all fail, as an unreachable provider would.
type countingTransport struct {
	base     http.RoundTripper
	calls    atomic.Int32
	unusable atomic.Bool
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	if c.unusable.Load() {
		return nil, errors.New("connection refused")
	}
	return c.base.RoundTrip(r)
}

type oidcHarness struct {
	*harness
	idp *idp.IdP
	tr  *countingTransport
}

// newOIDCHarness runs Foreman with the methods given, oidc among them,
// against a fake identity provider over https. extra adds settings.
func newOIDCHarness(t *testing.T, methods string, extra map[string]string) *oidcHarness {
	t.Helper()
	var hn *harness
	f, err := idp.Start(t.TempDir(), func() time.Time { return hn.clock.Now() })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.Close)
	env := map[string]string{
		"FOREMAN_AUTH_METHODS":        methods,
		"FOREMAN_PUBLIC_URL":          oidcPublicURL,
		"FOREMAN_OIDC_ISSUER":         f.URL,
		"FOREMAN_OIDC_CLIENT_ID":      idp.ClientID,
		"FOREMAN_OIDC_CLIENT_SECRET":  idp.ClientSecret,
		"FOREMAN_OIDC_REQUIRED_GROUP": idp.Group,
	}
	for k, v := range extra {
		env[k] = v
	}
	tr := &countingTransport{base: f.Transport()}
	hn = newHarnessDeps(t, env, nil, func(d *Deps) { d.OIDCTransport = tr })
	return &oidcHarness{harness: hn, idp: f, tr: tr}
}

// begin opens the start route and returns the provider's authorize address
// and the binding cookie.
func (o *oidcHarness) begin(next string) (string, *http.Cookie) {
	o.t.Helper()
	path := PathOIDCStart
	if next != "" {
		path += "?next=" + url.QueryEscape(next)
	}
	rec := o.get(path)
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, o.idp.URL+"/auth?") {
		o.t.Fatalf("start: %d to %q", rec.Code, loc)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == oidcCookie {
			return loc, c
		}
	}
	o.t.Fatal("start set no binding cookie")
	return "", nil
}

// approve has the provider approve the sign-in and returns the callback's
// path and query.
func (o *oidcHarness) approve(authorize string) string {
	o.t.Helper()
	resp, err := o.idp.Client.Get(authorize)
	if err != nil {
		o.t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusFound || err != nil || !strings.HasPrefix(loc.String(), oidcPublicURL+PathOIDCCallback+"?") {
		o.t.Fatalf("authorize: %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	return loc.RequestURI()
}

// signIn runs a whole sign-in and returns the callback's answer.
func (o *oidcHarness) signIn(next string) *httptest.ResponseRecorder {
	o.t.Helper()
	authorize, cookie := o.begin(next)
	return o.get(o.approve(authorize), withCookie(cookie))
}

// noSecretsLogged checks the log holds no claim, code, token or secret
// (SPEC 14.8, 14.14).
func (o *oidcHarness) noSecretsLogged(extra ...string) {
	o.t.Helper()
	logs := o.logs.String()
	for _, s := range append([]string{idp.ClientSecret, idp.Subject, idp.Username, "code="}, extra...) {
		if strings.Contains(logs, s) {
			o.t.Errorf("the log holds %q", s)
		}
	}
}

// SPEC 12.7: a first sign-in creates the user, linked by issuer and
// subject, as the first admin; the session is a normal one; the redirect
// address, scopes, PKCE and nonce are as the plan fixes them.
func TestOIDCSignInCreatesUser(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	if n := o.tr.calls.Load(); n != 0 {
		t.Fatalf("Foreman called the provider %d times before anyone signed in (SPEC 3.7)", n)
	}
	authorize, bind := o.begin("/worlds?sort=name")
	if !bind.HttpOnly || bind.SameSite != http.SameSiteLaxMode || !bind.Secure || bind.Path != PathOIDCStart {
		t.Errorf("binding cookie %+v", bind)
	}
	cb := o.approve(authorize)
	a := o.idp.Authorizes()[0]
	if a.Scope != "openid profile email groups" || a.ChallengeMethod != "S256" || !a.HasNonce ||
		a.RedirectURI != oidcPublicURL+PathOIDCCallback || a.ClientID != idp.ClientID {
		t.Errorf("authorize request %+v", a)
	}
	rec := o.get(cb, withCookie(bind))
	o.wantRedirect(rec, "/worlds?sort=name")
	session := sessionCookie(rec)
	if session == nil {
		t.Fatal("no session cookie")
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		cleared = cleared || (c.Name == oidcCookie && c.MaxAge < 0)
	}
	if !cleared {
		t.Error("the callback left the binding cookie")
	}
	u, found, err := o.store.UserByOIDC(context.Background(), o.idp.URL, idp.Subject)
	if err != nil || !found || u.Username != idp.Username || u.PasswordHash != "" || u.Role != "admin" {
		t.Fatalf("user %+v found=%v err=%v", u, found, err)
	}
	if page := o.get(PathWorlds, withCookie(session)); page.Code != http.StatusOK || !strings.Contains(page.Body.String(), idp.Username) {
		t.Fatalf("signed-in page: %d", page.Code)
	}
	if got := o.audits(AuditUserCreate); len(got) != 1 || got[0] != idp.Username+"  oidc" {
		t.Errorf("user-create entries %q", got)
	}
	// A second sign-in finds the same user and creates nothing.
	o.wantRedirect(o.signIn(""), PathWorlds)
	if got := o.audits(AuditUserCreate); len(got) != 1 {
		t.Errorf("user-create entries after a second sign-in %q", got)
	}
	o.noSecretsLogged()
}

// SPEC 12.7: the user is found by subject, so a name change at the
// provider keeps the same account.
func TestOIDCMatchesBySubjectNotName(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	o.wantRedirect(o.signIn(""), PathWorlds)
	c := idp.DefaultClaims()
	c.Username = "renamed"
	o.idp.SetNext(c)
	o.wantRedirect(o.signIn(""), PathWorlds)
	u, _, _ := o.store.UserByOIDC(context.Background(), o.idp.URL, idp.Subject)
	if u.Username != idp.Username {
		t.Errorf("user is now %q", u.Username)
	}
	if _, found, _ := o.store.UserByUsername(context.Background(), "renamed"); found {
		t.Error("a name change made a second user")
	}
}

// SPEC 12.7, 14.14: each bad answer from the provider is refused, signs no
// one in, creates no user, and logs nothing of the token.
func TestOIDCRefusesBadTokens(t *testing.T) {
	tests := []struct {
		name   string
		edit   func(*idp.Claims)
		status int
		reason string
	}{
		{"wrong nonce", func(c *idp.Claims) { c.Nonce = "not-the-nonce" }, http.StatusBadRequest, "nonce does not match"},
		{"wrong audience", func(c *idp.Claims) { c.Audience = "another-client" }, http.StatusBadRequest, "failed its checks"},
		{"wrong issuer", func(c *idp.Claims) { c.Issuer = "https://elsewhere.example.com" }, http.StatusBadRequest, "failed its checks"},
		{"expired", func(c *idp.Claims) { c.Expired = true }, http.StatusBadRequest, "failed its checks"},
		{"missing group", func(c *idp.Claims) { c.Groups = []string{"other"} }, http.StatusForbidden, "required group"},
		{"no groups", func(c *idp.Claims) { c.Groups = nil }, http.StatusForbidden, "required group"},
		{"no username", func(c *idp.Claims) { c.Username = "" }, http.StatusForbidden, "no usable username"},
		{"no subject", func(c *idp.Claims) { c.Subject = "" }, http.StatusBadRequest, "no subject"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := newOIDCHarness(t, "oidc", nil)
			c := idp.DefaultClaims()
			tt.edit(&c)
			o.idp.SetNext(c)
			rec := o.signIn("")
			if rec.Code != tt.status || sessionCookie(rec) != nil {
				t.Fatalf("got %d (session %v), want %d", rec.Code, sessionCookie(rec) != nil, tt.status)
			}
			if has, _ := o.store.HasUsers(context.Background()); has {
				t.Error("a refused sign-in created a user")
			}
			if !strings.Contains(o.logs.String(), "single sign-on failed") || !strings.Contains(o.logs.String(), tt.reason) {
				t.Log(o.logs.String())
				t.Error("the refusal was not logged")
			}
			o.noSecretsLogged("not-the-nonce", "another-client")
			if strings.Contains(rec.Body.String(), idp.Username) || strings.Contains(rec.Body.String(), idp.Subject) {
				t.Error("the page shows a claim")
			}
		})
	}
}

// SPEC 12.7: the callback must come back to the browser that started it,
// once, within its time.
func TestOIDCCallbackNeedsItsBrowserOnce(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)

	authorize, bind := o.begin("")
	cb := o.approve(authorize)
	if rec := o.get(cb); rec.Code != http.StatusBadRequest {
		t.Errorf("no binding cookie: %d", rec.Code)
	}
	other := *bind //nolint:gosec // a copy of the binding cookie with another value, sent by the test
	other.Value = "another-state"
	if rec := o.get(cb, withCookie(&other)); rec.Code != http.StatusBadRequest {
		t.Errorf("wrong binding cookie: %d", rec.Code)
	}

	authorize, bind = o.begin("")
	cb = o.approve(authorize)
	o.wantRedirect(o.get(cb, withCookie(bind)), PathWorlds)
	if rec := o.get(cb, withCookie(bind)); rec.Code != http.StatusBadRequest || sessionCookie(rec) != nil {
		t.Errorf("replayed callback: %d", rec.Code)
	}

	authorize, bind = o.begin("")
	cb = o.approve(authorize)
	o.clock.Advance(oidcPendingTTL + time.Second)
	if rec := o.get(cb, withCookie(bind)); rec.Code != http.StatusBadRequest {
		t.Errorf("expired sign-in: %d", rec.Code)
	}
	if rec := o.get(PathOIDCCallback+"?error=access_denied&state=x", withCookie(bind)); rec.Code != http.StatusBadRequest {
		t.Errorf("provider error: %d", rec.Code)
	}
}

// SPEC 14.17: a return path that is not a local page goes home.
func TestOIDCForeignNextGoesHome(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	for _, next := range []string{"//evil.example.com/x", "https://evil.example.com/", "/\\evil.example.com"} {
		o.wantRedirect(o.signIn(next), PathWorlds)
	}
}

// SPEC 12.11: a callback that names a sign-in this browser started counts
// against the address when it fails, in the table password sign-in uses.
func TestOIDCCallbackFailuresAreLimited(t *testing.T) {
	o := newOIDCHarness(t, "local,oidc", nil)
	c := idp.DefaultClaims()
	c.Nonce = "not-the-nonce"
	o.idp.SetNext(c)
	for i := range auth.LimitPerAddress {
		if i > 0 && i%auth.OIDCStartsPerAddress == 0 {
			o.clock.Advance(auth.OIDCStartWindow) // past the start limit, not the failure window
		}
		if rec := o.signIn(""); rec.Code != http.StatusBadRequest {
			t.Fatalf("failure %d: %d", i, rec.Code)
		}
	}
	o.clock.Advance(auth.OIDCStartWindow)
	tokens := o.idp.Tokens()
	rec := o.signIn("")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over the limit: %d", rec.Code)
	}
	if o.idp.Tokens() != tokens {
		t.Error("a limited callback still exchanged its code")
	}
	o.addUser("alice", testPassword)
	if rec := o.post(PathSignIn, signInForm("alice", testPassword)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("password sign-in from the same address: %d", rec.Code)
	}
}

// SPEC 12.11, 14.14: a callback with no sign-in of its own, which any page
// can make a browser load, costs nothing and calls no one, so it cannot
// lock the visitor's address out.
func TestOIDCForgedCallbacksAreFree(t *testing.T) {
	o := newOIDCHarness(t, "local,oidc", nil)
	for i := range 3 * auth.LimitPerAddress {
		if rec := o.get(PathOIDCCallback + "?state=x&code=y"); rec.Code != http.StatusBadRequest {
			t.Fatalf("forged callback %d: %d", i, rec.Code)
		}
	}
	if n := o.tr.calls.Load(); n != 0 {
		t.Errorf("forged callbacks made %d calls to the provider", n)
	}
	o.addUser("alice", testPassword)
	o.wantRedirect(o.post(PathSignIn, signInForm("alice", testPassword)), PathWorlds)
	o.wantRedirect(o.signIn(""), PathWorlds)
}

// SPEC 12.7: one address may start only so many sign-ins a minute, so it
// cannot push other people's started sign-ins out.
func TestOIDCStartsAreLimited(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	for range auth.OIDCStartsPerAddress {
		o.begin("")
	}
	rec := o.get(PathOIDCStart)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("over the start limit: %d", rec.Code)
	}
	if rec := o.get(PathOIDCStart, withPeer("198.51.100.7:1")); rec.Code != http.StatusSeeOther {
		t.Errorf("another address: %d", rec.Code)
	}
	o.clock.Advance(auth.OIDCStartWindow)
	o.begin("")
}

// SPEC 12.7: a username Foreman cannot use gets its own message, and does
// not count against the address, since trying again can never work.
func TestOIDCBadUsernameIsNotCounted(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	c := idp.DefaultClaims()
	c.Username = "bad\u0007name"
	o.idp.SetNext(c)
	for i := range auth.LimitPerAddress {
		if i > 0 && i%auth.OIDCStartsPerAddress == 0 {
			o.clock.Advance(auth.OIDCStartWindow)
		}
		rec := o.signIn("")
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "no username Foreman can use") {
			t.Fatalf("bad username %d: %d", i, rec.Code)
		}
	}
	o.clock.Advance(auth.OIDCStartWindow)
	o.idp.SetNext(idp.DefaultClaims())
	o.wantRedirect(o.signIn(""), PathWorlds)
}

// SPEC 12.7, 12.9: callbacks racing for one new subject make one user, and
// exactly one user-create entry.
func TestOIDCConcurrentFirstSignIn(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	const n = 8
	cbs := make([]string, n)
	cookies := make([]*http.Cookie, n)
	for i := range n {
		authorize, c := o.begin("")
		cbs[i], cookies[i] = o.approve(authorize), c
	}
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { codes[i] = o.get(cbs[i], withCookie(cookies[i])).Code })
	}
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusSeeOther {
			t.Errorf("callback %d: %d", i, c)
		}
	}
	if got := o.audits(AuditUserCreate); len(got) != 1 {
		t.Errorf("user-create entries %q, want one", got)
	}
	u, found, _ := o.store.UserByOIDC(context.Background(), o.idp.URL, idp.Subject)
	if !found || u.Username != idp.Username {
		t.Errorf("user %+v found=%v", u, found)
	}
}

// SPEC 12.7: discovery waits for the first sign-in; after a failure it
// answers at once for a short while, then tries again; a working document
// is kept.
func TestOIDCDiscoveryRetriedAfterFailure(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	o.tr.unusable.Store(true)
	rec := o.get(PathOIDCStart)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "cannot reach the single sign-on provider") {
		t.Fatalf("provider down: %d", rec.Code)
	}
	o.tr.unusable.Store(false)
	calls := o.tr.calls.Load()
	if rec := o.get(PathOIDCStart); rec.Code != http.StatusServiceUnavailable || o.tr.calls.Load() != calls {
		t.Errorf("right after a failure: %d with %d calls", rec.Code, o.tr.calls.Load()-calls)
	}
	o.clock.Advance(11 * time.Second)
	o.wantRedirect(o.signIn(""), PathWorlds)
	before := o.tr.calls.Load()
	o.begin("")
	if after := o.tr.calls.Load(); after != before {
		t.Errorf("a second start called the provider %d times; discovery should be kept", after-before)
	}
}

// SPEC 12.7: a name clash with a password user is refused with a
// page that says why; nothing is linked.
func TestOIDCNameClashWithPasswordUser(t *testing.T) {
	o := newOIDCHarness(t, "local,oidc", nil)
	o.addUser(idp.Username, testPassword)
	rec := o.signIn("")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "signs in another way") || sessionCookie(rec) != nil {
		t.Fatalf("clash: %d", rec.Code)
	}
	if _, found, _ := o.store.UserByOIDC(context.Background(), o.idp.URL, idp.Subject); found {
		t.Error("the password user was linked")
	}
	o.noSecretsLogged()
}

// SPEC 12.7: a second subject with a linked user's name is refused too.
func TestOIDCNameClashWithLinkedUser(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	o.wantRedirect(o.signIn(""), PathWorlds)
	c := idp.DefaultClaims()
	c.Subject = "another-subject"
	o.idp.SetNext(c)
	if rec := o.signIn(""); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "signs in another way") {
		t.Fatalf("second subject: %d", rec.Code)
	}
}

// SPEC 12.7: a user made by trusted headers is linked once, on
// the first OIDC sign-in, and keeps signing in both ways.
func TestOIDCLinksHeaderUser(t *testing.T) {
	env := map[string]string{"FOREMAN_AUTH_REQUIRED_GROUP": testGroup}
	for k, v := range headerEnv {
		env[k] = v
	}
	o := newOIDCHarness(t, "headers,oidc", env)
	if rec := o.get(PathWorlds, asUser(idp.Username)...); rec.Code != http.StatusOK {
		t.Fatalf("header sign-in: %d", rec.Code)
	}
	if got := o.audits(AuditUserCreate); len(got) != 1 || got[0] != idp.Username+"  headers" {
		t.Errorf("user-create after headers %q", got)
	}
	header, _, _ := o.store.UserByUsername(context.Background(), idp.Username)
	o.wantRedirect(o.signIn(""), PathWorlds)
	linked, found, _ := o.store.UserByOIDC(context.Background(), o.idp.URL, idp.Subject)
	if !found || linked.ID != header.ID {
		t.Fatalf("linked %+v, want the header user %d", linked, header.ID)
	}
	if got := o.audits(AuditUserCreate); len(got) != 1 {
		t.Errorf("linking created a user: %q", got)
	}
	if rec := o.get(PathWorlds, asUser(idp.Username)...); rec.Code != http.StatusOK {
		t.Errorf("header sign-in after the link: %d", rec.Code)
	}
}

// SPEC 12.3, 12.7: with oidc alone there is no setup page and no password
// form; an anonymous visitor goes to sign-in even before any user exists.
func TestOIDCAloneHasNoSetup(t *testing.T) {
	o := newOIDCHarness(t, "oidc", nil)
	if rec := o.get(PathSetup); rec.Code != http.StatusNotFound {
		t.Errorf("setup page: %d", rec.Code)
	}
	if rec := o.post(PathSignIn, signInForm("alice", testPassword)); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("password sign-in: %d", rec.Code)
	}
	if rec := o.get(PathSignIn); rec.Code != http.StatusOK {
		t.Errorf("sign-in page: %d", rec.Code)
	}
	o.wantRedirect(o.get("/worlds"), PathSignIn+"?next=%2Fworlds")
}

// SPEC 12.9: setup records the first admin's creation.
func TestSetupRecordsUserCreate(t *testing.T) {
	hn := newHarness(t, nil)
	hn.post(PathSetup, url.Values{"username": {"alice"}, "password": {testPassword}, "confirm": {testPassword}})
	if got := hn.audits("user-create"); len(got) != 1 || got[0] != "alice  setup" {
		t.Errorf("user-create entries %q", got)
	}
}

// DESIGN 9: a refused single sign-on says so in its heading and offers the
// likely next step as a button: retry when trying again can work, a way back
// to the sign-in page when it cannot. A retry carries the page asked for.
func TestOIDCRefusalPagesOfferTheNextStep(t *testing.T) {
	const heading = "Single sign-on did not work"
	retry := PathOIDCStart + "?next=%2Fschedule"
	check := func(t *testing.T, rec *httptest.ResponseRecorder, status int, label, href string) {
		t.Helper()
		body := rec.Body.String()
		if rec.Code != status || !strings.Contains(body, "<h1>"+heading+"</h1>") {
			t.Fatalf("got %d without the heading %q", rec.Code, heading)
		}
		if !strings.Contains(body, `<a class="button button--primary" href="`+href+`">`+label+"</a>") {
			t.Errorf("no primary button %q to %s:\n%s", label, href, between(body, `<div class="panel">`, "</main>"))
		}
	}
	t.Run("provider unavailable", func(t *testing.T) {
		o := newOIDCHarness(t, "oidc", nil)
		o.tr.unusable.Store(true)
		check(t, o.get(PathOIDCStart+"?next=%2Fschedule"), http.StatusServiceUnavailable, "Try single sign-on again", retry)
	})
	t.Run("failed", func(t *testing.T) {
		o := newOIDCHarness(t, "oidc", nil)
		c := idp.DefaultClaims()
		c.Nonce = "not-the-nonce"
		o.idp.SetNext(c)
		check(t, o.signIn("/schedule"), http.StatusBadRequest, "Try single sign-on again", retry)
	})
	t.Run("failed with no page asked for", func(t *testing.T) {
		o := newOIDCHarness(t, "oidc", nil)
		check(t, o.get(PathOIDCCallback), http.StatusBadRequest, "Try single sign-on again", PathOIDCStart)
	})
	t.Run("name taken", func(t *testing.T) {
		o := newOIDCHarness(t, "local,oidc", nil)
		o.addUser(idp.Username, testPassword)
		check(t, o.signIn("/schedule"), http.StatusForbidden, "Back to sign in", PathSignIn)
	})
	t.Run("bad username", func(t *testing.T) {
		o := newOIDCHarness(t, "oidc", nil)
		c := idp.DefaultClaims()
		c.Username = ""
		o.idp.SetNext(c)
		check(t, o.signIn("/schedule"), http.StatusForbidden, "Back to sign in", PathSignIn)
	})
}
