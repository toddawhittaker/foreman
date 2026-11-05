package releases

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// tagStub serves one tag's reply at hubPath/<version> and counts requests.
func tagStub(t *testing.T, body string) (*Client, *atomic.Int32, *atomic.Value) {
	t.Helper()
	var hits atomic.Int32
	var path atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		path.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newClient(srv.URL+hubPath, srv.URL+"/latest", srv.Client(), newFakeClock().Now), &hits, &path
}

// SPEC 3.7: the switch page names the size of the download; it comes from
// the tag's own record on Docker Hub, asked once per version.
func TestImageSizeReadsTheTagAndKeepsIt(t *testing.T) {
	c, hits, path := tagStub(t, `{"name":"2.0.76","full_size":345678901}`)
	for range 2 {
		n, err := c.ImageSize(context.Background(), "2.0.76")
		if err != nil || n != 345678901 {
			t.Fatalf("got %d, %v", n, err)
		}
	}
	if hits.Load() != 1 || path.Load() != hubPath+"/2.0.76" {
		t.Errorf("%d requests, the last to %v, want one to the tag", hits.Load(), path.Load())
	}
}

// Anything wrong is ErrUnreachable, never the body, and is not kept.
func TestImageSizeFailures(t *testing.T) {
	for _, body := range []string{`{"full_size":0}`, `not json ` + bodyMarker, `{}`} {
		c, hits, _ := tagStub(t, body)
		for range 2 {
			_, err := c.ImageSize(context.Background(), "2.0.76")
			if !errors.Is(err, ErrUnreachable) || strings.Contains(err.Error(), bodyMarker) {
				t.Errorf("%q: got %v", body, err)
			}
		}
		if hits.Load() != 2 {
			t.Errorf("%q: %d requests, want a failure not to be kept", body, hits.Load())
		}
	}
}

// A version that is not a supported release never reaches the network, so
// it cannot change the path asked for.
func TestImageSizeRefusesABadVersion(t *testing.T) {
	c, hits, _ := tagStub(t, `{"full_size":1}`)
	for _, v := range []string{"../../x", "2.0", "latest", "3.0.1"} {
		if _, err := c.ImageSize(context.Background(), v); !errors.Is(err, ErrUnreachable) {
			t.Errorf("%q: got %v", v, err)
		}
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests for bad versions", hits.Load())
	}
}
