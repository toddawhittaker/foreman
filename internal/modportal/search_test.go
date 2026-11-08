package modportal

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSearchRecordedList(t *testing.T) {
	p, c, _ := fakePortal(t)
	ctx := context.Background()
	for _, tc := range []struct{ query, want string }{
		{"flib", "flib"},
		{"FLOW CONTROL", "Flow Control"},
		{"krastorio", "Krastorio2Assets,Krastorio2"},  // both prefix; more downloads first
		{"krastorio2", "Krastorio2,Krastorio2Assets"}, // exact name first
		{"aai", "aai-containers,aai-industry"},        // prefix, by downloads
		{"armor", "Power Armor MK3"},                  // substring of the name, any case
		{"planets", "visible-planets"},                // name
		{"in space", "visible-planets"},               // title only
		{"  flib  ", "flib"},                          // spaces around the query
		{"no such mod anywhere", ""},
	} {
		hits, err := c.Search(ctx, "2.0", tc.query)
		if err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		if got := names(hits); got != tc.want {
			t.Errorf("%q: got %q, want %q", tc.query, got, tc.want)
		}
	}
	if p.ListCalls() != 1 {
		t.Errorf("the list was fetched %d times for one line, want 1", p.ListCalls())
	}
}

func TestSearchHitFields(t *testing.T) {
	_, c, _ := fakePortal(t)
	hits, err := c.Search(context.Background(), "2.0", "flib")
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits %v, err %v", hits, err)
	}
	h := hits[0]
	if h.Name != "flib" || h.Title == "" || h.Summary == "" || h.LatestVersion == "" ||
		h.FactorioVersion != "2.0" || h.Downloads != 1082004 {
		t.Errorf("unexpected hit %+v", h)
	}
}

func TestSearchGroupOrder(t *testing.T) {
	c := handlerClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(listBody([]listRow{
			{name: "zzaa", title: "Zz", downloads: 9000},               // contains, in name
			{name: "other", title: "About AA things", downloads: 8000}, // contains, in title
			{name: "aab", title: "B", downloads: 50},                   // prefix
			{name: "aac", title: "C", downloads: 70},                   // prefix
			{name: "AA", title: "A", downloads: 1},                     // exact, any case
			{name: "unrelated", title: "U", downloads: 99999},
			{name: "aad", title: "D", downloads: 70}, // ties go by name
			{name: "gone", title: "aa", downloads: 5, noRelease: true},
		}))
	})
	hits, err := c.Search(context.Background(), "2.0", "aa")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(hits), "AA,aac,aad,aab,zzaa,other"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Every match comes back, best first, so the page can say how many there
// are and page through them (SPEC 9.1).
func TestSearchEveryMatch(t *testing.T) {
	c := handlerClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(listBody(manyRows(40)))
	})
	hits, err := c.Search(context.Background(), "2.0", "mod")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 40 || hits[0].Name != "mod-39" || hits[39].Name != "mod-00" {
		t.Errorf("got %d hits, first %q, last %q", len(hits), hits[0].Name, hits[len(hits)-1].Name)
	}
}

func TestSearchRequestIsKeyless(t *testing.T) {
	var path, query, auth string
	c := handlerClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, query, auth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		_, _ = w.Write(listBody(nil))
	})
	if _, err := c.Search(context.Background(), "2.1", "x"); err != nil {
		t.Fatal(err)
	}
	if path != "/api/mods" || query != "page_size=max&version=2.1" || auth != "" {
		t.Errorf("request was %q ? %q, auth %q", path, query, auth)
	}
}

func TestSearchEmptyQueryAndBadLineAskNothing(t *testing.T) {
	p, c, _ := fakePortal(t)
	if hits, err := c.Search(context.Background(), "2.0", "  "); err != nil || hits != nil {
		t.Errorf("empty query: %v, %v", hits, err)
	}
	for _, line := range []string{"", "2", "2.0.1", "2.x", "../2.0", "2.0&a=b", "2."} {
		if _, err := c.Search(context.Background(), line, "flib"); err == nil {
			t.Errorf("line %q was accepted", line)
		}
	}
	if p.ListCalls() != 0 {
		t.Errorf("%d requests were made", p.ListCalls())
	}
}

func TestSearchCache(t *testing.T) {
	p, c, clk := fakePortal(t)
	ctx := context.Background()
	search := func(line string) {
		t.Helper()
		if _, err := c.Search(ctx, line, "flib"); err != nil {
			t.Fatal(err)
		}
	}
	search("2.0")
	search("2.0")
	if p.ListCalls() != 1 {
		t.Fatalf("a second search asked the portal again (%d)", p.ListCalls())
	}
	search("1.1")
	if p.ListCalls() != 2 {
		t.Fatalf("another line must be fetched on its own (%d)", p.ListCalls())
	}
	clk.Advance(14*time.Minute + 59*time.Second)
	search("2.0")
	if p.ListCalls() != 2 {
		t.Fatalf("the list expired early (%d)", p.ListCalls())
	}
	clk.Advance(2 * time.Second)
	search("2.0")
	if p.ListCalls() != 3 {
		t.Fatalf("the list did not expire after 15 minutes (%d)", p.ListCalls())
	}
}

func TestSearchNeverCachesAnError(t *testing.T) {
	p, c, _ := fakePortal(t)
	p.FailAPI(http.StatusInternalServerError)
	if _, err := c.Search(context.Background(), "2.0", "flib"); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("got %v, want ErrUnreachable", err)
	}
	p.FailAPI(0)
	hits, err := c.Search(context.Background(), "2.0", "flib")
	if err != nil || len(hits) != 1 {
		t.Fatalf("after recovery: %v, %v", hits, err)
	}
	if p.ListCalls() != 2 {
		t.Errorf("calls %d, want 2", p.ListCalls())
	}
}

func TestSearchFailures(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"not json": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		},
		"too large": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"results":[{"name":"` + strings.Repeat("a", maxList) + `"}]}`))
		},
	} {
		_, err := handlerClient(t, h).Search(context.Background(), "2.0", "a")
		if !errors.Is(err, ErrUnreachable) {
			t.Errorf("%s: got %v, want ErrUnreachable", name, err)
		}
	}
}

func TestSearchTimesOut(t *testing.T) {
	c := handlerClient(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	c.callTimeout = 100 * time.Millisecond
	start := time.Now()
	_, err := c.Search(context.Background(), "2.0", "a")
	if !errors.Is(err, ErrUnreachable) || time.Since(start) > 3*time.Second {
		t.Errorf("got %v after %v", err, time.Since(start))
	}
}

func TestSearchPortalDown(t *testing.T) {
	c := NewForTest("http://127.0.0.1:1")
	if _, err := c.Search(context.Background(), "2.0", "a"); !errors.Is(err, ErrUnreachable) {
		t.Errorf("got %v, want ErrUnreachable", err)
	}
}

func TestLimitsAreTheRulings(t *testing.T) {
	c := New()
	if c.base != "https://mods.factorio.com" || c.callTimeout != 10*time.Second ||
		c.downloadTimeout != 5*time.Minute || c.maxDownload != 200<<20 ||
		maxList != 16<<20 || maxFull != 2<<20 || cacheLifetime != 15*time.Minute ||
		maxRedirects != 2 || cap(c.full) != 4 {
		t.Errorf("a limit differs from the rulings: %+v", c)
	}
}
