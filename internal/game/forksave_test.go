package game

import (
	"context"
	"testing"
	"time"
)

// waitRig starts a world and returns a way to feed it log lines and a
// wait for a save asked when the given number of saves had begun.
func waitRig(t *testing.T) (line func(string), wait func(before int) <-chan error) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Fork save")
	r.start(id)
	r.c.saveSettle = 100 * time.Millisecond
	cid := r.container(id).ID
	w := r.c.world(id)
	line = func(msg string) {
		r.c.handleLine(w, cid, "2026-01-01T00:00:00.000000000Z  28.765 Info AppManager.cpp:304: "+msg)
	}
	wait = func(before int) <-chan error {
		done := make(chan error, 1)
		go func() { done <- r.c.waitSaved(context.Background(), w, before) }()
		return done
	}
	return line, wait
}

func stillWaiting(t *testing.T, done <-chan error, why string) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("the save returned (%v) %s", err, why)
	case <-time.After(300 * time.Millisecond):
	}
}

func returns(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("save: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the save did not return after its own finish")
	}
}

// SPEC 6.10: with fork saving a /server-save sent while another save is
// still being written is queued behind it. The snapshot must wait for the
// save begun after the command, not take the earlier save's "Saving
// finished" as its own while the queued save is still writing.
func TestSaveWaitsForSaveQueuedBehindForkedSave(t *testing.T) {
	line, wait := waitRig(t)
	line("Saving to _autosave1 (non-blocking).") // being written when asked
	done := wait(1)
	line("Saving finished")
	line("Saving game as /factorio/saves/world.zip")
	stillWaiting(t, done, "on the earlier save's finish")
	line("Saving finished")
	returns(t, done)
}

// The follower can read the line that began the earlier save only after
// /server-save was sent. The queued save begins in the same tick the
// earlier one ends, so the wait does not end on that finish either.
func TestSaveWaitsWhenEarlierStartIsReadLate(t *testing.T) {
	line, wait := waitRig(t)
	done := wait(0)
	line("Saving to _autosave-late (non-blocking).")
	line("Saving finished")
	line("Saving game as /factorio/saves/world.zip")
	stillWaiting(t, done, "on the earlier save's finish")
	line("Saving finished")
	returns(t, done)
}

// A plain save, blocking or forked, still ends the wait.
func TestSaveWaitEndsOnItsOwnFinish(t *testing.T) {
	line, wait := waitRig(t)
	done := wait(0)
	line("Saving game as /factorio/saves/world.zip")
	line("Saving finished")
	returns(t, done)
}

// A save begun before the log was followed is never counted; its finish
// alone does not leave the wait stuck behind a start it never saw.
func TestSaveWaitWithUnseenStart(t *testing.T) {
	line, wait := waitRig(t)
	line("Saving finished")
	done := wait(1)
	line("Saving game as /factorio/saves/world.zip")
	line("Saving finished")
	returns(t, done)
}
