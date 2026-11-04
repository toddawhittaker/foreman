package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

var headerEnv = map[string]string{
	"FOREMAN_AUTH_USER_HEADER":   "X-Authentik-Username",
	"FOREMAN_AUTH_GROUPS_HEADER": "X-Authentik-Groups",
	"FOREMAN_TRUSTED_PROXIES":    "10.0.0.0/8",
}

// testGroup is the required group of a harness that does not name its own.
const testGroup = "foreman"

func headerHarness(t *testing.T, methods, group string) *harness {
	if group == "" {
		group = testGroup
	}
	env := map[string]string{"FOREMAN_AUTH_METHODS": methods, "FOREMAN_AUTH_REQUIRED_GROUP": group}
	for k, v := range headerEnv {
		env[k] = v
	}
	return newHarness(t, env)
}

// asUser is a request from the trusted proxy for name, in the test group.
func asUser(name string) []func(*http.Request) {
	return []func(*http.Request){
		withPeer(peerProxy),
		withHeader("X-Authentik-Username", name),
		withHeader("X-Authentik-Groups", testGroup),
	}
}

func TestHeaderSignInFromTrustedPeerCreatesUsers(t *testing.T) {
	hn := headerHarness(t, "headers", "")
	asAlice := asUser("alice")
	rec := hn.get(PathWorlds, asAlice...)
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
	hn.get(PathWorlds, asAlice...)
	rec = hn.get(PathWorlds, asUser("bob")...)
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
		rec := hn.get(PathWorlds, spoof...)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "through the proxy") {
			t.Fatalf("got %d", rec.Code)
		}
		if _, ok, _ := hn.store.UserByUsername(context.Background(), "admin"); ok {
			t.Error("a spoofed header created a user")
		}
	})
	t.Run("with local sign-in", func(t *testing.T) {
		hn := headerHarness(t, "local,headers", "")
		hn.wantRedirect(hn.get(PathWorlds, spoof...), PathSetup)
		if has, _ := hn.store.HasUsers(context.Background()); has {
			t.Error("a spoofed header created a user")
		}
		hn.addUser("alice", testPassword)
		hn.wantRedirect(hn.get(PathWorlds, spoof...), PathSignIn)
	})
}

// fakeAddrs stands in for the adopted game container's addresses.
type fakeAddrs []netip.Addr

func (f fakeAddrs) GameAddresses() []netip.Addr { return f }

func headerHarnessWithGame(t *testing.T, methods string, addrs ...netip.Addr) *harness {
	env := map[string]string{"FOREMAN_AUTH_METHODS": methods, "FOREMAN_AUTH_REQUIRED_GROUP": testGroup}
	for k, v := range headerEnv {
		env[k] = v
	}
	return newHarnessAddrs(t, env, func(data string) world.Dirs {
		return world.Dirs{Worlds: filepath.Join(data, "worlds"), Snapshots: filepath.Join(data, "snapshots"), Imports: filepath.Join(data, "import")}
	}, nil, fakeAddrs(addrs))
}

// SPEC 12.6: the game container is never taken for the proxy, even when its
// address lies inside FOREMAN_TRUSTED_PROXIES.
func TestHeadersFromTheGameContainerAreIgnored(t *testing.T) {
	gameAddr := netip.MustParseAddr("10.0.0.9")
	asGame := func(peer string) []func(*http.Request) {
		return []func(*http.Request){withPeer(peer), withHeader("X-Authentik-Username", "alice"), withHeader("X-Authentik-Groups", testGroup)}
	}
	t.Run("headers only", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "headers", gameAddr)
		rec := hn.get(PathWorlds, asGame("10.0.0.9:4000")...)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "through the proxy") {
			t.Fatalf("from the game: %d", rec.Code)
		}
		if _, ok, _ := hn.store.UserByUsername(context.Background(), "alice"); ok {
			t.Error("a header from the game created a user")
		}
		hn.get(PathWorlds, asGame("10.0.0.9:4001")...)
		if n := strings.Count(hn.logs.String(), "refused trusted-header sign-in"); n != 1 {
			t.Errorf("the refusal was logged %d times, want once: %s", n, hn.logs.String())
		}
		if strings.Contains(hn.logs.String(), "alice") {
			t.Error("the log holds a header value")
		}
		// The same request from another trusted address signs in.
		if rec := hn.get(PathWorlds, asUser("alice")...); rec.Code != http.StatusOK {
			t.Fatalf("from the proxy: %d", rec.Code)
		}
	})
	t.Run("an IPv4-mapped peer is the same address", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "headers", gameAddr)
		if rec := hn.get(PathWorlds, asGame("[::ffff:10.0.0.9]:4000")...); rec.Code != http.StatusForbidden {
			t.Fatalf("mapped peer: %d", rec.Code)
		}
	})
	t.Run("local sign-in still works from the game", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "local,headers", gameAddr)
		hn.addUser("alice", testPassword)
		rec := hn.post(PathSignIn, signInForm("alice", testPassword), withPeer("10.0.0.9:4000"))
		if sessionCookie(rec) == nil {
			t.Fatalf("password sign-in from the game: %d", rec.Code)
		}
	})
}

func TestRequiredGroupRefusesWithForbidden(t *testing.T) {
	hn := headerHarness(t, "headers", "foreman-admins")
	as := func(groups string) *httptest.ResponseRecorder {
		return hn.get(PathWorlds, withPeer(peerProxy), withHeader("X-Authentik-Username", "alice"), withHeader("X-Authentik-Groups", groups))
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
	hn.get(PathWorlds, asUser("alice")...)
	rec := hn.post(PathSignIn, signInForm("alice", testPassword))
	if rec.Code != http.StatusUnauthorized || sessionCookie(rec) != nil {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestHeaderNameThatIsNotAUsernameIsRefused(t *testing.T) {
	hn := headerHarness(t, "headers", "")
	rec := hn.get(PathWorlds, asUser(strings.Repeat("a", 65))...)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d", rec.Code)
	}
}
