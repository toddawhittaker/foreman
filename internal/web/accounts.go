package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"git.thewhittakers.org/todd/foreman/internal/auth"
	"git.thewhittakers.org/todd/foreman/internal/store"
)

const (
	maxUsernameChars = 64
	// maxLoggedUsername keeps a hostile username from flooding the log.
	maxLoggedUsername = 64

	msgBadLogin = "The username or password is not right. Check both and try again."
	msgBusy     = "Foreman is busy. Wait a moment and try again."
)

// checkUsername trims name and returns a problem in plain words, or "".
func checkUsername(name string) (clean, problem string) {
	clean = strings.TrimSpace(name)
	switch {
	case clean == "":
		return clean, "Enter a username"
	case utf8.RuneCountInString(clean) > maxUsernameChars:
		return clean, fmt.Sprintf("Use at most %d characters", maxUsernameChars)
	case strings.IndexFunc(clean, unicode.IsControl) >= 0:
		return clean, "Remove control characters from the username"
	}
	return clean, ""
}

// readForm parses the body of a POST. It answers the request itself and
// returns false when the body is unreadable or over the size cap.
func (s *server) readForm(w http.ResponseWriter, r *http.Request) bool {
	err := r.ParseForm()
	if err == nil {
		return true
	}
	status := http.StatusBadRequest
	detail := "Foreman could not read the form. Go back, reload the page and try again."
	if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
		status = http.StatusRequestEntityTooLarge
		detail = "The form was too large. Go back, reload the page and try again."
	}
	s.page(w, r, status, "", ErrorPage{Status: status, Detail: detail})
	return false
}

func (s *server) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	has, err := s.store.HasUsers(r.Context())
	if err != nil {
		s.fail(w, r, "check for users", err)
		return
	}
	if has {
		http.Redirect(w, r, PathSignIn, http.StatusSeeOther)
		return
	}
	s.page(w, r, http.StatusOK, "", SetupPage{MinPasswordLength: auth.MinPasswordChars})
}

// handleSetup creates the first admin (SPEC 12.3). The store makes the
// create atomic, so two racing posts cannot both succeed.
func (s *server) handleSetup(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	has, err := s.store.HasUsers(ctx)
	if err != nil {
		s.fail(w, r, "check for users", err)
		return
	}
	if has {
		http.Redirect(w, r, PathSignIn, http.StatusSeeOther)
		return
	}
	if !s.readForm(w, r) {
		return
	}
	username, usernameProblem := checkUsername(r.PostFormValue("username"))
	password, confirm := r.PostFormValue("password"), r.PostFormValue("confirm")
	pg := SetupPage{Username: username, UsernameError: usernameProblem, MinPasswordLength: auth.MinPasswordChars}
	switch {
	case password == "":
		pg.PasswordError = "Enter a password"
	case len(password) > auth.MaxPasswordBytes:
		pg.PasswordError = fmt.Sprintf("Use at most %d bytes", auth.MaxPasswordBytes)
	case auth.CheckPassword(password) != nil:
		pg.PasswordError = fmt.Sprintf("Use at least %d characters", auth.MinPasswordChars)
	}
	if confirm == "" {
		pg.ConfirmError = "Enter the password again"
	} else if pg.PasswordError == "" && confirm != password {
		pg.ConfirmError = "Enter the same password in both fields"
	}
	if pg.UsernameError != "" || pg.PasswordError != "" || pg.ConfirmError != "" {
		s.page(w, r, http.StatusBadRequest, "", pg)
		return
	}

	hash, err := auth.Hash(password)
	if err != nil {
		s.fail(w, r, "hash password", err)
		return
	}
	u, err := s.store.CreateFirstAdmin(ctx, username, hash, s.now())
	if errors.Is(err, store.ErrAdminExists) {
		http.Redirect(w, r, PathSignIn, http.StatusSeeOther)
		return
	}
	if err != nil {
		s.fail(w, r, "create first admin", err)
		return
	}
	s.log.Info("first admin created", "username", clip(u.Username))
	if err := s.sessions.Start(ctx, w, u.ID); err != nil {
		s.fail(w, r, "start session", err)
		return
	}
	http.Redirect(w, r, PathHome, http.StatusSeeOther)
}

