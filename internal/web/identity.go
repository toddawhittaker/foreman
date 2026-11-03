package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/store"
)

type signedIn int

const (
	nobody signedIn = iota
	isSignedIn
	refused // a trusted proxy named a user Foreman will not admit
)

// identify works out who made the request (SPEC 12.5, 12.8). Trusted
// headers come first and need no cookie, since the proxy vouches for every
// request. Otherwise the session cookie counts, when local sign-in is on.
func (s *server) identify(r *http.Request) (store.User, signedIn, error) {
	ctx := r.Context()
	if s.cfg.AuthHeaders {
		id := s.headers.Identify(r)
		switch id.Outcome {
		case auth.HeaderNotAllowed:
			return store.User{}, refused, nil
		case auth.HeaderOK:
			name, problem := checkUsername(id.Username)
			if problem != "" {
				return store.User{}, refused, nil
			}
			u, err := s.headerUser(ctx, name)
			if err != nil {
				return store.User{}, nobody, err
			}
			return u, isSignedIn, nil
		}
	}
	if !s.cfg.AuthLocal {
		return store.User{}, nobody, nil
	}
	id, ok, err := s.sessions.Lookup(ctx, r)
	if err != nil || !ok {
		return store.User{}, nobody, err
	}
	u, found, err := s.store.UserByID(ctx, id)
	if err != nil || !found {
		return store.User{}, nobody, err
	}
	return u, isSignedIn, nil
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
