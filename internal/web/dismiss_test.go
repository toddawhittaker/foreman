package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
)

// SPEC 17.9: closing a Done box forgets the world's last result, but only the
// one the page showed, only a success or a cancelled stop, and nothing else
// the world holds.
func TestDismissForgetsOnlyTheMatchingResult(t *testing.T) {
	g := newGameRuns()
	defer g.close()
	end := func(id string, res actionResult) actionResult {
		g.run(id, true, func(context.Context) actionResult { return res })
		waitRuns(t, func() bool { return !g.inFlight(id) })
		got, ok := g.result(id)
		if !ok {
			t.Fatalf("%s holds no result after %+v", id, res)
		}
		return got
	}

	done := end("w", actionResult{Action: game.AuditUpdate})
	g.dismiss("w", done.At.UnixNano()+1)
	if _, ok := g.result("w"); !ok {
		t.Error("a close naming another time forgot the result")
	}
	g.dismiss("other", done.At.UnixNano())
	if _, ok := g.result("w"); !ok {
		t.Error("a close for another world forgot the result")
	}
	g.dismiss("w", done.At.UnixNano())
	if _, ok := g.result("w"); ok {
		t.Error("a matching close left the result")
	}
	g.dismiss("w", done.At.UnixNano()) // closing twice is harmless

	cancelled := end("w", actionResult{Action: game.AuditStop, Err: game.ErrStopCancelled})
	g.dismiss("w", cancelled.At.UnixNano())
	if _, ok := g.result("w"); ok {
		t.Error("a cancelled stop could not be closed")
	}

	failed := end("w", actionResult{Action: game.AuditUpdate, Err: errors.New("boom")})
	g.dismiss("w", failed.At.UnixNano())
	if _, ok := g.result("w"); !ok {
		t.Error("a failure was forgotten by a close")
	}

	// A newer result stays when the close names the older one.
	older := end("v", actionResult{Action: game.AuditUpdate})
	newer := end("v", actionResult{Action: game.AuditSwitch})
	g.dismiss("v", older.At.UnixNano())
	if res, ok := g.result("v"); !ok || res.Action != game.AuditSwitch {
		t.Errorf("a stale close changed the result: %+v, %v", res, ok)
	}
	g.dismiss("v", newer.At.UnixNano())
	if _, ok := g.result("v"); ok {
		t.Error("the newer result could not be closed")
	}
}

// A close touches neither the actions asked for during another nor the ones
// still running.
func TestDismissLeavesAsidesAndRunningActions(t *testing.T) {
	g := newGameRuns()
	defer g.close()
	first, second := make(chan struct{}), make(chan struct{})
	g.run("w", true, func(context.Context) actionResult {
		<-first
		return actionResult{Action: game.AuditUpdate}
	})
	g.run("w", true, func(context.Context) actionResult {
		<-second
		return actionResult{Action: game.AuditRestart, Err: errors.New("boom")}
	})
	close(first)
	waitRuns(t, func() bool { _, ok := g.result("w"); return ok })
	res, _ := g.result("w")
	g.dismiss("w", res.At.UnixNano())
	if _, ok := g.result("w"); ok {
		t.Error("the result was not forgotten")
	}
	if !g.inFlight("w") {
		t.Error("the close ended the running action")
	}
	close(second)
	waitRuns(t, func() bool { return !g.inFlight("w") })
	if got := g.others("w"); len(got) != 1 || got[0].Action != game.AuditRestart {
		t.Errorf("asides %+v after the close", got)
	}
}

// DESIGN 7.3: only a success or a cancelled stop has a close button; a
// failure's box stays.
func TestOnlyDoneBoxesHaveACloseButton(t *testing.T) {
	at := scheduleNow
	withResult := func(res actionResult) string {
		res.At = at
		return controlHTML(t, serverCase(game.StateStopped, func(p *ServerPage) {
			p.HaveResult, p.Result = true, res
		}))
	}
	form := []string{`action="/worlds/base/dismiss-result"`, `hx-post="/worlds/base/dismiss-result"`, `data-dismiss-result`,
		`name="at" value="` + strconv.FormatInt(at.UnixNano(), 10) + `"`, `id="act-dismiss"`, `<span class="visually-hidden">Close this message</span>`}
	wantAll(t, "a restore", withResult(actionResult{Action: game.ActionRestore}), form...)
	wantAll(t, "a snapshot", withResult(actionResult{Action: actionSnapshot, Taken: "Manual"}), form...)
	wantAll(t, "a cancelled stop", withResult(actionResult{Action: game.AuditStop, Err: game.ErrStopCancelled}), form...)
	wantNone(t, "a failed update", withResult(actionResult{Action: game.AuditUpdate, Err: errors.New("boom")}), "act-dismiss", "dismiss-result")
	wantNone(t, "a refused start", withResult(actionResult{Action: game.AuditStart, Err: precheckErr()}), "act-dismiss", "dismiss-result")
	wantNone(t, "a start that worked", withResult(actionResult{Action: game.AuditStart}), "act-dismiss", "dismiss-result")
	wantNone(t, "no result", controlHTML(t, serverCase(game.StateStopped, func(*ServerPage) {})), "act-dismiss")
}

var dismissAt = regexp.MustCompile(`name="at" value="(\d+)"`)

// SPEC 17.9, 17.10: after a switch, closing the Done box removes it and its
// Switch back offer, and it stays gone on a reload. A close that names an
// older result, or has no time, changes nothing.
func TestDismissOverHTTP(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "countdown": {"0"}}))
	body := g.waitPage("The worlds are switched, from Alpha to Bravo.", `id="act-dismiss"`)
	m := dismissAt.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no close form in the page:\n%s", body)
	}
	path := gamePath(b, "dismiss-result")

	g.wantRedirectHome(g.post(path, url.Values{"at": {"1"}}))
	g.wantRedirectHome(g.post(path, url.Values{}))
	g.wantRedirectHome(g.post(path, url.Values{"at": {"nope"}}))
	g.wantContains("after closes that did not match", g.get(PathServer), "The worlds are switched", "Switch back to Alpha")

	g.wantRedirectHome(g.post(path, url.Values{"at": {m[1]}}))
	g.wantMissing("after the close", g.get(PathServer), "The worlds are switched", "Switch back to Alpha", "act-dismiss")

	// A script request gets no body and the same effect.
	g.wantRedirectTo(PathServerResult, g.post(gamePath(b, "take-snapshot"), url.Values{}))
	body = g.waitPage(`id="act-dismiss"`)
	m = dismissAt.FindStringSubmatch(body)
	rep := g.do(http.MethodPost, path, url.Values{"at": {m[1]}}, http.Header{"Cookie": {g.cookie.String()}, "HX-Request": {"true"}})
	if rep.StatusCode != http.StatusNoContent || rep.body != "" {
		t.Errorf("htmx close: %d %q, want 204 and no body", rep.StatusCode, rep.body)
	}
	g.wantMissing("after the script close", g.get(PathServer), "act-dismiss")
}
