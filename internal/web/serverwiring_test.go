package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
)

// The Server page's live parts and the results it links on (SPEC 7.6, 17.7,
// 17.9, DESIGN 7.1).

// SPEC 7.6: the stored result replaces the cancelled operation's stand-in.
// Once the cancelled run is over, a later snapshot or download, which clears
// the stored result without starting an operation, must not bring the
// message back.
func TestCancelPendingEndsWithTheRun(t *testing.T) {
	g := newGameRuns()
	defer g.close()

	g.markCancelled("w")
	if g.cancelPending("w") {
		t.Error("a cancel was pending with nothing running")
	}

	release := make(chan struct{})
	g.run("w", true, func(context.Context) actionResult {
		<-release
		return actionResult{Action: game.AuditStop, Err: game.ErrStopCancelled}
	})
	g.markCancelled("w")
	if !g.cancelPending("w") {
		t.Error("no cancel was pending while the cancelled run was still going")
	}
	close(release)
	waitRuns(t, func() bool { _, ok := g.result("w"); return ok })
	if g.cancelPending("w") {
		t.Error("the cancel was still pending after the run stored its result")
	}

	snapshot := make(chan struct{})
	g.run("w", true, func(context.Context) actionResult {
		<-snapshot
		return actionResult{Action: actionSnapshot}
	})
	if g.cancelPending("w") {
		t.Error("a snapshot after the cancel brought the cancelled message back")
	}
	close(snapshot)

	g.markCancelled("w")
	g.unmarkCancelled("w")
	if g.cancelPending("w") {
		t.Error("a cancel that did not happen stayed pending")
	}
}

// SPEC 7.6: after Cancel stop, taking a snapshot never shows the cancelled
// message again, during the snapshot or after it.
func TestSnapshotAfterCancelNeverShowsTheCancelledMessage(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", fixtureNoSA, false)
	g.startGame(id)
	if err := g.fake.Join(game.ContainerName(id), "alice"); err != nil {
		t.Fatal(err)
	}
	g.waitPage(`<tr role="row" id="player-row-alice">`)
	g.wantRedirectHome(g.post(gamePath(id, "stop"), url.Values{"countdown": {"120"}}))
	g.waitPage("Cancel stop")
	g.wantRedirectTo(PathServerResult, g.post(gamePath(id, "cancel-stop"), url.Values{}))
	g.waitPage("The stop was canceled.")

	g.wantRedirectTo(PathServerResult, g.post(gamePath(id, "take-snapshot"), url.Values{}))
	deadline := time.Now().Add(10 * time.Second)
	for {
		body := g.get(PathServer)
		g.wantMissing("page after the snapshot began", body, "was canceled")
		if strings.Contains(body, "Foreman took the snapshot") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the snapshot never finished:\n%s", body)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// SPEC 17.9, DESIGN 5: the Done message takes focus when the page follows the
// admin's own action, and only then.
func TestDoneTakesFocusAfterTheAdminsOwnAction(t *testing.T) {
	g := newGameRig(t, image21)
	g.newWorld("Base", fixtureNoSA, false)
	g.wantRedirectTo(PathServerResult, g.post(gamePath("base", "take-snapshot"), url.Values{}))
	g.waitPage("Foreman took the snapshot")
	g.wantContains("own result", g.get(PathServerResult), `id="done-message" tabindex="-1" autofocus>`, "data-focus-result")
	plain := g.get(PathServer)
	g.wantContains("plain load", plain, `id="done-message" tabindex="-1">`)
	g.wantMissing("plain load", plain, "autofocus", "data-focus-result")
}

// SPEC 17.9: a download Docker Hub has no image for links the page of the
// world whose game version is wrong; for a switch that is the world switched
// to. The raw text of the daemon stays off the page.
func TestNoImageFailureLinksTheWorldPage(t *testing.T) {
	notFound := &game.ImageDownloadError{Image: image20, NotFound: true, Err: &docker.APIError{Status: 404, Message: "manifest unknown"}}
	for name, c := range map[string]struct {
		res  actionResult
		path string
	}{
		"download": {actionResult{Action: game.AuditImageDownload, Err: notFound}, worldPath("base")},
		"switch":   {actionResult{Action: game.AuditSwitch, Err: notFound, Target: "other"}, worldPath("other")},
	} {
		p := serverCase(game.StateStopped, func(p *ServerPage) {
			p.Result, p.HaveResult = c.res, true
		})
		html := fragmentHTML(t, p, "server-control")
		wantAll(t, name, html,
			"Foreman could not download "+image20+": Docker Hub has no image with that version. ",
			`<a href="`+c.path+`" id="act-failure-next">Check the world&#39;s game version on its page.</a>`)
		wantNone(t, name, html, "manifest unknown")
		if got := english(resultAnnouncement(p)); !strings.Contains(got, "Check the world's game version on its page.") {
			t.Errorf("%s: announcement %q lacks the next step", name, got)
		}
	}
}

// The switch box a refused confirmation shows keeps the admin's choice: after
// "Switch, leave stopped", the changed page must not tick "Start after
// switching" again.
func TestChangedSwitchPageKeepsLeaveStopped(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)

	// Shown running, stopped by the time it is confirmed: with no choice, a
	// running game is wanted running again.
	rep := g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}})
	g.wantContains("no choice", rep.body, `name="start" value="1" checked`)

	rep = g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}, "choice": {"stopped"}})
	g.wantContains("leave stopped", rep.body, `name="start" value="1"`, "changed since the page showed it")
	g.wantMissing("leave stopped", rep.body, `name="start" value="1" checked`)
}