func (s *server) handleSignInForm(w http.ResponseWriter, r *http.Request) {
	s.page(w, r, http.StatusOK, "", SignInPage{})
}

// handleSignIn checks a local account (SPEC 12.4). It never says whether
// the username or the password was wrong.
func (s *server) handleSignIn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.readForm(w, r) {
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	client := auth.ClientAddr(r, s.cfg.TrustedProxies)

	pg := SignInPage{Username: username}
	if username == "" {
		pg.UsernameError = "Enter your username"
	}
	if password == "" {
		pg.PasswordError = "Enter your password"
	}
	if pg.UsernameError != "" || pg.PasswordError != "" {
		s.page(w, r, http.StatusBadRequest, "", pg)
		return
	}
	// Allow counts this attempt now, so parallel guesses cannot all pass
	// before any of them fails (SPEC 12.4).
	if ok, wait := s.limiter.Allow(username, client); !ok {
		s.log.Info("sign-in refused: too many failures", "client", client)
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		s.page(w, r, http.StatusTooManyRequests, "", SignInPage{Username: username, RateLimited: true, RetryAfter: wait})
		return
	}

	u, found, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		s.limiter.Release(username, client)
		s.fail(w, r, "look up user", err)
		return
	}
	// Header-created users have no password and cannot sign in here.
	if !found || u.PasswordHash == "" {
		if err := auth.VerifyDummy(password); errors.Is(err, auth.ErrBusy) {
			s.signInBusy(w, r, pg, client)
			return
		}
		s.signInFailed(w, r, pg, client)
		return
	}
	ok, needsRehash, err := auth.Verify(password, u.PasswordHash)
	if errors.Is(err, auth.ErrBusy) {
		s.signInBusy(w, r, pg, client)
		return
	}
	if err != nil {
		s.log.Error("stored password hash is unusable", "username", clip(username), "error", err)
	}
	if err != nil || !ok {
		s.signInFailed(w, r, pg, client)
		return
	}

	s.limiter.Succeed(username, client)
	if needsRehash {
		s.rehash(r, u.ID, password)
	}
	if err := s.sessions.Start(ctx, w, u.ID); err != nil {
		s.fail(w, r, "start session", err)
		return
	}
	http.Redirect(w, r, PathHome, http.StatusSeeOther)
}

// signInFailed keeps the attempt Allow counted. The log names the client
// only: a mistyped password often lands in the username field.
func (s *server) signInFailed(w http.ResponseWriter, r *http.Request, pg SignInPage, client string) {
	s.log.Info("sign-in failed", "client", client)
	pg.PasswordError = msgBadLogin
	s.page(w, r, http.StatusUnauthorized, "", pg)
}

// signInBusy answers when too many password checks are already running.
// No password was checked, so the attempt is given back to the limiter.
func (s *server) signInBusy(w http.ResponseWriter, r *http.Request, pg SignInPage, client string) {
	s.limiter.Release(pg.Username, client)
	s.log.Info("sign-in refused: busy", "client", client)
	pg.PasswordError = msgBusy
	w.Header().Set("Retry-After", "1")
	s.page(w, r, http.StatusServiceUnavailable, "", pg)
}

// rehash stores a fresh hash after a good sign-in with an older one. A
// failure is logged and ignored: the old hash still works.
func (s *server) rehash(r *http.Request, userID int64, password string) {
	hash, err := auth.Hash(password)
	if err == nil {
		err = s.store.SetPasswordHash(r.Context(), userID, hash)
	}
	if err != nil {
		s.log.Error("rehash password", "user_id", userID, "error", err)
	}
}

// handleSignOut ends the session, if there is one. Header users have none;
// the proxy decides whether they stay signed in.
func (s *server) handleSignOut(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.End(r.Context(), w, r); err != nil {
		s.fail(w, r, "end session", err)
		return
	}
	http.Redirect(w, r, PathSignedOut, http.StatusSeeOther)
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxLoggedUsername {
		return s
	}
	return string([]rune(s)[:maxLoggedUsername])
}
