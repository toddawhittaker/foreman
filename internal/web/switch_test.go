package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

const fixtureNoSA = "2.1.24-no-space-age.zip"

func (g *gameRig) wantContains(what, body string, wants ...string) {
	g.t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			g.t.Errorf("%s lacks %q:\n%s", what, w, body)
		}
	}
}

func (g *gameRig) wantMissing(what, body string, nots ...string) {
	g.t.Helper()
	for _, w := range nots {
		if strings.Contains(body, w) {
			g.t.Errorf("%s has %q:\n%s", what, w, body)
		}
	}
}

// SPEC 7.9: a running game is stopped, both worlds are snapshotted, and the
// new world becomes the active one and is started. The confirmation says so
// first, with the stop countdown (DESIGN 7.4).
func TestSwitchRunningWorld(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	g.startGame(a)

	form := g.get(switchPath(b))
	g.wantContains("confirmation", form,
		"Switch from Alpha to Bravo?", `name="from" value="`+a+`"`, `name="countdown"`, `value="60"`,
		"save the game and stop Alpha", "Start the game on Bravo", `&#34;Before switch&#34;`, "button--danger", "Switch to Bravo")

	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}}))
	body := g.waitPage("Active world: Bravo", running, "The worlds are switched")
	g.wantMissing("server page", body, "Start server")
	if err := g.waitResult(b, game.AuditSwitch); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a, b} {
		list, err := g.worlds.Snapshots(id)
		if err != nil || len(list) != 1 || list[0].Label != world.LabelBeforeSwitch {
			t.Errorf("snapshots of %s: %+v, %v", id, list, err)
		}
	}
	if got := g.containerStatus(a); got == "running" {
		t.Errorf("the old world's game is %s", got)
	}
	last := g.audit()[0]
	if last.Action != game.AuditSwitch || last.Username != "alice" || last.WorldID != a {
		t.Errorf("audit entry %+v", last)
	}

	// Back again is a full switch, with its own confirmation.
	g.wantContains("switch back form", g.get(switchPath(a)), "Switch from Bravo to Alpha?", `name="from" value="`+b+`"`)
}

// SPEC 7.9: with the game stopped, the confirmation leaves out the
// countdown and offers "Start Bravo after switching", ticked. Unticked, the
// game stays stopped.
func TestSwitchStoppedWorld(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)

	form := g.get(switchPath(b))
	g.wantContains("confirmation", form, "Switch from Alpha to Bravo?", "Start Bravo after switching",
		`<input type="checkbox" name="start" value="1" checked>`, "if the box below is ticked; otherwise leave the server stopped")
	g.wantMissing("confirmation", form, `name="countdown"`, "Switch, leave stopped")

	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}}))
	g.waitPage("Active world: Bravo", stopped, "The worlds are switched")
	if _, ok := g.fake.Daemon().Container(game.ContainerName(b)); ok {
		t.Error("a stopped switch started the new world")
	}
}

// SPEC 7.9: with the box ticked, a stopped game's switch starts the new
// world in the same run.
func TestSwitchStoppedWorldAndStart(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "start": {"1"}}))
	g.waitPage("Active world: Bravo", running, "The worlds are switched")
}

