package releases

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"
)

// SPEC 10.2 and 10.7: both lists are cached for 5 minutes and errors are not cached,
// so an admin is never told "Docker Hub is down" for longer than it was.

func TestVersionsAreCachedForFiveMinutes(t *testing.T) {
	hub := newHub(t)
	clock := newFakeClock()
	c := testClient(hub.URL(), "http://unused.invalid/", clock)

	if _, err := c.Versions(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := len(hub.requests())

	clock.Advance(4*time.Minute + 59*time.Second)
	got, err := c.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(hub.requests()); n != first {
		t.Fatalf("a call inside 5 minutes made %d new requests", n-first)
	}
	if !reflect.DeepEqual(got, wantFixtureVersions) {
		t.Fatalf("cached result differs: %q", got)
	}
}

func TestVersionsAreFetchedAgainAfterFiveMinutes(t *testing.T) {
	hub := newHub(t)
	clock := newFakeClock()
	c := testClient(hub.URL(), "http://unused.invalid/", clock)
	if _, err := c.Versions(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := len(hub.requests())

	// A new release appears upstream.
	def := hub.serve
	hub.serve = func(w http.ResponseWriter, name string, page int) {
		if name == "2.1." && page == 1 {
			_, _ = w.Write([]byte(`{"next":null,"results":[{"name":"2.1.25"},{"name":"2.1.24"}]}`))
			return
		}
		if name == "2.1." {
			_, _ = w.Write([]byte(`{"next":null,"results":[]}`))
			return
		}
		def(w, name, page)
	}
	clock.Advance(5*time.Minute + time.Second)

	got, err := c.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(hub.requests()) <= first {
		t.Fatal("no new request after the cache expired")
	}
	if len(got) == 0 || got[0] != "2.1.25" {
		t.Fatalf("stale list after expiry: %q", got)
	}
}

func TestVersionsErrorsAreNotCached(t *testing.T) {
	hub := newHub(t)
	def := hub.serve
	hub.serve = func(w http.ResponseWriter, name string, page int) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	c := testClient(hub.URL(), "http://unused.invalid/", nil)

	if _, err := c.Versions(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	// Docker Hub recovers; the clock has not moved.
	hub.serve = def
	got, err := c.Versions(context.Background())
	if err != nil {
		t.Fatalf("the earlier error was cached: %v", err)
	}
	if !reflect.DeepEqual(got, wantFixtureVersions) {
		t.Fatalf("got %q", got)
	}
}

func TestVersionsPartialFailureCachesNothing(t *testing.T) {
	hub := newHub(t)
	def := hub.serve
	// The 2.0 line fails while the others succeed.
	hub.serve = func(w http.ResponseWriter, name string, page int) {
		if name == "2.0." {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		def(w, name, page)
	}
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	if _, err := c.Versions(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	hub.serve = def
	got, err := c.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wantFixtureVersions) {
		t.Fatalf("a partial list was cached: %q", got)
	}
}

// A caller who edits the returned slice must not change what the next
// caller sees.
func TestVersionsReturnedSliceIsACopy(t *testing.T) {
	hub := newHub(t)
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	// The first call fills the cache and the second is served from it. Edit
	// both results, then check a third caller still sees the real list.
	for range 2 {
		got, err := c.Versions(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for i := range got {
			got[i] = "tampered"
		}
	}
	second, err := c.Versions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second, wantFixtureVersions) {
		t.Fatalf("cache was changed through the returned slice: %q", second)
	}
}

func TestLatestIsCachedForFiveMinutes(t *testing.T) {
	stub := newStub(t, http.StatusOK, fixture(t, "latest.json"))
	clock := newFakeClock()
	c := testClient("http://unused.invalid/", stub.URL, clock)

	for range 3 {
		if _, err := c.Latest(context.Background()); err != nil {
			t.Fatal(err)
		}
		clock.Advance(90 * time.Second)
	}
	// 0s, 90s, 180s: all inside five minutes of the first fetch.
	if n := stub.hits.Load(); n != 1 {
		t.Fatalf("%d fetches inside 5 minutes, want 1", n)
	}

	clock.Advance(3 * time.Minute) // now 7m30s after the first fetch
	stub.set(http.StatusOK, []byte(`{"stable":{"headless":"2.0.78"},"experimental":{"headless":"2.1.25"}}`))
	got, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n := stub.hits.Load(); n != 2 {
		t.Fatalf("%d fetches after expiry, want 2", n)
	}
	if got != (Latest{Stable: "2.0.78", Experimental: "2.1.25"}) {
		t.Fatalf("stale latest after expiry: %+v", got)
	}
}

func TestLatestErrorsAreNotCached(t *testing.T) {
	stub := newStub(t, http.StatusInternalServerError, []byte(bodyMarker))
	c := testClient("http://unused.invalid/", stub.URL, nil)

	if _, err := c.Latest(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
	stub.set(http.StatusOK, fixture(t, "latest.json"))
	got, err := c.Latest(context.Background())
	if err != nil {
		t.Fatalf("the earlier error was cached: %v", err)
	}
	if got != wantFixtureLatest {
		t.Fatalf("got %+v", got)
	}
}

// The two lists have separate caches and separate failures.
func TestAFailingLatestDoesNotSpoilVersionsOrTheOtherWayRound(t *testing.T) {
	hub := newHub(t)
	stub := newStub(t, http.StatusInternalServerError, nil)
	c := testClient(hub.URL(), stub.URL, nil)

	if _, err := c.Latest(context.Background()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("Latest err = %v, want ErrUnreachable", err)
	}
	got, err := c.Versions(context.Background())
	if err != nil {
		t.Fatalf("Versions failed because Latest did: %v", err)
	}
	if !reflect.DeepEqual(got, wantFixtureVersions) {
		t.Fatalf("got %q", got)
	}
	if n := stub.hits.Load(); n != 1 {
		t.Fatalf("Versions touched the latest-releases URL (%d hits)", n)
	}
}
