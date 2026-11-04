package web

import (
	"bufio"
	"context"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

const image21 = "factoriotools/factorio:2.1.24@sha256:a0c019b7608975a58f1694de54a2326e13a5aa2d123c18f48e5396181e366e28"

// gameRig is a signed-in harness whose server control runs against the
// fake game.
type gameRig struct {
	*liveServer
	fake   *fakegame.Game
	ctrl   *game.Controller
	cookie *http.Cookie
}

// newGameRig makes a live server with a controller on the fake game, whose
// images are the ones given, and signs alice in.
func newGameRig(t *testing.T, images ...string) *gameRig {
	t.Helper()
	fake := fakegame.New(t, fakegame.Config{Images: images})
	return newGameRigOn(t, fake.Host(), fake)
}

func newGameRigOn(t *testing.T, dockerHost string, fake *fakegame.Game) *gameRig {
	t.Helper()
	rig := &gameRig{fake: fake}
	makeGame := func(worlds *world.Manager, st *store.Store) *game.Controller {
		dc, err := docker.New(dockerHost)
		if err != nil {
			t.Fatal(err)
		}
		c, err := game.New(game.Config{
			Docker: dc, Worlds: worlds, Audit: st, Switches: st,
			HostDataDir: filepath.Dir(worlds.Dirs().Worlds), GameUID: 845, GameGID: 845,
			Images: map[string]string{"2.1": image21},
			Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
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
	hn := newHarnessWith(t, nil, makeGame)
	ts := newLiveServerFor(t, hn) // long requests need a real connection
	rig.liveServer = ts
	rig.cookie = hn.signIn()
	return rig
}

// newWorld makes an active 2.1 world without Space Age holding the
// fixture save.
func (g *gameRig) newWorld(name, fixture string, spaceAge bool) string {
	g.t.Helper()
	id, err := g.worlds.Create(context.Background(), name, "2.1.24", spaceAge)
	if err != nil {
		g.t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", fixture)) //nolint:gosec // a fixed test fixture
	if err != nil {
		g.t.Fatal(err)
	}
	p := filepath.Join(g.worlds.Dirs().Worlds, id, world.SavesDir, "base.zip")
	if err := os.WriteFile(p, b, 0o600); err != nil { //nolint:gosec // a test temp folder
		g.t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, at, at); err != nil {
		g.t.Fatal(err)
	}
	return id
}

func (g *gameRig) get(path string) string {
	g.t.Helper()
	return g.harness.get(path, withCookie(g.cookie)).Body.String()
}

func (g *gameRig) post(path string, form url.Values) reply {
	g.t.Helper()
	return g.do(http.MethodPost, path, form, http.Header{"Cookie": {g.cookie.String()}})
}

// waitPage polls the server page until it holds every text.
func (g *gameRig) waitPage(texts ...string) string {
	g.t.Helper()
	var body string
	deadline := time.Now().Add(10 * time.Second)
	for {
		body = g.get(PathServer)
		ok := true
		for _, s := range texts {
			ok = ok && strings.Contains(body, s)
		}
		if ok {
			return body
		}
		if time.Now().After(deadline) {
			g.t.Fatalf("the server page never showed %q:\n%s", texts, body)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (g *gameRig) wantRedirectHome(rep reply) {
	g.t.Helper()
	if rep.StatusCode != http.StatusSeeOther || rep.Header.Get("Location") != PathServer {
		g.t.Fatalf("got %d to %q, want 303 to /\n%s", rep.StatusCode, rep.Header.Get("Location"), rep.body)
	}
}

func (g *gameRig) audit() []store.AuditEntry {
	g.t.Helper()
	list, err := g.store.ListAudit(context.Background(), 100)
	if err != nil {
		g.t.Fatal(err)
	}
	return list
}

func (g *gameRig) commands(id string) []string {
	for _, c := range g.fake.State().Containers {
		if c.Name == game.ContainerName(id) {
			return c.Commands
		}
	}
	return nil
}

const running = `<span class="state state--running">Running</span>`
const stopped = `<span class="state state--stopped">Stopped</span>`

// SPEC 7.1 to 7.3, 7.5, 7.6: the page shows the state, starts the active
// world in the background and stops it gracefully; each action names the
// signed-in user in the audit log.
func TestServerPageStartsAndStops(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)

	body := g.waitPage(stopped, "Start server")
	if strings.Contains(body, "Stop server") || strings.Contains(body, "Force stop") {
		t.Errorf("a stopped game offers a stop:\n%s", body)
	}
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	body = g.waitPage(running, "Stop server", "Restart server", "Force stop", "Nobody is online.")
	if strings.Contains(body, ">Start server<") {
		t.Error("a running game offers a start")
	}

	if err := g.fake.Join(game.ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	g.waitPage("<td>bob</td>")

	form := g.get(gamePath(id, "stop"))
	if !strings.Contains(form, `name="countdown"`) || !strings.Contains(form, `value="60"`) || !strings.Contains(form, `name="no_countdown"`) {
		t.Errorf("stop form:\n%s", form)
	}
	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"1"}}))
	g.waitPage(stopped)
	if cmds := g.commands(id); !slices.Contains(cmds, "/server-save") || !slices.ContainsFunc(cmds, func(c string) bool {
		return strings.HasPrefix(c, "The server will save and stop in 1 second")
	}) {
		t.Errorf("RCON commands %q: want a warning and a save", cmds)
	}

	var actions []string
	for _, e := range g.audit() {
		if e.Username != "alice" || e.WorldID != id {
			t.Errorf("audit entry %+v is not alice's on %s", e, id)
		}
		actions = append(actions, e.Action)
	}
	slices.Sort(actions)
	if !slices.Equal(actions, []string{game.AuditStart, game.AuditStop}) {
		t.Errorf("audit actions %q", actions)
	}
}

// SPEC 7.4, 17.5: a refused start lists each problem and, when every one
// allows it, the refusal box is the confirmation: it names the snapshot
// taken first and how to undo, and its button starts at once.
func TestStartAnyway(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-space-age-testmod.zip", true)

	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	body := g.waitPage("the save that would load does not match the world", "foreman-test", "Start anyway")
	for _, want := range []string{`"Before start anyway"`, "restore that snapshot from the Snapshots page",
		`<form method="post" action="/worlds/` + id + `/start" class="actions">`, `name="anyway" value="1"`, "Refused"} {
		if !strings.Contains(body, want) {
			t.Errorf("refusal box lacks %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"Fix the world first", "Try to start again", "/start-anyway"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("page offers start anyway but has %q:\n%s", unwanted, body)
		}
	}
	// A box that no longer matches is refused, and the page says so.
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{"anyway": {"1"}, "seen": {"stale"}}))
	body = g.waitPage("changed since the page showed it")
	if list, _ := g.worlds.Snapshots(id); len(list) != 0 {
		t.Errorf("a stale start anyway took snapshots: %+v", list)
	}
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{"anyway": {"1"}, "seen": {seenIn(t, body)}}))
	g.waitPage(running)
	list, err := g.worlds.Snapshots(id)
	if err != nil || len(list) != 1 || list[0].Label != game.LabelStartAnyway {
		t.Errorf("snapshots %+v, %v: want one labelled %q", list, err, game.LabelStartAnyway)
	}
}

// SPEC 3.7: Start never downloads the image; the page offers the download
// as its own action.
func TestImageMissingOffersDownload(t *testing.T) {
	g := newGameRig(t)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)

	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage("game image for this world", "Download game image")
	g.wantRedirectHome(g.post(gamePath(id, "pull-image"), url.Values{}))
	g.waitPage(stopped, "Start server")
	if strings.Contains(g.get(PathServer), "Download game image") {
		t.Error("the download is still offered after it worked")
	}
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage(running)
}

