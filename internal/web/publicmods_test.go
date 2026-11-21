package web

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Tests for the public mods page's backend (SPEC 9.16, 14.18).

// publicRig is a mods rig with a running world called Base whose mods
// folder holds the given zips, and the public page switched on.
type publicRig struct {
	*modsRig
	id string
}

// newPublicRig starts the world first and adds the mods after, so the
// pre-start check has nothing to say about them.
func newPublicRig(t *testing.T, zips ...string) *publicRig {
	t.Helper()
	g := newModsRig(t, false)
	id := g.world20("Base")
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage(running)
	for _, z := range zips {
		n, v, _ := strings.Cut(z, "_")
		g.writeMod(id, n, v)
	}
	p := &publicRig{modsRig: g, id: id}
	p.setting(true)
	return p
}

func (p *publicRig) setting(on bool) {
	p.t.Helper()
	v := PublicModsOff
	if on {
		v = PublicModsOn
	}
	if err := p.store.SetAppSetting(context.Background(), SettingPublicMods, v, "alice", p.clock.Now()); err != nil {
		p.t.Fatal(err)
	}
}

// anon asks the live server with no cookie.
func (p *publicRig) anon(path string, header ...string) reply {
	p.t.Helper()
	h := http.Header{}
	for i := 0; i+1 < len(header); i += 2 {
		h.Set(header[i], header[i+1])
	}
	return p.do(http.MethodGet, path, nil, h)
}

// server is a server over the rig's parts, for the functions the pages
// call without a request.
func (p *publicRig) server() *server {
	s := &server{
		cfg: p.cfg, store: p.store, worlds: p.worlds, game: p.ctrl, log: p.slog, now: p.clock.Now,
		publicSlots: make(chan struct{}, maxPublicDownloads),
		planner:     &mods.Planner{Worlds: p.worlds, Portal: modportal.NewForTest(p.portal.URL)},
		runs:        newGameRuns(),
	}
	p.t.Cleanup(s.runs.close)
	s.marks = newPortalMarks(p.clock.Now, s.runs, s.checkPortalForPublic)
	return s
}

func zipNames(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		if f.Method != zip.Store {
			t.Errorf("%s is compressed, want stored", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rc)
		_ = rc.Close()
		out[f.Name] = buf.Bytes()
	}
	return out
}

// SPEC 9.16: off by default, and then every public path is an unknown path.
func TestPublicPathsAnswerAsUnknownWhileOff(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	p.setting(false)
	const unknown = "/nope/deeper"
	want := p.anon(unknown)
	paths := []string{
		PathPublicMods, PathPublicMods + "/", PathPublic, PathPublic + "anything",
		PublicModFilePath(p.id, "alpha_1.0.0.zip"), PublicModsAllPath(p.id),
		PathPublic + "static/x.css", PathPublic + "language",
		// The mux would redirect this one to the path with a slash.
		strings.TrimSuffix(PathPublicStatic, "/"),
	}
	for _, path := range paths {
		got := p.anon(path)
		// The page's language menu returns to the path asked for, so that is
		// the one thing that may differ.
		same := strings.ReplaceAll(got.body, path, "P") == strings.ReplaceAll(want.body, unknown, "P")
		if got.StatusCode != http.StatusNotFound || !same {
			t.Errorf("GET %s: %d, body like an unknown path's: %v", path, got.StatusCode, same)
		}
		if gh, wh := got.Header.Clone(), want.Header.Clone(); !headersEqual(gh, wh) {
			t.Errorf("GET %s: headers %v, want an unknown path's %v", path, gh, wh)
		}
		for _, method := range []string{http.MethodPost, http.MethodHead, http.MethodPut} {
			if rep := p.do(method, path, nil, nil); rep.StatusCode != http.StatusNotFound {
				t.Errorf("%s %s: %d, want 404", method, path, rep.StatusCode)
			}
		}
	}
}

func headersEqual(a, b http.Header) bool {
	a.Del("Date")
	b.Del("Date")
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if strings.Join(v, ",") != strings.Join(b[k], ",") {
			return false
		}
	}
	return true
}

