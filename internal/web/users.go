package web

// The Users tab: every user, deleting another user, and changing your own
// password (SPEC 12.12).

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
)

// usersTabOn shows the Users tab in the settings sub-navigation.
const usersTabOn = true

// pathUserPassword is where the password form posts.
const pathUserPassword = "/settings/users/password"

// Audit actions of SPEC 12.9.
const (
	AuditUserDelete     = "user-delete"
	AuditPasswordChange = "password-change"
)

// Done messages the page shows after a redirect.
const (
	userDoneDeleted  = "deleted"
	userDonePassword = "password"
)

// How a user signs in, as the tab names it.
const (
	SignInPassword = "password"
	SignInProxy    = "proxy"
	SignInOIDC     = "oidc"
)

// signInKind says how u signs in. A user with a password signs in with it;
// one linked to an identity provider uses single sign-on; any other was
// made by trusted headers.
func signInKind(u store.User) string {
	switch {
	case u.PasswordHash != "":
		return SignInPassword
	case u.OIDCSubject != "":
		return SignInOIDC
	}
	return SignInProxy
}

// UserRow is one user as the tab shows it: never the password hash.
type UserRow struct {
	ID       int64
	Username string
	Kind     string
	Created  time.Time
	// Me is the signed-in user, who cannot delete themselves.
	Me bool
}

func userRow(u store.User, me int64) UserRow {
	return UserRow{ID: u.ID, Username: u.Username, Kind: signInKind(u), Created: u.CreatedAt, Me: u.ID == me}
}

// KindWord is the sign-in kind in words.
func (r UserRow) KindWord() i18n.Msg {
	switch r.Kind {
	case SignInPassword:
		return i18n.M("users.kind.password")
	case SignInOIDC:
		return i18n.M("users.kind.oidc")
	}
	return i18n.M("users.kind.proxy")
}

// DeletePath is the user's delete confirm page.
func (r UserRow) DeletePath() string { return userDeletePath(r.ID) }

// PasswordForm is the change-password form. Passwords are never sent back.
type PasswordForm struct {
	CurrentError i18n.Msg
	NewError     i18n.Msg
	ConfirmError i18n.Msg
}

// UsersPage is the Users tab (SPEC 12.12).
type UsersPage struct {
	Users []UserRow
	// MyKind is how the signed-in user signs in; only "password" gets the form.
	MyKind string
	Form   PasswordForm
	// Error is a refusal of the whole form, such as too many tries.
	Error     i18n.Msg
	Done      string
	MinLength int
}

func (UsersPage) templateName() string { return "users" }
func (UsersPage) navItem() string      { return PathSchedule }

func (p UsersPage) hasErrors() bool { return !p.Error.IsZero() || len(p.Problems()) > 0 }

// Problems lists the form's errors.
func (p UsersPage) Problems() []Problem {
	return problems(
		Problem{"password-current", p.Form.CurrentError},
		Problem{"password-new", p.Form.NewError},
		Problem{"password-confirm", p.Form.ConfirmError},
	)
}

// PasswordPath is where the password form posts.
func (UsersPage) PasswordPath() string { return pathUserPassword }

// UserDeletePage asks before a user is deleted.
type UserDeletePage struct {
	User   UserRow
	Tokens int
}

func (UserDeletePage) templateName() string { return "userdelete" }
func (UserDeletePage) navItem() string      { return PathSchedule }

// BackPath is the Users tab.
func (UserDeletePage) BackPath() string { return usersPath() }

func (s *server) usersRoutes() {
	s.mux.HandleFunc("GET "+usersPath(), s.requireUser(s.handleUsers))
	s.mux.HandleFunc("POST "+pathUserPassword, s.requireUser(s.handlePasswordChange))
	s.mux.HandleFunc("GET "+usersPath()+"/{id}/delete", s.requireUser(s.handleUserDeleteAsk))
	s.mux.HandleFunc("POST "+usersPath()+"/{id}/delete", s.requireUser(s.handleUserDelete))
}

func (s *server) usersPage(r *http.Request, me store.User) (UsersPage, error) {
	all, err := s.store.ListUsers(r.Context())
	if err != nil {
		return UsersPage{}, err
	}
	p := UsersPage{MyKind: signInKind(me), MinLength: auth.MinPasswordChars}
	for _, u := range all {
		p.Users = append(p.Users, userRow(u, me.ID))
	}
	return p, nil
}