// SPEC 7.6: the countdown is 0 to 600 seconds; anything else is refused
// with the field's error, and nothing is stopped.
func TestStopCountdownIsChecked(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	for _, v := range []string{"abc", "-1", "601", "1.5"} {
		rep := g.post(gamePath(id, "stop"), url.Values{"countdown": {v}})
		if rep.StatusCode != http.StatusBadRequest || !strings.Contains(rep.body, "Enter a whole number of seconds from 0 to 600") {
			t.Errorf("countdown %q: %d", v, rep.StatusCode)
		}
	}
	if len(g.audit()) != 0 {
		t.Errorf("a refused countdown reached the controller: %+v", g.audit())
	}
}

func TestReadCountdown(t *testing.T) {
	cases := []struct {
		form url.Values
		want game.StopOptions
		bad  bool
	}{
		{url.Values{}, game.StopOptions{Countdown: game.StopCountdown}, false},
		{url.Values{"countdown": {" 90 "}}, game.StopOptions{Countdown: 90 * time.Second}, false},
		{url.Values{"countdown": {"0"}}, game.StopOptions{}, false},
		{url.Values{"countdown": {"600"}}, game.StopOptions{Countdown: 10 * time.Minute}, false},
		{url.Values{"countdown": {"30"}, "no_countdown": {"on"}}, game.StopOptions{}, false},
		{url.Values{"countdown": {"x"}, "no_countdown": {"on"}}, game.StopOptions{}, false},
		{url.Values{"countdown": {"601"}}, game.StopOptions{}, true},
	}
	for _, c := range cases {
		r, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader(c.form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		form, got := readCountdown(r)
		if got != c.want || (form.CountdownError != "") != c.bad {
			t.Errorf("%v: got %+v, error %q", c.form, got, form.CountdownError)
		}
	}
}

// SPEC 7.3: only the active world starts.
func TestOnlyTheActiveWorldStarts(t *testing.T) {
	g := newGameRig(t, image21)
	g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	other := g.newWorld("Other", "2.1.24-no-space-age.zip", false)
	for _, action := range []string{"start", "restart"} {
		if rep := g.post(gamePath(other, action), url.Values{}); rep.StatusCode != http.StatusConflict {
			t.Errorf("%s of an inactive world: %d", action, rep.StatusCode)
		}
	}
	if len(g.audit()) != 0 {
		t.Error("an inactive world's start reached the controller")
	}
}

// SPEC 7.7: a forced stop has its own confirmation that says progress
// since the last save is lost.
func TestForceStop(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage(running)
	if form := g.get(gamePath(id, "force-stop")); !strings.Contains(form, "Lose all progress since the last save") {
		t.Errorf("confirmation:\n%s", form)
	}
	g.wantRedirectHome(g.post(gamePath(id, "force-stop"), url.Values{}))
	g.waitPage(stopped)
	if slices.Contains(g.commands(id), "/server-save") {
		t.Error("a forced stop saved")
	}
}

// SPEC 7.1: with Docker unreachable the page still renders, says Unknown and
// why, and offers no action; a snapshot is refused in plain words.
func TestDockerUnreachable(t *testing.T) {
	g := newGameRigOn(t, "unix://"+filepath.Join(t.TempDir(), "no.sock"), nil)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	body := g.waitPage(`state--unknown">Unknown</span>`, "Foreman cannot reach Docker")
	for _, offer := range []string{"Start server", "Stop server", "Force stop"} {
		if strings.Contains(body, offer) {
			t.Errorf("Unknown offers %q", offer)
		}
	}
	rep := g.post(snapshotsPath(id), url.Values{})
	if rep.StatusCode != http.StatusServiceUnavailable || !strings.Contains(rep.body, "Foreman cannot reach Docker, so nothing was done.") {
		t.Errorf("snapshot with Docker unreachable: %d\n%s", rep.StatusCode, rep.body)
	}
	if list, _ := g.worlds.Snapshots(id); len(list) != 0 {
		t.Error("a snapshot was taken without knowing whether the game runs")
	}
}

// SPEC 6.10: a snapshot of a running world saves the game first.
func TestRunningSnapshotSavesFirst(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage(running)
	rep := g.post(snapshotsPath(id), url.Values{"label": {"Live"}})
	if rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("snapshot: %d\n%s", rep.StatusCode, rep.body)
	}
	if !slices.Contains(g.commands(id), "/server-save") {
		t.Error("the game was not saved before the snapshot")
	}
}