// SPEC 7.9, DESIGN 7.1: a switch that cannot go ahead is refused on its page
// in plain words, and nothing reaches the controller.
func TestSwitchRefusals(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)

	for name, c := range map[string]struct {
		method, path string
		form         url.Values
		status       int
		want         string
	}{
		"the active world":   {http.MethodGet, switchPath(a), nil, http.StatusOK, "This is already the active world."},
		"post to the active": {http.MethodPost, switchPath(a), url.Values{"from": {a}}, http.StatusConflict, "This is already the active world."},
		"a stale form":       {http.MethodPost, switchPath(b), url.Values{"from": {b}}, http.StatusConflict, "no longer the active world"},
		"no from":            {http.MethodPost, switchPath(b), url.Values{}, http.StatusBadRequest, "does not say which world is active"},
		"a bad countdown":    {http.MethodPost, switchPath(b), url.Values{"from": {a}, "countdown": {"601"}}, http.StatusBadRequest, ""},
		"an unknown world":   {http.MethodPost, switchPath("nope"), url.Values{"from": {a}}, http.StatusNotFound, ""},
	} {
		var rep reply
		if c.method == http.MethodGet {
			rep = g.do(c.method, c.path, nil, http.Header{"Cookie": {g.cookie.String()}})
		} else {
			rep = g.post(c.path, c.form)
		}
		if rep.StatusCode != c.status || !strings.Contains(rep.body, c.want) {
			t.Errorf("%s: %d\n%s", name, rep.StatusCode, rep.body)
		}
	}
	// The countdown refusal keeps the form for a running game.
	g.startGame(a)
	rep := g.post(switchPath(b), url.Values{"from": {a}, "countdown": {"601"}})
	g.wantContains("countdown refusal", rep.body, "Enter a whole number of seconds from 0 to 600", `aria-invalid="true"`)
	if rep.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d", rep.StatusCode)
	}
	if n := len(g.audit()); n != 1 { // only the start
		t.Errorf("a refused switch was audited: %+v", g.audit())
	}
}

// SPEC 7.4, 7.9, 17.8: the new world's start problems show on the
// confirmation page, before the admin confirms, with two choices: switch
// and start anyway, which names the snapshots and goes ahead at once, or
// switch and leave the game stopped. Nothing asks twice.
func TestSwitchAnyway(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", "2.1.24-space-age-testmod.zip", true)
	g.startGame(a)

	form := g.get(switchPath(b))
	g.wantContains("confirmation", form, "The save that would load in Bravo does not match that world:", "foreman-test",
		`&#34;Before switch&#34;`, `&#34;Before start anyway&#34;`, "own mods folder, then start Bravo again",
		`name="choice" value="anyway"`, "Switch and start anyway", `name="choice" value="stopped"`, "Switch, leave stopped",
		`name="countdown"`, "To undo, switch back to Alpha")
	g.wantMissing("confirmation", form, ">Switch to Bravo</button>", "Start Bravo after switching", "import the world again")

	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}, "choice": {"anyway"}, "seen": {seenIn(t, form)}}))
	g.waitPage("Active world: Bravo", running, "The worlds are switched")
	list, err := g.worlds.Snapshots(b)
	if err != nil || len(list) != 2 {
		t.Errorf("snapshots of Bravo %+v, %v: want the switch's and the start anyway's", list, err)
	}
}

// SPEC 7.9: "Switch, leave stopped" switches past the problems without
// starting; the next Start shows them.
func TestSwitchLeaveStopped(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", "2.1.24-space-age-testmod.zip", true)
	g.startGame(a)

	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}, "choice": {"stopped"}}))
	g.waitPage("Active world: Bravo", stopped, "The worlds are switched")
	if _, ok := g.fake.Daemon().Container(game.ContainerName(b)); ok {
		t.Error("a switch that leaves the game stopped made a container for the new world")
	}
	g.wantRedirectHome(g.post(gamePath(b, "start"), url.Values{}))
	g.waitPage("foreman-test", "Start anyway")
}

