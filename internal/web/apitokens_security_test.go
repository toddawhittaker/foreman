package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/store"
)

// SPEC 12.10: only the owner renews; another user's renew is not found and
// changes nothing.
func TestAPITokenRenewOwnerOnly(t *testing.T) {
	hn := newHarness(t, nil)
	alice := hn.signIn()
	hn.createToken(alice, "Claude", auth.ScopeRead)
	tok := hn.tokenNamed("Claude")
	hn.addUser("bob", testPassword)
	bob := sessionCookie(hn.post(PathSignIn, signInForm("bob", testPassword)))
	if rec := hn.post(tokenPath(tok.ID, "renew"), url.Values{}, withCookie(bob)); rec.Code != http.StatusNotFound {
		t.Errorf("bob renewing alice's token: %d", rec.Code)
	}
	if len(hn.audits(AuditTokenRenew)) != 0 {
		t.Error("a refused renew was audited")
	}
	// bob's page never offers Renew on alice's token, but does offer Revoke (admin).
	body := hn.get(PathAPITokens, withCookie(bob)).Body.String()
	if strings.Contains(body, tokenPath(tok.ID, "renew")) || !strings.Contains(body, tokenPath(tok.ID, "revoke")) {
		t.Error("bob's page offers renew on alice's token, or no revoke")
	}
}

// SPEC 12.10: any admin revokes any token.
func TestAPITokenAdminRevokesOthers(t *testing.T) {
	hn := newHarness(t, nil)
	alice := hn.signIn()
	hn.createToken(alice, "Claude", auth.ScopeRead)
	tok := hn.tokenNamed("Claude")
	hn.addUser("bob", testPassword)
	bob := sessionCookie(hn.post(PathSignIn, signInForm("bob", testPassword)))
	hn.wantRedirect(hn.post(tokenPath(tok.ID, "revoke"), url.Values{}, withCookie(bob)), doneURL(tokenDoneRevoked, "Claude"))
	if got := hn.audits(AuditTokenRevoke); len(got) != 1 || got[0] != "bob  token=Claude owner=alice scope=read" {
		t.Errorf("audit %q", got)
	}
}

// The revoke rule lives in the handler, not auth.Revoke: a user who is
// neither the owner nor an admin (a future viewer role) is refused.
func TestMayRevoke(t *testing.T) {
	tok := store.APIToken{UserID: 1}
	cases := []struct {
		u    store.User
		want bool
	}{
		{store.User{ID: 1, Role: "viewer"}, true},
		{store.User{ID: 2, Role: roleAdmin}, true},
		{store.User{ID: 2, Role: "viewer"}, false},
		{store.User{ID: 2}, false},
	}
	for _, c := range cases {
		if got := mayRevoke(c.u, tok); got != c.want {
			t.Errorf("user %d role %q: %v, want %v", c.u.ID, c.u.Role, got, c.want)
		}
	}
}

// SPEC 12.1, 14.7: every route needs a signed-in user, and a cross-site
// post is refused.
func TestAPITokenRoutesNeedSignInAndSameOrigin(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.createToken(c, "Claude", auth.ScopeRead)
	tok := hn.tokenNamed("Claude")
	posts := []string{PathAPITokens, pathAPITokenMCP, tokenPath(tok.ID, "renew"), tokenPath(tok.ID, "revoke")}
	for _, p := range posts {
		if rec := hn.post(p, url.Values{"name": {"x"}, "scope": {"read"}, "mcp": {"on"}}); rec.Code == http.StatusOK || rec.Header().Get("Location") != "" && !strings.HasPrefix(rec.Header().Get("Location"), PathSignIn) {
			t.Errorf("signed-out POST %s: %d to %q", p, rec.Code, rec.Header().Get("Location"))
		}
		rec := hn.post(p, url.Values{"name": {"y"}, "scope": {"read"}, "mcp": {"on"}}, withCookie(c), withHeader("Origin", "https://evil.example"), withHeader("Sec-Fetch-Site", "cross-site"))
		if rec.Code != http.StatusForbidden {
			t.Errorf("cross-site POST %s: %d", p, rec.Code)
		}
	}
	for _, p := range []string{PathAPITokens, tokenPath(tok.ID, "revoke")} {
		if rec := hn.get(p); rec.Code == http.StatusOK {
			t.Errorf("signed-out GET %s: 200", p)
		}
	}
	if len(hn.audits(AuditTokenRevoke))+len(hn.audits(AuditMCPSet))+len(hn.audits(AuditTokenRenew)) != 0 {
		t.Error("a refused post changed something")
	}
	if ts, _ := hn.store.ListAPITokens(t.Context()); len(ts) != 1 {
		t.Errorf("%d tokens, want 1", len(ts))
	}
}

// SPEC 12.10: a token cannot sign in to the UI; pages never read the
// Authorization header, so a token cannot renew itself.
func TestAPITokenCannotUseThePages(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	secret, _ := hn.createToken(c, "Claude", auth.ScopeRead)
	tok := hn.tokenNamed("Claude")
	bearer := withHeader("Authorization", "Bearer "+secret)
	if rec := hn.get(PathAPITokens, bearer); rec.Code == http.StatusOK {
		t.Error("a bearer token opened the tokens page")
	}
	if rec := hn.post(tokenPath(tok.ID, "renew"), url.Values{}, bearer); rec.Code == http.StatusSeeOther && !strings.HasPrefix(rec.Header().Get("Location"), PathSignIn) {
		t.Error("a bearer token renewed itself")
	}
	if len(hn.audits(AuditTokenRenew)) != 0 {
		t.Error("a bearer token renewed itself")
	}
}
