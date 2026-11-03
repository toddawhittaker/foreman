package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeSession struct {
	userID   int64
	lastSeen time.Time
}

type fakeSessions struct {
	rows    map[string]fakeSession
	touches int
	err     error // returned by every method when set
}

func newFakeSessions() *fakeSessions { return &fakeSessions{rows: map[string]fakeSession{}} }

func (f *fakeSessions) CreateSession(_ context.Context, h string, id int64, now time.Time) error {
	if f.err != nil {
		return f.err
	}
	f.rows[h] = fakeSession{id, now}
	return nil
}

func (f *fakeSessions) SessionUser(_ context.Context, h string) (int64, time.Time, bool, error) {
	if f.err != nil {
		return 0, time.Time{}, false, f.err
	}
	s, ok := f.rows[h]
	return s.userID, s.lastSeen, ok, nil
}

func (f *fakeSessions) TouchSession(_ context.Context, h string, now time.Time) error {
	if f.err != nil {
		return f.err
	}
	f.touches++
	s := f.rows[h]
	s.lastSeen = now
	f.rows[h] = s
	return nil
}

func (f *fakeSessions) DeleteSession(_ context.Context, h string) error {
	if f.err != nil {
		return f.err
	}
	delete(f.rows, h)
	return nil
}

const idle = 24 * time.Hour

func setup(secure bool) (*SessionManager, *fakeSessions, *fakeClock) {
	st, clk := newFakeSessions(), newClock()
	return NewSessionManager(st, idle, secure, clk.Now), st, clk
}

// start signs user 7 in and returns the cookie the browser would hold.
func start(t *testing.T, m *SessionManager) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := m.Start(context.Background(), rec, 7); err != nil {
		t.Fatal(err)
	}
	cs := rec.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("got %d cookies", len(cs))
	}
	return cs[0]
}

func withCookie(c *http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(c)
	return r
}

func TestStartIssuesTokenAndStoresOnlyItsHash(t *testing.T) {
	m, st, _ := setup(false)
	c := start(t, m)
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token is not 32 bytes of base64url: %q (%v)", c.Value, err)
	}
	sum := sha256.Sum256([]byte(c.Value))
	want := hex.EncodeToString(sum[:])
	if len(st.rows) != 1 {
		t.Fatalf("%d sessions stored", len(st.rows))
	}
	for h, row := range st.rows {
		if h != want || row.userID != 7 {
			t.Errorf("stored hash %q user %d, want %q user 7", h, row.userID, want)
		}
		if strings.Contains(h, c.Value) {
			t.Error("the token itself was stored")
		}
	}
	if c2 := start(t, m); c2.Value == c.Value {
		t.Error("two sessions share a token")
	}
}

func TestCookieAttributes(t *testing.T) {
	for _, secure := range []bool{false, true} {
		m, _, _ := setup(secure)
		c := start(t, m)
		if c.Name != "foreman_session" || c.Path != "/" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Errorf("secure=%v: bad cookie %+v", secure, c)
		}
		if c.Secure != secure {
			t.Errorf("Secure=%v, want %v", c.Secure, secure)
		}
		if !c.Expires.IsZero() || c.MaxAge != 0 {
			t.Errorf("cookie must be a browser-session cookie: expires=%v maxage=%d", c.Expires, c.MaxAge)
		}
	}
}

func TestLookup(t *testing.T) {
	ctx := context.Background()
	t.Run("live session", func(t *testing.T) {
		m, _, _ := setup(false)
		id, ok, err := m.Lookup(ctx, withCookie(start(t, m)))
		if err != nil || !ok || id != 7 {
			t.Fatalf("id=%d ok=%v err=%v", id, ok, err)
		}
	})
	t.Run("no cookie", func(t *testing.T) {
		m, _, _ := setup(false)
		if _, ok, err := m.Lookup(ctx, httptest.NewRequest(http.MethodGet, "/", nil)); ok || err != nil {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	})
	t.Run("unknown, malformed and short tokens", func(t *testing.T) {
		m, _, _ := setup(false)
		other := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
		for _, v := range []string{other, "", "!!!", "abc", strings.Repeat("A", 300)} {
			if _, ok, err := m.Lookup(ctx, withCookie(&http.Cookie{Name: CookieName, Value: v, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})); ok || err != nil {
				t.Errorf("%q: ok=%v err=%v", v, ok, err)
			}
		}
	})
	t.Run("a stored hash is not a token", func(t *testing.T) {
		m, st, _ := setup(false)
		start(t, m)
		for h := range st.rows {
			if _, ok, _ := m.Lookup(ctx, withCookie(&http.Cookie{Name: CookieName, Value: h, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})); ok {
				t.Error("the hash worked as a token")
			}
		}
	})
}

func TestIdleExpiry(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		elapsed time.Duration
		live    bool
	}{
		{"just created", 0, true},
		{"almost idle", idle - time.Second, true},
		{"exactly the idle limit", idle, true},
		{"one nanosecond past", idle + time.Nanosecond, false},
		{"long past", 30 * 24 * time.Hour, false},
	}
	for _, c := range cases {
		m, st, clk := setup(false)
		ck := start(t, m)
		clk.Advance(c.elapsed)
		_, ok, err := m.Lookup(ctx, withCookie(ck))
		if err != nil || ok != c.live {
			t.Errorf("%s: ok=%v err=%v, want %v", c.name, ok, err, c.live)
		}
		if !c.live && len(st.rows) != 0 {
			t.Errorf("%s: expired session was not deleted", c.name)
		}
	}
}

