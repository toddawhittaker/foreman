package web

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

	"git.thewhittakers.org/todd/foreman/internal/auth"
)

func signInForm(user, pw string) url.Values {
	return url.Values{"username": {user}, "password": {pw}}
}

func TestEmptyDatabaseSendsVisitorsToSetup(t *testing.T) {
	hn := newHarness(t, nil)
	hn.wantRedirect(hn.get("/"), PathSetup)
}

func TestSetupCreatesAdminAndSignsIn(t *testing.T) {
	hn := newHarness(t, nil)
	if rec := hn.get(PathSetup); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Use at least 12 characters") {
		t.Fatalf("setup form: %d", rec.Code)
	}
	rec := hn.post(PathSetup, url.Values{"username": {"  alice "}, "password": {testPassword}, "confirm": {testPassword}})
	hn.wantRedirect(rec, PathHome)
	c := sessionCookie(rec)
	if c == nil || c.Value == "" {
		t.Fatal("setup set no session cookie")
	}
	u, ok, err := hn.store.UserByUsername(context.Background(), "alice")
	if err != nil || !ok || u.Role != "admin" || u.PasswordHash == "" || strings.Contains(u.PasswordHash, testPassword) {
		t.Fatalf("stored user: %+v ok=%v err=%v", u, ok, err)
	}
	home := hn.get("/", withCookie(c))
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), "alice") {
		t.Fatalf("home after setup: %d", home.Code)
	}
}

func TestSetupRefusesBadInput(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"no username", url.Values{"password": {testPassword}, "confirm": {testPassword}}, "Enter a username"},
		{"blank username", url.Values{"username": {"   "}, "password": {testPassword}, "confirm": {testPassword}}, "Enter a username"},
		{"long username", url.Values{"username": {strings.Repeat("a", 65)}, "password": {testPassword}, "confirm": {testPassword}}, "at most 64"},
		{"control character", url.Values{"username": {"al\x07ice"}, "password": {testPassword}, "confirm": {testPassword}}, "control characters"},
		{"no password", url.Values{"username": {"alice"}}, "Enter a password"},
		{"short password", url.Values{"username": {"alice"}, "password": {"short"}, "confirm": {"short"}}, "Use at least 12 characters"},
		{"huge password", url.Values{"username": {"alice"}, "password": {strings.Repeat("p", 2000)}, "confirm": {strings.Repeat("p", 2000)}}, "at most 1024 bytes"},
		{"mismatch", url.Values{"username": {"alice"}, "password": {testPassword}, "confirm": {testPassword + "x"}}, "same password in both fields"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hn := newHarness(t, nil)
			rec := hn.post(PathSetup, c.form)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
				t.Fatalf("got %d, want 400 containing %q", rec.Code, c.want)
			}
			if pw := c.form.Get("password"); pw != "" && strings.Contains(rec.Body.String(), pw) {
				t.Error("the page echoes the password")
			}
			if has, _ := hn.store.HasUsers(context.Background()); has {
				t.Error("a rejected setup created a user")
			}
		})
	}
}

func TestSecondSetupIsRefused(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	hn.wantRedirect(hn.get(PathSetup), PathSignIn)
	rec := hn.post(PathSetup, url.Values{"username": {"bob"}, "password": {testPassword}, "confirm": {testPassword}})
	hn.wantRedirect(rec, PathSignIn)
	if sessionCookie(rec) != nil {
		t.Error("a refused setup started a session")
	}
	if _, ok, _ := hn.store.UserByUsername(context.Background(), "bob"); ok {
		t.Error("a second setup created a user")
	}
}

func TestNoSetupOrSignInWithoutLocalMethod(t *testing.T) {
	hn := newHarness(t, map[string]string{
		"FOREMAN_AUTH_METHODS": "headers", "FOREMAN_AUTH_USER_HEADER": "X-User", "FOREMAN_TRUSTED_PROXIES": "10.0.0.0/8",
	})
	for _, p := range []string{PathSetup, PathSignIn} {
		if rec := hn.get(p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: got %d, want 404", p, rec.Code)
		}
		if rec := hn.post(p, signInForm("alice", testPassword)); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: got %d, want 404", p, rec.Code)
		}
	}
}

