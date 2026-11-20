// Package web serves Foreman's HTTP interface.
package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/schedule"
	"github.com/toddawhittaker/foreman/internal/secret"
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
	// Favorites holds the favorites (SPEC 5.12); nil offers none.
	Favorites *world.Favorites
	// Game runs the game servers; nil means this mode has no server
	// control, and "/" shows the world list.
	Game *game.Controller
	// GameNets gives the game networks, whose addresses may not
	// sign in by trusted headers (SPEC 12.6). Game answers when it is nil.
	GameNets GameSubnetter
	// Releases lists Factorio versions and releases (SPEC 10.2, 10.7);
	// nil means Foreman makes no outbound calls for them.
	Releases Releases
	// Updates is the background check for a newer Foreman release
	// (SPEC 14.10); nil shows no notice in About.
	Updates UpdateCheck
	// Previewer makes the map previews of Generate (SPEC 5.6); nil means the
	// page offers no preview.
	Previewer MapPreviewer
	// Account finds and stores the factorio.com account (SPEC 9.11); nil
	// means no account is set in the environment and no key is set.
	Account *factorioaccount.Service
	// Portal is the Factorio mod portal (SPEC 9); nil means the mods pages
	// are off.
	Portal ModPortal
	// Notifier and Scheduler are the webhook sender and the restart schedule;
	// both do nothing yet.
	Notifier  *notify.Notifier
	Scheduler *schedule.Scheduler
	// HostDataDir is the data directory's path on the Docker host (SPEC
	// 3.5), so a page can name a folder the admin reaches there.
	HostDataDir string
	Log         *slog.Logger
	Now         func() time.Time // time.Now when nil
	// OIDCTransport makes the calls to the identity provider; nil means the
	// default. Tests give one that trusts their certificate.
	OIDCTransport http.RoundTripper
}

type server struct {
	// generalMu serialises saves of the General settings.
	generalMu sync.Mutex
	cfg       *config.Config
	store     *store.Store
	worlds    *world.Manager
	// favorites is nil when this handler offers none (SPEC 5.12).
	favorites *world.Favorites
	game      *game.Controller
	releases  Releases
	updates   UpdateCheck
	// previewer and previews: the map preview maker, and the pictures it
	// made, held for 15 minutes (SPEC 5.6).
	previewer MapPreviewer
	previews  *previewStore
	// drafts are the mods review page's answers (SPEC 9.14).
	drafts  *draftStore
	account *factorioaccount.Service
	// portal and planner: the mod portal and the plans made from it (SPEC 9).
	portal    ModPortal
	planner   *mods.Planner
	hostData  string
	notifier  *notify.Notifier
	scheduler *schedule.Scheduler
	runs      *gameRuns
	console   *consoleHistory
	// settings holds the game settings pages' state, which the MCP admin
	// tools share (SPEC 21.7).
	settings *settingsHandlers
	// confirms holds the MCP admin tools' confirm codes (SPEC 21.8).
	confirms *confirmStore
	// playerResults holds each user's last player action result (DESIGN 8.12).
	playerResults *playerResults
	log           *slog.Logger
	now           func() time.Time
	render        *Renderer
	sessions      *auth.SessionManager
	limiter       *auth.Limiter
	// tokens manages API tokens (SPEC 12.10).
	tokens  *auth.APITokens
	headers auth.HeaderConfig
	// gameNets and gameSeen: the ranges to refuse headers from, and the
	// ones already logged.
	gameNets GameSubnetter
	gameSeen sync.Map
	mux      *http.ServeMux
	// uploadLimits holds the body limit of each upload route, by pattern.
	uploadLimits map[string]limitFunc
	// uploadSlots holds one entry per upload in progress (see guardUpload).
	uploadSlots chan struct{}
	// publicSlots holds one entry per public download in progress. marks
	// keeps the portal's answers for the public mods page, and is nil when
	// Foreman has no portal (SPEC 9.16, 14.18).
	publicSlots chan struct{}
	marks       *portalMarks
	// oidcTransport is Deps.OIDCTransport.
	oidcTransport http.RoundTripper
}

// Handler is the HTTP handler for the whole app.
type Handler struct {
	http.Handler
	runs *gameRuns
	// srv is the server behind the handler, for the background sweeps.
	srv *server
}

// Close ends the live pages, then interrupts the server control actions
// still running, and waits for both. Call it before the server shuts down,
// which would otherwise wait on the live pages, and before the store
// closes. It never stops a game (SPEC 3.8).
func (h *Handler) Close() { h.runs.close() }

