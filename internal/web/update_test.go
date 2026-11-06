package web

import (
	"bytes"
	"context"
	"errors"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/releases"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Tests for updating a world's game version and rolling it back (SPEC 10.2
// to 10.4, 14). The fake game holds the 2.1.24 image; the newer version a
// test updates to is put there with addImage so no download is needed,
// except where a test is about the download.

const (
	oldVersion = "2.1.24"
	newVersion = "2.1.25"
)

// fakeReleases stands in for the Docker Hub and factorio.com client and
// counts the calls it gets.
type fakeReleases struct {
	mu                     sync.Mutex
	versions               []string
	latest                 releases.Latest
	versionsErr, latestErr error
	versionCalls           int
	latestCalls            int
	size                   int64
	sizeCalls              int
}

func (f *fakeReleases) Versions(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.versionCalls++
	if f.versionsErr != nil {
		return nil, f.versionsErr // the real client returns nothing with an error
	}
	return f.versions, nil
}

func (f *fakeReleases) Latest(context.Context) (releases.Latest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.latestCalls++
	if f.latestErr != nil {
		return releases.Latest{}, f.latestErr
	}
	return f.latest, nil
}

// ImageSize answers size, or fails when none is set, and counts its calls
// apart from the others.
func (f *fakeReleases) ImageSize(context.Context, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizeCalls++
	if f.size == 0 {
		return 0, errHub
	}
	return f.size, nil
}

func (f *fakeReleases) sizeAsked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sizeCalls
}

func (f *fakeReleases) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.versionCalls + f.latestCalls
}

var errHub = errors.New("releases: Docker Hub cannot be reached")

