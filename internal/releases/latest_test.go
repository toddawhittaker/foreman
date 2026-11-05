package releases

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var wantFixtureLatest = Latest{Stable: "2.0.77", Experimental: "2.1.24"}

// 10.2: the stable and experimental headless versions come from factorio.com.
func TestLatestReadsTheHeadlessVersions(t *testing.T) {
	stub := newStub(t, http.StatusOK, fixture(t, "latest.json"))
	c := testClient("http://unused.invalid/", stub.URL, nil)

	got, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != wantFixtureLatest {
		t.Fatalf("got %+v, want %+v", got, wantFixtureLatest)
	}
	if n := stub.hits.Load(); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

func TestLatestDoesNotAskDockerHub(t *testing.T) {
	hub := newHub(t)
	stub := newStub(t, http.StatusOK, fixture(t, "latest.json"))
	c := testClient(hub.URL(), stub.URL, nil)
	if _, err := c.Latest(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(hub.requests()); n != 0 {
		t.Fatalf("Latest made %d Docker Hub requests", n)
	}
}

// The values reach pages and image tags, so a bad one must not come back.
func TestLatestNeverReturnsAHostileVersion(t *testing.T) {
	for _, bad := range []string{"2.0.77; rm -rf", "../x", "$(id)", "2.0.77\n", "<script>", "2.0", "latest", ""} {
		t.Run(strconv.Quote(bad), func(t *testing.T) {
			body := `{"stable":{"headless":` + strconv.Quote(bad) + `},"experimental":{"headless":` + strconv.Quote(bad) + `}}`
			stub := newStub(t, http.StatusOK, []byte(body))
			c := testClient("http://unused.invalid/", stub.URL, nil)

			got, err := c.Latest(context.Background())
			if err != nil {
				if !errors.Is(err, ErrUnreachable) {
					t.Fatalf("err = %v, want ErrUnreachable or a clean empty result", err)
				}
				return
			}
			if got.Stable != "" || got.Experimental != "" {
				t.Fatalf("hostile value came back: %+v", got)
			}
		})
	}
}

// 14.10: redirects are refused by the client New builds.
func TestLatestRefusesRedirects(t *testing.T) {
	target := newStub(t, http.StatusOK, fixture(t, "latest.json"))
	redir := redirectTo(t, target.URL)
	c := productionClient("http://unused.invalid/", redir.URL)

	_, err := c.Latest(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if n := target.hits.Load(); n != 0 {
		t.Fatalf("the redirect target was requested %d times", n)
	}
}

func TestLatestFailuresAreErrUnreachableAndNeverLeakTheBody(t *testing.T) {
	valid := `{"stable":{"headless":"2.0.77"},"experimental":{"headless":"2.1.24"}`
	cases := map[string]struct {
		status int
		body   []byte
	}{
		"server error":         {500, []byte(bodyMarker)},
		"not found":            {404, []byte(`{"error":"` + bodyMarker + `"}`)},
		"bad JSON":             {200, []byte(`{"stable": {` + bodyMarker)},
		"not JSON":             {200, []byte("<html>" + bodyMarker + "</html>")},
		"empty body":           {200, nil},
		"wrong shape":          {200, []byte(`{"stable":"` + bodyMarker + `"}`)},
		"just over 64 KB":      {200, pad(valid, 64*1024+64)},
		"far over 64 KB":       {200, pad(valid, 512*1024)},
		"error status and big": {500, pad(valid, 512*1024)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stub := newStub(t, tc.status, tc.body)
			c := testClient("http://unused.invalid/", stub.URL, nil)
			got, err := c.Latest(context.Background())
			if !errors.Is(err, ErrUnreachable) {
				t.Fatalf("err = %v (latest %+v), want ErrUnreachable", err, got)
			}
			if strings.Contains(err.Error(), bodyMarker) {
				t.Fatalf("error text contains the upstream body: %v", err)
			}
		})
	}
}

func TestLatestAcceptsABodyWellUnderTheLimit(t *testing.T) {
	body := pad(`{"stable":{"headless":"2.0.77"},"experimental":{"headless":"2.1.24"}`, 32*1024)
	stub := newStub(t, http.StatusOK, body)
	c := testClient("http://unused.invalid/", stub.URL, nil)
	got, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != wantFixtureLatest {
		t.Fatalf("got %+v", got)
	}
}

func TestLatestTimeoutIsErrUnreachable(t *testing.T) {
	release := make(chan struct{})
	stub := newStub(t, http.StatusOK, fixture(t, "latest.json"))
	stub.before = func() { <-release }
	// Runs before the stub's own close, which waits for open handlers.
	t.Cleanup(func() { close(release) })

	hc := &http.Client{Timeout: 100 * time.Millisecond}
	c := newClient("http://unused.invalid/", stub.URL, hc, newFakeClock().Now)
	_, err := c.Latest(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}

func TestLatestCancelledContextIsErrUnreachableWithoutARequest(t *testing.T) {
	stub := newStub(t, http.StatusOK, fixture(t, "latest.json"))
	c := testClient("http://unused.invalid/", stub.URL, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Latest(ctx)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	if n := stub.hits.Load(); n != 0 {
		t.Fatalf("%d requests with a cancelled context", n)
	}
}

func TestLatestConcurrentCallsMakeOneFetch(t *testing.T) {
	gate := make(chan struct{})
	stub := newStub(t, http.StatusOK, fixture(t, "latest.json"))
	stub.before = func() { <-gate }
	c := testClient("http://unused.invalid/", stub.URL, nil)

	const callers = 12
	results := make([]Latest, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = c.Latest(context.Background())
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
		if results[i] != wantFixtureLatest {
			t.Fatalf("caller %d got %+v", i, results[i])
		}
	}
	if n := stub.hits.Load(); n != 1 {
		t.Fatalf("%d upstream fetches, want 1", n)
	}
}
