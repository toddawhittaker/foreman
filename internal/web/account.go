package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/game"
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

// The Settings tab opens the restart schedule, so it marks this page's section (DESIGN 6).
func (AccountPage) navItem() string   { return generalPath() }
func (p AccountPage) hasErrors() bool { return len(p.Problems()) > 0 || !p.Error.IsZero() }

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

// accountState reads where the account stands now. The error is for the
// database failing.
func (s *server) accountState(ctx context.Context) (AccountPage, error) {
	_, src, meta, err := s.account.Get(ctx)
	if err != nil {
		return AccountPage{}, err
	}
	return AccountPage{Source: src, CanStore: s.account.CanStore(), UpdatedAt: meta.UpdatedAt, UpdatedBy: meta.UpdatedBy}, nil
}

func (s *server) handleAccountForm(w http.ResponseWriter, r *http.Request, u store.User) {
	p, err := s.accountState(r.Context())
	if err != nil {
		s.fail(w, r, "read the factorio.com account", err)
		return
	}
	p.Return = returnPath(r.URL.Query().Get("return"))
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

// setAccount stores the factorio.com account and records it in the audit
// log under by. The page returned holds the refusal when the account cannot
// be stored or a field is refused, and nothing is stored then; the error is
// for the database failing.
func (s *server) setAccount(ctx context.Context, by game.Actor, username, token string) (AccountPage, error) {
	p, err := s.accountState(ctx)
	if err != nil {
		return AccountPage{}, err
	}
	// Without a key the page has no fields to point a field error at.
	if p.NoKeyNotice() {
		p.Error = i18n.M("account.error.nokey")
		return p, nil
	}
	username, usernameProblem := checkAccountField(username, i18n.M("account.username.missing"))
	token, tokenProblem := checkAccountField(token, i18n.M("account.token.missing"))
	if !usernameProblem.IsZero() || !tokenProblem.IsZero() {
		p.UsernameError, p.TokenError = usernameProblem, tokenProblem
		return p, nil
	}
	err = s.account.Set(ctx, factorioaccount.Account{Username: secret.New(username), Token: secret.New(token)}, by.Username)
	if p.refused(err) {
		return p, nil
	}
	if err != nil {
		return AccountPage{}, err
	}
	s.audit(ctx, by, AuditAccountSet, "", "")
	return p, nil
}

// removeAccountBy deletes the stored account and records it in the audit
// log under by. With nothing stored there is nothing to remove, and no audit
// entry; removed says whether anything was. The page returned holds the
// refusal when the account cannot be removed.
func (s *server) removeAccountBy(ctx context.Context, by game.Actor) (removed bool, _ AccountPage, _ error) {
	p, err := s.accountState(ctx)
	if err != nil {
		return false, AccountPage{}, err
	}
	if !p.Stored() && !p.Unreadable() {
		return false, p, nil
	}
	err = s.account.Remove(ctx)
	if p.refused(err) {
		return false, p, nil
	}
	if err != nil {
		return false, AccountPage{}, err
	}
	s.audit(ctx, by, AuditAccountRemove, "", "")
	return true, p, nil
}

// refused words a change the service refused on the page and reports true;
// any other error is the caller's to fail on. The page never carries err.
func (p *AccountPage) refused(err error) bool {
	switch {
	case errors.Is(err, factorioaccount.ErrFromEnvironment):
		p.Error = i18n.M("account.error.env")
	case errors.Is(err, factorioaccount.ErrNoSecretKey):
		p.Error = i18n.M("account.error.nokey")
	default:
		return false
	}
	return true
}

func (s *server) handleAccount(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	ret := returnPath(r.PostFormValue("return"))
	if r.PostFormValue("action") == accountActionRemove {
		_, p, err := s.removeAccountBy(r.Context(), actor(u))
		s.accountAnswer(w, r, u, p, err, ret, "remove the factorio.com account", accountDoneRemoved)
		return
	}
	p, err := s.setAccount(r.Context(), actor(u), r.PostFormValue("username"), r.PostFormValue("token"))
	s.accountAnswer(w, r, u, p, err, ret, "store the factorio.com account", accountDoneSaved)
}

// accountAnswer shows a refused change on the page, fails on a real error,
// and otherwise goes to the result.
func (s *server) accountAnswer(w http.ResponseWriter, r *http.Request, u store.User, p AccountPage, err error, ret, what, done string) {
	if err != nil {
		s.fail(w, r, what, err)
		return
	}
	p.Return = ret
	switch {
	case len(p.Problems()) > 0:
		s.page(w, r, http.StatusBadRequest, u.Username, p)
	case !p.Error.IsZero():
		s.page(w, r, http.StatusConflict, u.Username, p)
	case done == accountDoneSaved && ret != "":
		http.Redirect(w, r, ret, http.StatusSeeOther)
	default:
		http.Redirect(w, r, accountDoneURL(done, ret), http.StatusSeeOther)
	}
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
