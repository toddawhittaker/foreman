package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
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
	hn.get("/", withCookie(sessionCookie(rec)), withHeader("Authorization", "Bearer sekrit-bearer"))
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