// SPEC 6.5: restoring a running world offers the countdown, stops the game,
// restores, starts it again, and names the undo.
func TestRestoreRunningWorld(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	snap, err := g.worlds.TakeSnapshot(id, "First", "alice")
	if err != nil {
		t.Fatal(err)
	}
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage(running)

	form := g.get(restorePath(id, snap.Name))
	if !strings.Contains(form, `name="countdown"`) || !strings.Contains(form, "Start the game again") {
		t.Errorf("restore confirmation of a running world:\n%s", form)
	}
	g.wantRedirectHome(g.post(restorePath(id, snap.Name), url.Values{"no_countdown": {"on"}}))
	g.waitPage(running, "The world is restored.")
	list, _ := g.worlds.Snapshots(id)
	if len(list) != 2 || list[0].Reason != world.ReasonBeforeRestore {
		t.Errorf("snapshots after restore %+v", list)
	}
}

// openEvents opens the world's event stream. The caller closes the body.
func (g *gameRig) openEvents(id string, cookie bool) *http.Response {
	g.t.Helper()
	req, err := http.NewRequestWithContext(g.t.Context(), http.MethodGet, g.url+gamePath(id, "events"), nil)
	if err != nil {
		g.t.Fatal(err)
	}
	if cookie {
		req.AddCookie(g.cookie)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	return resp
}

// readEvent reads one event's name and data.
func readEvent(t *testing.T, r *bufio.Reader) (name, data string, err error) {
	t.Helper()
	var lines []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return "", "", err
		}
		line = strings.TrimSuffix(line, "\n")
		switch {
		case line == "" && name != "":
			return name, strings.Join(lines, "\n"), nil
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			lines = append(lines, strings.TrimPrefix(line, "data: "))
		}
	}
}

