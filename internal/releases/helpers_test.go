package releases

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const hubPath = "/v2/repositories/factoriotools/factorio/tags"

// bodyMarker is planted in every upstream body that must never reach an
// error message.
const bodyMarker = "UPSTREAM-BODY-MARKER-9f3a"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeClock is the injected "now"; tests move it by hand.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// hubRequest is one request the fake Docker Hub saw.
type hubRequest struct {
	Method, Path, Name, PageSize string
	Page                         int
	HasAuth                      bool
}

// hubFake stands in for Docker Hub's tag list. serve writes the reply for a
// line name such as "2.1." and a page number; the default serves the
// testdata fixtures.
type hubFake struct {
	*httptest.Server
	serve func(w http.ResponseWriter, name string, page int)
	// next gives the "next" URL for fixture pages; default is the same host.
	next func(name string, page int) string
	// before runs on every request ahead of the reply.
	before func()

	mu   sync.Mutex
	reqs []hubRequest
}

func newHub(t *testing.T) *hubFake {
	t.Helper()
	h := &hubFake{}
	h.serve = h.serveFixture
	h.next = func(name string, page int) string {
		return h.URL() + "?name=" + name + "&page_size=100&page=" + strconv.Itoa(page+1)
	}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		h.mu.Lock()
		h.reqs = append(h.reqs, hubRequest{
			Method: r.Method, Path: r.URL.Path,
			Name: r.URL.Query().Get("name"), PageSize: r.URL.Query().Get("page_size"),
			Page: page, HasAuth: r.Header.Get("Authorization") != "",
		})
		h.mu.Unlock()
		if h.before != nil {
			h.before()
		}
		h.serve(w, r.URL.Query().Get("name"), page)
	}))
	t.Cleanup(h.Close)
	return h
}

// URL is the tags endpoint to hand to newClient.
func (h *hubFake) URL() string { return h.Server.URL + hubPath }

func (h *hubFake) serveFixture(w http.ResponseWriter, name string, page int) {
	file := "hub_" + strings.ReplaceAll(strings.TrimSuffix(name, "."), ".", "_") + "_" + strconv.Itoa(page) + ".json"
	b, err := os.ReadFile(filepath.Join("testdata", file)) //nolint:gosec // a fixed test fixture
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"next":null,"results":[]}`))
		return
	}
	b = bytes.ReplaceAll(b, []byte("@NEXT@"), []byte(h.next(name, page)))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b) //nolint:gosec // a fixed test fixture, not user input
}

func (h *hubFake) requests() []hubRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hubRequest(nil), h.reqs...)
}

// count is how many requests asked for this line name, such as "2.1.".
func (h *hubFake) count(name string) int {
	n := 0
	for _, r := range h.requests() {
		if r.Name == name {
			n++
		}
	}
	return n
}

// lineStub serves one fixed reply and counts hits; the latest-releases
// fake and every redirect target use it.
type lineStub struct {
	*httptest.Server
	hits atomic.Int32

	mu     sync.Mutex
	status int
	body   []byte
	// before runs on every request ahead of the reply.
	before func()
}

func newStub(t *testing.T, status int, body []byte) *lineStub {
	t.Helper()
	s := &lineStub{status: status, body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		if s.before != nil {
			s.before()
		}
		s.mu.Lock()
		status, body := s.status, s.body
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *lineStub) set(status int, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.body = status, body
}

// redirectTo makes a server that answers every request with a 302.
func redirectTo(t *testing.T, target string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testClient builds a client over a plain http.Client with a short timeout.
// Redirect refusal is New's job and is tested through productionClient.
func testClient(hubURL, latestURL string, clock *fakeClock) *Client {
	if clock == nil {
		clock = newFakeClock()
	}
	return newClient(hubURL, latestURL, &http.Client{Timeout: 3 * time.Second}, clock.Now)
}

// pad grows a JSON object body to at least size bytes with a filler field,
// so the body stays valid JSON.
func pad(prefix string, size int) []byte {
	var b bytes.Buffer
	b.WriteString(prefix)
	b.WriteString(`,"pad":"`)
	b.WriteString(bodyMarker)
	for b.Len() < size {
		b.WriteString("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	}
	b.WriteString(`"}`)
	return b.Bytes()
}

// productionClient is what New builds, pointed at test servers, so the
// redirect policy under test is the shipped one.
func productionClient(hubURL, latestURL string) *Client {
	c := New()
	c.hubURL, c.latestURL = hubURL, latestURL
	return c
}
