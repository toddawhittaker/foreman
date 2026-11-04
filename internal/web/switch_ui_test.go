package web

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
)

// SPEC 16.3: the reload after a switch would drop the announce event, so
// the moved event carries the sentence the next page reads out.
func TestMovedSentence(t *testing.T) {
	names := map[string]string{"alpha": "Alpha", "bravo": "Bravo"}
	f := switchPageOf("alpha", failedSwitch(true), names)
	f.HaveResult, f.Result = true, actionResult{Action: game.AuditSwitch, Err: errors.New("start the game: it failed")}
	if got := f.movedSentence(); !strings.HasPrefix(got, "Bravo is now the active world. Error: ") {
		t.Errorf("failed switch: %q", got)
	}

	done := game.Status{Operation: &game.Operation{Action: game.AuditSwitch, Done: true, Switch: &game.SwitchInfo{From: "alpha", To: "bravo", Presented: true}}}
	p := switchPageOf("alpha", done, names)
	p.HaveResult, p.Result = true, actionResult{Action: game.AuditSwitch}
	if got, want := p.movedSentence(), "Bravo is now the active world. The worlds are switched."; got != want {
		t.Errorf("finished switch: %q, want %q", got, want)
	}
}

// SPEC 7.9, 16.3: the old page is told to follow only once the operation has
// ended and its result is stored, so the sentence always says how it ended.
func TestMovedWaitsForTheEndOfTheOperation(t *testing.T) {
	sw := &game.SwitchInfo{From: "alpha", To: "bravo", Presented: true}
	for name, c := range map[string]struct {
		op     game.Operation
		result bool
		want   bool
	}{
		"running":         {game.Operation{Action: game.AuditSwitch, Switch: sw}, false, false},
		"running, result": {game.Operation{Action: game.AuditSwitch, Switch: sw}, true, false},
		"done, no result": {game.Operation{Action: game.AuditSwitch, Switch: sw, Done: true}, false, false},
		"done":            {game.Operation{Action: game.AuditSwitch, Switch: sw, Done: true}, true, true},
		"failed":          {game.Operation{Action: game.AuditSwitch, Switch: sw, Done: true, Error: "boom"}, true, true},
		"not presented":   {game.Operation{Action: game.AuditSwitch, Switch: &game.SwitchInfo{From: "alpha", To: "bravo"}, Done: true, Error: "boom"}, true, false},
		"the other world": {game.Operation{Action: game.AuditSwitch, Switch: &game.SwitchInfo{From: "bravo", To: "alpha", Presented: true}, Done: true}, true, false},
	} {
		op := c.op
		p := switchPageOf("alpha", game.Status{Operation: &op}, switchNames)
		p.HaveResult = c.result
		if got := p.Moved(); got != c.want {
			t.Errorf("%s: Moved = %v, want %v", name, got, c.want)
		}
	}
}

// The moved event itself carries the sentence, escaped because the page
// swaps it in as HTML.
func TestMovedEventCarriesTheSentence(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("<i>Bravo</i>", fixtureNoSA, false)
	buf, _, closeIt := g.collectEvents(a)
	defer closeIt()
	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}}))
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(buf.String(), "event: moved\ndata: ") {
		if time.Now().After(deadline) {
			t.Fatalf("no moved event:\n%s", buf.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := buf.String(); !strings.Contains(got, "event: moved\ndata: &lt;i&gt;Bravo&lt;/i&gt; is now the active world. The worlds are switched.") {
		t.Errorf("moved sentence:\n%s", got)
	}
}

// DESIGN 7.1: a world page that cannot offer the switch says why, in one line.
func TestWorldPageSaysWhyThereIsNoSwitch(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	active := g.get(worldPath(a))
	g.wantContains("active world", active, "This is the active world.")
	g.wantMissing("active world", active, "Switch to this world")
	g.wantContains("other world", g.get(worldPath(b)), "Switch to this world", "leaves the server stopped if it is stopped")

	g.startGame(a)
	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "countdown": {"30"}}))
	g.waitPage("Switching from Alpha to Bravo")
	busy := g.get(worldPath(b))
	g.wantContains("busy world", busy, "An operation is running on the server, such as a switch.", `href="/"`)
	g.wantMissing("busy world", busy, "Switch to this world")
}

// DESIGN 7.3: a refused switch is an error message with a way back.
func TestSwitchRefusalIsAMessage(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	g.wantContains("refusal", g.get(switchPath(a)),
		`<div class="message message--error">`, "<strong>Error:</strong> This is already the active world.", `href="`+worldPath(a)+`">Back to Alpha`)
}

// SPEC 7.7: while a switch runs, the page says what Force stop does to it.
func TestForceStopNoteDuringASwitch(t *testing.T) {
	st := game.Status{State: game.StateStopping, Operation: &game.Operation{Action: game.AuditSwitch}}
	got := switchPageOf("alpha", st, switchNames).ActionsNote()
	if !strings.Contains(got, "Force stop ends the switch; the game is stopped without saving.") {
		t.Errorf("note: %q", got)
	}
}