// newUpdateRig is a game rig whose handler also has rel as its release
// source; a nil rel leaves the source unset, which means no outbound calls.
// The session cookie of the first handler is valid for the second, since
// sessions live in the store.
func newUpdateRig(t *testing.T, rel *fakeReleases) *gameRig {
	t.Helper()
	fake := fakegame.New(t, fakegame.Config{Images: []string{image21}})
	g := newGameRigOn(t, fake.Host(), fake)
	d := Deps{Config: g.cfg, Store: g.store, Worlds: g.worlds, Game: g.ctrl, Log: g.slog, Now: g.clock.Now}
	if rel != nil {
		d.Releases = rel
	}
	h, err := NewHandler(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	g.h = h
	g.liveServer = newLiveServerFor(t, g.harness)
	return g
}

// addImage makes Docker hold the image of a game version.
func (g *gameRig) addImage(version string) {
	g.t.Helper()
	g.fake.SetImage(game.ImageRef(version), true)
}

func (g *gameRig) version(id string) string {
	g.t.Helper()
	root, err := g.worlds.OpenWorld(id)
	if err != nil {
		g.t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := world.ReadMeta(root)
	if err != nil {
		g.t.Fatal(err)
	}
	return meta.GameVersion
}

func (g *gameRig) savePath(id string) string {
	return filepath.Join(g.worlds.Dirs().Worlds, id, world.SavesDir, "base.zip")
}

func (g *gameRig) readSave(id string) []byte {
	g.t.Helper()
	b, err := os.ReadFile(g.savePath(id))
	if err != nil {
		g.t.Fatal(err)
	}
	return b
}

func (g *gameRig) hasAudit(action string) bool {
	for _, e := range g.audit() {
		if e.Action == action {
			return true
		}
	}
	return false
}

func updateURL(id string) string   { return worldPath(id) + "/update" }
func rollbackURL(id string) string { return worldPath(id) + "/rollback" }
func releasesURL(id string) string { return worldPath(id) + "/releases" }

// wantUnchanged fails when a refused update or rollback changed the world:
// its version, its save, its snapshots or the audit log.
func (g *gameRig) wantUnchanged(what, id, version string, save []byte, snaps int) {
	g.t.Helper()
	if got := g.version(id); got != version {
		g.t.Errorf("%s changed the version to %s", what, got)
	}
	if !bytes.Equal(g.readSave(id), save) {
		g.t.Errorf("%s changed the save", what)
	}
	if list, _ := g.worlds.Snapshots(id); len(list) != snaps {
		g.t.Errorf("%s changed the snapshots: %+v", what, list)
	}
	if g.hasAudit(game.AuditUpdate) || g.hasAudit(game.AuditRollback) {
		g.t.Errorf("%s was audited as done", what)
	}
}

// hiddenFields reads the hidden inputs of the form that posts to action,
// as a browser would send them.
func hiddenFields(t *testing.T, body, action string) url.Values {
	t.Helper()
	i := strings.Index(body, `action="`+action+`"`)
	if i < 0 {
		t.Fatalf("no form posts to %s:\n%s", action, body)
	}
	rest := body[i:]
	if end := strings.Index(rest, "</form>"); end >= 0 {
		rest = rest[:end]
	}
	out := url.Values{}
	for _, m := range regexp.MustCompile(`<input type="hidden" name="([^"]*)" value="([^"]*)"`).FindAllStringSubmatch(rest, -1) {
		out.Set(m[1], html.UnescapeString(m[2]))
	}
	return out
}

func withFields(form url.Values, kv ...string) url.Values {
	out := url.Values{}
	for k, v := range form {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out.Set(kv[i], kv[i+1])
	}
	return out
}

// finished checks that a POST was answered with a redirect and, when it went
// to the Server page, where the action runs in the background, waits for the
// action to end and returns its error. A stopped world's action may instead
// be done before the redirect.
func (g *gameRig) finished(rep reply, id, action string) error {
	g.t.Helper()
	if rep.StatusCode != http.StatusSeeOther {
		g.t.Fatalf("got %d, want a redirect\n%s", rep.StatusCode, rep.body)
	}
	if rep.Header.Get("Location") == PathServer {
		return g.waitResult(id, action)
	}
	return nil
}

// updateStopped updates a stopped world and waits for the update to end.
func (g *gameRig) updateStopped(id, version string) {
	g.t.Helper()
	if err := g.finished(g.post(updateURL(id), url.Values{"game_version": {version}}), id, game.AuditUpdate); err != nil {
		g.t.Fatal(err)
	}
}

// updateRunning updates a running world without a countdown and waits for
// the game to be running again.
func (g *gameRig) updateRunning(id, version string) {
	g.t.Helper()
	g.wantRedirectHome(g.post(updateURL(id), url.Values{"game_version": {version}, "countdown": {"0"}}))
	if err := g.waitResult(id, game.AuditUpdate); err != nil {
		g.t.Fatal(err)
	}
	g.waitPage(running)
}

// SPEC 14: every update route needs a signed-in admin; a POST also needs a
// same-origin request. A refused request changes and fetches nothing.
func TestUpdateRoutesRefuseBadRequests(t *testing.T) {
	rel := &fakeReleases{versions: []string{newVersion}, latest: releases.Latest{Stable: newVersion}}
	g := newUpdateRig(t, rel)
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.addImage(newVersion)
	save := g.readSave(id)
	cookie := http.Header{"Cookie": {g.cookie.String()}}
	withHdr := func(k, v string) http.Header {
		h := cookie.Clone()
		h.Set(k, v)
		return h
	}
	form := url.Values{"game_version": {newVersion}, "countdown": {"0"}}

	routes := []struct {
		method, path string
		getOnly      bool
	}{
		{http.MethodGet, updateURL(id), false},
		{http.MethodPost, updateURL(id), false},
		{http.MethodGet, rollbackURL(id), false},
		{http.MethodPost, rollbackURL(id), false},
		{http.MethodGet, releasesURL(id), true},
		{http.MethodGet, PathVersions, true},
	}
	for _, r := range routes {
		var f url.Values
		if r.method == http.MethodPost {
			f = form
		}
		if rep := g.do(r.method, r.path, f, nil); rep.StatusCode != http.StatusSeeOther || !strings.HasPrefix(rep.Header.Get("Location"), PathSignIn) {
			t.Errorf("signed out %s %s: %d to %q", r.method, r.path, rep.StatusCode, rep.Header.Get("Location"))
		}
		if r.method == http.MethodPost {
			for name, h := range map[string]http.Header{
				"cross-site":     withHdr("Sec-Fetch-Site", "cross-site"),
				"same-site":      withHdr("Sec-Fetch-Site", "same-site"),
				"foreign Origin": withHdr("Origin", "https://evil.example"),
			} {
				if rep := g.do(r.method, r.path, f, h); rep.StatusCode != http.StatusForbidden {
					t.Errorf("%s %s %s: %d, want 403", name, r.method, r.path, rep.StatusCode)
				}
			}
		} else if r.getOnly {
			if rep := g.do(http.MethodPost, r.path, form, cookie); rep.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("POST %s: %d, want 405", r.path, rep.StatusCode)
			}
		}
		for _, m := range []string{http.MethodPut, http.MethodDelete} {
			if rep := g.do(m, r.path, form, cookie); rep.StatusCode != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: %d, want 405", m, r.path, rep.StatusCode)
			}
		}
	}
	for _, bad := range []string{"nope", "%2e%2e", "..%2f..%2fetc", "base%00", "BASE"} {
		for _, r := range []struct{ method, path string }{
			{http.MethodGet, worldPath(bad) + "/update"}, {http.MethodPost, worldPath(bad) + "/update"},
			{http.MethodGet, worldPath(bad) + "/rollback"}, {http.MethodPost, worldPath(bad) + "/rollback"},
			{http.MethodGet, worldPath(bad) + "/releases"},
		} {
			var f url.Values
			if r.method == http.MethodPost {
				f = form
			}
			if rep := g.do(r.method, r.path, f, cookie); rep.StatusCode < 400 {
				t.Errorf("unknown world %s %s: %d", r.method, r.path, rep.StatusCode)
			}
		}
	}
	if n := rel.calls(); n != 0 {
		t.Errorf("a refused request reached the release source %d times", n)
	}
	g.wantUnchanged("a refused request", id, oldVersion, save, 0)
	if len(g.mutations()) != 0 {
		t.Errorf("a refused request reached Docker: %q", g.mutations())
	}
}

