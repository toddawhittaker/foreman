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

// fakeNets stands in for the game networks the controller reports.
type fakeNets struct {
	subnets []netip.Prefix
	known   bool
}

func (f fakeNets) GameSubnets() ([]netip.Prefix, bool) { return f.subnets, f.known }

func gameNets(subnets ...string) fakeNets {
	f := fakeNets{known: true}
	for _, s := range subnets {
		f.subnets = append(f.subnets, netip.MustParsePrefix(s))
	}
	return f
}

func headerHarnessWithGame(t *testing.T, methods string, nets fakeNets) *harness {
	env := map[string]string{"FOREMAN_AUTH_METHODS": methods, "FOREMAN_AUTH_REQUIRED_GROUP": testGroup}
	for k, v := range headerEnv {
		env[k] = v
	}
	env["FOREMAN_TRUSTED_PROXIES"] = "10.0.0.0/8,fd00::/16"
	return newHarnessNets(t, env, func(data string) world.Dirs {
		return world.Dirs{Worlds: filepath.Join(data, "worlds"), Snapshots: filepath.Join(data, "snapshots"), Imports: filepath.Join(data, "import")}
	}, nil, nets)
}

func asPeer(peer string) []func(*http.Request) {
	return []func(*http.Request){withPeer(peer), withHeader("X-Authentik-Username", "alice"), withHeader("X-Authentik-Groups", testGroup)}
}

// SPEC 4.4, 12.6: a game is never taken for the proxy. With NET_RAW it can
// take any free address on its network, so every peer in a game network's
// range is refused, even inside FOREMAN_TRUSTED_PROXIES, and every peer is
// refused while the ranges are not known.
func TestHeadersFromAGameNetworkAreIgnored(t *testing.T) {
	t.Run("an address in the game range that is not the game's own", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "headers", gameNets("10.0.1.0/24"))
		rec := hn.get(PathWorlds, asPeer("10.0.1.200:4000")...)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "through the proxy") {
			t.Fatalf("from the game range: %d", rec.Code)
		}
		if _, ok, _ := hn.store.UserByUsername(context.Background(), "alice"); ok {
			t.Error("a header from the game range created a user")
		}
		hn.get(PathWorlds, asPeer("10.0.1.9:4001")...)
		if n := strings.Count(hn.logs.String(), "refused trusted-header sign-in"); n != 1 {
			t.Errorf("the refusal was logged %d times, want once: %s", n, hn.logs.String())
		}
		if strings.Contains(hn.logs.String(), "alice") {
			t.Error("the log holds a header value")
		}
	})
	t.Run("a trusted proxy outside the game ranges signs in", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "headers", gameNets("10.0.1.0/24", "fd00:1::/64"))
		if rec := hn.get(PathWorlds, asPeer(peerProxy)...); rec.Code != http.StatusOK {
			t.Fatalf("from the proxy: %d", rec.Code)
		}
	})
	t.Run("an IPv4-mapped peer is in the range", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "headers", gameNets("10.0.1.0/24"))
		if rec := hn.get(PathWorlds, asPeer("[::ffff:10.0.1.9]:4000")...); rec.Code != http.StatusForbidden {
			t.Fatalf("mapped peer: %d", rec.Code)
		}
	})
	t.Run("an IPv6 game range", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "headers", gameNets("10.0.1.0/24", "fd00:1::/64"))
		if rec := hn.get(PathWorlds, asPeer("[fd00:1::abcd]:4000")...); rec.Code != http.StatusForbidden {
			t.Fatalf("IPv6 peer in the game range: %d", rec.Code)
		}
		if rec := hn.get(PathWorlds, asPeer("[fd00:2::5]:4000")...); rec.Code != http.StatusOK {
			t.Fatalf("IPv6 proxy outside the game range: %d", rec.Code)
		}
	})
	t.Run("unknown game networks refuse every header sign-in", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "headers", fakeNets{})
		if rec := hn.get(PathWorlds, asPeer(peerProxy)...); rec.Code != http.StatusForbidden {
			t.Fatalf("from the proxy while unknown: %d", rec.Code)
		}
		hn.get(PathWorlds, asPeer(peerProxy)...)
		if n := strings.Count(hn.logs.String(), "game networks are not yet known"); n != 1 {
			t.Errorf("logged %d times, want once", n)
		}
	})
	t.Run("the refusal page says why while the networks are unknown", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "headers", fakeNets{})
		body := hn.get(PathWorlds, asPeer(peerProxy)...).Body.String()
		if !strings.Contains(body, "not yet seen the game container") || !strings.Contains(body, "cap_drop: [NET_RAW]") {
			t.Errorf("headers only: the page does not say why: %s", body)
		}
		if strings.Contains(body, "Password sign-in") || strings.Contains(body, "alice") {
			t.Error("headers only: the page offers password sign-in or shows a header value")
		}
		hn = headerHarnessWithGame(t, "local,headers", fakeNets{})
		hn.addUser("bob", testPassword)
		rec := hn.get(PathWorlds, asPeer(peerProxy)...)
		body = rec.Body.String()
		if rec.Code != http.StatusForbidden || !strings.Contains(body, "not yet seen the game container") || !strings.Contains(body, "Password sign-in still works") {
			t.Errorf("local and headers: %d %s", rec.Code, body)
		}
		if strings.Contains(body, "alice") {
			t.Error("the page shows a header value")
		}
		// With no proxy headers, a visitor is sent to sign in as usual.
		hn.wantRedirect(hn.get(PathWorlds, withPeer(peerProxy)), PathSignIn)
	})
	t.Run("password sign-in and sessions still work", func(t *testing.T) {
		hn := headerHarnessWithGame(t, "local,headers", fakeNets{})
		hn.addUser("alice", testPassword)
		rec := hn.post(PathSignIn, signInForm("alice", testPassword), withPeer("10.0.1.9:4000"))
		c := sessionCookie(rec)
		if c == nil {
			t.Fatalf("password sign-in from the game range: %d", rec.Code)
		}
		if rec := hn.get(PathWorlds, withPeer("10.0.1.9:4000"), withCookie(c)); rec.Code != http.StatusOK {
			t.Fatalf("session while the networks are unknown: %d", rec.Code)
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
