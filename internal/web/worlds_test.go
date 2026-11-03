package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func (hn *harness) signIn() *http.Cookie {
	hn.t.Helper()
	hn.addUser("alice", testPassword)
	c := sessionCookie(hn.post(PathSignIn, signInForm("alice", testPassword)))
	if c == nil {
		hn.t.Fatal("sign-in set no cookie")
	}
	return c
}

func TestRootRedirectsToWorlds(t *testing.T) {
	hn := newHarness(t, nil)
	hn.wantRedirect(hn.get("/"), PathWorlds)
}

func TestWorldListNeedsSignIn(t *testing.T) {
	hn := newHarness(t, nil)
	hn.wantRedirect(hn.get(PathWorlds), PathSetup)
}

func TestWorldListEmptyAndFilled(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()

	body := hn.get(PathWorlds, withCookie(c)).Body.String()
	if !strings.Contains(body, "No worlds yet") {
		t.Errorf("empty state missing:\n%s", body)
	}

	ctx := context.Background()
	if _, err := hn.worlds.Create(ctx, "Zeta", "2.0.72", true); err != nil {
		t.Fatal(err)
	}
	if _, err := hn.worlds.Create(ctx, "Alpha <b>", "1.1.110", false); err != nil {
		t.Fatal(err)
	}
	rec := hn.get(PathWorlds, withCookie(c))
	body = rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	// The first world made is active and listed first (SPEC 5.4).
	z, a := strings.Index(body, "Zeta"), strings.Index(body, "Alpha &lt;b&gt;")
	if z < 0 || a < 0 || z > a {
		t.Errorf("want Zeta (active) before Alpha: %d, %d", z, a)
	}
	if strings.Count(body, `class="badge badge--active"`) != 1 {
		t.Error("want exactly one Active badge")
	}
	if strings.Contains(body, "No worlds yet") {
		t.Error("empty state shown with worlds")
	}
}
