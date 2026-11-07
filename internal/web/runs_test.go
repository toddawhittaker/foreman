package web

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

func precheckErr() error {
	return &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{{Text: "The save is from a newer game."}}}}
}

func waitRuns(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 300 && !ok(); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if !ok() {
		t.Fatal("timed out waiting for the actions to end")
	}
}

// A world's page keeps reloading while an action runs on it, and the result
// is stored in the same step that ends the run, so the page never sees a
// moment with neither.
func TestInFlightEndsWithTheResultStored(t *testing.T) {
	g := newGameRuns()
	defer g.close()
	if g.inFlight("w") {
		t.Fatal("an action in flight before any ran")
	}
	release := make(chan struct{})
	g.run("w", true, func(context.Context) actionResult {
		<-release
		return actionResult{Action: game.AuditUpdate}
	})
	if !g.inFlight("w") || g.inFlight("other") {
		t.Error("the running action is not in flight on its own world only")
	}
	close(release)
	waitRuns(t, func() bool { return !g.inFlight("w") })
	if res, ok := g.result("w"); !ok || res.Action != game.AuditUpdate {
		t.Errorf("result %+v, %v after the action left flight", res, ok)
	}
}

// An image download holds no world lock, so a start asked for during
// it is the world's result, not an aside.
func TestStartDuringDownloadIsTheResult(t *testing.T) {
	g := newGameRuns()
	defer g.close()
	release := make(chan struct{})
	g.run("w", false, func(context.Context) actionResult {
		<-release
		return actionResult{Action: game.AuditImageDownload}
	})
	startErr := precheckErr()
	g.run("w", true, func(context.Context) actionResult {
		return actionResult{Action: game.AuditStart, Err: startErr}
	})
	waitRuns(t, func() bool { _, ok := g.result("w"); return ok })
	if res, _ := g.result("w"); res.Action != game.AuditStart || !errors.Is(res.Err, startErr) {
		t.Errorf("result %+v", res)
	}
	close(release)
	waitRuns(t, func() bool { return len(g.others("w")) == 1 })
	if res, _ := g.result("w"); res.Action != game.AuditStart {
		t.Errorf("the download replaced the start's result: %+v", res)
	}
}

// A download asked for while a start runs, and one that finishes after
// it, never hide the start's result.
func TestDownloadNeverHidesAnActionsResult(t *testing.T) {
	g := newGameRuns()
	defer g.close()
	release := make(chan struct{})
	g.run("w", true, func(context.Context) actionResult {
		<-release
		return actionResult{Action: game.AuditStart, Err: game.ErrStartFailed}
	})
	g.run("w", false, func(context.Context) actionResult {
		return actionResult{Action: game.AuditImageDownload}
	})
	waitRuns(t, func() bool { return len(g.others("w")) == 1 })
	close(release)
	waitRuns(t, func() bool { _, ok := g.result("w"); return ok })
	if res, _ := g.result("w"); res.Action != game.AuditStart {
		t.Errorf("result %+v", res)
	}

	// Alone, a download's result is the world's result.
	g.run("w", false, func(context.Context) actionResult {
		return actionResult{Action: game.AuditImageDownload}
	})
	waitRuns(t, func() bool { res, ok := g.result("w"); return ok && res.Action == game.AuditImageDownload })
	if len(g.others("w")) != 0 {
		t.Errorf("asides %+v", g.others("w"))
	}
}

// A refusal by the pre-start check is a normal refusal wherever it lands:
// it is worded, and it is not a server error to log.
func TestPrecheckErrorIsNeverAServerError(t *testing.T) {
	err := precheckErr()
	if status, _ := gameErrorText(err); status != http.StatusConflict {
		t.Errorf("status %d", status)
	}
	got := english(failureText(err))
	if strings.Contains(got, "could not finish") || !strings.Contains(got, "The save is from a newer game.") {
		t.Errorf("worded as %q", got)
	}
	p := serverCase(game.StateStopped, func(p *ServerPage) {
		p.Others = []actionResult{{Action: game.AuditStart, Err: err}}
	})
	if e := p.OtherErrors(); len(e) != 1 || strings.Contains(english(e[0]), "could not finish") {
		t.Errorf("other errors %q", e)
	}
}
