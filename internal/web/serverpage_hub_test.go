package web

import (
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The Server page as the hub for the active world (SPEC 7.6, 7.9, 17.7 to
// 17.10).

func fragmentHTML(t *testing.T, p ServerPage, name string) string {
	t.Helper()
	var b strings.Builder
	if err := newTestRenderer(t).Fragment(&b, i18n.English, p, name); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func wantAll(t *testing.T, what, html string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(html, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, html)
		}
	}
}

func wantNone(t *testing.T, what, html string, nots ...string) {
	t.Helper()
	for _, w := range nots {
		if strings.Contains(html, w) {
			t.Errorf("%s has %q:\n%s", what, w, html)
		}
	}
}

// Every link and button in a live region keeps an id (DESIGN 5).
func wantIDs(t *testing.T, html string) {
	t.Helper()
	for _, tag := range regexp.MustCompile(`<(a|button)\b[^>]*>`).FindAllString(html, -1) {
		if !strings.Contains(tag, ` id="`) {
			t.Errorf("action without an id: %s", tag)
		}
	}
}

var hubNames = map[string]string{"alpha": "Alpha", "bravo": "Bravo", "base": longName}

// VISION principle 1, SPEC 17.9: every Done message offers its undo as a
// button that opens the undo's own confirmation, and says when it happened.
func TestDoneOffersUndoAndItsTime(t *testing.T) {
	at := msgT0
	cases := map[string]struct {
		res       actionResult
		rollback  *RollbackOffer
		path      string
		label     string
		sentences []string
	}{
		"restore": {res: actionResult{Action: game.ActionRestore, Undo: "20261104T100000Z"},
			path: restorePath("base", "20261104T100000Z"), label: "Undo this restore"},
		"rollback": {res: actionResult{Action: game.AuditRollback, Undo: "20261104T100001Z"},
			path: restorePath("base", "20261104T100001Z"), label: "Undo this rollback"},
		"update": {res: actionResult{Action: game.AuditUpdate, Version: "2.1.24"},
			rollback: &RollbackOffer{Version: "2.1.23", Path: rollbackPath("base")},
			path:     rollbackPath("base"), label: "Roll back to 2.1.23"},
		"switch": {res: actionResult{Action: game.AuditSwitch, Target: "base", From: "alpha"},
			path: switchPath("alpha"), label: "Switch back to Alpha", sentences: []string{"The worlds are switched, from Alpha to "}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			c.res.At = at
			p := serverCase(game.StateRunning, func(p *ServerPage) {
				p.Result, p.HaveResult, p.Rollback, p.WorldNames = c.res, true, c.rollback, hubNames
			})
			html := controlHTML(t, p)
			wantAll(t, name, html, append(c.sentences, `href="`+c.path+`" class="button" id="act-undo">`+c.label+`</a>`,
				`Finished at <time datetime="2026-11-04T10:00:00Z">2026-11-04 10:00 UTC</time>.`)...)
			wantNone(t, name, html, "from the Snapshots page", "use Roll back on the world")
			wantIDs(t, html)
		})
	}
}

// SPEC 7.6: a cancelled stop is not an error. The page says nothing
// changed and drops the step list of skipped steps.
func TestCancelledStopIsReportedAsDone(t *testing.T) {
	for action, want := range map[string]string{
		game.AuditStop:    "The stop was cancelled. Nothing was saved or stopped, and nothing changed.",
		game.AuditRestart: "The restart was cancelled.",
		game.AuditSwitch:  "The switch to Bravo was cancelled. " + html.EscapeString(longName) + " is still the active world, and nothing changed.",
	} {
		p := serverCase(game.StateRunning, func(p *ServerPage) {
			p.Result, p.HaveResult = actionResult{Action: action, Err: game.ErrStopCancelled, Target: "bravo", At: msgT0}, true
			p.WorldNames = hubNames
			p.Status.Operation = &game.Operation{Action: action, Done: true, Error: game.ErrStopCancelled.Error(),
				Steps: []game.Step{{Name: game.StepWarn, State: game.StepSkipped}}}
			if action == game.AuditSwitch {
				p.Status.Operation.Switch = &game.SwitchInfo{From: "base", To: "bravo"}
			}
		})
		html := controlHTML(t, p)
		wantAll(t, action, html, "<strong>Done:</strong> "+want)
		wantNone(t, action, html, "Error:", `<ol class="steps"`, "act-undo", "Switch back")
		if got := english(resultAnnouncement(p)); !strings.Contains(got, "was cancelled.") {
			t.Errorf("%s: announcement %q", action, got)
		}
	}
}