func TestSignInSucceeds(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("Alice", testPassword)
	if rec := hn.get(PathSignIn); rec.Code != http.StatusOK {
		t.Fatalf("sign-in form: %d", rec.Code)
	}
	rec := hn.post(PathSignIn, signInForm(" alice ", testPassword))
	hn.wantRedirect(rec, PathHome)
	c := sessionCookie(rec)
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie: %+v", c)
	}
	if home := hn.get("/", withCookie(c)); home.Code != http.StatusOK {
		t.Fatalf("home: %d", home.Code)
	}
}

func TestSignInFailuresLookAlike(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	// A header-style user has no password.
	if _, err := hn.store.CreateUser(context.Background(), "bob", "", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	attempts := map[string]url.Values{
		"wrong password":   signInForm("alice", "not the password"),
		"unknown user":     signInForm("nobody", "not the password"),
		"header-only user": signInForm("bob", "anything at all"),
	}
	var bodies []string
	for name, form := range attempts {
		rec := hn.post(PathSignIn, form)
		if rec.Code != http.StatusUnauthorized || sessionCookie(rec) != nil {
			t.Fatalf("%s: got %d, cookie %v", name, rec.Code, sessionCookie(rec))
		}
		body := rec.Body.String()
		if !strings.Contains(body, "The username or password is not right") {
			t.Errorf("%s: no combined error message", name)
		}
		// The refilled username is the only difference between the pages.
		bodies = append(bodies, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(body, "alice", "X"), "nobody", "X"), "bob", "X"))
	}
	for _, b := range bodies[1:] {
		if b != bodies[0] {
			t.Error("failure pages differ, so they reveal which part was wrong")
		}
	}
}

