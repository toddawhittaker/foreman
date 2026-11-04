package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/gamedir"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	adoptedContainer = "factorio"
	adoptedNetwork   = "games"
)

// adoptedOptions shape the game container and the game folder.
type adoptedOptions struct {
	env          []string // added to the image's environment
	running      bool
	rconHostPort string
	// folder fills the game folder before the container starts; nil leaves
	// it empty (Fresh).
	folder func(t *testing.T, dir string)
}

// adoptedRig is a signed-in harness in adopted mode, over the fake game
// with one container that was already there.
type adoptedRig struct {
	*gameRig
	dir string
}

func newAdoptedRig(t *testing.T, o adoptedOptions) *adoptedRig {
	t.Helper()
	dir := t.TempDir()
	if o.folder != nil {
		o.folder(t, dir)
	}
	fake := fakegame.New(t, fakegame.Config{SelfNetwork: adoptedNetwork, Adopted: &fakegame.Adopted{
		Name: adoptedContainer, GameDir: dir, Env: append([]string{"DLC_SPACE_AGE=false"}, o.env...),
		RCONHostPort: o.rconHostPort, Running: o.running,
	}})
	rig := &gameRig{fake: fake}
	makeGame := func(worlds *world.Manager, st *store.Store) *game.Controller {
		dc, err := docker.New(fake.Host())
		if err != nil {
			t.Fatal(err)
		}
		c, err := game.New(game.Config{
			Docker: dc, Worlds: worlds, Audit: st, Switches: st, GameUID: 845, GameGID: 845,
			GameContainer: adoptedContainer, GameDir: dir,
			Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		t.Cleanup(func() { cancel(); <-done })
		rig.ctrl = c
		return c
	}
	env := map[string]string{"FOREMAN_MODE": "adopted", "FOREMAN_GAME_CONTAINER": adoptedContainer, "FOREMAN_GAME_DIR": dir}
	hn := newHarnessIn(t, env, func(data string) world.Dirs {
		return world.Dirs{Worlds: filepath.Join(dir, "worlds"), Snapshots: filepath.Join(data, "snapshots"), Imports: filepath.Join(data, "import")}
	}, makeGame)
	rig.liveServer = newLiveServerFor(t, hn)
	rig.cookie = hn.signIn()
	return &adoptedRig{gameRig: rig, dir: dir}
}

// unadoptedFolder lays out a game folder the image made itself: real
// saves, mods and config (SPEC 4.8).
func unadoptedFolder(t *testing.T, dir string) {
	t.Helper()
	for _, sub := range gamedir.FixedLinks {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixtureNoSA))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "saves", "base.zip"), b, 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "rconpw"), []byte("pw-original\n"), 0o640); err != nil { //nolint:gosec // the mode the image's group needs
		t.Fatal(err)
	}
}

// brokenFolder is a folder whose saves is a file, so it is neither fresh,
// unadopted nor ready.
func brokenFolder(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "saves"), []byte("x"), 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
}

var adoptForm = url.Values{"name": {"Gate"}, "game_version": {"2.1.24"}, "countdown": {"0"}}

// adoptIt posts the adoption form and waits for the page to show the world.
func (a *adoptedRig) adoptIt(t *testing.T) (id, body string) {
	t.Helper()
	a.wantRedirectHome(a.post(PathAdopt, adoptForm))
	body = a.waitPage("Active world: Gate", "The game folder is adopted")
	st, err := gamedir.Inspect(a.dir)
	if err != nil || st.Kind != gamedir.Ready {
		t.Fatalf("game folder after adoption: %+v, %v", st, err)
	}
	return st.ActiveID, body
}

// SPEC 4.8: an unadopted folder offers adoption in place of Start, and the
// adoption page says what will be stopped, snapshotted and moved.
func TestUnadoptedFolderOffersAdoption(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder, running: true})

	body := a.waitPage("Adopt this game folder")
	a.wantContains("server page", body, `href="`+PathAdopt+`"`, "<h2 id=\"none-heading\">Adopt the game folder</h2>")
	a.wantMissing("server page", body, "Start server", "Download game image", "Stop server", "No active world")

	form := a.get(PathAdopt)
	a.wantContains("adoption page", form,
		"Adopt the game folder as a new world?", "button--danger", `name="name"`, `name="game_version"`, `value="2.1.24"`,
		`name="space_age"`, `name="countdown"`, `value="60"`, "Warn players online with the countdown", "stop it",
		`"Before adoption"`, "pre-foreman-20261104T153000Z", "kept", "Start the game again, on the new world.",
		"Link the game folder's saves, mods and config to the new world", `aria-describedby="adopt-space-age-hint"`, `id="adopt-space-age-hint"`,
		"Shown in Foreman. Up to 100 characters.")
	a.wantMissing("adoption page", form, " checked>")
}