// NewHandler returns the HTTP handler for the whole app.
func NewHandler(d Deps) (*Handler, error) {
	renderer, err := sharedRenderer()
	if err != nil {
		return nil, err
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	account := d.Account
	if account == nil {
		if account, err = factorioaccount.New(factorioaccount.Account{}, secret.Secret{}, d.Store); err != nil {
			return nil, err
		}
	}
	s := &server{
		cfg:       d.Config,
		store:     d.Store,
		favorites: d.Favorites,
		worlds:    d.Worlds,
		game:      d.Game,
		releases:  d.Releases,
		updates:   d.Updates,
		previewer: d.Previewer,
		previews:  newPreviewStore(now),
		drafts:    newDraftStore(now),
		account:   account,
		hostData:  d.HostDataDir,
		notifier:  d.Notifier,
		scheduler: d.Scheduler,
		runs:      newGameRuns(),
		confirms:  newConfirmStore(now),
		gameNets:  d.GameNets,
		console:   newConsoleHistory(),
		log:       d.Log,
		now:       now,
		render:    renderer,
		sessions:  auth.NewSessionManager(d.Store, d.Config.SessionIdle, d.Config.SecureCookies(), now),
		limiter:   auth.NewLimiter(now),
		tokens:    auth.NewAPITokens(d.Store, now),
		headers: auth.HeaderConfig{
			UserHeader:    d.Config.UserHeader,
			GroupsHeader:  d.Config.GroupsHeader,
			RequiredGroup: d.Config.RequiredGroup,
			Trusted:       d.Config.TrustedProxies,
		},
		mux:          http.NewServeMux(),
		uploadLimits: map[string]limitFunc{},
		uploadSlots:  make(chan struct{}, maxUploads),
		publicSlots:  make(chan struct{}, maxPublicDownloads),

		oidcTransport: d.OIDCTransport,
	}
	if s.gameNets == nil && d.Game != nil {
		s.gameNets = d.Game
	}
	if d.Portal != nil {
		s.portal, s.planner = d.Portal, &mods.Planner{Worlds: d.Worlds, Portal: d.Portal}
		if d.Game != nil {
			d.Game.SetModFetcher(accountFetcher{account: account, portal: d.Portal, limit: s.modUploadLimit})
			d.Game.SetModCheck(s.planner)
		}
		s.marks = newPortalMarks(now, s.checkPortalForPublic)
	}
	s.applyRetention(context.Background())
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
	return &Handler{Handler: h, runs: s.runs, srv: s}, nil
}

func (s *server) routes() {
	s.mux.HandleFunc("GET "+HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	s.mux.Handle("GET "+PathStatic, s.render.Static())
	s.mux.Handle("GET "+PathFavicon, s.render.Favicon())
	s.mux.HandleFunc("GET "+PathSignedOut, s.handleSignedOut)
	s.mux.HandleFunc("POST "+PathSignOut, s.handleSignOut)
	s.mux.HandleFunc("POST "+PathLanguage, s.handleLanguage)
	s.mux.HandleFunc("POST "+PathTheme, s.handleTheme)
	s.gameRoutes()
	s.consoleRoutes()
	s.chatRoutes()
	s.playersRoutes()
	s.notifyRoutes()
	s.scheduleRoutes()
	s.worldRoutes()
	s.saveRoutes()
	s.snapshotRoutes()
	s.updateRoutes()
	s.generateRoutes()
	s.cloneRoutes()
	s.favoriteRoutes()
	s.settingsRoutes()
	s.accountRoutes()
	s.aboutRoutes()
	s.modRoutes()
	s.designRoutes()
	s.publicModsRoutes()
	s.publicModsPageRoutes()
	s.publicModsSettingsRoutes()
	s.apiTokenRoutes()
	s.generalRoutes()
	s.usersRoutes()
	s.auditRoutes()
	s.mcpRoutes()
	// Setup and the password form are local sign-in's alone (SPEC 12.3); the
	// sign-in page also offers OIDC (SPEC 12.7).
	if s.cfg.AuthLocal || s.cfg.AuthOIDC {
		s.mux.HandleFunc("GET "+PathSignIn, s.handleSignInForm)
	}
	if s.cfg.AuthLocal {
		s.mux.HandleFunc("GET "+PathSetup, s.handleSetupForm)
		s.mux.HandleFunc("POST "+PathSetup, s.handleSetup)
		s.mux.HandleFunc("POST "+PathSignIn, s.handleSignIn)
	}
	s.oidcRoutes()
	s.mux.HandleFunc("/", s.handleUnknown)
}

// handleUnknown answers every path no route claims: 405 when the path
// exists for another method, otherwise 404.
func (s *server) handleUnknown(w http.ResponseWriter, r *http.Request) {
	// While the public page is off, nothing under /public/ shows that it
	// exists, not even through the method it allows (SPEC 9.16).
	if s.publicClosed(r) {
		s.pageNotFound(w, r)
		return
	}
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
	s.pageNotFound(w, r)
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

// clientGone reports whether err came from a browser that left: the
// request was cancelled or the connection broke while writing.
func clientGone(r *http.Request, err error) bool {
	return r.Context().Err() != nil ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed)
}

// page renders p. If rendering fails it logs the error and sends a plain 500,
// unless the browser left: that is no server fault and nobody is left to answer.
func (s *server) page(w http.ResponseWriter, r *http.Request, status int, user string, p Page) {
	r = s.dropSpoofedRenewed(r, user)
	err := s.render.Render(w, r, status, user, s.lamp(r, user), s.tokenBanner(r, user), s.newerRelease(r, user), p)
	if err == nil {
		return
	}
	if clientGone(r, err) {
		s.log.Debug("render page: client gone", "method", r.Method, "path", r.URL.Path, "error", err)
		return
	}
	s.log.Error("render page", "method", r.Method, "path", r.URL.Path, "error", err)
	if rerr := s.render.Render(w, r, http.StatusInternalServerError, "", LiveState{}, nil, "", ErrorPage{Status: http.StatusInternalServerError}); rerr != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// lamp is the active world's server state for the main navigation, shown
// only to a signed-in user and only with server control. It makes no Docker
// call (game.Controller.Status); a failed lookup shows no lamp.
func (s *server) lamp(r *http.Request, user string) LiveState {
	if user == "" || s.game == nil || s.store == nil {
		return LiveState{}
	}
	id, found, err := s.store.ActiveWorld(r.Context())
	if err != nil || !found {
		return LiveState{}
	}
	l := s.liveState(id, true)
	if s.worlds != nil {
		if name, err := s.worlds.Name(id); err == nil {
			l.Name = name
		}
	}
	return l
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
	s.page(w, r, http.StatusOK, "", s.signedOutPage())
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
	s.savePreference(w, r, LangCookie, loc.String())
}

// ThemeCookie holds the theme the menu chose, one of themes (SPEC 17.3).
const ThemeCookie = "foreman_theme"

// themes are the choices the menu offers. Auto follows the system setting.
var themes = []string{"auto", "light", "dark"}

// themeOf is the theme r's page is drawn in: the cookie's when it is one of
// themes, else auto.
func themeOf(r *http.Request) string {
	if c, err := r.Cookie(ThemeCookie); err == nil && slices.Contains(themes, c.Value) {
		return c.Value
	}
	return "auto"
}

// handleTheme stores the menu's theme in a cookie and goes back to the page
// the menu was on (SPEC 17.3). Like the language, it needs no sign-in.
func (s *server) handleTheme(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	theme := r.PostFormValue("theme")
	if !slices.Contains(themes, theme) {
		s.page(w, r, http.StatusBadRequest, "", ErrorPage{Status: http.StatusBadRequest})
		return
	}
	s.savePreference(w, r, ThemeCookie, theme)
}

// savePreference keeps a user menu choice in a year-long cookie for this
// browser and sends it back to the page the menu was on.
func (s *server) savePreference(w http.ResponseWriter, r *http.Request, name, value string) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure follows the configuration, as the session cookie does
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
	to := menuReturnPath(r.PostFormValue("return"))
	if to == "" {
		to = "/"
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// menuReturnPath is where the user menu sends the browser back to:
// what returnPath allows (SPEC 14.17), and also the sign-in, setup and
// signed-out pages, which returnPath refuses only so a sign-in cannot
// return to itself. For those it returns Foreman's own path, never the
// input, keeping only a sign-in's next once it passes returnPath.
func menuReturnPath(p string) string {
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
	case PathOIDCStart, PathOIDCCallback:
		// The callback's query holds the provider's code, state and words
		// (SPEC 14.8, 14.14), so a page there returns to sign-in, never to it.
		return PathSignIn
	case PathSignIn:
		if next := returnPath(u.Query().Get("next")); next != "" {
			return PathSignIn + "?next=" + url.QueryEscape(next)
		}
		return PathSignIn
	}
	return ""
}
