package releases

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tag list becomes the version picker (SPEC 10.7): only exact patch
// releases of the supported lines (10.8), newest first.
var wantFixtureVersions = []string{
	"2.1.24", "2.1.23", "2.1.10", "2.1.9",
	"2.0.77", "2.0.72", "2.0.9",
	"1.1.110", "1.1.9",
}

func TestVersionsReturnsOnlySupportedPatchReleasesNewestFirst(t *testing.T) {
	hub := newHub(t)
	c := testClient(hub.URL(), "http://unused.invalid/", nil)

	got, err := c.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wantFixtureVersions) {
		t.Fatalf("versions\n got %q\nwant %q", got, wantFixtureVersions)
	}
}

func TestVersionsNeverReturnsAHostileOrMalformedName(t *testing.T) {
	hub := newHub(t)
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	got, err := c.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range got {
		for _, bad := range []string{";", "/", "..", "$", " ", "\n", "rootless", "stable", "latest", "rc", "２"} {
			if strings.Contains(v, bad) {
				t.Errorf("version %q contains %q", v, bad)
			}
		}
		if strings.Count(v, ".") != 2 {
			t.Errorf("version %q is not major.minor.patch", v)
		}
	}
	seen := map[string]bool{}
	for _, v := range got {
		if seen[v] {
			t.Errorf("version %q listed twice", v)
		}
		seen[v] = true
	}
}

func TestVersionsLeavesOutUnsupportedLines(t *testing.T) {
	hub := newHub(t)
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	got, _ := c.Versions(context.Background())
	for _, bad := range []string{"2.2.0", "3.0.1", "12.0.1", "0.2.0", "11.1.0", "0.17.79"} {
		for _, v := range got {
			if v == bad {
				t.Errorf("unsupported version %q came back", bad)
			}
		}
	}
}