// SPEC 4.8: a stopped game's adoption leaves it stopped and asks for no
// countdown.
func TestAdoptionFormForAStoppedGame(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder})
	a.waitPage("Adopt this game folder")
	form := a.get(PathAdopt)
	a.wantContains("adoption page", form, "Leave the game stopped")
	a.wantMissing("adoption page", form, `name="countdown"`, "Start the game again")
}

// SPEC 4.8: a folder that is neither fresh, unadopted nor ready is named,
// and no server control is offered, with or without an active world.
func TestBrokenFolderIsNamedAndRefusesControls(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: brokenFolder})
	body := a.waitPage("message--error")
	a.wantContains("no-world page", body, "The game folder needs fixing",
		"Foreman cannot control the game yet. In the game folder, saves is a file, not a folder or link. Fix it by hand, then reload this page.")
	a.wantMissing("no-world page", body, "Start server", "Adopt this game folder", "No active world", "saves saves")

	// With a world on record the page still names the problem and leaves out every control.
	a.newWorld("Alpha", fixtureNoSA, false)
	body = a.waitPage("message--error")
	a.wantContains("server page", body, "In the game folder, saves is a file, not a folder or link.", "Active world: Alpha")
	a.wantMissing("server page", body, "Start server", "Force stop", "Stop server", "Restart server", "Adopt this game folder")

	form := a.get(PathAdopt)
	a.wantContains("adoption page", form, "saves is a file, not a folder or link", "Check again")
	a.wantMissing("adoption page", form, "button--danger")
}

// SPEC 4.8: adopting runs the whole adoption and leaves the folder ready,
// the world active, and a "Before adoption" snapshot and an audit entry.
func TestAdoptionHappyPath(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder})
	a.waitPage("Adopt this game folder")

	id, body := a.adoptIt(t)
	a.wantContains("server page", body, "Running version", "<code>2.1.24</code>", "Go to Snapshots")
	a.wantMissing("server page", body, "Adopt this game folder", "Download game image", "message--warning")

	snaps, err := a.worlds.Snapshots(id)
	if err != nil || !slices.ContainsFunc(snaps, func(s world.Snapshot) bool { return s.Label == "Before adoption" }) {
		t.Errorf("snapshots: %+v, %v", snaps, err)
	}
	pre, _ := filepath.Glob(filepath.Join(a.dir, gamedir.PreForemanPrefix+"*"))
	if len(pre) != 1 {
		t.Errorf("pre-foreman folders: %v", pre)
	}
	last := a.audit()[0]
	if last.Action != game.AuditAdopt || last.Username != "alice" || last.WorldID != id {
		t.Errorf("audit entry %+v", last)
	}
	// Adopted: the form has nothing left to do, and Start works.
	a.wantContains("adoption page", a.get(PathAdopt), "nothing to adopt")
	a.wantContains("server page", a.get(PathServer), "Start server")
}

// SPEC 4.8, 7.6: a running game's adoption shows its steps live, from the
// first, on the Server page while the new world is not the active one yet,
// and ends on that world's page.
func TestAdoptionShowsItsStepsWhileRunning(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder, running: true})
	a.waitPage("Adopt this game folder")
	if err := a.fake.Join(adoptedContainer, "bob"); err != nil {
		t.Fatal(err)
	}

	form := url.Values{"name": {"Gate"}, "game_version": {"2.1.24"}, "countdown": {"2"}}
	a.wantRedirectHome(a.post(PathAdopt, form))
	body := a.waitPage("Adopting the game folder", "Warn players")
	a.wantContains("steps", body, "Adopting the game folder as Gate", "/events?adopting=1", "Check the game container", "Move the original folders into a pre-foreman folder")
	a.wantMissing("steps", body, "Active world:", "Adopt this game folder", "Players online")

	body = a.waitPage("Active world: Gate", running)
	a.wantContains("done", body, "The game folder is adopted")
}

