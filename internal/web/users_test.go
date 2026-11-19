package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/store"
)

const newPassword = "a brand new passphrase"

// SPEC 12.12: the tab lists every user with how they sign in and when
// they were created; no Delete for yourself; no hash on the page.
func TestUsersTabListsEveryUser(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	ctx := context.Background()
	if _, err := hn.store.CreateUser(ctx, "bob", "", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	carol, err := hn.store.CreateOIDCUser(ctx, "carol", "https://id.example.com/", "sub-c", hn.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	rec := hn.get(usersPath(), withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{"alice", "bob", "carol", "Password", "Proxy", "Single sign-on", userDeletePath(carol.ID), `id="password-current"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	alice, _, _ := hn.store.UserByUsername(ctx, "alice")
	if strings.Contains(body, userDeletePath(alice.ID)) {
		t.Error("the page offers to delete yourself")
	}
	if strings.Contains(body, "$argon2id$") {
		t.Error("the page shows a password hash")
	}
}

// SPEC 12.12: deleting yourself is refused, so the last user always stays.
func TestUserDeleteSelfRefused(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	alice, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	if rec := hn.get(userDeletePath(alice.ID), withCookie(c)); rec.Code != http.StatusForbidden {
		t.Errorf("ask: %d", rec.Code)
	}
	if rec := hn.post(userDeletePath(alice.ID), url.Values{}, withCookie(c)); rec.Code != http.StatusForbidden {
		t.Errorf("delete: %d", rec.Code)
	}
	if _, ok, _ := hn.store.UserByID(context.Background(), alice.ID); !ok {
		t.Error("alice was deleted")
	}
	if len(hn.audits(AuditUserDelete)) != 0 {
		t.Error("a refused delete was recorded")
	}
}

// SPEC 12.12, 12.9, 12.10: the confirm page names the user and their token
// count; the delete is recorded first, ends their sessions, and their
// token then gets 401 at /mcp. Their audit rows keep the name.
func TestUserDeleteEndsSessionsAndTokens(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	ctx := context.Background()
	bob := hn.addUser("bob", testPassword)
	bobCookie := hn.signInAgain("bob")
	turnMCPOn(t, hn)
	secret, _ := apiToken(t, hn, bob, "Claude", auth.ScopeRead)
	apiToken(t, hn, bob, "Other", auth.ScopeRead)
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusOK {
		t.Fatalf("bob's token before the delete: %d", rec.Code)
	}
	if err := hn.store.AddAudit(ctx, store.AuditEntry{Time: hn.clock.Now(), UserID: bob.ID, Username: "bob", Action: AuditTokenCreate, Detail: "x"}); err != nil {
		t.Fatal(err)
	}

	ask := hn.get(userDeletePath(bob.ID), withCookie(c)).Body.String()
	for _, want := range []string{"Delete user bob?", "Delete their 2 API tokens.", `id="act-user-delete"`} {
		if !strings.Contains(ask, want) {
			t.Errorf("the confirm page lacks %q", want)
		}
	}
	if strings.Contains(ask, `id="user-delete-returns"`) {
		t.Error("a password user is warned they come back")
	}

	rec := hn.post(userDeletePath(bob.ID), url.Values{}, withCookie(c))
	hn.wantRedirect(rec, usersPath()+"?done=deleted&name=bob")
	if _, ok, _ := hn.store.UserByID(ctx, bob.ID); ok {
		t.Fatal("bob survived")
	}
	if got := strings.Join(hn.audits(AuditUserDelete), "|"); got != "alice  bob; API tokens: 2" {
		t.Errorf("audit %q", got)
	}
	if got := hn.audits(AuditTokenCreate); len(got) != 1 || !strings.HasPrefix(got[0], "bob ") {
		t.Errorf("bob's own audit rows: %v", got)
	}
	if rec := hn.get(usersPath(), withCookie(bobCookie)); rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), PathSignIn) {
		t.Errorf("bob's session still works: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusUnauthorized {
		t.Errorf("bob's token after the delete: %d", rec.Code)
	}
	if !strings.Contains(hn.get(usersPath()+"?done=deleted&name=bob", withCookie(c)).Body.String(), "User bob is deleted.") {
		t.Error("no done message")
	}
}

// SPEC 12.12, ruling 17: a proxy or OIDC user is warned to come back.
func TestUserDeleteWarnsProxyAndOIDCUsersReturn(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	ctx := context.Background()
	bob, _ := hn.store.CreateUser(ctx, "bob", "", hn.clock.Now())
	carol, _ := hn.store.CreateOIDCUser(ctx, "carol", "https://id.example.com/", "s", hn.clock.Now())
	if body := hn.get(userDeletePath(bob.ID), withCookie(c)).Body.String(); !strings.Contains(body, "remove them at the proxy") {
		t.Error("no proxy warning")
	}
	if body := hn.get(userDeletePath(carol.ID), withCookie(c)).Body.String(); !strings.Contains(body, "remove them at the identity provider") {
		t.Error("no identity provider warning")
	}
}

// SPEC 12.9: user-delete is written before the delete, which is refused
// when the entry cannot be written.
func TestUserDeleteRefusedWithoutAudit(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	bob := hn.addUser("bob", testPassword)
	breakAuditLog(t)
	if rec := hn.post(userDeletePath(bob.ID), url.Values{}, withCookie(c)); rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d", rec.Code)
	}
	if _, ok, _ := hn.store.UserByID(context.Background(), bob.ID); !ok {
		t.Error("bob was deleted without an audit entry")
	}
}

func TestUserDeleteUnknownIsNotFound(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	for _, p := range []string{userDeletePath(99), usersPath() + "/x/delete"} {
		if rec := hn.get(p, withCookie(c)); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func passwordForm(current, next, confirm string) url.Values {
	return url.Values{"current": {current}, "password": {next}, "confirm": {confirm}}
}

// SPEC 12.12, 12.4: a change needs the current password and the new one
// twice, at least 12 characters; it ends the other sessions, keeps this
// browser signed in, and is recorded with no detail.
func TestPasswordChange(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	other := hn.signInAgain("alice")
	before := hn.passwordHash("alice")

	for name, f := range map[string]url.Values{
		"short":    passwordForm(testPassword, "short", "short"),
		"mismatch": passwordForm(testPassword, newPassword, newPassword+"x"),
		"missing":  passwordForm("", "", ""),
	} {
		if rec := hn.post(pathUserPassword, f, withCookie(c)); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	rec := hn.post(pathUserPassword, passwordForm("wrong password!", newPassword, newPassword), withCookie(c))
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "The current password is wrong.") {
		t.Errorf("wrong current: %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), newPassword) || strings.Contains(rec.Body.String(), "wrong password!") {
		t.Error("a refused page sent a password back")
	}
	if hn.passwordHash("alice") != before {
		t.Fatal("a refused change changed the password")
	}

	rec = hn.post(pathUserPassword, passwordForm(testPassword, newPassword, newPassword), withCookie(c))
	hn.wantRedirect(rec, usersPath()+"?done=password")
	fresh := sessionCookie(rec)
	if fresh == nil {
		t.Fatal("no fresh session for this browser")
	}
	if !strings.HasPrefix(hn.passwordHash("alice"), "$argon2id$") || hn.passwordHash("alice") == before {
		t.Error("the hash did not change to argon2id")
	}
	if code := hn.get(usersPath(), withCookie(fresh)).Code; code != http.StatusOK {
		t.Errorf("this browser was signed out: %d", code)
	}
	for name, ck := range map[string]*http.Cookie{"other": other, "old": c} {
		if code := hn.get(usersPath(), withCookie(ck)).Code; code != http.StatusSeeOther {
			t.Errorf("the %s session survived: %d", name, code)
		}
	}
	if got := strings.Join(hn.audits(AuditPasswordChange), "|"); got != "alice  " {
		t.Errorf("audit %q", got)
	}
	if sessionCookie(hn.post(PathSignIn, signInForm("alice", newPassword))) == nil {
		t.Error("the new password does not sign in")
	}
	if strings.Contains(hn.logs.String(), newPassword) || strings.Contains(hn.logs.String(), testPassword) {
		t.Error("the log holds a password")
	}
}

// SPEC 12.11: wrong current passwords count in the sign-in limiter.
func TestPasswordChangeRateLimited(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	limited := false
	for range 20 {
		rec := hn.post(pathUserPassword, passwordForm("wrong password!", newPassword, newPassword), withCookie(c))
		if rec.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("wrong current passwords were never limited")
	}
	if rec := hn.post(PathSignIn, signInForm("alice", testPassword)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("sign-in after the limit: %d", rec.Code)
	}
}

// SPEC 12.12: a user with no password gets no form and cannot post one.
func TestPasswordChangeRefusedWithoutPassword(t *testing.T) {
	hn := headerHarness(t, "headers", "")
	rec := hn.get(usersPath(), asUser("bob")...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, `id="password-current"`) || !strings.Contains(body, "managed there") {
		t.Error("a proxy user sees a password form")
	}
	if rec := hn.post(pathUserPassword, passwordForm("", newPassword, newPassword), asUser("bob")...); rec.Code != http.StatusForbidden {
		t.Errorf("post: %d", rec.Code)
	}
	if u, _, _ := hn.store.UserByUsername(context.Background(), "bob"); u.PasswordHash != "" {
		t.Error("a proxy user got a password")
	}
}