// SPEC 7.4, 17.8: problems that allow no start anyway show on the
// confirmation with only "Switch, leave stopped". A go-ahead anyway the
// page did not offer, or a plain switch that would now start into problems,
// sends the page back saying what changed, and nothing is done.
func TestSwitchAnywayRefusedWhenNotAllowed(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	if err := removeSaves(g, b); err != nil {
		t.Fatal(err)
	}
	form := g.get(switchPath(b))
	g.wantContains("confirmation", form, "The world has no save.", "Switch, leave stopped", `<p><strong>Error:</strong>`)
	g.wantMissing("confirmation", form, "Switch and start anyway", `name="seen"`)

	for _, v := range []url.Values{
		{"from": {a}, "choice": {"anyway"}, "seen": {"x"}},
		{"from": {a}, "start": {"1"}},
	} {
		rep := g.post(switchPath(b), v)
		if rep.StatusCode != http.StatusConflict {
			t.Errorf("%v: status %d", v, rep.StatusCode)
		}
		g.wantContains("changed page", rep.body, "changed since the page showed it", "The world has no save.")
	}
	if list, _ := g.worlds.Snapshots(a); len(list) != 0 {
		t.Errorf("a refused switch took snapshots: %+v", list)
	}
	if n := len(g.audit()); n != 0 {
		t.Errorf("a switch sent back to its page was audited: %+v", g.audit())
	}
}

func removeSaves(g *gameRig, id string) error {
	root, err := g.worlds.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return root.Remove("saves/base.zip")
}