// SPEC 4.8: a double-submitted adoption form starts one adoption. Every
// other post is refused as already running, and the page keeps following
// the first.
func TestConcurrentAdoptionPostsStartOne(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder, running: true})
	a.waitPage("Adopt this game folder")

	const posts = 6
	form := url.Values{"name": {"Gate"}, "game_version": {"2.1.24"}, "countdown": {"2"}}
	replies := make(chan reply, posts)
	start := make(chan struct{})
	for range posts {
		go func() {
			<-start
			replies <- a.post(PathAdopt, form)
		}()
	}
	close(start)
	started, refused := 0, 0
	for range posts {
		rep := <-replies
		switch {
		case rep.StatusCode == http.StatusSeeOther:
			started++
		case rep.StatusCode == http.StatusConflict && strings.Contains(rep.body, "An adoption is already running"):
			refused++
		default:
			t.Errorf("status %d\n%s", rep.StatusCode, rep.body)
		}
	}
	if started != 1 || refused != posts-1 {
		t.Fatalf("%d adoptions started and %d refused, want 1 and %d", started, refused, posts-1)
	}
	a.waitPage("Adopting the game folder as Gate", "/events?adopting=1")
	a.waitPage("Active world: Gate", "The game folder is adopted")
}

// SPEC 4.11: a refused container is explained on the adoption page, names
// the variable and never its value, and changes nothing.
func TestAdoptionRefusalNamesTheVariableNotItsValue(t *testing.T) {
	const value = "s3cr3t-value"
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder, env: []string{"LOAD_LATEST_SAVE=" + value}})
	a.waitPage("Adopt this game folder")

	rep := a.post(PathAdopt, adoptForm)
	if rep.StatusCode != http.StatusConflict {
		t.Fatalf("status %d\n%s", rep.StatusCode, rep.body)
	}
	a.wantContains("refusal", rep.body, "LOAD_LATEST_SAVE", "message--error", "Check again")
	a.wantMissing("refusal", rep.body, value, "button--danger")
	if st, _ := gamedir.Inspect(a.dir); st.Kind != gamedir.Unadopted {
		t.Errorf("the folder is %s", st.Kind)
	}
	if got := a.audit(); len(got) != 0 {
		t.Errorf("audit: %+v", got)
	}
}

// SPEC 7.4: a Space Age world on a container that turns it off is refused
// in words, on the form.
func TestAdoptionSpaceAgeMismatch(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder})
	a.waitPage("Adopt this game folder")
	form := url.Values{"name": {"Gate"}, "game_version": {"2.1.24"}, "space_age": {"on"}}
	rep := a.post(PathAdopt, form)
	if rep.StatusCode != http.StatusConflict {
		t.Fatalf("status %d\n%s", rep.StatusCode, rep.body)
	}
	a.wantContains("refusal", rep.body, "Space Age", "DLC_SPACE_AGE")

	// Space Age means nothing to a 1.1 world, so a ticked box is ignored.
	form = url.Values{"name": {"Old"}, "game_version": {"1.1.110"}, "space_age": {"on"}, "countdown": {"0"}}
	a.wantRedirectHome(a.post(PathAdopt, form))
}

// DESIGN 7.2: field problems keep the form and name the field.
func TestAdoptionFieldProblems(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder, running: true})
	a.waitPage("Adopt this game folder")
	for name, c := range map[string]struct {
		form url.Values
		want []string
	}{
		"no name":       {url.Values{"name": {" "}, "game_version": {"2.1.24"}}, []string{`href="#adopt-name"`}},
		"bad version":   {url.Values{"name": {"Gate"}, "game_version": {"two"}}, []string{`href="#adopt-version"`, `value="two"`}},
		"bad countdown": {url.Values{"name": {"Gate"}, "game_version": {"2.1.24"}, "countdown": {"601"}}, []string{`href="#countdown"`, `value="601"`}},
	} {
		rep := a.post(PathAdopt, c.form)
		if rep.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, rep.StatusCode)
		}
		a.wantContains(name, rep.body, append(c.want, "error-summary", "button--danger")...)
	}
	if got := a.audit(); len(got) != 0 {
		t.Errorf("a refused form was audited: %+v", got)
	}
}

// SPEC 4.9: a save made by another Factorio than the container's gets a
// warning, and the running version is shown.
func TestVersionWarningOnTheServerPage(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder, env: []string{"VERSION=2.1.30"}})
	a.waitPage("Adopt this game folder")
	a.wantRedirectHome(a.post(PathAdopt, adoptForm))
	a.waitPage("Active world: Gate")
	body := a.waitPage("message--warning")
	a.wantContains("server page", body, "<code>2.1.30</code>", "was made by Factorio 2.1.24", "the game container runs Factorio 2.1.30")
}