func (s *server) handleUsers(w http.ResponseWriter, r *http.Request, u store.User) {
	p, err := s.usersPage(r, u)
	if err != nil {
		s.fail(w, r, "list users", err)
		return
	}
	q := r.URL.Query()
	switch d := q.Get("done"); d {
	case userDoneDeleted, userDonePassword:
		p.Done = d
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// targetUser reads the user the path names, or answers the request itself.
// Deleting yourself is refused: another user must do it, so the last user
// is never deleted (SPEC 12.12).
func (s *server) targetUser(w http.ResponseWriter, r *http.Request, u store.User) (store.User, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.notFound(w, r, u)
		return store.User{}, false
	}
	t, found, err := s.store.UserByID(r.Context(), id)
	if err != nil {
		s.fail(w, r, "look up user", err)
		return store.User{}, false
	}
	if !found {
		s.notFound(w, r, u)
		return store.User{}, false
	}
	if t.ID == u.ID {
		s.page(w, r, http.StatusForbidden, u.Username, ErrorPage{Status: http.StatusForbidden, Detail: i18n.M("users.delete.self")})
		return store.User{}, false
	}
	return t, true
}

func (s *server) handleUserDeleteAsk(w http.ResponseWriter, r *http.Request, u store.User) {
	t, ok := s.targetUser(w, r, u)
	if !ok {
		return
	}
	tokens, err := s.store.APITokensOf(r.Context(), t.ID)
	if err != nil {
		s.fail(w, r, "list a user's API tokens", err)
		return
	}
	s.page(w, r, http.StatusOK, u.Username, UserDeletePage{User: userRow(t, u.ID), Tokens: len(tokens)})
}

// handleUserDelete deletes another user. user-delete is recorded first, and
// the delete is refused when the record cannot be written (SPEC 12.9).
// Sessions and API tokens go with the user by cascade.
func (s *server) handleUserDelete(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	t, ok := s.targetUser(w, r, u)
	if !ok {
		return
	}
	ctx := r.Context()
	tokens, err := s.store.APITokensOf(ctx, t.ID)
	if err != nil {
		s.fail(w, r, "list a user's API tokens", err)
		return
	}
	err = s.store.AddAudit(ctx, store.AuditEntry{
		Time: s.now(), UserID: u.ID, Username: u.Username,
		Action: AuditUserDelete, Detail: userDeleteDetail(t.Username, len(tokens)),
	})
	if err != nil {
		s.fail(w, r, "record a user delete in the audit log", err)
		return
	}
	found, err := s.store.DeleteUser(ctx, t.ID, u.ID)
	if err != nil {
		s.fail(w, r, "delete user", err)
		return
	}
	if !found {
		s.notFound(w, r, u)
		return
	}
	s.log.Info("user deleted", "username", clip(t.Username), "by", clip(u.Username))
	http.Redirect(w, r, usersPath()+"?done="+userDoneDeleted, http.StatusSeeOther)
}

// userDeleteDetail is user-delete's detail: the name and how many API
// tokens go with the user. The record is written before the delete, which
// can still find nothing, so it says the delete was requested, as
// world-delete does.
func userDeleteDetail(name string, tokens int) string {
	return fmt.Sprintf("%s: requested; API tokens: %d", name, tokens)
}

// handlePasswordChange changes the signed-in user's own password. Only a
// user with a password has one to change: a password would let a proxy
// or single sign-on user past the proxy or identity provider. A wrong
// current password counts in the sign-in limiter (SPEC 12.11). The change
// ends every session of the user and starts a fresh one for this browser.
func (s *server) handlePasswordChange(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	if signInKind(u) != SignInPassword {
		s.page(w, r, http.StatusForbidden, u.Username, ErrorPage{Status: http.StatusForbidden, Detail: i18n.M("users.password.managed")})
		return
	}
	ctx := r.Context()
	current, next, confirm := r.PostFormValue("current"), r.PostFormValue("password"), r.PostFormValue("confirm")
	var f PasswordForm
	if current == "" {
		f.CurrentError = i18n.M("users.password.current.missing")
	}
	switch {
	case next == "":
		f.NewError = i18n.M("users.password.new.missing")
	case len(next) > auth.MaxPasswordBytes:
		f.NewError = i18n.M("users.password.new.long", "count", auth.MaxPasswordBytes)
	case auth.CheckPassword(next) != nil:
		f.NewError = i18n.M("users.password.new.short", "count", auth.MinPasswordChars)
	}
	if confirm == "" {
		f.ConfirmError = i18n.M("users.password.confirm.missing")
	} else if confirm != next {
		f.ConfirmError = i18n.M("users.password.confirm.mismatch")
	}
	refuse := func(status int, f PasswordForm, whole i18n.Msg) {
		p, err := s.usersPage(r, u)
		if err != nil {
			s.fail(w, r, "list users", err)
			return
		}
		p.Form, p.Error = f, whole
		s.page(w, r, status, u.Username, p)
	}
	if !f.CurrentError.IsZero() || !f.NewError.IsZero() || !f.ConfirmError.IsZero() {
		refuse(http.StatusBadRequest, f, i18n.Msg{})
		return
	}

	client := auth.ClientAddr(r, s.cfg.TrustedProxies)
	if ok, wait := s.limiter.Allow(u.Username, client); !ok {
		s.log.Info("password change refused: too many failures", "client", client)
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		refuse(http.StatusTooManyRequests, PasswordForm{}, i18n.M("users.password.limited"))
		return
	}
	ok, _, err := auth.Verify(current, u.PasswordHash)
	if errors.Is(err, auth.ErrBusy) {
		s.limiter.Release(u.Username, client)
		w.Header().Set("Retry-After", "1")
		refuse(http.StatusServiceUnavailable, PasswordForm{}, i18n.M("users.password.busy"))
		return
	}
	if err != nil {
		s.log.Error("stored password hash is unusable", "username", clip(u.Username), "error", err)
	}
	if err != nil || !ok {
		s.log.Info("password change: wrong current password", "client", client)
		refuse(http.StatusUnauthorized, PasswordForm{CurrentError: i18n.M("users.password.current.wrong")}, i18n.Msg{})
		return
	}
	s.limiter.Succeed(u.Username, client)

	hash, err := auth.Hash(next)
	if err != nil {
		s.fail(w, r, "hash password", err)
		return
	}
	_, hadSession, err := s.sessions.Check(ctx, r)
	if err != nil {
		s.fail(w, r, "check the session", err)
		return
	}
	if err := s.store.SetPasswordEndSessions(ctx, u.ID, hash); err != nil {
		s.fail(w, r, "set password", err)
		return
	}
	if hadSession {
		err = s.sessions.Start(ctx, w, u.ID)
	}
	if err != nil {
		s.fail(w, r, "start a session after a password change", err)
		return
	}
	s.audit(ctx, actor(u), AuditPasswordChange, "", "")
	http.Redirect(w, r, usersPath()+"?done="+userDonePassword, http.StatusSeeOther)
}
