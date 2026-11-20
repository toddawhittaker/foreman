package updatecheck

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		tag, running, want string
	}{
		{"v1.3.0", "1.2.0", "1.3.0"},
		{"1.3.0", "v1.2.0", "1.3.0"},
		{"v1.2.1", "v1.2.0-5-gabc123", "1.2.1"},
		{"v2.0.0", "1.9.9", "2.0.0"},
		{"v1.10.0", "1.9.0", "1.10.0"},     // numbers, not text, are compared
		{"v1.3.0<script>", "1.2.0", ""},    // "v1.3.0script" after cleaning
		{"v1.2.0", "v1.2.0-5-gabc123", ""}, // commits past the same tag
		{"v1.2.0", "1.2.0", ""},
		{"v1.1.9", "1.2.0", ""},
		{"", "1.2.0", ""},
		{"v1.3", "1.2.0", ""},
		{"vX.Y.Z", "1.2.0", ""},
		{"v1.3.0", "", ""},
		{"v1.3.0", "abc1234", ""},
	}
	for _, c := range cases {
		got, ok := Newer(c.tag, c.running)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("Newer(%q, %q) = %q, %v; want %q", c.tag, c.running, got, ok, c.want)
		}
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func on(context.Context) bool  { return true }
func off(context.Context) bool { return false }

func testChecker(url string, enabled func(context.Context) bool) *Checker {
	c := New("v1.2.0", enabled, quiet())
	c.url = url
	return c
}

func TestCheckFindsNewer(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, `{"tag_name":"v1.3.0","html_url":"https://evil.example"}`)
	}))
	defer srv.Close()
	c := testChecker(srv.URL, on)
	c.checkOnce(context.Background())
	if got := c.Newer(context.Background()); got != "1.3.0" {
		t.Fatalf("Newer = %q, want 1.3.0", got)
	}
	if ua != "Foreman/v1.2.0" {
		t.Errorf("User-Agent = %q", ua)
	}
}

// A failure keeps the last good answer.
func TestCheckFailures(t *testing.T) {
	big := `{"tag_name":"v9.0.0","x":"` + strings.Repeat("a", maxBody) + `"}`
	cases := map[string]http.HandlerFunc{
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		},
		"oversized": func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, big) },
		"bad JSON":  func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"tag_name":`) },
		"status":    func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.Handle("/", h)
			mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"tag_name":"v9.0.0"}`)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()
			c := testChecker(srv.URL+"/", on)
			c.latest = "1.2.5"
			c.checkOnce(context.Background())
			if got := c.Newer(context.Background()); got != "1.2.5" {
				t.Fatalf("Newer = %q, want the last good answer", got)
			}
		})
	}
}

func TestCheckTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c := testChecker(srv.URL, on)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.fetch(ctx); err == nil {
		t.Fatal("a reply that never comes should fail")
	}
}

func TestSwitchOffHidesAndSkips(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"tag_name":"v1.3.0"}`)
	}))
	defer srv.Close()
	c := testChecker(srv.URL, off)
	c.latest = "1.3.0"
	c.checkOnce(context.Background())
	if calls.Load() != 0 {
		t.Error("a check ran with the switch off")
	}
	if got := c.Newer(context.Background()); got != "" {
		t.Errorf("Newer = %q with the switch off", got)
	}
}

func TestRunSchedule(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"tag_name":"v1.3.0"}`)
	}))
	defer srv.Close()
	c := testChecker(srv.URL, on)
	c.first, c.every = 10*time.Millisecond, 20*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if calls.Load() < 3 {
		t.Fatalf("checks = %d, want repeated checks", calls.Load())
	}
}

func TestRunSkipsBuildWithoutVersion(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	c := testChecker(srv.URL, on)
	c.running = ""
	c.first = time.Millisecond
	done := make(chan struct{})
	go func() { c.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept going for a build with no version")
	}
	if calls.Load() != 0 {
		t.Error("a build with no version asked GitHub")
	}
}
