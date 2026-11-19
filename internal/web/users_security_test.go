package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/toddawhittaker/foreman/cmd/fakeidp/idp"
	"github.com/toddawhittaker/foreman/internal/auth"
)

// Trust-boundary tests for the Users tab (SPEC 12.8, 12.9, 12.11, 12.12)
// that users_test.go leaves out.

// SPEC 12.12: two users deleting each other at the same moment always
// leave one of them, so nobody can delete the last user.
func TestUserDeleteMutualLeavesOne(t *testing.T) {
	hn := newHarness(t, nil)
	hn.signIn()
	ctx := context.Background()
	for i := range 20 {
		a := hn.addUser(fmt.Sprintf("a%d", i), testPassword)
		b := hn.addUser(fmt.Sprintf("b%d", i), testPassword)
		ca, cb := hn.signInAgain(a.Username), hn.signInAgain(b.Username)
		var wg sync.WaitGroup
		wg.Go(func() { hn.post(userDeletePath(b.ID), url.Values{}, withCookie(ca)) })
		wg.Go(func() { hn.post(userDeletePath(a.ID), url.Values{}, withCookie(cb)) })
		wg.Wait()
		_, aLeft, _ := hn.store.UserByID(ctx, a.ID)
		_, bLeft, _ := hn.store.UserByID(ctx, b.ID)
		if !aLeft && !bLeft {
			t.Fatalf("round %d: both users deleted each other", i)
		}
	}
}

// SPEC 12.9: when user-delete cannot be recorded, nothing about the user
// changes: their session and their API token keep working.
func TestUserDeleteWithoutAuditKeepsSessionsAndTokens(t *testing.T) {
	hn := newHarness(t, nil)
	alice := hn.signIn()
	bob := hn.addUser("bob", testPassword)
	bobCookie := hn.signInAgain("bob")
	turnMCPOn(t, hn)
	secret, _ := apiToken(t, hn, bob, "Claude", auth.ScopeRead)
	breakAuditLog(t)
	if rec := hn.post(userDeletePath(bob.ID), url.Values{}, withCookie(alice)); rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete with a broken audit log: %d", rec.Code)
	}
	if rec := hn.get(usersPath(), withCookie(bobCookie)); rec.Code != http.StatusOK {
		t.Errorf("bob's session after a refused delete: %d", rec.Code)
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("bob's token after a refused delete: %d", rec.Code)
	}
}

// SPEC 12.8, 14.7: another site cannot make a signed-in browser delete a
// user or change its password.
func TestUserActionsRefuseCrossSiteRequests(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	bob := hn.addUser("bob", testPassword)
	before := hn.passwordHash("alice")
	for name, mod := range map[string]func(*http.Request){
		"cross-site":     withHeader("Sec-Fetch-Site", "cross-site"),
		"foreign Origin": withHeader("Origin", "https://evil.example"),
	} {
		if rec := hn.post(userDeletePath(bob.ID), url.Values{}, withCookie(c), mod); rec.Code != http.StatusForbidden {
			t.Errorf("%s delete: %d", name, rec.Code)
		}
		if rec := hn.post(pathUserPassword, passwordForm(testPassword, newPassword, newPassword), withCookie(c), mod); rec.Code != http.StatusForbidden {
			t.Errorf("%s password change: %d", name, rec.Code)
		}
	}
	if _, ok, _ := hn.store.UserByID(context.Background(), bob.ID); !ok {
		t.Error("a cross-site request deleted bob")
	}
	if hn.passwordHash("alice") != before {
		t.Error("a cross-site request changed the password")
	}
	if len(hn.audits(AuditUserDelete))+len(hn.audits(AuditPasswordChange)) != 0 {
		t.Error("a refused request was audited")
	}
}

// SPEC 12.11: once wrong current passwords hit the limit, even the right one
// is refused until the window passes, so the limit cannot be used to guess.
func TestPasswordChangeLimitRefusesTheRightPasswordToo(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	before := hn.passwordHash("alice")
	for i := 0; ; i++ {
		rec := hn.post(pathUserPassword, passwordForm("wrong password!", newPassword, newPassword), withCookie(c))
		if rec.Code == http.StatusTooManyRequests {
			break
		}
		if i > 2*auth.LimitPerAddress {
			t.Fatal("wrong current passwords were never limited")
		}
	}
	rec := hn.post(pathUserPassword, passwordForm(testPassword, newPassword, newPassword), withCookie(c))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("the right password over the limit: %d", rec.Code)
	}
	if hn.passwordHash("alice") != before {
		t.Error("a limited request changed the password")
	}
}

// SPEC 12.12: a single sign-on user has no password and cannot set one, so
// they cannot get past the identity provider.
func TestOIDCUserCannotSetAPassword(t *testing.T) {
	o := newOIDCHarness(t, "local,oidc", nil)
	session := sessionCookie(o.signIn(""))
	if session == nil {
		t.Fatal("no session")
	}
	page := o.get(usersPath(), withCookie(session)).Body.String()
	if strings.Contains(page, `id="password-current"`) {
		t.Error("a single sign-on user sees a password form")
	}
	for _, f := range []url.Values{passwordForm("", newPassword, newPassword), passwordForm("anything at all", newPassword, newPassword)} {
		if rec := o.post(pathUserPassword, f, withCookie(session)); rec.Code != http.StatusForbidden {
			t.Errorf("post: %d", rec.Code)
		}
	}
	if o.passwordHash(idp.Username) != "" {
		t.Error("a single sign-on user got a password")
	}
	if rec := o.post(PathSignIn, signInForm(idp.Username, newPassword)); sessionCookie(rec) != nil {
		t.Error("the password signs in")
	}
}

// SPEC 12.12 (ruling 17): a deleted proxy user is made again at their next
// request, as a new user. Their old API tokens stay gone and the new user
// is recorded as user-create.
func TestDeletedProxyUserComesBack(t *testing.T) {
	hn := headerHarness(t, "headers", "")
	turnMCPOn(t, hn)
	if rec := hn.get(usersPath(), asUser("alice")...); rec.Code != http.StatusOK {
		t.Fatalf("alice: %d", rec.Code)
	}
	if rec := hn.get(usersPath(), asUser("bob")...); rec.Code != http.StatusOK {
		t.Fatalf("bob: %d", rec.Code)
	}
	ctx := context.Background()
	bob, _, _ := hn.store.UserByUsername(ctx, "bob")
	secret, _ := apiToken(t, hn, bob, "Claude", auth.ScopeRead)
	hn.wantRedirect(hn.post(userDeletePath(bob.ID), url.Values{}, asUser("alice")...), usersPath()+"?done=deleted")

	if rec := hn.get(usersPath(), asUser("bob")...); rec.Code != http.StatusOK {
		t.Fatalf("bob after the delete: %d", rec.Code)
	}
	if _, found, _ := hn.store.UserByUsername(ctx, "bob"); !found {
		t.Fatal("bob did not come back")
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusUnauthorized {
		t.Errorf("the deleted bob's token: %d", rec.Code)
	}
	bobs := 0
	for _, e := range hn.audits(AuditUserCreate) {
		if e == "bob  headers" {
			bobs++
		}
	}
	if got := hn.audits(AuditUserCreate); len(got) != 3 || bobs != 2 {
		t.Errorf("user-create entries %q", got)
	}
}