// SPEC 7.6: while players are warned, the page shows the time left, keeps
// listing who was online, and offers Cancel stop, which in a switch posts
// to the world being stopped.
func TestCountdownShowsTimeLeftPlayersAndCancel(t *testing.T) {
	now := msgT0
	countdown := func(sw *game.SwitchInfo) ServerPage {
		return serverCase(game.StateStopping, func(p *ServerPage) {
			p.Now, p.WorldNames = now, hubNames
			p.Status.Operation = &game.Operation{Action: game.AuditStop, Switch: sw,
				Players: []game.Player{{Name: "alice", Joined: now.Add(-time.Hour), Duration: time.Hour}},
				Steps: []game.Step{{Name: game.StepWarn, State: game.StepProgress, Until: now.Add(17*time.Second + time.Millisecond)},
					{Name: game.StepSave, State: game.StepWaiting}}}
			if sw != nil {
				p.Status.Operation.Action = game.AuditSwitch
			}
		})
	}
	p := countdown(nil)
	html := controlHTML(t, p)
	// The browser counts down from data-until; without script the text shows the time left at render.
	wantAll(t, "control", html, `Warn players: <span data-until="`+now.Add(17*time.Second+time.Millisecond).UTC().Format(time.RFC3339)+`">18 seconds left</span>`,
		`<form method="post" action="/worlds/base/cancel-stop" class="actions"><button type="submit" class="button button--primary" id="act-cancel-stop">Cancel stop</button></form>`)
	wantIDs(t, html)
	players := fragmentHTML(t, p, "server-players")
	wantAll(t, "players", players, "<caption>1 online when the stop began</caption>", `<td role="cell">alice</td>`)

	// In a switch the page follows the old world, which is the one stopping.
	sw := countdown(&game.SwitchInfo{From: "alpha", To: "bravo"})
	wantAll(t, "switch", controlHTML(t, sw), `action="/worlds/alpha/cancel-stop"`)

	// Once the countdown is over, there is nothing to cancel.
	over := countdown(nil)
	over.Status.Operation.Steps[0].State = game.StepDone
	wantNone(t, "after the countdown", controlHTML(t, over), "Cancel stop")
}

func TestStepTimeLeft(t *testing.T) {
	l := stepList{Page: ServerPage{Now: msgT0}}
	for d, want := range map[time.Duration]string{
		60 * time.Second:        "60 seconds left",
		1500 * time.Millisecond: "2 seconds left",
		time.Second:             "1 second left",
		0:                       "ending now",
		-time.Second:            "ending now",
	} {
		if got := english(l.Left(game.Step{State: game.StepProgress, Until: msgT0.Add(d)})); got != want {
			t.Errorf("%v: got %q, want %q", d, got, want)
		}
	}
	if got := l.Left(game.Step{State: game.StepProgress}); !got.IsZero() {
		t.Errorf("a step with no countdown says %q", got)
	}
}

// SPEC 17.9: after a crash loop, one box says what happened and offers the
// ways back: restore "Before start anyway", and switch back while Foreman
// still holds the switch. After a Foreman restart only the restore is left.
func TestCrashLoopOffersRecovery(t *testing.T) {
	snap := &world.Snapshot{Name: "20261104T095900Z",
		Manifest: world.Manifest{Label: game.LabelStartAnyway, Reason: game.ReasonStartAnyway, TakenAt: msgT0.Add(-time.Minute)}}
	switched := serverCase(game.StateCrashLooping, func(p *ServerPage) {
		p.StartAnywaySnap, p.WorldNames = snap, hubNames
		p.Result, p.HaveResult = actionResult{Action: game.AuditSwitch, Target: "base", From: "alpha", At: msgT0}, true
		p.Status.LastCrash = msgT1
	})
	html := controlHTML(t, switched)
	wantAll(t, "crash loop", html,
		`href="`+restorePath("base", snap.Name)+`" class="button" id="act-recover-restore">Restore &#34;Before start anyway&#34; (<time datetime="2026-11-04T09:59:00Z">2026-11-04 09:59 UTC</time>)</a>`,
		`href="/worlds/alpha/switch" class="button" id="act-switch-back">Switch back to Alpha</a>`,
		"then try to start it again.", ">Try to start again<")
	wantNone(t, "crash loop", html, "<strong>Note:</strong>", "Worlds page", `id="act-undo"`)
	if n := strings.Count(html, "Switch back to Alpha"); n != 1 {
		t.Errorf("switch back offered %d times", n)
	}
	wantIDs(t, html)

	restarted := serverCase(game.StateCrashLooping, func(p *ServerPage) { p.StartAnywaySnap = snap })
	html = controlHTML(t, restarted)
	wantAll(t, "after a restart", html, `id="act-recover-restore"`)
	wantNone(t, "after a restart", html, "Switch back")

	bare := controlHTML(t, serverCase(game.StateCrashLooping, func(*ServerPage) {}))
	wantNone(t, "no snapshot", bare, "put things back", "act-recover-restore")
}