// SPEC 4.12: a published RCON port is warned about, on the page with a
// world and on the one without.
func TestRCONPublishedWarning(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder, rconHostPort: "27015"})
	body := a.waitPage("publishes its RCON port (27015 unless RCON_PORT says otherwise) on the host")
	a.wantContains("no-world page", body, "Remove the port mapping")
	// The warning is read once, with the page. Nothing live may repeat it.
	a.wantMissing("no-world page", body[strings.Index(body, "<main"):strings.Index(body, `id="announcer"`)], "aria-live", "role=")

	a.wantRedirectHome(a.post(PathAdopt, adoptForm))
	body = a.waitPage("Active world: Gate", "publishes its RCON port (27015 unless RCON_PORT says otherwise) on the host")
	a.wantContains("server page", body, "message--warning")

	clean := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder})
	a.wantMissing("page", clean.waitPage("Adopt this game folder"), "RCON port")
}

// An adoption refused because Foreman is shutting down does not hold the
// adoption slot, so a later try is not told one is running (SPEC 4.8).
func TestAdoptionRefusedInShutdownReleasesTheSlot(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder})
	a.waitPage("Adopt this game folder")
	a.h.(*Handler).Close()
	for i := range 2 {
		rep := a.post(PathAdopt, adoptForm)
		if rep.StatusCode != http.StatusServiceUnavailable && i == 0 {
			t.Fatalf("adopt while shutting down: %d\n%s", rep.StatusCode, rep.body)
		}
		if strings.Contains(rep.body, "An adoption is already running") {
			t.Errorf("try %d was told an adoption is running:\n%s", i+1, rep.body)
		}
	}
}

