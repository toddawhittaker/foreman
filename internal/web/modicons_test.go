package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Tests for the mod icon endpoint and the pages that use it (SPEC 14.5, 14.6,
// 14.10). The run's result and the zip read are stand-ins: the endpoint's
// own rules are what is under test.

const testIndexJSON = `{"mods":{"fake-planet":"1.0.0"},"icons":[
	{"kind":"item","name":"fake-widget","path":"__fake-planet__/graphics/widget.png","size":64},
	{"kind":"space-location","name":"fake-planet","path":"__fake-planet__/graphics/planet.png","size":256},
	{"kind":"starmap","name":"fake-planet","path":"__fake-planet__/graphics/map.png","size":512}]}`

func testPNG(t testing.TB, side int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, side, side))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// iconRig is a game rig whose icon lookups and zip reads are stand-ins. reads
// records each read of a zip.
type iconRig struct {
	*modsRig
	id    string
	defs  *game.ModSettingDefs
	cache bool
	run   string
	pic   []byte
	err   error

	mu    sync.Mutex
	reads []string
}

func newIconRig(t *testing.T) *iconRig {
	t.Helper()
	g := &iconRig{modsRig: newModsRig(t, false), cache: true, run: "defs:k1", pic: testPNG(t, 4)}
	g.id = g.world20("Base", "fmtest_1.0.0")
	idx, err := modsettings.DecodeIconIndex([]byte(testIndexJSON))
	if err != nil {
		t.Fatal(err)
	}
	g.defs = fmtestSettingDefs(t)
	g.defs.Icons = idx
	oldRun, oldRead := modIconRun, readModIcon
	modIconRun = func(_ *server, id string) (*game.ModSettingDefs, string, bool) {
		if id != g.id || !g.cache {
			return nil, "", false
		}
		return g.defs, g.run, true
	}
	readModIcon = func(_ *world.Manager, id, mod, version, p string, size, out int) ([]byte, error) {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.reads = append(g.reads, fmt.Sprintf("%s %s %s %s %d %d", id, mod, version, p, size, out))
		return g.pic, g.err
	}
	t.Cleanup(func() { modIconRun, readModIcon = oldRun, oldRead })
	return g
}

func (g *iconRig) icon(path string) reply {
	g.t.Helper()
	return g.do(http.MethodGet, path, nil, http.Header{"Cookie": {g.cookie.String()}})
}

func (g *iconRig) readCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.reads)
}

// SPEC 14.6: the endpoint answers a fresh PNG for an object the world's run
// lists, with the headers that keep the browser from guessing, and the path
// handed to the zip reader has the mod's prefix back.
func TestModIconServesAPicture(t *testing.T) {
	g := newIconRig(t)
	base := "/worlds/" + g.id + "/icons/"
	rep := g.icon(base + "item/fake-widget.png")
	if rep.StatusCode != http.StatusOK || rep.body != string(g.pic) {
		t.Fatalf("status %d, body %d bytes", rep.StatusCode, len(rep.body))
	}
	for k, want := range map[string]string{
		"Content-Type":           "image/png",
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "private, max-age=300",
	} {
		if got := rep.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	want := g.id + " fake-planet 1.0.0 __fake-planet__/graphics/widget.png 64 64"
	if len(g.reads) != 1 || g.reads[0] != want {
		t.Errorf("reads %q, want %q", g.reads, want)
	}
	// Icons are scaled to at most 64 pixels, star map pictures to 128.
	g.icon(base + "space-location/fake-planet.png")
	g.icon(base + "starmap/fake-planet.png")
	if got := g.reads[1:]; got[0] != g.id+" fake-planet 1.0.0 __fake-planet__/graphics/planet.png 256 64" ||
		got[1] != g.id+" fake-planet 1.0.0 __fake-planet__/graphics/map.png 512 128" {
		t.Errorf("reads %q", got)
	}
	// The tag "planet" is a space location.
	if rep := g.icon(base + "planet/fake-planet.png"); rep.StatusCode != http.StatusOK {
		t.Errorf("planet kind: %d", rep.StatusCode)
	}
}

// A second request for the same picture in the same run reads no zip; a new
// run (a new key) reads again, so a changed mod set never serves a stale
// picture.
func TestModIconIsCachedPerRun(t *testing.T) {
	g := newIconRig(t)
	p := "/worlds/" + g.id + "/icons/item/fake-widget.png"
	g.icon(p)
	g.icon(p)
	if n := g.readCount(); n != 1 {
		t.Errorf("%d reads for two requests, want 1", n)
	}
	g.run = "defs:k2"
	g.icon(p)
	if n := g.readCount(); n != 2 {
		t.Errorf("%d reads after a new run, want 2", n)
	}
}

// SPEC 14.5, 14.6: everything the request can vary is checked, and a refusal
// is a plain 404 that reads no zip.
func TestModIconRefusals(t *testing.T) {
	g := newIconRig(t)
	cases := []struct{ name, path string }{
		{"a kind outside the list", "/worlds/" + g.id + "/icons/bogus/fake-widget.png"},
		{"a kind in capitals", "/worlds/" + g.id + "/icons/Item/fake-widget.png"},
		{"a name in capitals", "/worlds/" + g.id + "/icons/item/Fake-Widget.png"},
		{"a name with a dot", "/worlds/" + g.id + "/icons/item/fake.widget.png"},
		{"a name with a space", "/worlds/" + g.id + "/icons/item/fake%20widget.png"},
		{"a name that climbs", "/worlds/" + g.id + "/icons/item/..%2f..%2fetc.png"},
		{"a backslash", "/worlds/" + g.id + "/icons/item/a%5Cb.png"},
		{"no extension", "/worlds/" + g.id + "/icons/item/fake-widget"},
		{"another extension", "/worlds/" + g.id + "/icons/item/fake-widget.svg"},
		{"an empty name", "/worlds/" + g.id + "/icons/item/.png"},
		{"a long name", "/worlds/" + g.id + "/icons/item/" + strings.Repeat("a", 101) + ".png"},
		{"an unknown world", "/worlds/nope/icons/item/fake-widget.png"},
		{"a world id that climbs", "/worlds/..%2f..%2fx/icons/item/fake-widget.png"},
		{"an object the run does not list", "/worlds/" + g.id + "/icons/item/iron-plate.png"},
		{"the wrong kind for a listed name", "/worlds/" + g.id + "/icons/fluid/fake-widget.png"},
	}
	for _, c := range cases {
		if rep := g.icon(c.path); rep.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", c.name, rep.StatusCode)
		}
	}
	if n := g.readCount(); n != 0 {
		t.Errorf("a refused request read a zip %d times", n)
	}
	// No cached run: 404 and no run is started (the stand-in is the only
	// lookup the handler uses).
	g.cache = false
	if rep := g.icon("/worlds/" + g.id + "/icons/item/fake-widget.png"); rep.StatusCode != http.StatusNotFound || g.readCount() != 0 {
		t.Errorf("not cached: %d, %d reads", rep.StatusCode, g.readCount())
	}
}

