package docker

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// fakeDaemon answers the three calls this client makes and records every
// request it sees, so tests can check what reached the daemon.
type fakeDaemon struct {
	t          *testing.T
	apiVersion string
	mu         sync.Mutex
	seen       []string // "METHOD path" as received, version prefix included
}

var versionPrefix = regexp.MustCompile(`^/v\d+\.\d+/`)

func (f *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.seen = append(f.seen, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	path := versionPrefix.ReplaceAllString(r.URL.Path, "/")
	if !Allowed(r.Method, path) {
		f.t.Errorf("daemon received %s %s, which is outside SPEC 14.3", r.Method, r.URL.Path)
	}
	switch path {
	case "/_ping":
		_, _ = w.Write([]byte("OK"))
	case "/version":
		_, _ = w.Write([]byte(`{"Version":"29.0.0","ApiVersion":"` + f.apiVersion + `"}`))
	case "/containers/self/json":
		_, _ = w.Write([]byte(`{"Id":"abc","Mounts":[{"Type":"bind","Source":"/srv/foreman","Destination":"/data"}],
			"NetworkSettings":{"Networks":{"game":{"IPAddress":"172.18.0.2"},"bridge":{}}}}`))
	default:
		http.Error(w, `{"message":"No such container"}`, http.StatusNotFound)
	}
}

func newFake(t *testing.T, apiVersion string) (*Client, *fakeDaemon) {
	t.Helper()
	f := &fakeDaemon{t: t, apiVersion: apiVersion}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := New("tcp://" + strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}

func TestEveryMethodStaysOnAllowlist(t *testing.T) {
	c, f := newFake(t, "1.47")
	ctx := context.Background()
	v, err := c.Version(ctx)
	if err != nil || v.APIVersion != "1.47" {
		t.Fatalf("Version = %+v, %v", v, err)
	}
	got, err := c.InspectContainer(ctx, "self")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Mounts) != 1 || got.Mounts[0].Source != "/srv/foreman" || got.Mounts[0].Destination != "/data" {
		t.Errorf("mounts = %+v", got.Mounts)
	}
	want := []string{"GET /version", "GET /version", "GET /v1.47/containers/self/json"}
	if strings.Join(f.seen, "|") != strings.Join(want, "|") {
		t.Errorf("daemon saw %v, want %v", f.seen, want)
	}
}

func TestInspectNotFound(t *testing.T) {
	c, _ := newFake(t, "1.47")
	_, err := c.InspectContainer(context.Background(), "missing")
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusNotFound {
		t.Fatalf("want a 404 APIError, got %v", err)
	}
}

func TestRefusesOutsideAllowlistWithoutSending(t *testing.T) {
	c, f := newFake(t, "1.47")
	cases := []struct{ method, path string }{
		{"POST", "/containers/x/exec"},
		{"POST", "/exec/x/start"},
		{"GET", "/volumes"},
		{"POST", "/networks/create"},
		{"POST", "/networks/x/connect"},
		{"DELETE", "/images/factorio"},
		{"POST", "/build"},
		{"GET", "/system/df"},
		{"POST", "/auth"},
		{"GET", "/containers/x/../../volumes/json"},
		{"GET", "/containers/x%2Fy/json"},
		{"POST", "/containers/x/json"},
		{"GET", "/containers//json"},
		{"GET", "/images//json"},
		{"GET", "/containers/./json"},
		{"GET", "/./version"},
		{"POST", "/containers/x/./start"},
		{"GET", "/images/./json"},
		{"GET", "/images/a/./b/json"},
	}
	for _, tc := range cases {
		resp, err := c.do(context.Background(), request{method: tc.method, path: tc.path})
		if err == nil {
			_ = resp.Body.Close()
			t.Errorf("%s %s was allowed", tc.method, tc.path)
		}
	}
	if len(f.seen) != 0 {
		t.Errorf("refused calls reached the daemon: %v", f.seen)
	}
}

func TestRefusesRedirects(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/version" {
			http.Redirect(w, r, "/volumes", http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := New("tcp://" + strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Version(context.Background()); err == nil {
		t.Fatal("a redirect was followed")
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(seen, "|") != "/version" {
		t.Errorf("daemon saw %v, want only /version", seen)
	}
}

func TestUnixClientRefusesRedirects(t *testing.T) {
	c, err := New("unix:///nonexistent.sock")
	if err != nil {
		t.Fatal(err)
	}
	if c.http.CheckRedirect == nil {
		t.Fatal("unix client follows redirects")
	}
	if err := c.http.CheckRedirect(nil, nil); err == nil {
		t.Error("CheckRedirect allowed a redirect")
	}
}

func TestAllowlistCoversSpec14_3(t *testing.T) {
	cases := []struct{ method, path string }{
		{"GET", "/_ping"},
		{"GET", "/version"},
		{"GET", "/containers/json"},
		{"GET", "/containers/abc/json"},
		{"POST", "/containers/abc/start"},
		{"POST", "/containers/abc/stop"},
		{"POST", "/containers/abc/kill"},
		{"GET", "/containers/abc/logs"},
		{"GET", "/containers/abc/stats"},
		{"GET", "/events"},
		{"POST", "/images/create"},
		{"GET", "/images/factoriotools/factorio:2.0.55/json"},
		{"POST", "/containers/create"},
		{"DELETE", "/containers/abc"},
	}
	for _, tc := range cases {
		if !Allowed(tc.method, tc.path) {
			t.Errorf("%s %s should be allowed", tc.method, tc.path)
		}
	}
}

func TestNegotiate(t *testing.T) {
	cases := []struct {
		daemon, want string
		wantErr      bool
	}{
		{"1.44", "1.44", false},
		{"1.47", "1.47", false},
		{MaxAPIVersion, MaxAPIVersion, false},
		{"1.99", MaxAPIVersion, false},
		{"2.0", MaxAPIVersion, false},
		{"1.43", "", true},
		{"1.9", "", true},
		{"", "", true},
		{"one.two", "", true},
	}
	for _, tc := range cases {
		got, err := negotiate(tc.daemon)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("negotiate(%q) = %q, %v", tc.daemon, got, err)
		}
	}
}

func TestOldDaemonRefusedBeforeInspect(t *testing.T) {
	c, f := newFake(t, "1.41")
	_, err := c.InspectContainer(context.Background(), "self")
	if err == nil || !strings.Contains(err.Error(), "upgrade Docker Engine") {
		t.Fatalf("want plain refusal, got %v", err)
	}
	if strings.Join(f.seen, "|") != "GET /version" {
		t.Errorf("daemon saw %v", f.seen)
	}
}

func TestUnixSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "docker.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	srv := httptest.NewUnstartedServer(&fakeDaemon{t: t, apiVersion: "1.52"})
	srv.Listener = l
	srv.Start()
	t.Cleanup(srv.Close)
	c, err := New("unix://" + sock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Version(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestNewRejectsBadHosts(t *testing.T) {
	for _, h := range []string{"", "http://x", "unix://", "tcp://", "npipe:////./pipe/docker"} {
		if _, err := New(h); err == nil {
			t.Errorf("New(%q) succeeded", h)
		}
	}
}

func TestLongCallsHaveNoTotalTimeout(t *testing.T) {
	for _, host := range []string{"unix:///x.sock", "tcp://127.0.0.1:1"} {
		c, err := New(host)
		if err != nil {
			t.Fatal(err)
		}
		if c.http.Timeout != shortCallLimit {
			t.Errorf("%s: short client timeout = %v", host, c.http.Timeout)
		}
		if c.long.Timeout != 0 {
			t.Errorf("%s: long client timeout = %v, want none", host, c.long.Timeout)
		}
		if c.long.CheckRedirect == nil || c.long.CheckRedirect(nil, nil) == nil {
			t.Errorf("%s: long client follows redirects", host)
		}
	}
}

func TestErrorMessage(t *testing.T) {
	cases := map[string]string{
		`{"message":"No such container: x"}`: "No such container: x",
		"  plain text from a proxy \n":       "plain text from a proxy",
		`{"other":1}`:                        `{"other":1}`,
		"":                                   "",
	}
	for in, want := range cases {
		if got := errorMessage([]byte(in)); got != want {
			t.Errorf("errorMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAPIErrorIs(t *testing.T) {
	cases := []struct {
		status int
		is     error
	}{
		{404, ErrNotFound}, {304, ErrNotModified}, {409, ErrConflict},
	}
	for _, tc := range cases {
		err := error(&APIError{Status: tc.status})
		for _, named := range []error{ErrNotFound, ErrNotModified, ErrConflict} {
			if want := errors.Is(named, tc.is); errors.Is(err, named) != want {
				t.Errorf("status %d: errors.Is(%v) should be %v", tc.status, named, want)
			}
		}
	}
	if errors.Is(&APIError{Status: 500}, ErrNotFound) {
		t.Error("a 500 matched ErrNotFound")
	}
}

func TestQueryDoesNotWidenThePath(t *testing.T) {
	c, f := newFake(t, "1.47")
	// This small fake has no container list, so the answer is a 404; only
	// the request it received matters here.
	resp, _ := c.do(context.Background(), request{
		method: "GET", path: "/containers/json",
		query: url.Values{"filters": {`{"label":["a=b"]}`}, "x": {"/volumes?"}},
	})
	if resp != nil {
		_ = resp.Body.Close()
	}
	if got := f.seen[len(f.seen)-1]; got != "GET /v1.47/containers/json" {
		t.Errorf("daemon saw %q", got)
	}
}