// SPEC 10.2: the release source is asked only when a fragment needs it,
// never at startup and never while a page renders.
func TestReleaseSourceIsNotCalledByPagesOrStartup(t *testing.T) {
	rel := &fakeReleases{versions: []string{newVersion}, latest: releases.Latest{Stable: newVersion}}
	g := newUpdateRig(t, rel)
	id := g.newWorld("Alpha", fixtureNoSA, false)
	if rel.calls() != 0 {
		t.Fatalf("startup called the release source %d times", rel.calls())
	}
	for _, p := range []string{PathServer, PathWorlds, worldPath(id), updateURL(id), rollbackURL(id), snapshotsPath(id)} {
		g.get(p)
	}
	if n := rel.calls(); n != 0 {
		t.Errorf("rendering pages called the release source %d times", n)
	}
	g.get(releasesURL(id))
	g.get(PathVersions)
	if rel.calls() == 0 {
		t.Error("the fragments never asked the release source")
	}
}

// SPEC 10.3: the confirmation of a running world asks how to warn players;
// one of a stopped world leaves out the countdown and says the server stays
// stopped.
func TestUpdateFormShowsCountdownOnlyWhenRunning(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)

	form := g.get(updateURL(id))
	g.wantContains("stopped form", form, "Update Alpha from Factorio "+oldVersion, `name="game_version"`,
		"Leave the server stopped", `action="`+updateURL(id)+`"`, "Update game version")
	g.wantMissing("stopped form", form, `name="countdown"`)

	g.startGame(id)
	form = g.get(updateURL(id))
	g.wantContains("running form", form, `name="countdown"`, `value="60"`, "Start the game again", "Before update")
	g.wantMissing("running form", form, "Leave the server stopped")
}