// SPEC 14.6: a user who is not signed in gets no picture, and no zip is read.
func TestModIconNeedsASignedInUser(t *testing.T) {
	g := newIconRig(t)
	rep := g.do(http.MethodGet, "/worlds/"+g.id+"/icons/item/fake-widget.png", nil, nil)
	if rep.StatusCode == http.StatusOK || g.readCount() != 0 || rep.Header.Get("Content-Type") == "image/png" {
		t.Errorf("signed out: %d, %d reads, %s", rep.StatusCode, g.readCount(), rep.Header.Get("Content-Type"))
	}
}

// A file the reader refuses or cannot find is a 404 and the page shows the
// name; a failure of Foreman's own is a 500 that says nothing of the cause.
func TestModIconReaderFailures(t *testing.T) {
	g := newIconRig(t)
	p := "/worlds/" + g.id + "/icons/item/fake-widget.png"
	for _, err := range []error{world.ErrModIconNotFound, fmt.Errorf("%w: the path is not allowed", world.ErrModIconRefused)} {
		g.err = err
		if rep := g.icon(p); rep.StatusCode != http.StatusNotFound || strings.Contains(rep.body, "path") {
			t.Errorf("%v: %d %q", err, rep.StatusCode, rep.body)
		}
	}
	g.err = errors.New("disk on fire: /srv/secret/path")
	if rep := g.icon(p); rep.StatusCode != http.StatusInternalServerError || strings.Contains(rep.body, "secret") {
		t.Errorf("a disk failure: %d %q", rep.StatusCode, rep.body)
	}
	// A failed read is not cached.
	g.err = nil
	if rep := g.icon(p); rep.StatusCode != http.StatusOK {
		t.Errorf("after the failure: %d", rep.StatusCode)
	}
}

