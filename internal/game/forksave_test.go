package game

import (
	"context"
	"errors"
	"testing"
	"time"
)

// waitRig starts a world and returns a way to feed it log lines and a
// wait for a save asked at that moment, as saveGame takes it before it
// sends /server-save.
func waitRig(t *testing.T) (line, raw func(string), wait func() <-chan error) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Fork save")
	r.start(id)
	r.c.saveTimeout = 2 * time.Second
	cid := r.container(id).ID
	w := r.c.world(id)
	line = func(msg string) {
		r.c.handleLine(w, cid, "2026-01-01T00:00:00.000000000Z  28.765 Info AppManager.cpp:304: "+msg)
	}
	raw = func(msg string) {
		r.c.handleLine(w, cid, "2026-01-01T00:00:00.000000000Z    0.900 "+msg)
	}
	wait = func() <-chan error {
		w.mu.Lock()
		m := w.saveMarkLocked()
		w.mu.Unlock()
		done := make(chan error, 1)
		go func() { done <- r.c.waitSaved(context.Background(), w, m) }()
		return done
	}
	return line, raw, wait
}

func stillWaiting(t *testing.T, done <-chan error, why string) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("the save returned (%v) %s", err, why)
	case <-time.After(200 * time.Millisecond):
	}
}

// ends returns what the wait ended with, well before its timeout.
func ends(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		t.Fatal("the save did not end when the log said it did")
		return nil
	}
}

// SPEC 6.10: with fork saving a /server-save sent while another save is
// still being written is queued behind it. The snapshot must wait for the
// save begun after the command, not take the earlier save's "Saving
// finished" as its own while the queued save is still writing.
func TestSaveWaitsForSaveQueuedBehindForkedSave(t *testing.T) {
	line, _, wait := waitRig(t)
	line("Saving to _autosave1 (non-blocking).") // being written when asked
	done := wait()
	line("Saving finished")
	stillWaiting(t, done, "on the earlier save's finish")
	line("Saving game as /factorio/saves/world.zip")
	stillWaiting(t, done, "before its own save finished")
	line("Saving finished")
	if err := ends(t, done); err != nil {
		t.Fatal(err)
	}
}

// The follower can read the line that began an autosave only after
// /server-save was sent; that autosave's finish is still not ours.
func TestSaveWaitsWhenEarlierStartIsReadLate(t *testing.T) {
	line, _, wait := waitRig(t)
	done := wait()
	line("Saving to _autosave-late (non-blocking).")
	line("Saving finished")
	line("Saving game as /factorio/saves/world.zip")
	stillWaiting(t, done, "on the earlier save's finish")
	line("Saving finished")
	if err := ends(t, done); err != nil {
		t.Fatal(err)
	}
}

// A plain save, blocking or forked, ends the wait at once.
func TestSaveWaitEndsOnItsOwnFinish(t *testing.T) {
	line, _, wait := waitRig(t)
	done := wait()
	line("Saving game as /factorio/saves/world.zip")
	line("Saving finished")
	if err := ends(t, done); err != nil {
		t.Fatal(err)
	}
}

// A save begun before the log was followed was never counted; its finish
// alone does not leave later saves waiting behind it.
func TestSaveWaitWithUnseenStart(t *testing.T) {
	line, _, wait := waitRig(t)
	line("Saving finished")
	done := wait()
	line("Saving game as /factorio/saves/world.zip")
	line("Saving finished")
	if err := ends(t, done); err != nil {
		t.Fatal(err)
	}
}

// A save cut off by the game's end (killed for memory while the child
// wrote) never logs its finish. The next run's saves are not held behind it.
func TestSaveWaitAfterRunEndedMidSave(t *testing.T) {
	line, raw, wait := waitRig(t)
	line("Saving game as /factorio/saves/world.zip")
	line("Saving process PID: 42")
	raw("Loading map /factorio/saves/world.zip: 123 bytes.")
	done := wait()
	line("Saving game as /factorio/saves/world.zip")
	line("Saving finished")
	if err := ends(t, done); err != nil {
		t.Fatal(err)
	}
}

// A child that dies or exits non-zero fails the save at once (SPEC 6.10):
// the game logs no finish for it.
func TestSaveWaitFailsWhenChildFails(t *testing.T) {
	for _, exit := range []string{
		"Child 42 exited with return value 1",
		"Child 42 was terminated by signal 9",
	} {
		t.Run(exit, func(t *testing.T) {
			line, _, wait := waitRig(t)
			done := wait()
			line("Saving game as /factorio/saves/world.zip")
			line(exit)
			if err := ends(t, done); !errors.Is(err, ErrSaveFailed) {
				t.Fatalf("got %v, want ErrSaveFailed", err)
			}
		})
	}
}

// A child that exits cleanly is not a failure; the finish line ends the save.
func TestSaveWaitIgnoresCleanChildExit(t *testing.T) {
	line, _, wait := waitRig(t)
	done := wait()
	line("Saving game as /factorio/saves/world.zip")
	line("Child 42 exited with return value 0")
	stillWaiting(t, done, "before the finish line")
	line("Saving finished")
	if err := ends(t, done); err != nil {
		t.Fatal(err)
	}
}
