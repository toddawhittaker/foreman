package web

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/i18n"
)

// The OIDC sign-in routes (SPEC 12.7). The provider sends the browser back
// to FOREMAN_PUBLIC_URL plus PathOIDCCallback.
const (
	PathOIDCStart    = "/signin/oidc"
	PathOIDCCallback = "/signin/oidc/callback"
)

const (
	// oidcCookie binds a callback to the browser that started the sign-in.
	oidcCookie = "foreman_oidc"
	// oidcPendingTTL is how long a started sign-in may take.
	oidcPendingTTL = 10 * time.Minute
	// maxOIDCPending bounds the sign-ins held; a new one drops the oldest.
	maxOIDCPending = 1000
)

// oidcRoutes registers the sign-in routes of the "oidc" method.
func (s *server) oidcRoutes() {
	if !s.cfg.AuthOIDC {
		return
	}
	o := &oidcSignIn{
		s: s,
		client: auth.NewOIDC(auth.OIDCSettings{
			Issuer:        s.cfg.OIDC.Issuer,
			ClientID:      s.cfg.OIDC.ClientID,
			ClientSecret:  s.cfg.OIDC.ClientSecret,
			RequiredGroup: s.cfg.OIDC.RequiredGroup,
			RedirectURL:   strings.TrimRight(s.cfg.PublicURL.String(), "/") + PathOIDCCallback,
			Transport:     s.oidcTransport,
			Now:           s.now,
		}),
		pending: newOIDCPending(s.now),
		starts:  auth.NewOIDCStartLimiter(s.now),
	}
	s.mux.HandleFunc("GET "+PathOIDCStart, o.start)
	s.mux.HandleFunc("GET "+PathOIDCCallback, o.callback)
}

type oidcSignIn struct {
	s       *server
	client  *auth.OIDC
	pending *oidcPending
	// starts bounds the sign-ins one address may start, so it cannot push
	// other people's out of pending (SPEC 12.7).
	starts *auth.AddrLimiter
}

// tooMany answers 429 with how long to wait.
func (o *oidcSignIn) tooMany(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
	o.s.page(w, r, http.StatusTooManyRequests, "", ErrorPage{
		Status: http.StatusTooManyRequests,
		Detail: SignInPage{RateLimited: true, RetryAfter: wait}.RateLimitMessage(),
	})
}

// start sends the browser to the identity provider, binding the sign-in to
// it with a cookie that holds the state.
func (o *oidcSignIn) start(w http.ResponseWriter, r *http.Request) {
	s := o.s
	client := auth.ClientAddr(r, s.cfg.TrustedProxies)
	if ok, wait := o.starts.Allow(client); !ok {
		s.log.Info("single sign-on start refused: too many starts", "client", client)
		o.tooMany(w, r, wait)
		return
	}
	req, err := o.client.Begin(r.Context())
	if err != nil {
		// Discovery errors name the provider's addresses, never a user's claims.
		s.log.Warn("single sign-on is unavailable", "error", err)
		s.page(w, r, http.StatusServiceUnavailable, "", oidcRefusal(http.StatusServiceUnavailable,
			i18n.M("accounts.oidc.unavailable"), true, returnPath(r.URL.Query().Get("next"))))
		return
	}
	o.pending.add(req.State, oidcStarted{
		nonce: req.Nonce, verifier: req.Verifier,
		next: returnPath(r.URL.Query().Get("next")),
	})
	http.SetCookie(w, o.cookie(req.State, int(oidcPendingTTL/time.Second)))
	http.Redirect(w, r, req.URL, http.StatusSeeOther)
}