// SPEC 10.3: only the active world is updated; another world's page says to
// switch first, and a POST for it changes nothing.
func TestUpdateOfAnInactiveWorldIsRefused(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	g.addImage(newVersion)
	saveA, saveB := g.readSave(a), g.readSave(b)

	bPage := g.get(worldPath(b))
	g.wantContains("inactive world page", bPage, "Switch to this world to update it")
	g.wantMissing("inactive world page", bPage, `id="act-update"`)
	aPage := g.get(worldPath(a))
	g.wantContains("active world page", aPage, `id="act-update"`, `href="`+updateURL(a)+`"`)
	g.wantMissing("active world page", aPage, "Switch to this world to update it")

	g.wantContains("update page", g.get(updateURL(b)), "Switch to this world to update it")
	rep := g.post(updateURL(b), url.Values{"game_version": {newVersion}})
	if rep.StatusCode != http.StatusConflict || !strings.Contains(rep.body, "Switch to this world to update it") {
		t.Errorf("POST: %d\n%s", rep.StatusCode, rep.body)
	}
	g.wantUnchanged("an update of an inactive world", b, oldVersion, saveB, 0)
	g.wantUnchanged("an update of an inactive world", a, oldVersion, saveA, 0)
	if len(g.mutations()) != 0 {
		t.Errorf("a refused update reached Docker: %q", g.mutations())
	}
}

// SPEC 10.3, 10.7: a typed version must be an exact release of a supported
// line and newer than the world's. Anything else is refused with the field's
// error, the typed text is never echoed as markup, and nothing is stopped,
// snapshotted or changed.
func TestUpdateRefusesBadVersions(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.startGame(id)
	save := g.readSave(id)
	base, audits := len(g.mutations()), len(g.audit())

	for name, c := range map[string]struct{ typed, want string }{
		"older, same line":  {"2.1.23", "newer"},
		"older, other line": {"2.0.77", "newer"},
		"equal":             {oldVersion, "newer"},
		"unsupported line":  {"3.0.1", "2.1"},
		"not a patch":       {"2.1", "2.1"},
		"a word":            {"latest", "2.1"},
		"empty":             {"", "2.1"},
		"trailing junk":     {"2.1.25; rm -rf /", "2.1"},
		"markup":            {`<script>alert(1)</script>`, "2.1"},
		"attribute escape":  {`2.1.25"><img src=x onerror=alert(1)>`, "2.1"},
	} {
		rep := g.post(updateURL(id), url.Values{"game_version": {c.typed}, "countdown": {"0"}})
		if rep.StatusCode < 400 || rep.StatusCode >= 500 {
			t.Errorf("%s: status %d", name, rep.StatusCode)
		}
		g.wantContains(name, rep.body, `id="update-version-error"`, `aria-invalid="true"`, c.want)
		g.wantMissing(name, rep.body, "<script>alert", "<img src=x", `" onerror`)
	}
	g.wantUntouched(id, "a refused version", base, audits, 0)
	g.wantUnchanged("a refused version", id, oldVersion, save, 0)
	if s := g.containerStatus(id); s != "running" {
		t.Errorf("a refused version left the game %s", s)
	}
}

// SPEC 10.7: the update form offers a select of newer releases and a typed
// field, says a version only moves forward, and never suggests an older
// version as an example.
func TestUpdateFormHasSelectTypedFieldAndForwardNotice(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	form := g.get(updateURL(id))
	g.wantContains("form", form, `<select id="update-version-select" name="game_version"`,
		`<option value="">Choose a version</option>`, `name="game_version_typed"`,
		"Or type a version", "only moves forward", "Roll back")
	g.wantMissing("form", form, "<datalist", "such as")
}

// SPEC 10.7: a filled typed field wins over the select, so a bad typed
// version is refused even when the select holds a good one.
func TestUpdateTypedVersionWinsOverSelect(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.addImage(newVersion)
	save := g.readSave(id)
	rep := g.post(updateURL(id), url.Values{"game_version": {newVersion}, "game_version_typed": {"2.1.23"}})
	if rep.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d", rep.StatusCode)
	}
	g.wantContains("refused", rep.body, `id="update-version-error"`, `value="2.1.23"`)
	g.wantUnchanged("a refused typed version", id, oldVersion, save, 0)
}

func TestChosenVersion(t *testing.T) {
	for _, c := range []struct{ picked, typed, want string }{
		{"2.1.25", "", "2.1.25"},
		{"2.1.25", "  ", "2.1.25"},
		{"2.1.25", " 2.1.26 ", "2.1.26"},
		{"", "2.1.26", "2.1.26"},
		{"", "", ""},
	} {
		if got := chosenVersion(c.picked, c.typed); got != c.want {
			t.Errorf("chosenVersion(%q, %q) = %q, want %q", c.picked, c.typed, got, c.want)
		}
	}
}

