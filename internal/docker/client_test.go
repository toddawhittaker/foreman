package docker

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
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
	if !allowed(r.Method, path) {
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
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
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
	if strings.Join(got.Networks, ",") != "bridge,game" {
		t.Errorf("networks = %v", got.Networks)
	}
	want := []string{"GET /_ping", "GET /version", "GET /version", "GET /v1.47/containers/self/json"}
	if strings.Join(f.seen, "|") != strings.Join(want, "|") {
		t.Errorf("daemon saw %v, want %v", f.seen, want)
	}
}

func TestInspectNotFound(t *testing.T) {
	c, _ := newFake(t, "1.47")
	_, err := c.InspectContainer(context.Background(), "missing")
	if !IsNotFound(err) {
		t.Fatalf("want not found, got %v", err)
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
	}
	for _, tc := range cases {
		resp, err := c.do(context.Background(), tc.method, tc.path, true)
		if err == nil {
			_ = resp.Body.Close()
			t.Errorf("%s %s was allowed", tc.method, tc.path)
		}
	}
	if len(f.seen) != 0 {
		t.Errorf("refused calls reached the daemon: %v", f.seen)
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
		if !allowed(tc.method, tc.path) {
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
	if err := c.Ping(context.Background()); err != nil {
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
