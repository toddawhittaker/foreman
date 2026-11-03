package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	testPassword = "correct horse battery"
	// peerUntrusted and peerProxy are direct peers; only the second lies
	// inside the trusted range the header tests configure.
	peerUntrusted = "192.0.2.10:4000"
	peerProxy     = "10.0.0.5:4000"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// syncBuffer is a log sink that is safe for the handler's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type harness struct {
	t      *testing.T
	h      http.Handler
	store  *store.Store
	clock  *fakeClock
	logs   *syncBuffer
	cfg    *config.Config
	worlds *world.Manager
	slog   *slog.Logger
}

// newHarness builds the handler over a real store in a temporary directory.
// env holds FOREMAN_ settings on top of the defaults.
func newHarness(t *testing.T, env map[string]string) *harness {
	t.Helper()
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "foreman.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clock := &fakeClock{t: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	data := t.TempDir()
	worlds, err := world.NewManager(world.Dirs{
		Worlds:    filepath.Join(data, "worlds"),
		Snapshots: filepath.Join(data, "snapshots"),
		Imports:   filepath.Join(data, "import"),
	}, st, clock.Now)
	if err != nil {
		t.Fatalf("world manager: %v", err)
	}
	h, err := NewHandler(Deps{Config: cfg, Store: st, Worlds: worlds, Log: logger, Now: clock.Now})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	return &harness{t: t, h: h, store: st, clock: clock, logs: logs, cfg: cfg, worlds: worlds, slog: logger}
}

func (hn *harness) serve(r *http.Request) *httptest.ResponseRecorder {
	hn.t.Helper()
	if r.RemoteAddr == "192.0.2.1:1234" { // httptest's default
		r.RemoteAddr = peerUntrusted
	}
	rec := httptest.NewRecorder()
	hn.h.ServeHTTP(rec, r)
	return rec
}

func (hn *harness) get(path string, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	hn.t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for _, m := range mods {
		m(r)
	}
	return hn.serve(r)
}

func (hn *harness) post(path string, form url.Values, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	hn.t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, m := range mods {
		m(r)
	}
	return hn.serve(r)
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(c) }
}

func withPeer(addr string) func(*http.Request) {
	return func(r *http.Request) { r.RemoteAddr = addr }
}

func withHeader(k, v string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set(k, v) }
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	return nil
}

func (hn *harness) wantRedirect(rec *httptest.ResponseRecorder, to string) {
	hn.t.Helper()
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != to {
		hn.t.Fatalf("got %d to %q, want 303 to %q", rec.Code, rec.Header().Get("Location"), to)
	}
}

// addUser stores a local account directly.
func (hn *harness) addUser(name, password string) store.User {
	hn.t.Helper()
	hash, err := auth.Hash(password)
	if err != nil {
		hn.t.Fatal(err)
	}
	u, err := hn.store.CreateUser(context.Background(), name, hash, hn.clock.Now())
	if err != nil {
		hn.t.Fatal(err)
	}
	return u
}

func (hn *harness) passwordHash(name string) string {
	hn.t.Helper()
	u, ok, err := hn.store.UserByUsername(context.Background(), name)
	if err != nil || !ok {
		hn.t.Fatalf("user %q: found %v, err %v", name, ok, err)
	}
	return u.PasswordHash
}

func (hn *harness) userCount() int {
	hn.t.Helper()
	var n int
	for _, name := range []string{"alice", "bob", "carol", "admin"} {
		if _, ok, err := hn.store.UserByUsername(context.Background(), name); err != nil {
			hn.t.Fatal(err)
		} else if ok {
			n++
		}
	}
	return n
}

func TestHealthzAndUnknownPaths(t *testing.T) {
	hn := newHarness(t, nil)
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, HealthPath, http.StatusOK},
		{http.MethodPost, HealthPath, http.StatusMethodNotAllowed},
		{http.MethodGet, "/nope", http.StatusNotFound},
		{http.MethodPost, "/nope", http.StatusNotFound},
		{http.MethodGet, PathSignedOut, http.StatusOK},
		{http.MethodGet, PathSignOut, http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		rec := hn.serve(httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s: got %d, want %d", c.method, c.path, rec.Code, c.want)
		}
	}
	rec := hn.get("/nope")
	if !strings.Contains(rec.Body.String(), "Page not found") {
		t.Error("the 404 response is not the designed error page")
	}
	if rec := hn.get(PathStatic + "foreman.css"); rec.Code != http.StatusOK {
		t.Errorf("static css: got %d", rec.Code)
	}
}

// SPEC 12.6: behind a proxy that rewrites Host, a browser that sends no
// Sec-Fetch-Site header is only let in when its Origin is the public URL.
func TestPublicURLOriginIsTrustedForPosts(t *testing.T) {
	post := func(env map[string]string, origin string) int {
		hn := newHarness(t, env)
		hn.addUser("alice", testPassword)
		return hn.post(PathSignIn, signInForm("alice", testPassword), withHeader("Origin", origin)).Code
	}
	public := map[string]string{"FOREMAN_PUBLIC_URL": "https://foreman.example.com/"}
	if got := post(public, "https://foreman.example.com"); got != http.StatusSeeOther {
		t.Errorf("public origin with FOREMAN_PUBLIC_URL set: %d, want 303", got)
	}
	if got := post(public, "https://evil.example"); got != http.StatusForbidden {
		t.Errorf("foreign origin with FOREMAN_PUBLIC_URL set: %d, want 403", got)
	}
	if got := post(nil, "https://foreman.example.com"); got != http.StatusForbidden {
		t.Errorf("public origin without FOREMAN_PUBLIC_URL: %d, want 403", got)
	}
}

func TestBodyOver64KiBIsRefused(t *testing.T) {
	hn := newHarness(t, nil)
	rec := hn.post(PathSetup, url.Values{"username": {"admin"}, "password": {strings.Repeat("x", 70<<10)}})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", rec.Code)
	}
	if hn.userCount() != 0 {
		t.Error("an oversized setup created a user")
	}
}
