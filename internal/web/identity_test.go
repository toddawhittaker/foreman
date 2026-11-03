package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var headerEnv = map[string]string{
	"FOREMAN_AUTH_USER_HEADER":   "X-Authentik-Username",
	"FOREMAN_AUTH_GROUPS_HEADER": "X-Authentik-Groups",
	"FOREMAN_TRUSTED_PROXIES":    "10.0.0.0/8",
}

func headerHarness(t *testing.T, methods, group string) *harness {
	env := map[string]string{"FOREMAN_AUTH_METHODS": methods}
	for k, v := range headerEnv {
		env[k] = v
	}
	if group != "" {
		env["FOREMAN_AUTH_REQUIRED_GROUP"] = group
	}
	return newHarness(t, env)
}

func TestHeaderSignInFromTrustedPeerCreatesUsers(t *testing.T) {
	hn := headerHarness(t, "headers", "")
	asAlice := []func(*http.Request){withPeer(peerProxy), withHeader("X-Authentik-Username", "alice")}
	rec := hn.get("/", asAlice...)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "alice") {
		t.Fatalf("first request: %d", rec.Code)
	}
	if sessionCookie(rec) != nil {
		t.Error("a header user got a session cookie")
	}
	ctx := context.Background()
	alice, ok, _ := hn.store.UserByUsername(ctx, "alice")
	if !ok || alice.PasswordHash != "" || alice.Role != "admin" {
		t.Fatalf("alice: %+v ok=%v", alice, ok)
	}
	// A second visit finds the same user; a second name makes a second user.
	hn.get("/", asAlice...)
	rec = hn.get("/", withPeer(peerProxy), withHeader("X-Authentik-Username", "bob"))
	if rec.Code != http.StatusOK {
		t.Fatalf("second user: %d", rec.Code)
	}
	again, _, _ := hn.store.UserByUsername(ctx, "alice")
	bob, ok, _ := hn.store.UserByUsername(ctx, "bob")
	if again.ID != alice.ID || !ok || bob.ID == alice.ID {
		t.Errorf("users: alice %d then %d, bob %d (found %v)", alice.ID, again.ID, bob.ID, ok)
	}
}

func TestSpoofedHeadersFromUntrustedPeerAreIgnored(t *testing.T) {
	spoof := []func(*http.Request){
		withPeer(peerUntrusted),
		withHeader("X-Authentik-Username", "admin"),
		withHeader("X-Forwarded-For", "10.0.0.5"),
	}
	t.Run("headers only", func(t *testing.T) {
		hn := headerHarness(t, "headers", "")
		rec := hn.get("/", spoof...)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "through the proxy") {
			t.Fatalf("got %d", rec.Code)
		}
		if _, ok, _ := hn.store.UserByUsername(context.Background(), "admin"); ok {
			t.Error("a spoofed header created a user")
		}
	})
	t.Run("with local sign-in", func(t *testing.T) {
		hn := headerHarness(t, "local,headers", "")
		hn.wantRedirect(hn.get("/", spoof...), PathSetup)
		if has, _ := hn.store.HasUsers(context.Background()); has {
			t.Error("a spoofed header created a user")
		}
		hn.addUser("alice", testPassword)
		hn.wantRedirect(hn.get("/", spoof...), PathSignIn)
	})
}

func TestRequiredGroupRefusesWithForbidden(t *testing.T) {
	hn := headerHarness(t, "headers", "foreman-admins")
	as := func(groups string) *httptest.ResponseRecorder {
		return hn.get("/", withPeer(peerProxy), withHeader("X-Authentik-Username", "alice"), withHeader("X-Authentik-Groups", groups))
	}
	if rec := as("staff, other"); rec.Code != http.StatusForbidden {
		t.Fatalf("outside the group: %d", rec.Code)
	}
	if _, ok, _ := hn.store.UserByUsername(context.Background(), "alice"); ok {
		t.Error("a refused user was created")
	}
	if rec := as("staff, foreman-admins"); rec.Code != http.StatusOK {
		t.Fatalf("inside the group: %d", rec.Code)
	}
}

func TestHeaderUserCannotSignInWithAPassword(t *testing.T) {
	hn := headerHarness(t, "local,headers", "")
	hn.get("/", withPeer(peerProxy), withHeader("X-Authentik-Username", "alice"))
	rec := hn.post(PathSignIn, signInForm("alice", testPassword))
	if rec.Code != http.StatusUnauthorized || sessionCookie(rec) != nil {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestHeaderNameThatIsNotAUsernameIsRefused(t *testing.T) {
	hn := headerHarness(t, "headers", "")
	rec := hn.get("/", withPeer(peerProxy), withHeader("X-Authentik-Username", strings.Repeat("a", 65)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d", rec.Code)
	}
}