// SPEC 7.1, 7.2: when Docker cannot be asked, figures that may be stale are
// left out, and the page does not claim nobody is online.
func TestUnknownStateShowsNoStaleFacts(t *testing.T) {
	p := serverCase(game.StateUnknown, func(p *ServerPage) {
		p.Status.Unknown = game.UnknownReason{Kind: game.UnknownDocker}
		p.Status.Save, p.Status.HaveUsage, p.Status.MemoryBytes = "old.zip", true, 1<<30
		p.Status.GameVersion = "2.1.20"
	})
	facts := fragmentHTML(t, p, "server-facts")
	wantAll(t, "facts", facts, "Foreman cannot reach Docker.")
	// The live facts carry the heading's badge, which says Unknown (DESIGN 7.5).
	p.Live = true
	wantAll(t, "live facts", fragmentHTML(t, p, "server-facts"), `<span id="server-title" hx-swap-oob="innerHTML">`, "Unknown")
	wantNone(t, "facts", facts, "old.zip", "CPU", "Memory", "Uptime", "2.1.20")
	players := fragmentHTML(t, p, "server-players")
	wantAll(t, "players", players, "Foreman cannot see whether anyone is online.")
	wantNone(t, "players", players, "Nobody")
}

// SPEC 7.9: during a switch the status names both worlds, and the active
// world line moves to the new world once it is active; the old world's run
// figures are no longer shown then.
func TestSwitchShowsTheNewActiveWorld(t *testing.T) {
	for presented, active := range map[bool]string{false: "Alpha", true: "Bravo"} {
		p := serverCase(game.StateStarting, func(p *ServerPage) {
			p.World = world.Summary{ID: "alpha", Name: "Alpha", GameVersion: "2.1.24", Active: true}
			p.WorldNames = hubNames
			p.Status.Save, p.Status.HaveUsage = "alpha.zip", true
			p.Status.Operation = &game.Operation{Action: game.AuditSwitch, Switch: &game.SwitchInfo{From: "alpha", To: "bravo", Presented: presented}}
		})
		link := fragmentHTML(t, p, "server-active")
		wantAll(t, "active", link, `href="/worlds/`+strings.ToLower(active)+`" id="act-world">World page</a>`)
		facts := fragmentHTML(t, p, "server-facts")
		wantAll(t, "facts", facts, "<dt>Switching</dt><dd>From Alpha to Bravo</dd>")
		if presented == strings.Contains(facts, "alpha.zip") {
			t.Errorf("presented %v: the old save shown is %v:\n%s", presented, !presented, facts)
		}
	}
}

// SPEC 17.7, 17.10: Take snapshot is one click on the Server page, offered
// whenever the snapshot would not be refused.
func TestTakeSnapshotOffered(t *testing.T) {
	for state, want := range map[game.State]bool{
		game.StateStopped: true, game.StateRunning: true, game.StateCrashLooping: true,
		game.StateStarting: false, game.StateStopping: false, game.StateCrashed: false, game.StateUnknown: false,
	} {
		html := controlHTML(t, serverCase(state, func(*ServerPage) {}))
		if got := strings.Contains(html, `<form method="post" action="/worlds/base/take-snapshot"><button type="submit" class="button" id="act-snapshot">Take snapshot</button></form>`); got != want {
			t.Errorf("%s: offered %v, want %v", state, got, want)
		}
	}
	busy := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Status.Operation = &game.Operation{Action: game.AuditRestart}
	})
	wantNone(t, "busy", controlHTML(t, busy), "Take snapshot")
	done := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: actionSnapshot, Taken: defaultLabel, At: msgT0}, true
	})
	wantAll(t, "done", controlHTML(t, done), `<strong>Done:</strong> Foreman took the snapshot &#34;Manual snapshot&#34; of `, `id="act-snapshots"`)
}