// SPEC 9.16: a missing setting means off; the page is on only for "on".
func TestPublicModsSettingDefaultsToOff(t *testing.T) {
	hn := newHarness(t, nil)
	s := &server{store: hn.store, log: hn.slog, now: hn.clock.Now}
	ctx := context.Background()
	if s.publicModsOn(ctx) {
		t.Error("a fresh database has the page on")
	}
	if err := s.setPublicMods(ctx, PublicModsOn, "alice"); err != nil || !s.publicModsOn(ctx) {
		t.Errorf("after turning on: on=%v, err=%v", s.publicModsOn(ctx), err)
	}
	if err := s.setPublicMods(ctx, PublicModsOff, "alice"); err != nil || s.publicModsOn(ctx) {
		t.Errorf("after turning off: on=%v, err=%v", s.publicModsOn(ctx), err)
	}
	if err := hn.store.SetAppSetting(ctx, SettingPublicMods, "yes", "alice", hn.clock.Now()); err != nil || s.publicModsOn(ctx) {
		t.Errorf("a value other than on turned the page on: err=%v", err)
	}
}

// SPEC 9.16: the world, its version and its enabled, non-built-in mods; a
// folder mod has no download, a turned-off mod is left out, and of two zips
// of one mod only the one the game loads is listed.
func TestPublicModsDataListsTheRunningWorldsMods(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0", "alpha_1.10.0", "alpha_1.2.0", "beta_2.0.0", "gamma_1.0.0")
	local := filepath.Join(p.modsDir(p.id), "local_3.0.0")
	if err := os.MkdirAll(local, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "info.json"), []byte(`{"name":"local","version":"3.0.0","title":"Local mod"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.modsDir(p.id), world.ModListFile),
		[]byte(`{"mods":[{"name":"base","enabled":true},{"name":"gamma","enabled":false}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Foreman has no portal here, so no marks are asked for.
	s := p.server()
	s.marks = nil
	data, err := s.publicModsData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Worlds) != 1 || !data.PortalUnchecked {
		t.Fatalf("data %+v: want one world and the portal unchecked", data)
	}
	w := data.Worlds[0]
	if w.ID != p.id || w.Name != "Base" || w.GameVersion != v20 {
		t.Errorf("world %+v", w)
	}
	got := map[string]PublicMod{}
	for _, m := range w.Mods {
		got[m.Name] = m
	}
	if len(got) != 3 || got["gamma"].Name != "" {
		t.Errorf("mods %+v: want alpha, beta and local only", w.Mods)
	}
	if a := got["alpha"]; a.Version != "1.10.0" || a.DownloadPath != PublicModFilePath(p.id, "alpha_1.10.0.zip") {
		t.Errorf("alpha %+v: want the highest version", a)
	}
	if l := got["local"]; !l.Folder || l.DownloadPath != "" || l.Title != "Local mod" {
		t.Errorf("folder mod %+v", l)
	}
	if w.AllPath != PublicModsAllPath(p.id) {
		t.Errorf("all path %q", w.AllPath)
	}
}

// SPEC 9.16: a world whose game is not running lists nothing.
func TestPublicModsDataIsEmptyUnlessTheGameRuns(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	s := p.server()
	p.wantRedirectHome(p.post(gamePath(p.id, "stop"), url.Values{"countdown": {"0"}}))
	p.waitPage(stopped)
	data, err := s.publicModsData(context.Background())
	if err != nil || len(data.Worlds) != 0 {
		t.Errorf("a stopped game: %+v, %v", data, err)
	}
	s.game = nil
	if data, err := s.publicModsData(context.Background()); err != nil || len(data.Worlds) != 0 {
		t.Errorf("no server control: %+v, %v", data, err)
	}
}

// SPEC 9.16, 9.3: a mod the portal lacks, or has hidden, is marked, and one
// the portal has carries no mark.
func TestPublicModsDataMarksWhatThePortalLacks(t *testing.T) {
	p := newPublicRig(t)
	p.portal.AddZip("alpha", "1.0.0")
	p.writeMod(p.id, "alpha", "1.0.0")
	p.writeMod(p.id, "zz-not-on-portal", "1.0.0")
	data, err := p.server().publicModsData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data.PortalUnchecked {
		t.Fatal("the portal answered, yet the page says it could not be checked")
	}
	for _, m := range data.Worlds[0].Mods {
		switch m.Name {
		case "alpha":
			if m.NotOnPortal || m.Hidden {
				t.Errorf("alpha is on the portal, yet marked %+v", m)
			}
		case "zz-not-on-portal":
			if !m.NotOnPortal || m.Hidden {
				t.Errorf("a mod the portal lacks is marked %+v", m)
			}
		}
	}
}

// SPEC 9.16: when the portal cannot be asked the list comes without marks
// and says so, and downloads do not depend on it.
func TestPublicModsDataWithoutThePortalStillListsAndDownloads(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	p.portal.FailAPI(http.StatusInternalServerError)
	data, err := p.server().publicModsData(context.Background())
	if err != nil || !data.PortalUnchecked || len(data.Worlds) != 1 || len(data.Worlds[0].Mods) != 1 {
		t.Fatalf("data %+v, %v: want the list and the portal unchecked", data, err)
	}
	if m := data.Worlds[0].Mods[0]; m.NotOnPortal || m.Hidden {
		t.Errorf("a failed check marked %+v", m)
	}
	if rep := p.anon(PublicModFilePath(p.id, "alpha_1.0.0.zip")); rep.StatusCode != http.StatusOK {
		t.Errorf("download with the portal down: %d", rep.StatusCode)
	}
}

// Marks are kept 15 minutes after an answer and 1 minute after a failure,
// per world and mods fingerprint, with one check at a time per world.
func TestPortalMarksKeepAnswersAndFailures(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	var calls atomic.Int32
	var fail atomic.Bool
	check := func(context.Context, string) ([]mods.Flag, error) {
		calls.Add(1)
		if fail.Load() {
			return nil, errors.New("portal down")
		}
		return []mods.Flag{{Code: mods.FlagNotOnPortal, Name: "a", Version: "1"}}, nil
	}
	runs := newGameRuns()
	t.Cleanup(runs.close)
	pm := newPortalMarks(clock.Now, runs, check)
	ctx := context.Background()
	ask := func(fp string) bool {
		_, ok := pm.get(ctx, "w", fp)
		return ok
	}

	if !ask("f1") || calls.Load() != 1 {
		t.Fatalf("first ask: calls %d", calls.Load())
	}
	clock.Advance(14*time.Minute + 59*time.Second)
	if !ask("f1") || calls.Load() != 1 {
		t.Errorf("an answer under 15 minutes old was asked again: calls %d", calls.Load())
	}
	clock.Advance(2 * time.Second)
	if !ask("f1") || calls.Load() != 2 {
		t.Errorf("an answer over 15 minutes old was not asked again: calls %d", calls.Load())
	}
	if !ask("f2") || calls.Load() != 3 {
		t.Errorf("a new fingerprint reused an answer: calls %d", calls.Load())
	}

	fail.Store(true)
	clock.Advance(16 * time.Minute)
	if ask("f2") || calls.Load() != 4 {
		t.Fatalf("a failing check: calls %d", calls.Load())
	}
	clock.Advance(59 * time.Second)
	if ask("f2") || calls.Load() != 4 {
		t.Errorf("a failure under a minute old was asked again: calls %d", calls.Load())
	}
	fail.Store(false)
	clock.Advance(2 * time.Second)
	if !ask("f2") || calls.Load() != 5 {
		t.Errorf("a failure over a minute old was not asked again: calls %d", calls.Load())
	}
	// Another world has its own answer.
	if _, ok := pm.get(ctx, "other", "f2"); !ok || calls.Load() != 6 {
		t.Errorf("another world reused an answer: calls %d", calls.Load())
	}
}

// One check runs at a time per world, and a visitor waits at most the
// limit; the answer that arrives later is there for the next one.
func TestPortalMarksOneCheckAtATimeAndABoundedWait(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	release := make(chan struct{})
	var calls atomic.Int32
	check := func(context.Context, string) ([]mods.Flag, error) {
		calls.Add(1)
		<-release
		return []mods.Flag{{Code: mods.FlagDeprecated, Name: "a", Version: "1"}}, nil
	}
	runs := newGameRuns()
	t.Cleanup(runs.close)
	pm := newPortalMarks(clock.Now, runs, check)
	pm.wait = 100 * time.Millisecond

	var wg sync.WaitGroup
	results := make([]bool, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = pm.get(context.Background(), "w", "f")
		}()
	}
	wg.Wait()
	for i, ok := range results {
		if ok {
			t.Errorf("visitor %d got an answer from a check that has not ended", i)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("%d checks ran at once for one world, want 1", calls.Load())
	}

	pm.wait = 5 * time.Second
	close(release)
	flags, ok := pm.get(context.Background(), "w", "f")
	if !ok || len(flags) != 1 || calls.Load() != 1 {
		t.Errorf("the late answer was not kept: ok=%v flags=%v calls=%d", ok, flags, calls.Load())
	}
}

// SPEC 9.16: a visitor who leaves stops waiting at once.
func TestPortalMarksStopWaitingWhenTheVisitorLeaves(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	runs := newGameRuns()
	t.Cleanup(runs.close)
	pm := newPortalMarks(time.Now, runs, func(context.Context, string) ([]mods.Flag, error) { <-block; return nil, nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan bool)
	go func() { _, ok := pm.get(ctx, "w", "f"); done <- ok }()
	select {
	case ok := <-done:
		if ok {
			t.Error("a cancelled wait returned an answer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the wait did not end when the visitor left")
	}
}

// A portal check nobody waits for any more still ends when Foreman shuts
// down, and shutdown waits for it, as for any other background work.
func TestPortalMarksCheckEndsAtShutdown(t *testing.T) {
	hn := newHarnessDeps(t, nil, nil, func(d *Deps) { d.Portal = modportal.NewForTest("http://127.0.0.1:1") })
	h := hn.h.(*Handler)
	pm := h.srv.marks
	started, ended := make(chan struct{}), make(chan struct{})
	pm.check = func(ctx context.Context, _ string) ([]mods.Flag, error) {
		close(started)
		<-ctx.Done()
		close(ended)
		return nil, ctx.Err()
	}
	pm.wait = 10 * time.Millisecond
	if _, ok := pm.get(context.Background(), "w", "f"); ok {
		t.Fatal("a check that has not ended gave an answer")
	}
	<-started
	closed := make(chan struct{})
	go func() { h.Close(); close(closed) }()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the check's context did not end at shutdown")
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not end")
	}
	pm.check = func(context.Context, string) ([]mods.Flag, error) {
		t.Error("a check started after shutdown")
		return nil, nil
	}
	if _, ok := pm.get(context.Background(), "w", "other"); ok {
		t.Error("a get after shutdown gave an answer")
	}
}

// SPEC 9.16, 5.11: one mod zip, whole or by range, and not modified when the
// browser already has it.
func TestPublicModFileDownload(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	path := PublicModFilePath(p.id, "alpha_1.0.0.zip")
	want := modZip(t, "alpha", "1.0.0")

	rep := p.anon(path)
	if rep.StatusCode != http.StatusOK || rep.body != string(want) {
		t.Fatalf("download: %d, %d bytes, want %d", rep.StatusCode, len(rep.body), len(want))
	}
	for k, v := range map[string]string{
		"Content-Type":           "application/zip",
		"Content-Disposition":    `attachment; filename="alpha_1.0.0.zip"; filename*=UTF-8''alpha_1.0.0.zip`,
		"Cache-Control":          "no-cache",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
	} {
		if got := rep.Header.Get(k); got != v {
			t.Errorf("%s: %q, want %q", k, got, v)
		}
	}
	if !strings.Contains(rep.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("frames are not refused: %v", rep.Header)
	}
	if len(rep.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("a download set a cookie: %v", rep.Header.Values("Set-Cookie"))
	}

	part := p.anon(path, "Range", "bytes=0-9")
	if part.StatusCode != http.StatusPartialContent || part.body != string(want[:10]) {
		t.Errorf("range: %d, %q", part.StatusCode, part.body)
	}
	if mod := p.anon(path, "If-Modified-Since", rep.Header.Get("Last-Modified")); mod.StatusCode != http.StatusNotModified {
		t.Errorf("conditional request: %d, want 304", mod.StatusCode)
	}
}

// SPEC 9.16: every downloadable mod zip in one stored zip, named for the
// world; a folder mod and a turned-off mod are not in it.
func TestPublicModsAllZip(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0", "alpha_1.1.0", "beta_2.0.0", "gamma_1.0.0")
	if err := os.MkdirAll(filepath.Join(p.modsDir(p.id), "local_1.0.0"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.modsDir(p.id), world.ModListFile),
		[]byte(`{"mods":[{"name":"gamma","enabled":false}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rep := p.anon(PublicModsAllPath(p.id))
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", rep.StatusCode, rep.body)
	}
	if got := rep.Header.Get("Content-Disposition"); !strings.Contains(got, `filename="Base-mods.zip"`) {
		t.Errorf("Content-Disposition %q, want Base-mods.zip", got)
	}
	if rep.Header.Get("Content-Type") != "application/zip" || rep.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("headers %v", rep.Header)
	}
	got := zipNames(t, []byte(rep.body))
	if len(got) != 2 || !bytes.Equal(got["alpha_1.1.0.zip"], modZip(t, "alpha", "1.1.0")) || !bytes.Equal(got["beta_2.0.0.zip"], modZip(t, "beta", "2.0.0")) {
		names := []string{}
		for n := range got {
			names = append(names, n)
		}
		t.Errorf("zip holds %v, want alpha 1.1.0 and beta 2.0.0 as they are", names)
	}
}

// SPEC 9.16: with only folder mods there is nothing to zip.
func TestPublicModsAllZipNeedsADownloadableMod(t *testing.T) {
	p := newPublicRig(t)
	if err := os.MkdirAll(filepath.Join(p.modsDir(p.id), "local_1.0.0"), 0o750); err != nil {
		t.Fatal(err)
	}
	if rep := p.anon(PublicModsAllPath(p.id)); rep.StatusCode != http.StatusNotFound {
		t.Errorf("a world with no mod zips: %d, want 404", rep.StatusCode)
	}
}

// SPEC 14.18: the downloads lift the server's deadlines for anyone, the
// page itself and every refusal keep them.
func TestPublicDownloadsLiftDeadlinesOnlyWhileServingAFile(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	lifted := func(path string) (int, int) {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		spy := &deadlineSpy{ResponseRecorder: httptest.NewRecorder()}
		p.h.ServeHTTP(spy, r)
		return spy.Code, spy.lifted
	}
	if code, n := lifted(PublicModFilePath(p.id, "alpha_1.0.0.zip")); code != http.StatusOK || n < 2 {
		t.Errorf("download: %d, deadlines changed %d", code, n)
	}
	if code, n := lifted(PublicModsAllPath(p.id)); code != http.StatusOK || n < 2 {
		t.Errorf("all.zip: %d, deadlines changed %d", code, n)
	}
	for _, path := range []string{PathPublicMods, PublicModFilePath(p.id, "nope.zip"), PublicModFilePath("other", "alpha_1.0.0.zip")} {
		if _, n := lifted(path); n != 0 {
			t.Errorf("%s changed %d deadlines", path, n)
		}
	}
}

// SPEC 14.18: at most 4 public downloads at once; the next is told to try
// again in 5 seconds, and a place frees when a download ends.
func TestPublicDownloadsAreLimitedToFour(t *testing.T) {
	p := newPublicRig(t)
	big := make([]byte, 8<<20)
	if err := os.WriteFile(filepath.Join(p.modsDir(p.id), "big_1.0.0.zip"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	path := PublicModFilePath(p.id, "big_1.0.0.zip")

	var held []*http.Response
	defer func() {
		for _, r := range held {
			_ = r.Body.Close()
		}
	}()
	for i := range maxPublicDownloads {
		resp, err := http.Get(p.url + path) //nolint:gosec,noctx,bodyclose // the test's own server
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, resp) //nolint:bodyclose // closed by the deferred loop; headers read, body left unread: the write blocks
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download %d: %d", i+1, resp.StatusCode)
		}
	}
	waitFor(t, func() bool { return p.anon(path).StatusCode == http.StatusServiceUnavailable })
	rep := p.anon(path)
	if rep.StatusCode != http.StatusServiceUnavailable || rep.Header.Get("Retry-After") != "5" {
		t.Errorf("fifth download: %d, Retry-After %q", rep.StatusCode, rep.Header.Get("Retry-After"))
	}
	// A page and a refusal need no place.
	if rep := p.anon(PathPublicMods); rep.StatusCode != http.StatusOK {
		t.Errorf("the page while downloads are full: %d", rep.StatusCode)
	}
	_ = held[0].Body.Close()
	waitFor(t, func() bool { return p.anon(PublicModFilePath(p.id, "nope.zip")).StatusCode == http.StatusNotFound })
	waitFor(t, func() bool {
		resp, err := http.Get(p.url + path) //nolint:gosec,noctx,bodyclose // the test's own server
		if err != nil {
			return false
		}
		held = append(held, resp) //nolint:bodyclose // closed by the deferred loop
		return resp.StatusCode == http.StatusOK
	})
}

// SPEC 14.18: a download whose reader stops reading is cut off after the
// idle limit, and its place is given back.
func TestPublicDownloadThatStallsIsCutOff(t *testing.T) {
	// Shorten the idle limit before the rig starts, so the server and its
	// handlers are shut down (cleanups run last first) before the limit is
	// put back.
	shortIdle(t, 300*time.Millisecond)
	p := newPublicRig(t)
	big := make([]byte, 8<<20)
	if err := os.WriteFile(filepath.Join(p.modsDir(p.id), "big_1.0.0.zip"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	path := PublicModFilePath(p.id, "big_1.0.0.zip")
	var held []*http.Response
	defer func() {
		for _, r := range held {
			_ = r.Body.Close()
		}
	}()
	for range maxPublicDownloads {
		resp, err := http.Get(p.url + path) //nolint:gosec,noctx,bodyclose // the test's own server
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, resp) //nolint:bodyclose // closed by the deferred loop
	}
	// All four stall, so the places come free again without the clients leaving.
	waitFor(t, func() bool {
		resp, err := http.Get(p.url + PublicModsAllPath(p.id)) //nolint:gosec,noctx // the test's own server
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	})
}

// SPEC 14.18: only GET.
func TestPublicDownloadsAllowOnlyGet(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	for _, path := range []string{PathPublicMods, PublicModFilePath(p.id, "alpha_1.0.0.zip"), PublicModsAllPath(p.id)} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
			if rep := p.do(method, path, nil, nil); rep.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: %d, want 405", method, path, rep.StatusCode)
			}
		}
	}
}

// SPEC 14.18: only GET. HEAD is refused too, so a probe never makes the
// server stream a whole mods zip to nobody.
func TestPublicRoutesRefuseHead(t *testing.T) {
	p := newPublicRig(t, "alpha_1.0.0")
	for _, path := range []string{
		PathPublicMods, PublicModFilePath(p.id, "alpha_1.0.0.zip"), PublicModsAllPath(p.id),
		PathPublicStatic + "foreman.css",
	} {
		rep := p.do(http.MethodHead, path, nil, nil)
		if rep.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("HEAD %s: %d, want 405", path, rep.StatusCode)
		}
		if allow := rep.Header.Get("Allow"); allow != http.MethodGet {
			t.Errorf("HEAD %s: Allow %q, want GET", path, allow)
		}
	}
}

// SPEC 9.16, 14.18: the portal is asked about zips only. A world with no zip
// to list has nothing to mark, so the visitor waits for no check and sees no
// "could not be checked" line.
func TestPublicModsDataSkipsThePortalWhenNoModHasAZip(t *testing.T) {
	p := newPublicRig(t)
	local := filepath.Join(p.modsDir(p.id), "local_3.0.0")
	if err := os.MkdirAll(local, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "info.json"), []byte(`{"name":"local","version":"3.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s := p.server()
	s.marks = newPortalMarks(p.clock.Now, s.runs, func(context.Context, string) ([]mods.Flag, error) {
		calls.Add(1)
		return nil, errors.New("portal down")
	})
	data, err := s.publicModsData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data.PortalUnchecked || calls.Load() != 0 {
		t.Errorf("unchecked=%v, portal asked %d times: want no check", data.PortalUnchecked, calls.Load())
	}
	if len(data.Worlds) != 1 || len(data.Worlds[0].Mods) != 1 {
		t.Errorf("data %+v: want the folder mod listed", data)
	}
}

// waitFor polls until ok, for a server that frees a place when its handler
// notices the client is gone.
func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("the condition was not met in time")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