// SPEC 7.6: the countdown of an update is checked like a stop's, and a bad
// one stops nothing.
func TestUpdateCountdownIsChecked(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.addImage(newVersion)
	g.startGame(id)
	save := g.readSave(id)

	rep := g.post(updateURL(id), url.Values{"game_version": {newVersion}, "countdown": {"601"}})
	g.wantContains("refusal", rep.body, "Enter a whole number of seconds from 0 to 600", `aria-invalid="true"`)
	if rep.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d", rep.StatusCode)
	}
	g.wantUnchanged("a bad countdown", id, oldVersion, save, 0)
	if s := g.containerStatus(id); s != "running" {
		t.Errorf("a bad countdown left the game %s", s)
	}
}

// SPEC 10.3: updating a stopped world snapshots it as "Before update", sets
// the version, audits it by name, and leaves the server stopped.
func TestUpdateStoppedWorld(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.addImage(newVersion)
	before := g.readSave(id)

	g.updateStopped(id, newVersion)
	if got := g.version(id); got != newVersion {
		t.Errorf("version %s, want %s", got, newVersion)
	}
	list, err := g.worlds.Snapshots(id)
	if err != nil || len(list) != 1 || list[0].Label != world.LabelBeforeUpdate || list[0].Reason != world.ReasonGameUpdate || list[0].TakenBy != "alice" {
		t.Fatalf("snapshots %+v, %v", list, err)
	}
	if got := g.containerStatus(id); got == "running" {
		t.Errorf("a stopped world was started by the update: %s", got)
	}
	if !bytes.Equal(g.readSave(id), before) {
		t.Error("the update changed the save")
	}
	last := g.audit()[0]
	if last.Action != game.AuditUpdate || last.Username != "alice" || last.WorldID != id || !strings.Contains(last.Detail, newVersion) {
		t.Errorf("audit entry %+v", last)
	}
	g.waitPage(stopped, "Start server")
	g.wantContains("world page", g.get(worldPath(id)), "<code>"+newVersion+"</code>")
}

// SPEC 10.3: a running world is warned, saved, stopped, snapshotted, moved to
// the new version and started again on the new image.
func TestUpdateRunningWorldRestarts(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.addImage(newVersion)
	g.startGame(id)
	if img := g.container(id).Image; img != game.ImageRef(oldVersion) {
		t.Fatalf("the game runs %s before the update", img)
	}
	// Players are warned only when someone is online.
	if err := g.fake.Join(game.ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	g.waitPage("<td>bob</td>")

	g.wantRedirectHome(g.post(updateURL(id), url.Values{"game_version": {newVersion}, "countdown": {"1"}}))
	if err := g.waitResult(id, game.AuditUpdate); err != nil {
		t.Fatal(err)
	}
	g.waitPage(running)
	if img := g.container(id).Image; img != game.ImageRef(newVersion) {
		t.Errorf("the game runs %s after the update, want %s", img, game.ImageRef(newVersion))
	}
	if got := g.version(id); got != newVersion {
		t.Errorf("version %s", got)
	}
	cmds := strings.Join(g.commands(id), "\n")
	if !strings.Contains(cmds, "The server will save and stop in 1 second") || !strings.Contains(cmds, "/server-save") {
		t.Errorf("the players were not warned and the game saved first: %q", cmds)
	}
	list, _ := g.worlds.Snapshots(id)
	if len(list) != 1 || list[0].Label != world.LabelBeforeUpdate {
		t.Errorf("snapshots %+v", list)
	}
}

// SPEC 10.3, 3.7: a missing image is downloaded as part of the update, before
// the game is stopped.
func TestUpdateDownloadsTheImageFirst(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{}) // Docker has no 2.1.25 image
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.startGame(id)

	g.updateRunning(id, newVersion)
	pullAt, stopAt := -1, -1
	for i, c := range g.fake.State().Calls {
		switch {
		case c == "POST /images/create":
			pullAt = i
		case strings.HasPrefix(c, "POST /containers/") && strings.HasSuffix(c, "/stop") && stopAt < 0:
			stopAt = i
		}
	}
	if pullAt < 0 || pullAt > stopAt {
		t.Errorf("the image download (call %d) did not come before the stop (call %d): %q", pullAt, stopAt, g.fake.State().Calls)
	}
	if got := g.version(id); got != newVersion {
		t.Errorf("version %s", got)
	}
}

