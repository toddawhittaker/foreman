package modportal

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.t = f.t.Add(d)
}

// fakePortal starts the fake portal and a client for it, with a clock the
// test moves by hand.
func fakePortal(t *testing.T) (*modportaltest.Portal, *Client, *fakeClock) {
	t.Helper()
	p := modportaltest.New(t)
	c := NewForTest(p.URL)
	clk := &fakeClock{t: time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)}
	c.now = clk.Now
	return p, c, clk
}

// handlerClient is a client for a handler the test writes itself.
func handlerClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewForTest(srv.URL)
}

// tempFile is an empty file the download writes to.
func tempFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "mod-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func fileSize(t *testing.T, f *os.File) int64 {
	t.Helper()
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

// listRow is one entry of a portal mod list, in the portal's shape.
type listRow struct {
	name, title string
	downloads   int
	noRelease   bool
}

func listBody(rows []listRow) []byte {
	var results []map[string]any
	for _, r := range rows {
		m := map[string]any{
			"name": r.name, "title": r.title, "summary": "summary of " + r.name,
			"downloads_count": r.downloads,
		}
		if !r.noRelease {
			m["latest_release"] = map[string]any{
				"version":   "1.2.3",
				"info_json": map[string]any{"factorio_version": "2.0"},
			}
		}
		results = append(results, m)
	}
	b, _ := json.Marshal(map[string]any{"pagination": nil, "results": results})
	return b
}

func names(hits []Hit) string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Name)
	}
	return strings.Join(out, ",")
}

func mustNoSecret(t *testing.T, where string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error", where)
	}
	s := err.Error()
	for _, bad := range []string{modportaltest.Token, "token=", "username="} {
		if strings.Contains(s, bad) {
			t.Fatalf("%s: error %q contains %q", where, s, bad)
		}
	}
}

func manyRows(n int) []listRow {
	var rows []listRow
	for i := 0; i < n; i++ {
		rows = append(rows, listRow{name: fmt.Sprintf("mod-%02d", i), title: "T", downloads: i})
	}
	return rows
}
