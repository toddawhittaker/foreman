package web

import (
	"bufio"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// SPEC 16.3, DESIGN 6 (Shell), #736: a signed-in page without a stream of
// its own streams only the status strip's state; the Server page keeps its
// own stream; a signed-out page streams nothing.
func TestStripStream(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()

	open := func(cookie bool) *http.Response {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, g.url+PathStripEvents, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cookie {
			req.AddCookie(g.cookie)
		}
		resp, err := g.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	anon := open(false)
	_ = anon.Body.Close()
	if anon.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous strip stream: %d", anon.StatusCode)
	}

	resp := open(true)
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	want := func(word string) {
		t.Helper()
		name, data, err := readEvent(t, r)
		if err != nil {
			t.Fatal(err)
		}
		lamp := `<span class="lamp lamp--` + strings.ToLower(word) + `"></span>&nbsp;` + word
		only := `<span id="site-state" hx-swap-oob="innerHTML">` + lamp + `</span><span id="nav-lamp" hx-swap-oob="innerHTML">` + lamp + `</span>`
		if name != stripEvent || data != only {
			t.Fatalf("event %q: %s\nwant only the strip and the tab: %s", name, data, only)
		}
	}
	want("Running")
	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"0"}}))
	// The stop passes through Stopping on its way.
	deadline := time.Now().Add(10 * time.Second)
	for {
		name, data, err := readEvent(t, r)
		if err != nil {
			t.Fatal(err)
		}
		if name != stripEvent {
			t.Fatalf("event %q", name)
		}
		if strings.Contains(data, "lamp--stopped") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no Stopped")
		}
	}
}

// The layout opens the strip's stream only on signed-in pages without one.
func TestStripStreamOnPages(t *testing.T) {
	g := newGameRig(t, image21)
	g.startRunning()
	worlds := g.get(PathWorlds)
	if !strings.Contains(worlds, `<div hidden hx-ext="sse" sse-connect="`+PathStripEvents+`" sse-swap="strip" hx-swap="none"></div>`) {
		t.Error("Worlds does not stream the strip")
	}
	server := g.get(PathServer)
	if strings.Contains(server, PathStripEvents) || strings.Count(server, "sse-connect=") != 1 {
		t.Error("the Server page must keep its one stream")
	}
	rec := g.harness.get(PathSignIn)
	if strings.Contains(rec.Body.String(), "sse-connect") {
		t.Error("a signed-out page streams")
	}
}