// callback finishes a sign-in. Each failure is logged with the client's
// address and fixed words only (SPEC 14.14). A callback that names no
// sign-in this browser started costs nothing and calls no one, so a hostile
// page cannot lock a visitor out by loading it; one that does counts
// against the address in the sign-in limiter (SPEC 12.11).
func (o *oidcSignIn) callback(w http.ResponseWriter, r *http.Request) {
	s := o.s
	ctx := r.Context()
	client := auth.ClientAddr(r, s.cfg.TrustedProxies)
	// The cookie has done its job whatever happens next.
	http.SetCookie(w, o.cookie("", -1))
	// next is the page the sign-in was for, known once its state is found.
	var next string
	failed := func(status int, detail i18n.Msg, retry bool, reason string) {
		s.log.Info("single sign-on failed", "client", client, "reason", reason)
		s.page(w, r, status, "", oidcRefusal(status, detail, retry, next))
	}
	tryAgain := i18n.M("accounts.oidc.failed")

	q := r.URL.Query()
	state := q.Get("state")
	bound, err := r.Cookie(oidcCookie)
	if state == "" || err != nil || subtle.ConstantTimeCompare([]byte(bound.Value), []byte(state)) != 1 {
		failed(http.StatusBadRequest, tryAgain, true, "the state does not match the browser's cookie")
		return
	}
	started, ok := o.pending.take(state)
	next = started.next
	if !ok {
		failed(http.StatusBadRequest, tryAgain, true, "the sign-in is unknown, used or expired")
		return
	}
	if ok, wait := s.limiter.AllowAddr(client); !ok {
		s.log.Info("sign-in refused: too many failures", "client", client)
		o.tooMany(w, r, wait)
		return
	}
	if q.Get("error") != "" {
		failed(http.StatusBadRequest, tryAgain, true, "the identity provider answered with an error")
		return
	}
	id, err := o.client.Finish(ctx, q.Get("code"), started.verifier, started.nonce)
	switch {
	case errors.Is(err, auth.ErrOIDCNotAllowed):
		failed(http.StatusForbidden, i18n.M("accounts.proxy.refused"), false, err.Error())
		return
	case errors.Is(err, auth.ErrOIDCUnavailable):
		// A provider that is down is not a guess; it gives the attempt back.
		s.limiter.ReleaseAddr(client)
		s.log.Warn("single sign-on is unavailable", "error", err)
		failed(http.StatusServiceUnavailable, i18n.M("accounts.oidc.unavailable"), true, "the identity provider is unavailable")
		return
	case err != nil:
		// Finish's errors hold fixed words only.
		failed(http.StatusBadRequest, tryAgain, true, err.Error())
		return
	}
	u, err := s.oidcUser(ctx, id)
	if !errors.Is(err, errOIDCNameTaken) {
		// Only a name clash is a refused sign-in; a bad name can never
		// work by trying again, and a store failure is Foreman's.
		s.limiter.ReleaseAddr(client)
	}
	switch {
	case errors.Is(err, errOIDCNameTaken):
		failed(http.StatusForbidden, i18n.M("accounts.oidc.name_taken"), false, err.Error())
		return
	case errors.Is(err, errOIDCBadName):
		failed(http.StatusForbidden, i18n.M("accounts.oidc.bad_username"), false, err.Error())
		return
	case err != nil:
		s.fail(w, r, "find the single sign-on user", err)
		return
	}
	if err := s.sessions.Start(ctx, w, u.ID); err != nil {
		s.fail(w, r, "start session", err)
		return
	}
	if next == "" {
		next = s.home()
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// oidcRefusal is the page for a sign-in that did not work. Where trying
// again can help it offers that, carrying the page asked for; otherwise it
// offers the sign-in page (DESIGN 9).
func oidcRefusal(status int, detail i18n.Msg, retry bool, next string) ErrorPage {
	p := ErrorPage{
		Status: status, Detail: detail, Title: i18n.M("accounts.oidc.heading"),
		ActionLabel: i18n.M("accounts.oidc.back"), ActionPath: PathSignIn,
	}
	if retry {
		p.ActionLabel, p.ActionPath = i18n.M("accounts.oidc.retry"), PathOIDCStart
		if next != "" {
			p.ActionPath += "?next=" + url.QueryEscape(next)
		}
	}
	return p
}

// cookie is the binding cookie, sent only to the OIDC routes. Secure
// follows the public URL as the session cookie does (SPEC 12.8).
func (o *oidcSignIn) cookie(state string, maxAge int) *http.Cookie {
	//nolint:gosec // Secure follows the public URL scheme (SPEC 12.8), so it is not always set.
	return &http.Cookie{
		Name:     oidcCookie,
		Value:    state,
		MaxAge:   maxAge,
		Path:     PathOIDCStart,
		HttpOnly: true,
		Secure:   o.s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	}
}

// oidcStarted is one sign-in waiting for its callback.
type oidcStarted struct {
	nonce, verifier string
	// next has passed returnPath.
	next             string
	created, expires time.Time
}

// oidcPending holds started sign-ins in memory, by state; a restart voids
// them. Each works once.
type oidcPending struct {
	mu  sync.Mutex
	m   map[string]oidcStarted
	now func() time.Time
}

func newOIDCPending(now func() time.Time) *oidcPending {
	return &oidcPending{m: map[string]oidcStarted{}, now: now}
}

func (p *oidcPending) add(state string, e oidcStarted) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	oldest := ""
	for k, o := range p.m {
		if !now.Before(o.expires) {
			delete(p.m, k)
			continue
		}
		if oldest == "" || o.created.Before(p.m[oldest].created) {
			oldest = k
		}
	}
	if len(p.m) >= maxOIDCPending {
		delete(p.m, oldest)
	}
	e.created, e.expires = now, now.Add(oidcPendingTTL)
	p.m[state] = e
}

// take removes the sign-in and returns it if it has not expired.
func (p *oidcPending) take(state string) (oidcStarted, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.m[state]
	delete(p.m, state)
	if !ok || !p.now().Before(e.expires) {
		return oidcStarted{}, false
	}
	return e, true
}