// SPEC 14.6: one decode at a time; a few requests wait; the rest are told to
// come back.
func TestModIconDecodeSlot(t *testing.T) {
	svc := newModIconService()
	ctx := context.Background()
	if ok, busy := svc.acquire(ctx); !ok || busy {
		t.Fatal("the first request did not get the slot")
	}
	var wg sync.WaitGroup
	got := make(chan bool, modIconQueue)
	for range modIconQueue {
		wg.Go(func() {
			ok, _ := svc.acquire(ctx)
			got <- ok
		})
	}
	// Wait until every waiter is queued.
	deadline := time.Now().Add(5 * time.Second)
	for {
		svc.mu.Lock()
		n := svc.waiting
		svc.mu.Unlock()
		if n == modIconQueue {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d waiting, want %d", n, modIconQueue)
		}
		time.Sleep(time.Millisecond)
	}
	if ok, busy := svc.acquire(ctx); ok || !busy {
		t.Errorf("a request past the queue: ok %v busy %v, want busy", ok, busy)
	}
	// A waiter whose request ends leaves without the slot and is not busy.
	svc.release()
	for range modIconQueue {
		if !<-got {
			t.Error("a queued request was refused")
		}
		svc.release()
	}
	wg.Wait()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	svc.slot <- struct{}{}
	if ok, busy := svc.acquire(cancelled); ok || busy {
		t.Errorf("a cancelled request: ok %v busy %v", ok, busy)
	}
}

// SPEC 14.6: a star map asks for every modded planet's picture at once, and a
// browser never asks again for an <image> that failed, so a burst the size of a
// large mod pack's star map waits its turn and every picture is served, with
// one decode at a time. With the old queue of 8, most of these were 503.
func TestModIconStarMapBurstIsServed(t *testing.T) {
	g := newIconRig(t)
	var running, most int
	var mu sync.Mutex
	readModIcon = func(*world.Manager, string, string, string, string, int, int) ([]byte, error) {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return g.pic, nil
	}
	svc := newModIconService()
	srv := &server{store: g.store, worlds: g.worlds, log: g.slog, now: g.clock.Now}
	const burst = 40
	codes := make(chan int, burst)
	var wg sync.WaitGroup
	for i := range burst {
		wg.Go(func() {
			// Two pictures, so later requests find one cached and the other still to make.
			name := []string{"fake-planet.png", "fake-widget.png"}[i%2]
			kind := []string{"starmap", "item"}[i%2]
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/worlds/"+g.id+"/icons/"+kind+"/"+name, nil)
			r.SetPathValue("id", g.id)
			r.SetPathValue("kind", kind)
			r.SetPathValue("name", name)
			srv.handleModIcon(svc, w, r)
			codes <- w.Code
		})
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != http.StatusOK {
			t.Errorf("a request in the burst got %d, want 200", c)
		}
	}
	if most != 1 {
		t.Errorf("%d decodes ran at once, want 1", most)
	}
}

// A request past the queue gets 503 with a hint to retry, not a 404 that
// would be cached as "no icon".
func TestModIconBusyAnswers503(t *testing.T) {
	g := newIconRig(t)
	svc := newModIconService()
	svc.slot <- struct{}{}
	svc.mu.Lock()
	svc.waiting = modIconQueue
	svc.mu.Unlock()
	srv := &server{store: g.store, worlds: g.worlds, log: g.slog, now: g.clock.Now}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/worlds/"+g.id+"/icons/item/fake-widget.png", nil)
	r.SetPathValue("id", g.id)
	r.SetPathValue("kind", "item")
	r.SetPathValue("name", "fake-widget.png")
	srv.handleModIcon(svc, w, r)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") == "" {
		t.Errorf("%d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	if g.readCount() != 0 {
		t.Error("a busy request read a zip")
	}
}

// SPEC 14.6: the cache is capped by the bytes it holds, drops the least
// recently used picture first, and keeps nothing larger than the cap.
func TestIconCacheEvictsByBytes(t *testing.T) {
	c := newIconCache(100)
	key := func(n string) iconKey { return iconKey{"w", "r", "item", n} }
	c.put(key("a"), make([]byte, 40))
	c.put(key("b"), make([]byte, 40))
	if _, ok := c.get(key("a")); !ok { // a is now the most recently used
		t.Fatal("a missing")
	}
	c.put(key("c"), make([]byte, 40)) // 120 bytes: b, the least recently used, goes
	if _, ok := c.get(key("b")); ok {
		t.Error("the least recently used picture stayed")
	}
	for _, n := range []string{"a", "c"} {
		if _, ok := c.get(key(n)); !ok {
			t.Errorf("%s was evicted", n)
		}
	}
	if c.size != 80 {
		t.Errorf("size %d, want 80", c.size)
	}
	// Replacing a picture counts its new size only.
	c.put(key("a"), make([]byte, 10))
	if c.size != 50 {
		t.Errorf("size after a replace %d, want 50", c.size)
	}
	// One picture over the cap is not kept, and does not push the rest out.
	c.put(key("big"), make([]byte, 101))
	if _, ok := c.get(key("big")); ok || c.size != 50 {
		t.Errorf("an oversize picture was kept: size %d", c.size)
	}
	// A picture that exactly fills the cap pushes the others out.
	c.put(key("full"), make([]byte, 100))
	if c.size != 100 || c.order.Len() != 1 {
		t.Errorf("size %d, %d entries", c.size, c.order.Len())
	}
	// A different world, run, or kind is a different picture.
	for _, k := range []iconKey{{"w2", "r", "item", "full"}, {"w", "r2", "item", "full"}, {"w", "r", "fluid", "full"}} {
		if _, ok := c.get(k); ok {
			t.Errorf("%v answered with another key's picture", k)
		}
	}
}

