package game

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

// SPEC 7.12: a scheduled restart warns for its whole countdown even with
// nobody online, so a player who joins in that minute can stop it.
func TestWarnEmptyCountsDownOnAnEmptyServer(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Empty")
	r.start(id)
	cid := r.container(id).ID

	done := r.inCountdown(id, func() error {
		return r.c.Restart(context.Background(), id, admin, StopOptions{Countdown: MaxStopCountdown, WarnEmpty: true}, StartOptions{})
	})
	if !slices.Contains(r.commands(id), warningText(MaxStopCountdown)) {
		t.Errorf("no warning sent; commands %q", r.commands(id))
	}
	// A player joins during the countdown, and Cancel stop ends it.
	r.joinBob(id)
	if err := r.c.CancelStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := r.waitDone(done); !errors.Is(err, ErrStopCancelled) {
		t.Fatalf("restart = %v, want ErrStopCancelled", err)
	}
	r.requireNothingStopped(id, cid)
}

// Without WarnEmpty an empty server stops at once, as before (SPEC 7.6).
func TestNoWarningOnAnEmptyServerByDefault(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Empty")
	r.start(id)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.c.Restart(ctx, id, admin, StopOptions{Countdown: MaxStopCountdown}, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(r.commands(id), warningText(MaxStopCountdown)) {
		t.Error("an empty server was warned")
	}
}

// SPEC 7.12: a scheduled restart never starts a game. With OnlyRunning a
// restart refuses when there is nothing running to stop.
func TestOnlyRunningRefusesAStoppedGame(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Stopped")
	err := r.c.Restart(context.Background(), id, admin, StopOptions{Countdown: MaxStopCountdown, OnlyRunning: true}, StartOptions{})
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("restart = %v, want ErrNotRunning", err)
	}
	if st := r.c.Status(id).State; st != StateStopped {
		t.Errorf("state = %s, want stopped", st)
	}
	if n := len(r.game.State().Containers); n != 0 {
		t.Errorf("%d containers made, want none", n)
	}
}

// A crash the monitor has not seen yet is not a running game either.
func TestOnlyRunningRefusesACrashedGame(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Crashed")
	r.start(id)
	cid := r.container(id).ID
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	err := r.c.Restart(context.Background(), id, admin, StopOptions{Countdown: MaxStopCountdown, OnlyRunning: true}, StartOptions{})
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("restart = %v, want ErrNotRunning", err)
	}
	if got := r.container(id).ID; got != cid {
		t.Errorf("container %s replaced by %s", cid, got)
	}
	if slices.Contains(r.commands(id), "/server-save") {
		t.Error("the crashed game was saved")
	}
}