// SPEC 16.3: the page of the world switched away from is told to load the
// Server page again, because it now shows another world.
func TestSwitchTellsTheOldPageToFollow(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	buf, _, closeIt := g.collectEvents(a)
	defer closeIt()

	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}}))
	g.waitPage("Active world: Bravo")
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(buf.String(), "event: moved") {
		if time.Now().After(deadline) {
			t.Fatalf("the old page was never told to follow:\n%s", buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := strings.Count(buf.String(), "event: moved"); n != 1 {
		t.Errorf("moved sent %d times", n)
	}
}

// A switch is one result on both worlds, so each page shows how it ended.
func TestRunOnKeepsTheResultOnEveryWorld(t *testing.T) {
	g := newGameRuns()
	defer g.close()
	want := errors.New("boom")
	g.runOn([]string{"a", "b"}, true, func(context.Context) actionResult {
		return actionResult{Action: game.AuditSwitch, Err: want, Target: "b"}
	})
	waitRuns(t, func() bool { _, ok := g.result("b"); return ok })
	for _, id := range []string{"a", "b"} {
		if res, ok := g.result(id); !ok || res.Action != game.AuditSwitch || !errors.Is(res.Err, want) || res.Target != "b" {
			t.Errorf("result of %s: %+v, %v", id, res, ok)
		}
	}
}

func renderControl(t *testing.T, p ServerPage) string {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := r.Fragment(&b, i18n.English, p, "server-control"); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func switchPageOf(world string, st game.Status, names map[string]string) ServerPage {
	return ServerPage{World: worldSummary(world), HasWorld: true, Status: st, WorldNames: names}
}

func worldSummary(id string) world.Summary { return world.Summary{ID: id, Name: id, Active: true} }

var switchNames = map[string]string{"alpha": "Alpha", "bravo": "Bravo"}

func failedSwitch(presented bool) game.Status {
	return game.Status{State: game.StateStopped, Operation: &game.Operation{
		Action: game.AuditSwitch, Done: true, Error: "start the game: it failed",
		Switch: &game.SwitchInfo{From: "alpha", To: "bravo", Presented: presented},
		Steps:  []game.Step{{Name: game.StepPresent, State: game.StepDone}, {Name: "Start the container", State: game.StepFailed}},
	}}
}

// SPEC 7.9: after a failure, a switch that had already made the new world
// active offers a switch back; one that had not offers only Start of the
// old world, which the page already has.
func TestFailedSwitchOffers(t *testing.T) {
	presented := renderControl(t, switchPageOf("bravo", failedSwitch(true), switchNames))
	for _, want := range []string{`href="/worlds/alpha/switch"`, "Switch back to Alpha", "Bravo is now the active world", "The switch from Alpha to Bravo failed", "Start server"} {
		if !strings.Contains(presented, want) {
			t.Errorf("presented failure lacks %q:\n%s", want, presented)
		}
	}
	notPresented := renderControl(t, switchPageOf("alpha", failedSwitch(false), switchNames))
	for _, want := range []string{"Nothing was switched. Alpha is still the active world.", "Start server"} {
		if !strings.Contains(notPresented, want) {
			t.Errorf("failure before presenting lacks %q:\n%s", want, notPresented)
		}
	}
	if strings.Contains(notPresented, "Switch back") {
		t.Errorf("a switch that changed nothing offers a switch back:\n%s", notPresented)
	}
}

// SPEC 7.10: the notice of an unfinished switch shows its words on the
// world it sits on. Presented offers Start of the new world (the page's own
// button) and a switch back; not presented, only Start of the old one.
func TestSwitchNoticeOffers(t *testing.T) {
	notice := func(presented bool, text string) game.Status {
		return game.Status{State: game.StateStopped, Notice: &game.SwitchNotice{
			SwitchInfo: game.SwitchInfo{From: "alpha", To: "bravo", Presented: presented}, Text: text}}
	}
	got := renderControl(t, switchPageOf("bravo", notice(true, "Bravo is active; the switch did not confirm it running."), switchNames))
	for _, want := range []string{"Bravo is active; the switch did not confirm it running.", "Switch back to Alpha", "Start server"} {
		if !strings.Contains(got, want) {
			t.Errorf("presented notice lacks %q:\n%s", want, got)
		}
	}
	got = renderControl(t, switchPageOf("alpha", notice(false, "Switch from Alpha to Bravo stopped at the snapshots. Alpha is still active and was not started again."), switchNames))
	if !strings.Contains(got, "was not started again") || !strings.Contains(got, "Start server") || strings.Contains(got, "Switch back") {
		t.Errorf("unpresented notice:\n%s", got)
	}
}

// The step list of a running switch shows on the page with the names.
func TestRunningSwitchShowsSteps(t *testing.T) {
	st := game.Status{State: game.StateStopping, Operation: &game.Operation{
		Action: game.AuditSwitch,
		Switch: &game.SwitchInfo{From: "alpha", To: "bravo"},
		Steps:  []game.Step{{Name: "Warn players", State: game.StepProgress}, {Name: game.StepSnapshotBoth, State: game.StepWaiting}},
	}}
	got := renderControl(t, switchPageOf("alpha", st, switchNames))
	for _, want := range []string{"Switching from Alpha to Bravo", "Warn players", game.StepSnapshotBoth} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Switch back") {
		t.Errorf("a running switch offers a switch back:\n%s", got)
	}
}

// DESIGN 7.1, SPEC 7.9: every controller refusal has words, and none is a 500.
func TestSwitchRefusalsHaveWords(t *testing.T) {
	for _, err := range []error{
		game.ErrSwitchSame, game.ErrSwitchNotActive, game.ErrSwitchNoTarget, game.ErrSwitchTargetRunning,
		game.ErrSwitchUnavailable, world.ErrBusy, game.ErrImageMissing, game.ErrNetwork,
	} {
		status, msg := gameErrorText(err)
		if status != http.StatusConflict || msg == "" || !strings.HasSuffix(msg, ".") {
			t.Errorf("%v: %d %q", err, status, msg)
		}
	}
	if status, _ := gameErrorText(game.ErrCountdown); status != http.StatusBadRequest {
		t.Errorf("countdown: %d", status)
	}
	if status, msg := gameErrorText(precheckErr()); status != http.StatusConflict || !strings.Contains(msg, "The save is from a newer game.") {
		t.Errorf("precheck: %d %q", status, msg)
	}
}

// SPEC 7.1, DESIGN 7.1: a Docker that stops answering while the switch is
// planned is a plain refusal on the page, in the words that name DOCKER_HOST,
// and not the failure page.
func TestUnreachableDockerHasPlainWords(t *testing.T) {
	err := fmt.Errorf("plan the switch: %w", &docker.UnreachableError{
		Target: "unix:///var/run/docker.sock",
		Err:    &url.Error{Op: "Get", URL: "http://docker/images/x/json?filters=%7B", Err: errors.New("dial unix: no such file")},
	})
	status, msg := gameErrorText(err)
	if status == http.StatusInternalServerError {
		t.Fatalf("status %d: the page would show the failure page", status)
	}
	for _, want := range []string{"Foreman cannot reach Docker at unix:///var/run/docker.sock", "(DOCKER_HOST)", "Nothing was done."} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	for _, raw := range []string{"dial", "filters", "http://"} {
		if strings.Contains(msg, raw) {
			t.Errorf("message %q holds raw detail %q", msg, raw)
		}
	}
}

// SPEC 6.2: the snapshot list words the reasons switching adds.
func TestSwitchSnapshotReasons(t *testing.T) {
	s := world.Snapshot{Manifest: world.Manifest{Reason: world.ReasonSwitch, Label: "Final copy"}}
	if got := snapshotReason(i18n.English, s); got != "Before switch" {
		t.Errorf("snapshotReason(switch) = %q, want %q", got, "Before switch")
	}
}

// The world page offers a switch to a world that is not active, and not to
// the active one.
func TestWorldPageOffersSwitch(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	g.wantContains("inactive world", g.get(worldPath(b)), "Switch to this world", `href="`+switchPath(b)+`"`)
	g.wantMissing("active world", g.get(worldPath(a)), "Switch to this world")
}

// SPEC 7.9: a start that fails after the new world became active leaves it
// active, says which step failed, and offers a switch back, which is a full
// switch with a confirmation of its own.
func TestSwitchFailsAfterPresenting(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	g.startGame(a)
	g.fake.SetRCONRefuse(game.ContainerName(b), true)

	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}}))
	body := g.waitPage("The switch from Alpha to Bravo failed", "Switch back to Alpha", "Bravo is now the active world")
	g.wantContains("server page", body, "Active world: Bravo", `href="`+switchPath(a)+`"`)
	g.wantMissing("server page", body, "The worlds are switched")
	g.wantContains("switch back form", g.get(switchPath(a)), "Switch from Bravo to Alpha?")
}