// SPEC 7.4, 10.3: an update the pre-start check refuses at the new version is
// shown on the Server page with its problems; when every one allows it, the
// box is the confirmation and "Update anyway" goes ahead. A box whose
// fingerprint no longer matches is refused and changes nothing.
func TestUpdateAnyway(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", "2.1.24-space-age-testmod.zip", true)
	g.addImage(newVersion)
	save := g.readSave(id)

	g.wantRedirectHome(g.post(updateURL(id), url.Values{"game_version": {newVersion}}))
	body := g.waitPage("Update anyway")
	g.wantContains("server page", body, `action="`+updateURL(id)+`"`, `name="anyway" value="1"`, `name="seen"`)
	g.wantMissing("server page", body, "Fix the world first")
	g.wantUnchanged("a refused update", id, oldVersion, save, 0)

	box := hiddenFields(t, body, updateURL(id))
	if box.Get("game_version") != newVersion {
		t.Errorf("the box does not carry the version: %v", box)
	}

	g.wantRedirectHome(g.post(updateURL(id), withFields(box, "seen", "stale")))
	g.waitPage("changed since the page showed it")
	g.wantUnchanged("a stale update anyway", id, oldVersion, save, 0)

	g.wantRedirectHome(g.post(updateURL(id), box))
	if err := g.waitResult(id, game.AuditUpdate); err != nil {
		t.Fatal(err)
	}
	if got := g.version(id); got != newVersion {
		t.Errorf("version %s after update anyway", got)
	}
	list, _ := g.worlds.Snapshots(id)
	if len(list) != 1 || list[0].Label != world.LabelBeforeUpdate {
		t.Errorf("snapshots %+v", list)
	}
}

// An "Update anyway" with no fingerprint is refused: it has no box behind it.
func TestUpdateAnywayNeedsAFingerprint(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", "2.1.24-space-age-testmod.zip", true)
	g.addImage(newVersion)
	save := g.readSave(id)

	g.wantRedirectHome(g.post(updateURL(id), url.Values{"game_version": {newVersion}, "anyway": {"1"}}))
	g.waitPage("changed since the page showed it")
	g.wantUnchanged("an update anyway with no fingerprint", id, oldVersion, save, 0)
}

// SPEC 10.4: before any update there is nothing to roll back to, so the world
// page offers no rollback and a POST changes nothing.
func TestNoRollbackBeforeAnUpdate(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	save := g.readSave(id)
	if _, err := g.worlds.TakeSnapshot(id, "Manual", "alice"); err != nil {
		t.Fatal(err)
	}

	g.wantMissing("world page", g.get(worldPath(id)), `id="act-rollback"`, "Roll back")
	if body := g.get(rollbackURL(id)); strings.Contains(body, "Roll back to") {
		t.Errorf("a rollback page offers a rollback:\n%s", body)
	}
	rep := g.post(rollbackURL(id), url.Values{})
	if rep.StatusCode != http.StatusConflict || !strings.Contains(rep.body, `id="action-error"`) {
		t.Errorf("POST: %d\n%s", rep.StatusCode, rep.body)
	}
	g.wantUnchanged("a rollback with nothing to roll back to", id, oldVersion, save, 1)
}

