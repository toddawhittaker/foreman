package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/auth"
)

func logLines(t *testing.T, hn *harness) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(hn.logs.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func TestAccessLogHasOneLinePerRequestWithoutQuery(t *testing.T) {
	hn := newHarness(t, nil)
	hn.get("/nope?token=hunter2")
	lines := logLines(t, hn)
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1: %s", len(lines), hn.logs.String())
	}
	l := lines[0]
	if l["level"] != "INFO" || l["method"] != "GET" || l["path"] != "/nope" || l["status"] != float64(404) || l["client"] != "192.0.2.10" {
		t.Errorf("log line: %v", l)
	}
	if _, ok := l["duration_ms"]; !ok {
		t.Error("no duration")
	}
	if strings.Contains(hn.logs.String(), "hunter2") {
		t.Error("the query string reached the log")
	}
}

// The username is left out: users often type a password into it.
func TestSignInFailureIsLoggedWithClientOnly(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	hn.post(PathSignIn, signInForm("alice", "wrong password"))
	var found bool
	for _, l := range logLines(t, hn) {
		if l["msg"] == "sign-in failed" {
			found = true
			if l["level"] != "INFO" || l["username"] != nil || l["client"] != "192.0.2.10" {
				t.Errorf("failure line: %v", l)
			}
		}
	}
	if !found {
		t.Error("no sign-in failure line")
	}
}

func TestLogsNeverHoldSecrets(t *testing.T) {
	hn := newHarness(t, nil)
	const wrong = "wrong password entirely"
	rec := hn.post(PathSetup, url.Values{"username": {"alice"}, "password": {testPassword}, "confirm": {testPassword}})
	setupToken := sessionCookie(rec).Value
	hn.get(PathHome, withCookie(sessionCookie(rec)), withHeader("Authorization", "Bearer sekrit-bearer"))
	hn.post(PathSignOut, nil, withCookie(sessionCookie(rec)))
	in := hn.post(PathSignIn, signInForm("alice", testPassword))
	signInToken := sessionCookie(in).Value
	hn.post(PathSignIn, signInForm("alice", wrong))
	hn.post(PathSignIn, signInForm("nobody", wrong))

	hash := hn.passwordHash("alice")
	logs := hn.logs.String()
	for name, secret := range map[string]string{
		"password": testPassword, "wrong password": wrong, "setup token": setupToken,
		"sign-in token": signInToken, "hash": hash, "header": "sekrit-bearer",
	} {
		if secret == "" {
			t.Fatalf("%s is empty, so the check proves nothing", name)
		}
		if strings.Contains(logs, secret) {
			t.Errorf("the log contains the %s", name)
		}
	}
}

func TestPanicBecomes500AndIsLogged(t *testing.T) {
	hn := newHarness(t, nil)
	s := &server{cfg: hn.cfg, log: hn.slog, render: mustRenderer(t)}
	h := s.accessLog(s.recoverPanic(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "Something went wrong") {
		t.Fatalf("got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("the panic text reached the page")
	}
	var sawPanic, sawStatus bool
	for _, l := range logLines(t, hn) {
		sawPanic = sawPanic || (l["level"] == "ERROR" && l["panic"] == "boom")
		sawStatus = sawStatus || (l["msg"] == "request" && l["status"] == float64(500))
	}
	if !sawPanic || !sawStatus {
		t.Errorf("panic logged %v, request logged as 500 %v", sawPanic, sawStatus)
	}
}

func mustRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A route's lifted deadlines and larger body limit are for signed-in
// requests only: anyone else keeps the short deadlines and the 64 KB cap
// (SPEC 14.6).
func TestLongRoutesRelaxLimitsOnlyForSignedInRequests(t *testing.T) {
	hn := newHarness(t, nil)
	cookie := hn.signIn()
	s := &server{
		cfg: hn.cfg, store: hn.store, log: hn.slog, now: hn.clock.Now,
		sessions:     auth.NewSessionManager(hn.store, hn.cfg.SessionIdle, hn.cfg.SecureCookies(), hn.clock.Now),
		mux:          http.NewServeMux(),
		uploadLimits: map[string]int64{},
	}
	const limit = 1 << 20
	probe := func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		w.Header().Set("X-Read", strconv.FormatInt(n, 10))
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		}
	}
	s.handleUpload("POST /up", limit, probe)
	s.handleLong("GET /down", probe)
	s.mux.HandleFunc("POST /form", probe)
	h := s.limitBody(s.mux)

	do := func(method, path string, size int, signedIn bool) (rec *httptest.ResponseRecorder, lifted int) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(strings.Repeat("x", size)))
		r.RemoteAddr = peerUntrusted
		if signedIn {
			r.AddCookie(cookie)
		}
		spy := &deadlineSpy{ResponseRecorder: httptest.NewRecorder()}
		h.ServeHTTP(spy, r)
		return spy.ResponseRecorder, spy.lifted
	}

	// Signed in: the fixed deadlines give way to idle ones and the upload limit applies.
	if rec, lifted := do("POST", "/up", 512<<10, true); rec.Code != 200 || rec.Header().Get("X-Read") != strconv.Itoa(512<<10) || lifted < 2 {
		t.Errorf("signed-in upload under its limit: %d, read %s, lifted %d", rec.Code, rec.Header().Get("X-Read"), lifted)
	}
	if rec, _ := do("POST", "/up", limit+1, true); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("signed-in upload over its limit: %d", rec.Code)
	}
	if _, lifted := do("GET", "/down", 0, true); lifted < 2 {
		t.Errorf("signed-in download changed %d deadlines, want at least 2", lifted)
	}

	// Anonymous: nothing lifted, and the body stops at the form cap.
	rec, lifted := do("POST", "/up", 512<<10, false)
	if rec.Code != http.StatusRequestEntityTooLarge || lifted != 0 {
		t.Errorf("anonymous upload: %d, lifted %d", rec.Code, lifted)
	}
	if read, _ := strconv.Atoi(rec.Header().Get("X-Read")); read > maxFormBytes {
		t.Errorf("anonymous upload read %d bytes, over the %d cap", read, maxFormBytes)
	}
	if _, lifted := do("GET", "/down", 0, false); lifted != 0 {
		t.Errorf("anonymous download lifted %d deadlines", lifted)
	}

	// An ordinary form keeps the small cap for everyone.
	if rec, _ := do("POST", "/form", 512<<10, true); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("form over %d bytes: %d", maxFormBytes, rec.Code)
	}
}

// The real routes refuse an anonymous request without lifting a deadline.
func TestSaveRoutesRefuseAnonymousWithoutLiftingDeadlines(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	id, _ := savesWorld(t, hn)
	body, ct := multipartBody(t, "save", "a.zip", bytes.Repeat([]byte("z"), 200<<10))

	for name, r := range map[string]*http.Request{
		"download": httptest.NewRequest(http.MethodGet, "/worlds/"+id+"/saves/new.zip", nil),
		"upload": func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/worlds/"+id+"/saves", body)
			r.Header.Set("Content-Type", ct)
			return r
		}(),
	} {
		r.RemoteAddr = peerUntrusted
		spy := &deadlineSpy{ResponseRecorder: httptest.NewRecorder()}
		hn.h.ServeHTTP(spy, r)
		if spy.Code != http.StatusSeeOther || spy.lifted != 0 {
			t.Errorf("anonymous %s: %d, lifted %d", name, spy.Code, spy.lifted)
		}
	}
}