// SPEC 16.3, DESIGN 7.5: the stream sends every region at once, then each
// change, with a sentence for the announcer when the state moves. It ends
// at Close, and refuses anyone not signed in.
func TestEventsStream(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)

	anon := g.openEvents(id, false)
	_ = anon.Body.Close()
	if anon.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous stream: %d", anon.StatusCode)
	}

	resp := g.openEvents(id, true)
	defer func() { _ = resp.Body.Close() }()
	r := bufio.NewReader(resp.Body)
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	seen := map[string]bool{}
	for len(seen) < len(liveRegions) {
		name, data, err := readEvent(t, r)
		if err != nil {
			t.Fatal(err)
		}
		seen[name] = true
		if name == "facts" && !strings.Contains(data, "Stopped") {
			t.Errorf("first facts: %s", data)
		}
	}

	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	for {
		name, data, err := readEvent(t, r)
		if err != nil {
			t.Fatalf("stream ended before the game ran: %v", err)
		}
		if name == "announce" && strings.Contains(data, `<div hx-swap-oob="innerHTML:#announcer">Server is now running</div>`) {
			break
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := readEvent(t, r); err != nil {
				return
			}
		}
	}()
	g.harness.h.(*Handler).Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not end the stream")
	}
}

// With server control, setup and sign-in land on the Server page; without
// it (TestSetupCreatesAdminAndSignsIn) they land on the world list.
func TestSignInLandsOnTheServerPage(t *testing.T) {
	fake := fakegame.New(t, fakegame.Config{Images: []string{image21}})
	hn := newHarnessWith(t, nil, func(worlds *world.Manager, st *store.Store) *game.Controller {
		c, _ := startController(t, fake.Host(), worlds, st, io.Discard)
		return c
	})
	hn.wantRedirect(hn.post(PathSetup, url.Values{"username": {"alice"}, "password": {testPassword}, "confirm": {testPassword}}), PathServer)
	hn.wantRedirect(hn.post(PathSignIn, signInForm("alice", testPassword)), PathServer)
}

func TestDurationText(t *testing.T) {
	cases := map[time.Duration]string{
		0:                                 "less than 1 min",
		59 * time.Second:                  "less than 1 min",
		12 * time.Minute:                  "12 min",
		3*time.Hour + 12*time.Minute:      "3 h 12 min",
		50*time.Hour + 30*time.Minute:     "2 d 2 h",
		3*time.Hour + 12*time.Minute + 59: "3 h 12 min",
	}
	for d, want := range cases {
		if got := durationText(d); got != want {
			t.Errorf("%s: %q, want %q", d, got, want)
		}
	}
	if got := durationISO(3*time.Hour + 12*time.Minute); got != "PT3H12M" {
		t.Errorf("ISO %q", got)
	}
}

// seenIn returns the fingerprint the refusal box's form sends.
func seenIn(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`name="seen" value="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no fingerprint in the box:\n%s", body)
	}
	return html.UnescapeString(m[1])
}