func TestVersionsSortsNumericallyNotAsText(t *testing.T) {
	hub := newHub(t)
	hub.serve = func(w http.ResponseWriter, name string, page int) {
		body := `{"next":null,"results":[]}`
		if name == "2.1." {
			body = `{"next":null,"results":[{"name":"2.1.9"},{"name":"2.1.100"},{"name":"2.1.10"},{"name":"2.1.2"}]}`
		}
		_, _ = w.Write([]byte(body))
	}
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	got, err := c.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2.1.100", "2.1.10", "2.1.9", "2.1.2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestVersionsAsksDockerHubOncePerSupportedLine(t *testing.T) {
	hub := newHub(t)
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	if _, err := c.Versions(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The 2.1 fixture has a second page; the others are one page each.
	for name, want := range map[string]int{"1.1.": 1, "2.0.": 1, "2.1.": 2} {
		if got := hub.count(name); got != want {
			t.Errorf("requests for name=%s: got %d, want %d", name, got, want)
		}
	}
	for _, r := range hub.requests() {
		if r.Method != http.MethodGet || r.Path != hubPath {
			t.Errorf("unexpected request %s %s", r.Method, r.Path)
		}
		if r.PageSize != "100" {
			t.Errorf("page_size = %q, want 100", r.PageSize)
		}
		if r.HasAuth {
			t.Error("a request carried an Authorization header")
		}
		if r.Name != "1.1." && r.Name != "2.0." && r.Name != "2.1." {
			t.Errorf("unexpected name filter %q", r.Name)
		}
	}
	var pages []int
	for _, r := range hub.requests() {
		if r.Name == "2.1." {
			pages = append(pages, r.Page)
		}
	}
	if !reflect.DeepEqual(pages, []int{1, 2}) {
		t.Errorf("2.1 pages asked for: %v, want [1 2]", pages)
	}
}

// 14.10: a hostile "next" must not send Foreman anywhere else.
func TestVersionsNeverFollowsANextURLToAnotherHost(t *testing.T) {
	evil := newStub(t, http.StatusOK, []byte(`{"next":null,"results":[{"name":"2.1.99"}]}`))
	for _, next := range []string{
		evil.URL + hubPath + "?page=2",
		evil.URL,
		"file:///etc/passwd",
		"http://169.254.169.254/latest/meta-data/",
		"//" + strings.TrimPrefix(evil.URL, "http://") + "/x",
	} {
		t.Run(next, func(t *testing.T) {
			before := evil.hits.Load()
			hub := newHub(t)
			hub.next = func(string, int) string { return next }
			c := testClient(hub.URL(), "http://unused.invalid/", nil)

			got, _ := c.Versions(context.Background())
			if n := evil.hits.Load() - before; n != 0 {
				t.Fatalf("the hostile next URL was requested %d times", n)
			}
			for _, v := range got {
				if v == "2.1.99" {
					t.Fatal("a version from the hostile host came back")
				}
			}
			// Page numbers are Foreman's own and stay on the configured host.
			for _, r := range hub.requests() {
				if r.Path != hubPath {
					t.Errorf("request to unexpected path %q", r.Path)
				}
			}
		})
	}
}

func TestVersionsStopsAtTenPagesPerLine(t *testing.T) {
	hub := newHub(t)
	hub.serve = func(w http.ResponseWriter, name string, page int) {
		// Every page claims there is another one.
		fmt.Fprintf(w, `{"next":%q,"results":[{"name":"2.1.%d"}]}`, hub.next(name, page), page)
	}
	c := testClient(hub.URL(), "http://unused.invalid/", nil)

	// Whether the capped result is an error or a partial list is not
	// settled; the request count is.
	_, _ = c.Versions(context.Background())
	for _, name := range []string{"1.1.", "2.0.", "2.1."} {
		if got := hub.count(name); got != 10 {
			t.Errorf("requests for name=%s: got %d, want exactly 10", name, got)
		}
	}
	for _, r := range hub.requests() {
		if r.Page < 1 || r.Page > 10 {
			t.Errorf("page %d is outside 1..10", r.Page)
		}
	}
}

func TestVersionsStopsWhenNextIsNull(t *testing.T) {
	hub := newHub(t)
	hub.serve = func(w http.ResponseWriter, name string, page int) {
		_, _ = w.Write([]byte(`{"next":null,"results":[{"name":"2.1.1"}]}`))
	}
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	if _, err := c.Versions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(hub.requests()); got != 3 {
		t.Errorf("got %d requests, want 3 (one per line)", got)
	}
}

// 14.10: redirects are refused, even from a plain http.Client.
func TestVersionsRefusesRedirects(t *testing.T) {
	target := newStub(t, http.StatusOK, fixture(t, "hub_2_1_2.json"))
	redir := redirectTo(t, target.URL+hubPath)
	c := testClient(redir.URL+hubPath, "http://unused.invalid/", nil)

	_, err := c.Versions(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if n := target.hits.Load(); n != 0 {
		t.Fatalf("the redirect target was requested %d times", n)
	}
}

func TestVersionsFailuresAreErrUnreachableAndNeverLeakTheBody(t *testing.T) {
	big := pad(`{"next":null,"results":[]`, 2*1024*1024+1024)
	cases := map[string]struct {
		status int
		body   []byte
	}{
		"server error":    {500, []byte(bodyMarker)},
		"not found":       {404, []byte(`{"message":"` + bodyMarker + `"}`)},
		"rate limited":    {429, []byte(bodyMarker)},
		"redirect status": {301, []byte(bodyMarker)},
		"bad JSON":        {200, []byte(`{"results": [` + bodyMarker)},
		"not JSON":        {200, []byte("<html>" + bodyMarker + "</html>")},
		"empty body":      {200, nil},
		"wrong shape":     {200, []byte(`{"results":"` + bodyMarker + `"}`)},
		"oversize page":   {200, big},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			hub := newHub(t)
			hub.serve = func(w http.ResponseWriter, _ string, _ int) {
				w.Header().Set("Content-Length", strconv.Itoa(len(tc.body)))
				w.WriteHeader(tc.status)
				_, _ = w.Write(tc.body)
			}
			c := testClient(hub.URL(), "http://unused.invalid/", nil)
			got, err := c.Versions(context.Background())
			if !errors.Is(err, ErrUnreachable) {
				t.Fatalf("err = %v (versions %q), want ErrUnreachable", err, got)
			}
			if strings.Contains(err.Error(), bodyMarker) {
				t.Fatalf("error text contains the upstream body: %v", err)
			}
		})
	}
}

// The limit must hold when the server sends no Content-Length.
func TestVersionsOversizeChunkedPageIsRefused(t *testing.T) {
	hub := newHub(t)
	hub.serve = func(w http.ResponseWriter, _ string, _ int) {
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`{"next":null,"results":[],"pad":"` + bodyMarker))
		fl.Flush()
		chunk := []byte(strings.Repeat("x", 64*1024))
		for i := 0; i < 48; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			fl.Flush()
		}
		_, _ = w.Write([]byte(`"}`))
	}
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	_, err := c.Versions(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if strings.Contains(err.Error(), bodyMarker) {
		t.Fatalf("error text contains the upstream body: %v", err)
	}
}