// SPEC 17.8: the confirmation shows what the switch will do for the game's
// state at that moment. When the state differs at confirm time, nothing is
// done, and the page comes back showing the new state.
func TestSwitchRefusedWhenRunningChanged(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)

	// Shown stopped, running by the time it is confirmed: a stop was never agreed to.
	form := g.get(switchPath(b))
	g.wantContains("stopped confirmation", form, `name="running" value="0"`)
	g.startGame(a)
	rep := g.post(switchPath(b), url.Values{"from": {a}, "running": {"0"}, "start": {"1"}})
	if rep.StatusCode != http.StatusConflict {
		t.Errorf("status %d\n%s", rep.StatusCode, rep.body)
	}
	g.wantContains("changed page", rep.body, "changed since the page showed it", `name="running" value="1"`, `name="countdown"`,
		`id="action-changed" tabindex="-1" autofocus`)
	if got := g.containerStatus(a); got != "running" {
		t.Errorf("the game was %s, want it left running", got)
	}
	if list, _ := g.worlds.Snapshots(a); len(list) != 0 {
		t.Errorf("a refused switch took snapshots: %+v", list)
	}

	// Shown running, stopped by the time it is confirmed.
	g.wantContains("running confirmation", g.get(switchPath(b)), `name="running" value="1"`)
	g.wantRedirectHome(g.post(gamePath(a, "stop"), url.Values{"countdown": {"0"}}))
	g.waitPage(stopped)
	rep = g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}})
	if rep.StatusCode != http.StatusConflict {
		t.Errorf("status %d\n%s", rep.StatusCode, rep.body)
	}
	g.wantContains("changed page", rep.body, "changed since the page showed it", `name="running" value="0"`)
	if list, _ := g.worlds.Snapshots(a); len(list) != 0 {
		t.Errorf("a refused switch took snapshots: %+v", list)
	}
	if got := g.fake.State().Containers; len(got) != 1 {
		t.Errorf("containers %+v: the refused switch started a world", got)
	}
}