func TestTouchAtMostOncePerMinute(t *testing.T) {
	ctx := context.Background()
	m, st, clk := setup(false)
	ck := start(t, m)
	steps := []struct {
		advance time.Duration
		touches int
	}{
		{0, 0},
		{30 * time.Second, 0},
		{29 * time.Second, 0}, // 59s since start
		{time.Second, 1},      // 60s: touch
		{59 * time.Second, 1}, // 59s since touch
		{time.Second, 2},      // 60s since touch
		{time.Hour, 3},
	}
	for i, s := range steps {
		clk.Advance(s.advance)
		if _, ok, err := m.Lookup(ctx, withCookie(ck)); !ok || err != nil {
			t.Fatalf("step %d: ok=%v err=%v", i, ok, err)
		}
		if st.touches != s.touches {
			t.Errorf("step %d: %d touches, want %d", i, st.touches, s.touches)
		}
	}
}

func TestTouchKeepsSessionAlive(t *testing.T) {
	ctx := context.Background()
	m, _, clk := setup(false)
	ck := start(t, m)
	// Use it every 20 hours for a week: never idle for more than a day.
	for range 8 {
		clk.Advance(20 * time.Hour)
		if _, ok, err := m.Lookup(ctx, withCookie(ck)); !ok || err != nil {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	}
}

func TestEnd(t *testing.T) {
	ctx := context.Background()
	m, st, _ := setup(true)
	ck := start(t, m)
	rec := httptest.NewRecorder()
	if err := m.End(ctx, rec, withCookie(ck)); err != nil {
		t.Fatal(err)
	}
	if len(st.rows) != 0 {
		t.Error("session was not deleted")
	}
	if _, ok, _ := m.Lookup(ctx, withCookie(ck)); ok {
		t.Error("session still works after sign-out")
	}
	cs := rec.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != CookieName || cs[0].Value != "" || cs[0].MaxAge >= 0 ||
		!cs[0].HttpOnly || !cs[0].Secure || cs[0].Path != "/" {
		t.Errorf("cookie not cleared properly: %+v", cs)
	}
	// Signing out with no session still clears the cookie and succeeds.
	rec = httptest.NewRecorder()
	if err := m.End(ctx, rec, httptest.NewRequest(http.MethodPost, "/", nil)); err != nil || len(rec.Result().Cookies()) != 1 {
		t.Errorf("err=%v cookies=%d", err, len(rec.Result().Cookies()))
	}
}

func TestStoreErrorsDoNotLeakTheToken(t *testing.T) {
	ctx := context.Background()
	m, st, clk := setup(false)
	ck := start(t, m)
	sum := sha256.Sum256([]byte(ck.Value))
	hash := hex.EncodeToString(sum[:])
	boom := errors.New("database is down")
	check := func(what string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: no error", what)
			return
		}
		if !errors.Is(err, boom) {
			t.Errorf("%s: store error not wrapped", what)
		}
		if s := err.Error(); strings.Contains(s, ck.Value) || strings.Contains(s, hash) {
			t.Errorf("%s: error mentions the token or its hash: %s", what, s)
		}
	}

	st.err = boom
	_, _, err := m.Lookup(ctx, withCookie(ck))
	check("lookup", err)
	check("start", m.Start(ctx, httptest.NewRecorder(), 1))
	check("end", m.End(ctx, httptest.NewRecorder(), withCookie(ck)))

	// Writes that fail after a good lookup: the expired-delete and touch paths.
	st.err = nil
	m2 := NewSessionManager(&failingWrites{fakeSessions: st, err: boom}, idle, false, clk.Now)
	clk.Advance(2 * time.Minute)
	_, _, err = m2.Lookup(ctx, withCookie(ck))
	check("touch", err)
	clk.Advance(idle)
	_, _, err = m2.Lookup(ctx, withCookie(ck))
	check("expired delete", err)
}

// failingWrites lets SessionUser succeed but fails writes.
type failingWrites struct {
	*fakeSessions
	err error
}

func (f *failingWrites) TouchSession(context.Context, string, time.Time) error { return f.err }
func (f *failingWrites) DeleteSession(context.Context, string) error           { return f.err }
