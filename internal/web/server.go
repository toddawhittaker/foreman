// Package web serves Foreman's HTTP interface.
package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// HealthPath answers 200 while the process can serve requests; the image's
// health check calls it.
const HealthPath = "/healthz"

// maxFormBytes caps every request body except uploads (see handleUpload);
// the forms hold a few short fields.
const maxFormBytes = 64 << 10

// Deps is what the handler needs from the rest of the app.
type Deps struct {
	Config *config.Config
	Store  *store.Store
	Worlds *world.Manager
	Log    *slog.Logger
	Now    func() time.Time // time.Now when nil
}

type server struct {
	cfg      *config.Config
	store    *store.Store
	worlds   *world.Manager
	log      *slog.Logger
	now      func() time.Time
	render   *Renderer
	sessions *auth.SessionManager
	limiter  *auth.Limiter
	headers  auth.HeaderConfig
	mux      *http.ServeMux
	// uploadLimits holds the body limit of each upload route, by pattern.
	uploadLimits map[string]int64
	// uploadSlots holds one entry per upload in progress (see guardUpload).
	uploadSlots chan struct{}
}

// NewHandler returns the HTTP handler for the whole app.
func NewHandler(d Deps) (http.Handler, error) {
	renderer, err := NewRenderer()
	if err != nil {
		return nil, err
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	s := &server{
		cfg:      d.Config,
		store:    d.Store,
		worlds:   d.Worlds,
		log:      d.Log,
		now:      now,
		render:   renderer,
		sessions: auth.NewSessionManager(d.Store, d.Config.SessionIdle, d.Config.SecureCookies(), now),
		limiter:  auth.NewLimiter(now),
		headers: auth.HeaderConfig{
			UserHeader:    d.Config.UserHeader,
			GroupsHeader:  d.Config.GroupsHeader,
			RequiredGroup: d.Config.RequiredGroup,
			Trusted:       d.Config.TrustedProxies,
		},
		mux:          http.NewServeMux(),
		uploadLimits: map[string]int64{},
		uploadSlots:  make(chan struct{}, maxUploads),
	}
	s.routes()

	cop := http.NewCrossOriginProtection()
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.page(w, r, http.StatusForbidden, "", ErrorPage{Status: http.StatusForbidden})
	}))
	// Behind a proxy that rewrites Host, the browser's Origin is the public URL.
	if u := s.cfg.PublicURL; u != nil {
		if err := cop.AddTrustedOrigin(u.Scheme + "://" + u.Host); err != nil {
			return nil, fmt.Errorf("trust FOREMAN_PUBLIC_URL as an origin: %w", err)
		}
	}
	var h http.Handler = s.mux
	h = cop.Handler(h)
	h = s.limitBody(h)
	h = s.recoverPanic(h)
	h = s.accessLog(h)
	return h, nil
}

func (s *server) routes() {
	s.mux.HandleFunc("GET "+HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	s.mux.Handle("GET "+PathStatic, s.render.Static())
	s.mux.HandleFunc("GET "+PathSignedOut, s.handleSignedOut)
	s.mux.HandleFunc("POST "+PathSignOut, s.handleSignOut)
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, PathWorlds, http.StatusSeeOther)
	})
	s.worldRoutes()
	s.saveRoutes()
	s.snapshotRoutes()
	if s.cfg.AuthLocal {
		s.mux.HandleFunc("GET "+PathSetup, s.handleSetupForm)
		s.mux.HandleFunc("POST "+PathSetup, s.handleSetup)
		s.mux.HandleFunc("GET "+PathSignIn, s.handleSignInForm)
		s.mux.HandleFunc("POST "+PathSignIn, s.handleSignIn)
	}
	s.mux.HandleFunc("/", s.handleUnknown)
}

// handleUnknown answers every path no route claims: 405 when the path
// exists for another method, otherwise 404.
func (s *server) handleUnknown(w http.ResponseWriter, r *http.Request) {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, pattern := s.mux.Handler(probe); pattern != "" && pattern != "/" {
			allowed = append(allowed, m)
		}
	}
	if len(allowed) > 0 {
		for _, m := range allowed {
			w.Header().Add("Allow", m)
		}
		s.page(w, r, http.StatusMethodNotAllowed, "", ErrorPage{Status: http.StatusMethodNotAllowed})
		return
	}
	s.page(w, r, http.StatusNotFound, "", ErrorPage{Status: http.StatusNotFound})
}

// NewHTTPServer returns an http.Server with the timeouts and size limits
// every Foreman listener uses.
func NewHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}

// page renders p. If rendering fails it logs the error and sends a plain 500.
func (s *server) page(w http.ResponseWriter, r *http.Request, status int, user string, p Page) {
	err := s.render.Render(w, status, user, p)
	if err == nil {
		return
	}
	s.log.Error("render page", "method", r.Method, "path", r.URL.Path, "error", err)
	if rerr := s.render.Render(w, http.StatusInternalServerError, "", ErrorPage{Status: http.StatusInternalServerError}); rerr != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// fail logs err and sends the 500 page. The page never carries err.
func (s *server) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	s.log.Error(what, "method", r.Method, "path", r.URL.Path, "error", err)
	s.page(w, r, http.StatusInternalServerError, "", ErrorPage{Status: http.StatusInternalServerError})
}

func (s *server) handleSignedOut(w http.ResponseWriter, r *http.Request) {
	s.page(w, r, http.StatusOK, "", SignedOutPage{})
}
