package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
)

// PathAccount is the factorio.com account page (SPEC 9.11).
const PathAccount = "/settings/factorio-account"

// The audit actions of the account page (SPEC 12.9). Their entries hold no
// username and no token (SPEC 14.8).
const (
	AuditAccountSet    = "factorio-account-set"
	AuditAccountRemove = "factorio-account-remove"
)

// maxAccountFieldChars bounds the username and the token, so a hostile post
// cannot fill the database.
const maxAccountFieldChars = 256

// The two things the form can do, and the two results the page can report.
const (
	accountActionRemove = "remove"
	accountDoneSaved    = "saved"
	accountDoneRemoved  = "removed"
)

// AccountPage is the factorio.com account page (SPEC 9.11, 11.4). It holds
// no username and no token, only whether an account is set.
type AccountPage struct {
	Source   factorioaccount.Source
	CanStore bool
	// UpdatedAt and UpdatedBy say when and by whom a stored account was saved.
	UpdatedAt time.Time
	UpdatedBy string
	// Return is a local path to go back to after saving (SPEC 14.17).
	Return        string
	Done          i18n.Msg
	Error         i18n.Msg
	UsernameError i18n.Msg
	TokenError    i18n.Msg
}

func (AccountPage) templateName() string { return "account" }
func (AccountPage) navItem() string      { return PathAccount }
func (p AccountPage) hasErrors() bool    { return len(p.Problems()) > 0 || !p.Error.IsZero() }

// Path is where the forms post.
func (AccountPage) Path() string { return PathAccount }

// FromEnv, Stored and Unreadable say which source the account has.
func (p AccountPage) FromEnv() bool     { return p.Source == factorioaccount.Env }
func (p AccountPage) Stored() bool      { return p.Source == factorioaccount.Stored }
func (p AccountPage) Unreadable() bool  { return p.Source == factorioaccount.Unreadable }
func (p AccountPage) NoKeyNotice() bool { return !p.FromEnv() && !p.CanStore }

// Problems lists the form's errors in the order the fields appear.
func (p AccountPage) Problems() []Problem {
	return problems(Problem{"account-username", p.UsernameError}, Problem{"account-token", p.TokenError})
}

// Retype is true after a refused post: the page never shows a value back,
// so the person types both again (SPEC 11.4).
func (p AccountPage) Retype() bool { return !p.UsernameError.IsZero() || !p.TokenError.IsZero() }

func (s *server) accountRoutes() {
	s.mux.HandleFunc("GET "+PathAccount, s.requireUser(s.handleAccountForm))
	s.mux.HandleFunc("POST "+PathAccount, s.requireUser(s.handleAccount))
}

// accountPage reads where the account stands now. The error is for the
// database failing.
func (s *server) accountPage(r *http.Request, ret string) (AccountPage, error) {
	_, src, meta, err := s.account.Get(r.Context())
	if err != nil {
		return AccountPage{}, err
	}
	return AccountPage{Source: src, CanStore: s.account.CanStore(), UpdatedAt: meta.UpdatedAt, UpdatedBy: meta.UpdatedBy, Return: ret}, nil
}

func (s *server) handleAccountForm(w http.ResponseWriter, r *http.Request, u store.User) {
	p, err := s.accountPage(r, returnPath(r.URL.Query().Get("return")))
	if err != nil {
		s.fail(w, r, "read the factorio.com account", err)
		return
	}
	switch r.URL.Query().Get("done") {
	case accountDoneSaved:
		p.Done = i18n.M("account.done.saved")
	case accountDoneRemoved:
		p.Done = i18n.M("account.done.removed")
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// checkAccountField trims v and returns a problem in plain words, or a zero
// Msg. The missing message is the caller's, as it names the field.
func checkAccountField(v string, missing i18n.Msg) (string, i18n.Msg) {
	clean := strings.TrimSpace(v)
	switch {
	case clean == "":
		return clean, missing
	case utf8.RuneCountInString(clean) > maxAccountFieldChars:
		return clean, i18n.M("accounts.username.long", "count", maxAccountFieldChars)
	case strings.IndexFunc(clean, unicode.IsControl) >= 0:
		return clean, i18n.M("account.field.control")
	}
	return clean, i18n.Msg{}
}

func (s *server) handleAccount(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	ret := returnPath(r.PostFormValue("return"))
	p, err := s.accountPage(r, ret)
	if err != nil {
		s.fail(w, r, "read the factorio.com account", err)
		return
	}
	if r.PostFormValue("action") == accountActionRemove {
		s.removeAccount(w, r, u, p)
		return
	}

	// Without a key the page has no fields to point a field error at.
	if p.NoKeyNotice() {
		p.Error = i18n.M("account.error.nokey")
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	username, usernameProblem := checkAccountField(r.PostFormValue("username"), i18n.M("account.username.missing"))
	token, tokenProblem := checkAccountField(r.PostFormValue("token"), i18n.M("account.token.missing"))
	if !usernameProblem.IsZero() || !tokenProblem.IsZero() {
		p.UsernameError, p.TokenError = usernameProblem, tokenProblem
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	err = s.account.Set(r.Context(), factorioaccount.Account{Username: secret.New(username), Token: secret.New(token)}, u.Username)
	if err != nil {
		s.accountRefused(w, r, u, p, "store the factorio.com account", err)
		return
	}
	s.audit(r.Context(), u, AuditAccountSet, "", "")
	to := ret
	if to == "" {
		to = accountDoneURL(accountDoneSaved, "")
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// removeAccount deletes the stored account. With nothing stored there is
// nothing to remove, and no audit entry.
func (s *server) removeAccount(w http.ResponseWriter, r *http.Request, u store.User, p AccountPage) {
	if p.Stored() || p.Unreadable() {
		if err := s.account.Remove(r.Context()); err != nil {
			s.accountRefused(w, r, u, p, "remove the factorio.com account", err)
			return
		}
		s.audit(r.Context(), u, AuditAccountRemove, "", "")
	}
	http.Redirect(w, r, accountDoneURL(accountDoneRemoved, p.Return), http.StatusSeeOther)
}

// accountRefused answers a change the service refused, or fails on any
// other error. The page never carries err.
func (s *server) accountRefused(w http.ResponseWriter, r *http.Request, u store.User, p AccountPage, what string, err error) {
	switch {
	case errors.Is(err, factorioaccount.ErrFromEnvironment):
		p.Error = i18n.M("account.error.env")
	case errors.Is(err, factorioaccount.ErrNoSecretKey):
		p.Error = i18n.M("account.error.nokey")
	default:
		s.fail(w, r, what, err)
		return
	}
	s.page(w, r, http.StatusConflict, u.Username, p)
}

// accountDoneURL is the account page showing the result of a change, and
// keeping the path to go back to.
func accountDoneURL(done, ret string) string {
	q := url.Values{"done": {done}}
	if ret != "" {
		q.Set("return", ret)
	}
	return PathAccount + "?" + q.Encode()
}
