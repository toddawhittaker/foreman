// Package web serves Foreman's HTTP interface.
package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
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
	// Game runs the game servers; nil means this mode has no server
	// control, and "/" shows the world list.
	Game *game.Controller
	// GameNets gives the game networks, whose addresses may not
	// sign in by trusted headers (SPEC 12.6). Game answers when it is nil.
	GameNets GameSubnetter
	// Releases lists Factorio versions and releases (SPEC 10.2, 10.7);
	// nil means Foreman makes no outbound calls for them.
	Releases Releases
	// HostDataDir is the data directory's path on the Docker host (SPEC
	// 3.5), so a page can name a folder the admin reaches there.
	HostDataDir string
	Log         *slog.Logger
	Now         func() time.Time // time.Now when nil
}

type server struct {
	cfg      *config.Config
	store    *store.Store
	worlds   *world.Manager
	game     *game.Controller
	releases Releases
	hostData string
	runs     *gameRuns
	console  *consoleHistory
	log      *slog.Logger
	now      func() time.Time
	render   *Renderer
	sessions *auth.SessionManager
	limiter  *auth.Limiter
	headers  auth.HeaderConfig
	// gameNets and gameSeen: the ranges to refuse headers from, and the
	// ones already logged.
	gameNets GameSubnetter
	gameSeen sync.Map
	mux      *http.ServeMux
	// uploadLimits holds the body limit of each upload route, by pattern.
	uploadLimits map[string]int64
	// uploadSlots holds one entry per upload in progress (see guardUpload).
	uploadSlots chan struct{}
}

// Handler is the HTTP handler for the whole app.
type Handler struct {
	http.Handler
	runs *gameRuns
}

// Close ends the live pages, then interrupts the server control actions
// still running, and waits for both. Call it before the server shuts down,
// which would otherwise wait on the live pages, and before the store
// closes. It never stops a game (SPEC 3.8).
func (h *Handler) Close() { h.runs.close() }

// NewHandler returns the HTTP handler for the whole app.
func NewHandler(d Deps) (*Handler, error) {
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
		game:     d.Game,
		releases: d.Releases,
		hostData: d.HostDataDir,
		runs:     newGameRuns(),
		gameNets: d.GameNets,
		console:  newConsoleHistory(),
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
	if s.gameNets == nil && d.Game != nil {
		s.gameNets = d.Game
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
	h = withLocale(h)
	h = s.limitBody(h)
	h = s.recoverPanic(h)
	h = s.accessLog(h)
	return &Handler{Handler: h, runs: s.runs}, nil
}

func (s *server) routes() {
	s.mux.HandleFunc("GET "+HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	s.mux.Handle("GET "+PathStatic, s.render.Static())
	s.mux.HandleFunc("GET "+PathSignedOut, s.handleSignedOut)
	s.mux.HandleFunc("POST "+PathSignOut, s.handleSignOut)
	s.mux.HandleFunc("POST "+PathLanguage, s.handleLanguage)
	s.gameRoutes()
	s.consoleRoutes()
	s.worldRoutes()
	s.saveRoutes()
	s.snapshotRoutes()
	s.updateRoutes()
	s.generateRoutes()
	s.cloneRoutes()
	s.settingsRoutes()
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
	err := s.render.Render(w, r, status, user, p)
	if err == nil {
		return
	}
	s.log.Error("render page", "method", r.Method, "path", r.URL.Path, "error", err)
	if rerr := s.render.Render(w, r, http.StatusInternalServerError, "", ErrorPage{Status: http.StatusInternalServerError}); rerr != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// fail logs err and sends the 500 page. The page never carries err.
func (s *server) fail(w http.ResponseWriter, r *http.Request, what string, err error) {
	// A browser that left mid-request is not a server fault.
	if r.Context().Err() == nil {
		s.log.Error(what, "method", r.Method, "path", r.URL.Path, "error", err)
	}
	s.page(w, r, http.StatusInternalServerError, "", ErrorPage{Status: http.StatusInternalServerError})
}

func (s *server) handleSignedOut(w http.ResponseWriter, r *http.Request) {
	s.page(w, r, http.StatusOK, "", SignedOutPage{})
}

// LangCookie holds the language the menu chose, as a supported BCP 47 tag
// (SPEC 22.2).
const LangCookie = "foreman_lang"

type localeKey struct{}

// withLocale picks each request's language, from the cookie first and then
// Accept-Language, and stores it in the request's context.
func withLocale(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var chosen string
		if c, err := r.Cookie(LangCookie); err == nil {
			chosen = c.Value
		}
		loc := i18n.Match(chosen, r.Header.Get("Accept-Language"))
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), localeKey{}, loc)))
	})
}

// localeOf is the language r is answered in; English when none was picked.
func localeOf(r *http.Request) i18n.Locale {
	if loc, ok := r.Context().Value(localeKey{}).(i18n.Locale); ok {
		return loc
	}
	return i18n.English
}

// handleLanguage stores the menu's choice in a cookie and goes back to the
// page the menu was on (SPEC 22.2). It needs no sign-in, so the sign-in
// page can be read in any language.
func (s *server) handleLanguage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	loc, ok := i18n.Parse(r.PostFormValue("lang"))
	if !ok {
		s.page(w, r, http.StatusBadRequest, "", ErrorPage{Status: http.StatusBadRequest})
		return
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure follows the configuration, as the session cookie does
		Name:     LangCookie,
		Value:    loc.String(),
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
	to := languageReturn(r.PostFormValue("return"))
	if to == "" {
		to = "/"
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// languageReturn is where the language menu sends the browser back to:
// what returnPath allows (SPEC 14.17), and also the sign-in, setup and
// signed-out pages, which returnPath refuses only so a sign-in cannot
// return to itself. For those it returns Foreman's own path, never the
// input, keeping only a sign-in's next once it passes returnPath.
func languageReturn(p string) string {
	if safe := returnPath(p); safe != "" {
		return safe
	}
	u, err := url.Parse(p)
	if err != nil {
		return ""
	}
	switch u.Path {
	case PathSetup, PathSignedOut:
		return u.Path
	case PathSignIn:
		if next := returnPath(u.Query().Get("next")); next != "" {
			return PathSignIn + "?next=" + url.QueryEscape(next)
		}
		return PathSignIn
	}
	return ""
}