// SPEC 10.4: rollback names the version and the snapshot, warns that play
// since the update is lost, restores the save exactly as it was before the
// update, keeps what was played as a "Before restore" snapshot, and is not
// offered again once done.
func TestRollbackStoppedWorld(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.addImage(newVersion)
	original := g.readSave(id)

	g.updateStopped(id, newVersion)
	played := append(bytes.Clone(original), []byte("played after the update")...)
	// The game replaces its save by renaming a new file over it, so a
	// snapshot that shares the old file is not changed.
	tmp := g.savePath(id) + ".tmp"
	if err := os.WriteFile(tmp, played, 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	if err := os.Rename(tmp, g.savePath(id)); err != nil {
		t.Fatal(err)
	}

	worldPage := g.get(worldPath(id))
	g.wantContains("world page", worldPage, `id="act-rollback"`, `href="`+rollbackURL(id)+`"`, "Roll back to "+oldVersion, `"Before update"`)
	page := g.get(rollbackURL(id))
	g.wantContains("rollback page", page, "Go back from Factorio "+newVersion+" to "+oldVersion+"?", `&#34;Before update&#34;`,
		"everything played since the update is lost", "Roll back to "+oldVersion, `action="`+rollbackURL(id)+`"`, "Before restore")
	g.wantMissing("rollback page", page, `name="countdown"`)

	if err := g.finished(g.post(rollbackURL(id), url.Values{}), id, game.AuditRollback); err != nil {
		t.Fatal(err)
	}
	if got := g.version(id); got != oldVersion {
		t.Errorf("version %s after rollback, want %s", got, oldVersion)
	}
	if !bytes.Equal(g.readSave(id), original) {
		t.Error("the save is not what it was before the update")
	}
	undo := false
	list, _ := g.worlds.Snapshots(id)
	for _, s := range list {
		undo = undo || (s.Reason == world.ReasonBeforeRestore && s.Label == "Before restore")
	}
	if !undo {
		t.Errorf("no Before restore snapshot to undo with: %+v", list)
	}
	if last := g.audit()[0]; last.Action != game.AuditRollback || last.Username != "alice" || last.WorldID != id {
		t.Errorf("audit entry %+v", last)
	}
	if got := g.containerStatus(id); got == "running" {
		t.Errorf("a stopped world was started by the rollback: %s", got)
	}
	g.wantMissing("world page after", g.get(worldPath(id)), `id="act-rollback"`)

	// The rollback cannot be done twice.
	if rep := g.post(rollbackURL(id), url.Values{}); rep.StatusCode != http.StatusConflict {
		t.Errorf("second rollback: %d", rep.StatusCode)
	}
	if got := g.version(id); got != oldVersion || !bytes.Equal(g.readSave(id), original) {
		t.Errorf("a second rollback changed the world: %s", got)
	}
}

// SPEC 10.4: rolling back a running world warns the players, restores, and
// starts the game again on the old image.
func TestRollbackRunningWorld(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.addImage(newVersion)
	g.startGame(id)
	g.updateRunning(id, newVersion)

	g.wantContains("rollback page", g.get(rollbackURL(id)), `name="countdown"`, `value="60"`, "Start the game again")
	g.wantRedirectHome(g.post(rollbackURL(id), url.Values{"countdown": {"0"}}))
	if err := g.waitResult(id, game.AuditRollback); err != nil {
		t.Fatal(err)
	}
	g.waitPage(running)
	if got := g.version(id); got != oldVersion {
		t.Errorf("version %s", got)
	}
	if img := g.container(id).Image; img != game.ImageRef(oldVersion) {
		t.Errorf("the game runs %s after the rollback", img)
	}
}

// SPEC 10.4, 3.7: if the old image is gone, the files are still restored,
// the undo snapshot exists, the game is not started, and the page offers the
// download.
func TestRollbackWithTheOldImageGone(t *testing.T) {
	g := newUpdateRig(t, &fakeReleases{})
	id := g.newWorld("Alpha", fixtureNoSA, false)
	g.addImage(newVersion)
	g.startGame(id)
	original := g.readSave(id)
	g.updateRunning(id, newVersion)
	g.fake.SetImage(game.ImageRef(oldVersion), false)

	g.wantRedirectHome(g.post(rollbackURL(id), url.Values{"countdown": {"0"}}))
	if err := g.waitResult(id, game.AuditRollback); err == nil {
		t.Error("a rollback with no old image reported success")
	}
	if got := g.version(id); got != oldVersion {
		t.Errorf("version %s: the files were not restored", got)
	}
	if !bytes.Equal(g.readSave(id), original) {
		t.Error("the save is not the one from before the update")
	}
	if list, _ := g.worlds.Snapshots(id); len(list) < 2 {
		t.Errorf("snapshots %+v: want the update's and the undo", list)
	}
	if got := g.containerStatus(id); got == "running" {
		t.Errorf("the game runs with no image: %s", got)
	}
	g.waitPage("Download game image")
}