// SPEC 4.4, DESIGN 7.3: a refused game network is a warning on the server
// page, and the world stays stoppable.
func TestNetworkWarningOnTheServerPage(t *testing.T) {
	r := newTestRenderer(t)
	p := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Status.Adopted, p.Status.RunningVersion = true, "2.1.24"
		p.Status.Layout = gamedir.State{Kind: gamedir.Ready, ActiveID: "base"}
		p.Status.NetworkWarning = "The game network overlaps FOREMAN_TRUSTED_PROXIES. Change one of them."
	})
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: p})
	for _, want := range []string{
		`<strong>Warning:</strong> The game network overlaps FOREMAN_TRUSTED_PROXIES. Change one of them.`,
		`id="act-stop"`, `id="act-force-stop"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	main := body[strings.Index(body, "<main"):strings.Index(body, `id="announcer"`)]
	if i := strings.Index(main, "overlaps FOREMAN_TRUSTED_PROXIES"); strings.Contains(main[:i], "aria-live") {
		t.Error("the warning sits in a live region")
	}
	p.Status.NetworkWarning = ""
	_, body = render(t, r, pageCase{status: 200, user: "todd", page: p})
	if strings.Contains(body, "FOREMAN_TRUSTED_PROXIES") {
		t.Error("a warning shows with none set")
	}
}

// SPEC 4.4: a network refusal is dropped once a later check finds the
// network clean, and kept until then.
func TestNetworkRefusalClearsAfterACleanCheck(t *testing.T) {
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	p := ServerPage{HaveResult: true, Result: actionResult{Action: game.AuditSwitch, Err: fmt.Errorf("refused: %w", game.ErrNetwork), At: at}}
	p.Status.Adopted = true
	p.Status.NetworkCleanAt = at.Add(-time.Minute)
	if p.Failure() == nil {
		t.Error("the refusal is hidden before any later clean check")
	}
	p.Status.NetworkCleanAt = at.Add(time.Minute)
	if f := p.Failure(); f != nil {
		t.Errorf("the refusal %+v outlived a clean check", f)
	}
	p.Result.Err = game.ErrImageMissing
	if p.Failure() == nil {
		t.Error("a clean network check hid a failure that was not about the network")
	}
}

// SPEC 4.9: in adopted mode the image is the container owner's, so a
// failure about it never offers a download.
func TestNoImageDownloadInAdoptedMode(t *testing.T) {
	p := ServerPage{HaveResult: true, Result: actionResult{Action: game.AuditStart, Err: game.ErrImageMissing}}
	p.Status.Adopted = true
	if f := p.Failure(); f == nil || f.Download {
		t.Errorf("adopted failure %+v offers a download", f)
	}
	p.Status.Adopted = false
	if f := p.Failure(); f == nil || !f.Download {
		t.Errorf("managed failure %+v offers no download", f)
	}
}

// Adoption exists only in adopted mode.
func TestAdoptIsNotFoundInManagedMode(t *testing.T) {
	g := newGameRig(t, image21)
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		var form url.Values
		if m == http.MethodPost {
			form = adoptForm
		}
		rep := g.do(m, PathAdopt, form, http.Header{"Cookie": {g.cookie.String()}})
		if rep.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s in managed mode: %d", m, PathAdopt, rep.StatusCode)
		}
	}
}

// SPEC 12.8, 14.7: the adoption form needs a session and a same-origin
// request, and reaches neither Docker nor the audit log without them.
func TestAdoptRefusesBadRequests(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder})
	a.waitPage("Adopt this game folder")
	before := len(a.mutations())
	cookie := http.Header{"Cookie": {a.cookie.String()}}
	with := func(k, v string) http.Header {
		h := cookie.Clone()
		h.Set(k, v)
		return h
	}
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		var form url.Values
		if m == http.MethodPost {
			form = adoptForm
		}
		if rep := a.do(m, PathAdopt, form, nil); rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") == PathServer {
			t.Errorf("signed out %s: %d to %q", m, rep.StatusCode, rep.Header.Get("Location"))
		}
	}
	for name, h := range map[string]http.Header{
		"cross-site":     with("Sec-Fetch-Site", "cross-site"),
		"foreign Origin": with("Origin", "https://evil.example"),
	} {
		if rep := a.do(http.MethodPost, PathAdopt, adoptForm, h); rep.StatusCode != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, rep.StatusCode)
		}
	}
	for _, m := range []string{http.MethodPut, http.MethodDelete} {
		if rep := a.do(m, PathAdopt, adoptForm, cookie); rep.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d, want 405", m, rep.StatusCode)
		}
	}
	time.Sleep(200 * time.Millisecond)
	if got := a.mutations(); len(got) != before {
		t.Errorf("refused requests reached Docker: %q", got[before:])
	}
	if got := a.audit(); len(got) != 0 {
		t.Errorf("refused requests were audited: %+v", got)
	}
}

// SPEC 4.7 to 4.12: every adopted-mode refusal reaches the admin in plain
// words, with the specific cause kept, and a Docker failure keeps its
// detail out of the page.
func TestAdoptedErrorsHavePlainWords(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{game.ErrContainerRefused, "game container"},
		{game.ErrNoGameContainer, "FOREMAN_GAME_CONTAINER"},
		{game.ErrNotAdopted, "Adopt it on the Server page first"},
		{game.ErrAlreadyAdopted, "nothing to adopt"},
		{game.ErrNotPresented, "Switch to this world instead"},
		{&game.LayoutError{Path: "mods", Reason: "mods is a file"}, "at mods: mods is a file"},
		{game.ErrSpaceAgeMismatch, "DLC_SPACE_AGE"},
		{game.ErrAdoptedImages, "neither checks nor downloads"},
		{game.ErrAdoptRCON, "config/rconpw"},
		{game.ErrSaveNewerThanImage, "newer"},
		{fmt.Errorf("%w: it turns on only some", game.ErrSpaceAgeMismatch), "only some"},
	} {
		status, msg := gameError(c.err)
		if status != http.StatusConflict || !strings.Contains(msg, c.want) || !strings.HasSuffix(msg, ".") {
			t.Errorf("gameError(%v) = %d, %q; want 409 with %q", c.err, status, msg, c.want)
		}
	}
	if status, _ := gameError(errors.New("boom")); status != http.StatusInternalServerError {
		t.Errorf("an unknown error is %d", status)
	}
}

// SPEC 16.3: a page that showed the adoption is told to load again when its
// stream opens after the world became the active one; a page that did not
// show it is not, so it cannot reload forever.
func TestAdoptionPageIsToldToReload(t *testing.T) {
	a := newAdoptedRig(t, adoptedOptions{folder: unadoptedFolder})
	a.waitPage("Adopt this game folder")
	id, _ := a.adoptIt(t)

	for param, wantMoved := range map[string]bool{"?" + adoptingParam + "=1": true, "": false} {
		buf, _, closeIt := a.collectStream(gamePath(id, "events") + param)
		deadline := time.Now().Add(3 * time.Second)
		for !strings.Contains(buf.String(), "event: control") && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)
		got := strings.Contains(buf.String(), "event: moved")
		closeIt()
		if got != wantMoved {
			t.Errorf("stream%s sent moved = %v, want %v:\n%s", param, got, wantMoved, buf.String())
		}
	}
}
