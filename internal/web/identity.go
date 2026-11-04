package web

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/store"
)

type signedIn int

const (
	nobody signedIn = iota
	isSignedIn
	refused // a trusted proxy named a user Foreman will not admit
)

// proxyStreamLimit is how long a live stream signed in by trusted headers
// stays open. Its own headers never change, so only a reconnect through
// the proxy checks the user again (SPEC 12.5, 16.3).
const proxyStreamLimit = 5 * time.Minute

// identify works out who made the request (SPEC 12.5, 12.8). Trusted
// headers come first and need no cookie, since the proxy vouches for every
// request. Otherwise the session cookie counts, when local sign-in is on.
func (s *server) identify(r *http.Request) (store.User, signedIn, error) {
	u, state, _, err := s.whoIs(r, s.sessions.Lookup)
	return u, state, err
}

// whoIs is identify with the session check given. byProxy reports that
// trusted headers named the user.
func (s *server) whoIs(r *http.Request, session func(context.Context, *http.Request) (int64, bool, error)) (u store.User, state signedIn, byProxy bool, err error) {
	ctx := r.Context()
	if s.cfg.AuthHeaders && !s.fromGame(r) {
		id := s.headers.Identify(r)
		switch id.Outcome {
		case auth.HeaderNotAllowed:
			return store.User{}, refused, false, nil
		case auth.HeaderOK:
			name, problem := checkUsername(id.Username)
			if problem != "" {
				return store.User{}, refused, false, nil
			}
			u, err := s.headerUser(ctx, name)
			if err != nil {
				return store.User{}, nobody, false, err
			}
			return u, isSignedIn, true, nil
		}
	}
	if !s.cfg.AuthLocal {
		return store.User{}, nobody, false, nil
	}
	id, ok, err := session(ctx, r)
	if err != nil || !ok {
		return store.User{}, nobody, false, err
	}
	u, found, err := s.store.UserByID(ctx, id)
	if err != nil || !found {
		return store.User{}, nobody, false, err
	}
	return u, isSignedIn, false, nil
}

// GameAddresser gives the adopted game container's current addresses.
type GameAddresser interface {
	GameAddresses() []netip.Addr
}

// fromGame reports whether the direct peer is the game container. The game
// is never the proxy, so its headers count for nothing, however trusted its
// address looks (SPEC 12.6). It is checked on every request, not only while
// a network refusal stands. Each address is logged once.
func (s *server) fromGame(r *http.Request) bool {
	if s.gameAddrs == nil {
		return false
	}
	addrs := s.gameAddrs.GameAddresses()
	if !auth.PeerIs(r, addrs) {
		return false
	}
	peer := auth.ClientAddr(r, nil) // the direct peer, unmapped, without the port
	if _, seen := s.gameSeen.LoadOrStore(peer, true); !seen {
		s.log.Warn("refused trusted-header sign-in from the game container's address", "address", peer)
	}
	return true
}

// stillSignedIn is a live stream's check at each heartbeat (SPEC 12.8,
// 16.3). It never refreshes the session, since the user did not act, and
// it ends a stream signed in by trusted headers after proxyStreamLimit.
func (s *server) stillSignedIn(r *http.Request, opened time.Time) bool {
	_, state, byProxy, err := s.whoIs(r, s.sessions.Check)
	if err != nil || state != isSignedIn {
		return false
	}
	return !byProxy || s.now().Sub(opened) < proxyStreamLimit
}

// headerUser finds the user a trusted proxy named, creating it on first
// sight with no password. The first user ever seen becomes the first admin.
func (s *server) headerUser(ctx context.Context, name string) (store.User, error) {
	u, found, err := s.store.UserByUsername(ctx, name)
	if err != nil || found {
		return u, err
	}
	now := s.now()
	u, err = s.store.CreateFirstAdmin(ctx, name, "", now)
	if errors.Is(err, store.ErrAdminExists) {
		u, err = s.store.CreateUser(ctx, name, "", now)
	}
	if errors.Is(err, store.ErrUsernameTaken) {
		// A concurrent request created the same user first.
		var ok bool
		if u, ok, err = s.store.UserByUsername(ctx, name); err == nil && !ok {
			err = errors.New("user vanished after a duplicate-name error")
		}
	}
	return u, err
}

// requireUser runs h for a signed-in user. Anyone else is sent to set up or
// sign in, or told to use the proxy when that is the only method.
func (s *server) requireUser(h func(http.ResponseWriter, *http.Request, store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, state, err := s.identify(r)
		if err != nil {
			s.fail(w, r, "identify user", err)
			return
		}
		switch {
		case state == isSignedIn:
			h(w, r, u)
		case state == refused:
			s.page(w, r, http.StatusForbidden, "", ErrorPage{
				Status: http.StatusForbidden,
				Detail: "Your account is not allowed to use Foreman. Ask the person who runs Foreman to give you access.",
			})
		case s.cfg.AuthLocal:
			s.redirectToSignIn(w, r)
		default:
			s.page(w, r, http.StatusForbidden, "", ErrorPage{
				Status: http.StatusForbidden,
				Detail: "Foreman did not get your sign-in from the proxy. Sign in through the proxy, then open this page again.",
			})
		}
	}
}

// redirectToSignIn sends an anonymous visitor to setup while no account
// exists and to sign-in afterwards.
func (s *server) redirectToSignIn(w http.ResponseWriter, r *http.Request) {
	has, err := s.store.HasUsers(r.Context())
	if err != nil {
		s.fail(w, r, "check for users", err)
		return
	}
	if !has {
		http.Redirect(w, r, PathSetup, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, PathSignIn, http.StatusSeeOther)
}