// SPEC 14.6, 14.10: on the mod settings form, an object the world's mods give
// an icon shows it from Foreman, the others show the wiki's picture while that
// setting is on, and the name otherwise. The page carries Referrer-Policy:
// no-referrer only while the wiki setting is on.
func TestModSettingsFormShowsModIcons(t *testing.T) {
	g := newIconRig(t)
	g.defs.Locale = modsettings.Locale{"en": {
		"fm-sta-int": {Name: "Startup integer", Description: "Needs [item=fake-widget] and [item=iron-plate] and [planet=fake-planet]."},
	}}
	fakeDefs(t, g.defs, nil)
	g.writeSettingsFile(g.id, gameFile(t, "2.0.77"))
	form := func() (string, http.Header) {
		rep := g.do(http.MethodGet, modSettingsEditPath(g.id)+"/form", nil, http.Header{"Cookie": {g.cookie.String()}})
		return rep.body, rep.Header
	}
	widget := `<img class="rt-obj-icon" src="/worlds/` + g.id + `/icons/item/fake-widget.png" alt="Fake widget" loading="lazy" onerror="this.replaceWith(this.alt)">`
	planet := `<img class="rt-obj-icon" src="/worlds/` + g.id + `/icons/space-location/fake-planet.png" alt="Fake planet" loading="lazy" onerror="this.replaceWith(this.alt)">`
	body, _ := form()
	wantIn(t, "form with the wiki off", body, widget, planet, `>Iron plate</span>`)
	if strings.Contains(body, "wiki.factorio.com") {
		t.Error("the wiki is involved with the setting off")
	}
	if n := strings.Count(body, "<img class=\"rt-obj-icon\""); n != 2 {
		t.Errorf("%d icons with the wiki off, want 2", n)
	}
	// With the wiki on, the rest comes from the wiki; the mod's own stay.
	if rep := g.post(generalPath(), generalForm(map[string]string{"wiki_icons": "on"})); rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %d", rep.StatusCode)
	}
	body, _ = form()
	wantIn(t, "form with the wiki on", body, widget, planet, `src="https://wiki.factorio.com/images/Iron_plate.png"`)
	if n := strings.Count(body, "<img class=\"rt-obj-icon\""); n != 3 {
		t.Errorf("%d icons with the wiki on, want 3", n)
	}
	// A world with no cached run shows no mod icon and no request for one.
	g.cache = false
	body, _ = form()
	if strings.Contains(body, "/icons/") {
		t.Errorf("an icon address without a cached run:\n%s", body)
	}
}

// SPEC 14.10: pages carry Referrer-Policy: no-referrer while the wiki setting
// is on, because the star map's SVG pictures cannot carry their own; a page
// with the setting off sets none.
func TestReferrerPolicyFollowsTheWikiSetting(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	if got := hn.get(generalPath(), withCookie(c)).Header().Get("Referrer-Policy"); got != "" {
		t.Errorf("Referrer-Policy with the wiki off: %q", got)
	}
	if rec := hn.post(generalPath(), generalForm(map[string]string{"wiki_icons": "on"}), withCookie(c)); rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d", rec.Code)
	}
	for _, path := range []string{generalPath(), "/no-such-page"} {
		if got := hn.get(path, withCookie(c)).Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy with the wiki on: %q", path, got)
		}
	}
	hn.post(generalPath(), generalForm(nil), withCookie(c))
	if got := hn.get(generalPath(), withCookie(c)).Header().Get("Referrer-Policy"); got != "" {
		t.Errorf("Referrer-Policy after turning it off: %q", got)
	}
}

// SPEC 5.6, 14.6: Generate's mod settings step shows the mod's icons too, in
// its form part and in the page a browser without script waits for.
func TestFlowModSettingsShowsModIcons(t *testing.T) {
	g := newIconRig(t)
	g.makeNewMap(g.id)
	g.defs.Locale = modsettings.Locale{"en": {
		"fm-sta-int": {Name: "Startup integer", Description: "Needs [item=fake-widget]."},
	}}
	fakeDefs(t, g.defs, nil)
	g.writeSettingsFile(g.id, gameFile(t, "2.0.77"))
	widget := `src="/worlds/` + g.id + `/icons/item/fake-widget.png" alt="Fake widget"`
	body, _ := g.flowForm(g.id)
	wantIn(t, "form", body, widget)
	wantIn(t, "waited page", g.get(flowModSettingsPath(g.id)+"?wait=1"), widget)
}