func TestSignInNeedsBothFields(t *testing.T) {
	hn := newHarness(t, nil)
	if rec := hn.post(PathSignIn, signInForm("", "")); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "Enter your username") || !strings.Contains(rec.Body.String(), "Enter your password") {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestSignInRateLimitReturns429(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	for i := 0; i < auth.LimitPerUsername; i++ {
		if rec := hn.post(PathSignIn, signInForm("alice", "wrong password")); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	rec := hn.post(PathSignIn, signInForm("alice", testPassword))
	if rec.Code != http.StatusTooManyRequests || sessionCookie(rec) != nil {
		t.Fatalf("got %d, want 429 even with the right password", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Too many failed sign-ins") || rec.Header().Get("Retry-After") == "" {
		t.Error("no wait message or Retry-After")
	}
	hn.clock.Advance(auth.LimitWindow + time.Minute)
	hn.wantRedirect(hn.post(PathSignIn, signInForm("alice", testPassword)), PathHome)
}

func TestSignOutEndsSession(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	c := sessionCookie(hn.post(PathSignIn, signInForm("alice", testPassword)))
	rec := hn.post(PathSignOut, nil, withCookie(c))
	hn.wantRedirect(rec, PathSignedOut)
	if cleared := sessionCookie(rec); cleared == nil || cleared.MaxAge >= 0 {
		t.Errorf("cookie not cleared: %+v", cleared)
	}
	// The old cookie is dead on the server side too.
	hn.wantRedirect(hn.get("/", withCookie(c)), PathSignIn)
	// Signing out with no session is harmless.
	hn.wantRedirect(hn.post(PathSignOut, nil), PathSignedOut)
}

func TestIdleSessionExpires(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	c := sessionCookie(hn.post(PathSignIn, signInForm("alice", testPassword)))

	hn.clock.Advance(6 * 24 * time.Hour)
	if rec := hn.get("/", withCookie(c)); rec.Code != http.StatusOK {
		t.Fatalf("after 6 days idle: %d", rec.Code)
	}
	// That request refreshed the session, so another 6 days is still fine.
	hn.clock.Advance(6 * 24 * time.Hour)
	if rec := hn.get("/", withCookie(c)); rec.Code != http.StatusOK {
		t.Fatalf("activity did not extend the session: %d", rec.Code)
	}
	hn.clock.Advance(7*24*time.Hour + time.Minute)
	hn.wantRedirect(hn.get("/", withCookie(c)), PathSignIn)
}

func TestSignInRehashesOldHash(t *testing.T) {
	hn := newHarness(t, nil)
	salt := []byte("0123456789abcdef")
	key := argon2.IDKey([]byte(testPassword), salt, 1, 8, 1, 32)
	b64 := base64.RawStdEncoding
	weak := fmt.Sprintf("$argon2id$v=%d$m=8,t=1,p=1$%s$%s", argon2.Version, b64.EncodeToString(salt), b64.EncodeToString(key))
	u, err := hn.store.CreateUser(context.Background(), "alice", weak, hn.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	hn.wantRedirect(hn.post(PathSignIn, signInForm("alice", testPassword)), PathHome)
	got, _, _ := hn.store.UserByID(context.Background(), u.ID)
	if got.PasswordHash == weak {
		t.Fatal("the weak hash was not replaced")
	}
	if ok, again, err := auth.Verify(testPassword, got.PasswordHash); !ok || again || err != nil {
		t.Fatalf("new hash: ok=%v needsRehash=%v err=%v", ok, again, err)
	}
}

func TestSecureFlagFollowsPublicURL(t *testing.T) {
	for _, c := range []struct {
		publicURL string
		secure    bool
	}{
		{"https://foreman.example.com", true},
		{"http://foreman.example.com", false},
		{"", false},
	} {
		env := map[string]string{}
		if c.publicURL != "" {
			env["FOREMAN_PUBLIC_URL"] = c.publicURL
		}
		hn := newHarness(t, env)
		hn.addUser("alice", testPassword)
		cookie := sessionCookie(hn.post(PathSignIn, signInForm("alice", testPassword)))
		if cookie == nil || cookie.Secure != c.secure {
			t.Errorf("public URL %q: cookie %+v, want Secure=%v", c.publicURL, cookie, c.secure)
		}
	}
}

func TestCrossOriginPostsAreRefused(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	cross := withHeader("Sec-Fetch-Site", "cross-site")

	rec := hn.post(PathSignIn, signInForm("alice", testPassword), cross)
	if rec.Code != http.StatusForbidden || sessionCookie(rec) != nil {
		t.Fatalf("cross-site sign-in: %d", rec.Code)
	}
	c := sessionCookie(hn.post(PathSignIn, signInForm("alice", testPassword)))
	if rec := hn.post(PathSignOut, nil, withCookie(c), cross); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site sign-out: %d", rec.Code)
	}
	if rec := hn.get("/", withCookie(c)); rec.Code != http.StatusOK {
		t.Error("the refused sign-out still ended the session")
	}
	// A foreign Origin is refused too, and the same-origin form works.
	r := withHeader("Origin", "https://evil.example")
	if rec := hn.post(PathSignIn, signInForm("alice", testPassword), r); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", rec.Code)
	}
	same := withHeader("Sec-Fetch-Site", "same-origin")
	hn.wantRedirect(hn.post(PathSignIn, signInForm("alice", testPassword), same), PathHome)
}

func TestSweepOnceRemovesIdleSessions(t *testing.T) {
	hn := newHarness(t, nil)
	u := hn.addUser("alice", testPassword)
	ctx := context.Background()
	old := hn.clock.Now().Add(-48 * time.Hour)
	if err := hn.store.CreateSession(ctx, "old", u.ID, old); err != nil {
		t.Fatal(err)
	}
	if err := hn.store.CreateSession(ctx, "fresh", u.ID, hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	sweepOnce(ctx, hn.store, 24*time.Hour, hn.clock.Now(), hn.slog)
	if _, _, found, _ := hn.store.SessionUser(ctx, "old"); found {
		t.Error("the idle session survived the sweep")
	}
	if _, _, found, _ := hn.store.SessionUser(ctx, "fresh"); !found {
		t.Error("the sweep removed a live session")
	}
	if !strings.Contains(hn.logs.String(), `"count":1`) {
		t.Errorf("sweep did not log its count: %s", hn.logs.String())
	}
}