// SPEC 7.6, 7.7: the forms that stop the game name who is online.
func TestOnlineWords(t *testing.T) {
	cases := []struct {
		o          Online
		text, gone string
	}{
		{Online{Asked: true}, "Nobody is online.", "Nobody is online."},
		{Online{Asked: true, Names: []string{"alice"}}, "1 player online: alice.", "These players are disconnected: alice."},
		{Online{Asked: true, Names: []string{"alice", "bob"}}, "2 players online: alice, bob.", "These players are disconnected: alice, bob."},
		{Online{Asked: true, Failed: true}, "Foreman could not ask the game who is online.", "Players online are disconnected."},
		{Online{}, "Nobody is online.", "Players online are disconnected."},
	}
	for _, c := range cases {
		if c.o.Asked && english(c.o.Text()) != c.text {
			t.Errorf("%+v: Text %q, want %q", c.o, c.o.Text(), c.text)
		}
		if english(c.o.Disconnected()) != c.gone {
			t.Errorf("%+v: Disconnected %q, want %q", c.o, c.o.Disconnected(), c.gone)
		}
	}
}

// SPEC 7.6, 12.9: Cancel stop over HTTP ends the countdown, audits
// "stop-cancel", and leaves the game running; with no countdown it says so.
func TestCancelStopOverHTTP(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", fixtureNoSA, false)
	g.startGame(id)
	if err := g.fake.Join(game.ContainerName(id), "alice"); err != nil {
		t.Fatal(err)
	}
	g.waitPage(`<td role="cell">alice</td>`)
	g.wantContains("force stop form", g.get(gamePath(id, "force-stop")), "These players are disconnected: alice.")

	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"120"}}))
	g.waitPage("Cancel stop", "seconds left", "1 online when the stop began", `<td role="cell">alice</td>`)

	// Time passing alone sends nothing: the browser counts down from
	// data-until, so Cancel stop is never swapped out under a tap (DESIGN
	// 7.6). The page reads the rig's clock, which moves only when told to.
	events, _, closeEvents := g.collectEvents(id)
	left := regexp.MustCompile(`Warn players: <span data-until="[^"]+">([\d,]+) seconds left</span>`)
	deadline := time.Now().Add(5 * time.Second)
	for !left.MatchString(events.String()) {
		if time.Now().After(deadline) {
			t.Fatalf("the first send lacks the time left:\n%s", events.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	for range 3 {
		g.clock.Advance(time.Second)
		time.Sleep(500 * time.Millisecond)
	}
	if n := strings.Count(events.String(), "event: control\n"); n != 1 {
		t.Fatalf("control sent %d times while only time passed, want 1:\n%s", n, events.String())
	}
	closeEvents()

	g.wantRedirectTo(PathServerResult, g.post(gamePath(id, "cancel-stop"), url.Values{}))
	body := g.waitPage(running, "The stop was cancelled.")
	g.wantMissing("server page", body, "Error:", "Cancel stop")
	if !g.hasAudit(game.AuditStopCancel) {
		t.Error("no stop-cancel audit entry")
	}

	// A cancel that finds no countdown, as when it just ended, goes to the
	// Server page like any other; no error page.
	g.wantRedirectTo(PathServerResult, g.post(gamePath(id, "cancel-stop"), url.Values{}))
}

// SPEC 17.9: a crash loop offers to restore "Before start anyway" only
// while it is the newest snapshot; a later one, such as a manual snapshot,
// means the world moved on and the offer would undo it.
func TestStartAnywayRestoreOnlyWhenNewest(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", fixtureNoSA, false)
	s := &server{worlds: g.worlds, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := g.worlds.AutoSnapshot(id, game.ReasonStartAnyway, game.LabelStartAnyway, "alice"); err != nil {
		t.Fatal(err)
	}
	if s.startAnywaySnapshot(id) == nil {
		t.Fatal("the newest snapshot is Before start anyway, but no restore is offered")
	}
	g.clock.Advance(time.Minute)
	if _, err := g.worlds.TakeSnapshot(id, "Later", "alice"); err != nil {
		t.Fatal(err)
	}
	if sn := s.startAnywaySnapshot(id); sn != nil {
		t.Errorf("a newer snapshot exists, but %q is offered", sn.Name)
	}
}

// SPEC 17.9: the Server page after the admin's own cancel stop or snapshot is
// marked, so its Done message takes focus; a plain load is not.
func TestFollowsOwnAction(t *testing.T) {
	for target, want := range map[string]bool{PathServer: false, PathServerResult: true, PathServer + "?result=2": false} {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		if got := followsOwnAction(r); got != want {
			t.Errorf("%s: %v, want %v", target, got, want)
		}
	}
}

// SPEC 7.6: the page says the action was cancelled from the finished
// operation alone, before the run's result is stored.
func TestCancelledShowsFromTheOperation(t *testing.T) {
	op := &game.Operation{Action: game.AuditRestart, Done: true, Error: game.ErrStopCancelled.Error()}
	res, ok := cancelledByOperation(game.Status{Operation: op}, time.Now())
	if !ok {
		t.Fatal("a cancelled operation was not recognised")
	}
	p := ServerPage{World: world.Summary{ID: "base", Name: "Base"}, Result: res, HaveResult: true}
	if got := english(p.Done()); !strings.Contains(got, "The restart was cancelled.") {
		t.Errorf("Done %q", got)
	}
	sw := &game.Operation{Action: game.AuditSwitch, Done: true, Error: game.ErrStopCancelled.Error(),
		Switch: &game.SwitchInfo{From: "base", To: "other"}}
	if res, _ := cancelledByOperation(game.Status{Operation: sw}, time.Now()); res.Target != "other" {
		t.Errorf("switch result %+v", res)
	}
	for name, o := range map[string]*game.Operation{
		"none":    nil,
		"running": {Action: game.AuditStop},
		"failed":  {Action: game.AuditStop, Done: true, Error: "the save timed out"},
		"worked":  {Action: game.AuditStop, Done: true},
	} {
		if _, ok := cancelledByOperation(game.Status{Operation: o}, time.Now()); ok {
			t.Errorf("%s: recognised as cancelled", name)
		}
	}
}

// SPEC 17.7: the Server page takes a snapshot in one click and shows the
// result there.
func TestSnapshotFromServerPage(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", fixtureNoSA, false)
	g.wantRedirectTo(PathServerResult, g.post(gamePath(id, "take-snapshot"), url.Values{}))
	g.waitPage(`<strong>Done:</strong> Foreman took the snapshot &#34;Manual snapshot&#34; of Base.`)
	snaps, err := g.worlds.Snapshots(id)
	if err != nil || len(snaps) != 1 || snaps[0].Label != defaultLabel {
		t.Errorf("snapshots %+v, %v", snaps, err)
	}
}

// SPEC 17.7, 17.10: the Server page links to the Worlds list, where each
// world has its Switch button, and the Done message after a switch offers
// the way back.
func TestServerPageSwitchesAndSwitchesBack(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	body := g.get(PathServer)
	g.wantContains("server page", body, `<a href="/worlds" id="act-switch-world">Switch world</a>`)
	g.wantMissing("server page", body, "Switch to Bravo", "Switch to Alpha")
	g.wantContains("worlds list", g.get(PathWorlds), `href="`+switchPath(b)+`"`)

	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "countdown": {"0"}}))
	body = g.waitPage(`id="server-title">Bravo <span`, "The worlds are switched, from Alpha to Bravo.")
	g.wantContains("after the switch", body, `href="`+switchPath(a)+`" class="button" id="act-undo">Switch back to Alpha</a>`)
}

// SPEC 3.7: a world that is not active says on its page when this host
// lacks its game image, before any switch.
func TestWorldPageSaysImageIsMissing(t *testing.T) {
	g := newGameRig(t)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	g.wantContains("other world", g.get(worldPath(b)), "<code>"+game.ImageRef("2.1.24")+"</code>, which is not on this host")
	g.wantMissing("active world", g.get(worldPath(a)), "which is not on this host")

	g.addImage("2.1.24")
	g.wantMissing("with the image", g.get(worldPath(b)), "which is not on this host")
}