func TestVersionsAcceptsAPageWellUnderTheLimit(t *testing.T) {
	hub := newHub(t)
	hub.serve = func(w http.ResponseWriter, name string, _ int) {
		if name == "2.1." {
			_, _ = w.Write(pad(`{"next":null,"results":[{"name":"2.1.5"}]`, 1024*1024))
			return
		}
		_, _ = w.Write([]byte(`{"next":null,"results":[]}`))
	}
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	got, err := c.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"2.1.5"}) {
		t.Fatalf("got %q", got)
	}
}

func TestVersionsFailsWholeWhenOneLineOrPageFails(t *testing.T) {
	cases := map[string]func(w http.ResponseWriter, name string, page int) bool{
		"second line fails": func(w http.ResponseWriter, name string, page int) bool {
			if name == "2.0." {
				w.WriteHeader(500)
				return true
			}
			return false
		},
		"second page fails": func(w http.ResponseWriter, name string, page int) bool {
			if name == "2.1." && page == 2 {
				w.WriteHeader(502)
				return true
			}
			return false
		},
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			hub := newHub(t)
			def := hub.serve
			hub.serve = func(w http.ResponseWriter, n string, p int) {
				if !fail(w, n, p) {
					def(w, n, p)
				}
			}
			c := testClient(hub.URL(), "http://unused.invalid/", nil)
			got, err := c.Versions(context.Background())
			if !errors.Is(err, ErrUnreachable) {
				t.Fatalf("err = %v, want ErrUnreachable (got %q)", err, got)
			}
		})
	}
}

func TestVersionsTimeoutIsErrUnreachable(t *testing.T) {
	release := make(chan struct{})
	hub := newHub(t)
	hub.before = func() { <-release }
	// Registered after the hub's own cleanup, so it runs first and frees
	// the blocked handler before the server closes.
	t.Cleanup(func() { close(release) })

	hc := &http.Client{Timeout: 100 * time.Millisecond}
	c := newClient(hub.URL(), "http://unused.invalid/", hc, newFakeClock().Now)
	start := time.Now()
	_, err := c.Versions(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("did not give up promptly")
	}
}

func TestVersionsUnreachableHostIsErrUnreachable(t *testing.T) {
	hub := newHub(t)
	url := hub.URL()
	hub.Server.Close()
	c := testClient(url, "http://unused.invalid/", nil)
	_, err := c.Versions(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}

func TestVersionsCancelledContextIsErrUnreachableWithoutARequest(t *testing.T) {
	hub := newHub(t)
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Versions(ctx)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if n := len(hub.requests()); n != 0 {
		t.Fatalf("%d requests with a cancelled context", n)
	}
}

// 10.7: nothing is fetched until a page asks.
func TestNewAndNewClientMakeNoRequest(t *testing.T) {
	hub := newHub(t)
	stub := newStub(t, 200, fixture(t, "latest.json"))
	_ = testClient(hub.URL(), stub.URL, nil)
	_ = New()
	time.Sleep(50 * time.Millisecond)
	if n := len(hub.requests()) + int(stub.hits.Load()); n != 0 {
		t.Fatalf("%d requests before any call", n)
	}
}

func TestVersionsConcurrentCallsMakeOneFetch(t *testing.T) {
	gate := make(chan struct{})
	hub := newHub(t)
	hub.before = func() { <-gate }
	c := testClient(hub.URL(), "http://unused.invalid/", nil)

	const callers = 12
	results := make([][]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = c.Versions(context.Background())
		}()
	}
	// Give every caller time to reach the client while the first is held.
	time.Sleep(150 * time.Millisecond)
	close(gate)
	wg.Wait()

	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if !reflect.DeepEqual(results[i], wantFixtureVersions) {
			t.Fatalf("caller %d got %q", i, results[i])
		}
	}
	for name, want := range map[string]int{"1.1.": 1, "2.0.": 1, "2.1.": 2} {
		if got := hub.count(name); got != want {
			t.Errorf("requests for name=%s: got %d, want %d", name, got, want)
		}
	}
}
