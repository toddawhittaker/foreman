package game

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// These tests pin SPEC 7.6: a countdown can be
// cancelled while it runs, before anything is saved or stopped. Cancelling
// a restart, switch, restore or update ends the whole action with nothing
// changed.

// joinBob puts a player on the world's game, so a stop has someone to warn.
func (r *rig) joinBob(id string) {
	r.t.Helper()
	if err := r.game.Join(ContainerName(id), "bob"); err != nil {
		r.t.Fatal(err)
	}
}

// inCountdown runs act in the background and waits until the world's
// operation shows a countdown. It returns what act returns.
func (r *rig) inCountdown(id string, act func() error) <-chan error {
	r.t.Helper()
	done := make(chan error, 1)
	go func() { done <- act() }()
	eventually(r.t, func() bool {
		op := r.c.Status(id).Operation
		if op == nil {
			return false
		}
		_, ok := op.CountdownEnds()
		return ok
	}, "the countdown to begin")
	return done
}

func (r *rig) waitDone(done <-chan error) error {
	r.t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		r.t.Fatal("the action did not end")
		return nil
	}
}

// requireNothingStopped fails when the game was saved, stopped or replaced.
func (r *rig) requireNothingStopped(id, container string) {
	r.t.Helper()
	if slices.Contains(r.commands(id), "/server-save") {
		r.t.Error("the game was saved although the stop was cancelled")
	}
	if cs := r.container(id); cs.ID != container || cs.Status != "running" {
		r.t.Errorf("container %s %s, want %s still running", cs.ID, cs.Status, container)
	}
	if st := r.waitState(id, StateRunning); !st.LastCrash.IsZero() {
		r.t.Error("the cancelled stop counted as a crash")
	}
}

func TestCancelStopDuringCountdown(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Cancel")
	r.start(id)
	r.joinBob(id)
	cid := r.container(id).ID
	begun := r.clock.Now()

	done := r.inCountdown(id, func() error {
		return r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: MaxStopCountdown})
	})
	op := r.c.Status(id).Operation
	ends, _ := op.CountdownEnds()
	if left := ends.Sub(begun); left < MaxStopCountdown-5*time.Second || left > MaxStopCountdown+5*time.Second {
		t.Errorf("the countdown ends %s after it began, want about %s", left, MaxStopCountdown)
	}
	if len(op.Players) != 1 || op.Players[0].Name != "bob" {
		t.Errorf("players during the countdown: %+v", op.Players)
	}

	if err := r.c.CancelStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := r.waitDone(done); !errors.Is(err, ErrStopCancelled) {
		t.Fatalf("stop returned %v, want ErrStopCancelled", err)
	}
	r.requireNothingStopped(id, cid)

	st := r.c.Status(id)
	if !st.Operation.Done || st.Operation.Error == "" {
		t.Errorf("operation %+v", st.Operation)
	}
	for _, s := range st.Operation.Steps {
		if s.State != StepSkipped {
			t.Errorf("step %q reads %q after the cancel, want skipped", s.Name, s.State)
		}
	}
	if _, ok := st.Operation.CountdownEnds(); ok {
		t.Error("a cancelled countdown still shows an end time")
	}
	if a := r.actions(); !slices.Equal(a, []string{AuditStart, AuditStop, AuditStopCancel}) {
		t.Errorf("audit %v", a)
	}
	// Players who were warned are told it is off.
	if !slices.ContainsFunc(r.commands(id), func(c string) bool { return strings.Contains(c, "cancelled") }) {
		t.Errorf("players were not told the stop is off: %q", r.commands(id))
	}
	// The game can be stopped again as usual.
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: 0}); err != nil {
		t.Errorf("stop after a cancel: %v", err)
	}
}

// A cancel with no countdown running is refused and is not audited.
func TestCancelStopWithoutCountdown(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("None")
	if err := r.c.CancelStop(context.Background(), id, admin); !errors.Is(err, ErrNoCountdown) {
		t.Errorf("stopped game: got %v", err)
	}
	r.start(id)
	if err := r.c.CancelStop(context.Background(), id, admin); !errors.Is(err, ErrNoCountdown) {
		t.Errorf("running game: got %v", err)
	}
	if a := r.actions(); !slices.Equal(a, []string{AuditStart}) {
		t.Errorf("a refused cancel was audited: %v", a)
	}
}

// Once the countdown has ended the stop goes on to its end; a late cancel
// is refused and changes nothing.
func TestCancelStopAfterCountdownEnded(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Late")
	r.start(id)
	r.joinBob(id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: 20 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if err := r.c.CancelStop(context.Background(), id, admin); !errors.Is(err, ErrNoCountdown) {
		t.Errorf("late cancel: got %v, want ErrNoCountdown", err)
	}
	if cs := r.container(id); cs.Status != "exited" {
		t.Errorf("container %s", cs.Status)
	}
	if a := r.actions(); !slices.Equal(a, []string{AuditStart, AuditStop}) {
		t.Errorf("audit %v", a)
	}
}

// A zero countdown warns nobody (SPEC 7.6), so there is nothing to cancel,
// and nobody is listed.
func TestZeroCountdownCannotBeCancelled(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Zero")
	r.start(id)
	r.joinBob(id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: 0}); err != nil {
		t.Fatal(err)
	}
	op := r.c.Status(id).Operation
	if _, ok := op.CountdownEnds(); ok || len(op.Players) != 0 {
		t.Errorf("a stop with no countdown shows one: %+v", op)
	}
	if err := r.c.CancelStop(context.Background(), id, admin); !errors.Is(err, ErrNoCountdown) {
		t.Errorf("got %v", err)
	}
}

// With nobody online there is no countdown to show or cancel.
func TestNoCountdownWhenNobodyIsOn(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Empty")
	r.start(id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: MaxStopCountdown}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.c.Status(id).Operation.CountdownEnds(); ok {
		t.Error("an empty server shows a countdown")
	}
}

// SPEC 12.9: a cancel that cannot be recorded is not done.
func TestCancelStopNeedsTheAuditLog(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Audit")
	r.start(id)
	r.joinBob(id)
	done := r.inCountdown(id, func() error {
		return r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: MaxStopCountdown})
	})
	r.audit.setFail(true)
	if err := r.c.CancelStop(context.Background(), id, admin); err == nil {
		t.Fatal("a cancel was accepted that the audit log could not record")
	}
	select {
	case err := <-done:
		t.Fatalf("the countdown ended: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	r.audit.setFail(false)
	if err := r.c.CancelStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := r.waitDone(done); !errors.Is(err, ErrStopCancelled) {
		t.Errorf("got %v", err)
	}
}

// A restart cancelled during its countdown ends there: the game runs on in
// the same container and nothing was saved, snapshotted or started.
func TestCancelRestart(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Restart")
	r.start(id)
	r.joinBob(id)
	cid := r.container(id).ID
	u := r.record(id, true)

	done := r.inCountdown(id, func() error {
		return r.c.Restart(context.Background(), id, admin, StopOptions{Countdown: MaxStopCountdown}, StartOptions{})
	})
	if err := r.c.CancelStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := r.waitDone(done); !errors.Is(err, ErrStopCancelled) {
		t.Fatalf("restart returned %v, want ErrStopCancelled", err)
	}
	r.requireNothingStopped(id, cid)
	u.check("cancelled restart")
	if len(r.calls("POST /containers/create")) != 1 {
		t.Errorf("a restart that was cancelled made a container: %v", r.calls("POST /containers/create"))
	}
	op := r.c.Status(id).Operation
	if op.Action != AuditRestart || !op.Done || op.Error == "" {
		t.Errorf("operation %+v", op)
	}
	for _, s := range op.Steps {
		if s.State != StepSkipped {
			t.Errorf("step %q reads %q, want skipped", s.Name, s.State)
		}
	}
}

// A restore cancelled during its countdown restores nothing.
func TestCancelRestore(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Restore")
	snap, err := r.c.Snapshot(context.Background(), id, admin, "good")
	if err != nil {
		t.Fatal(err)
	}
	r.start(id)
	r.joinBob(id)
	cid := r.container(id).ID
	u := r.record(id, true)

	done := r.inCountdown(id, func() error {
		_, err := r.c.Restore(context.Background(), id, admin, snap.Name, StopOptions{Countdown: MaxStopCountdown})
		return err
	})
	if err := r.c.CancelStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := r.waitDone(done); !errors.Is(err, ErrStopCancelled) {
		t.Fatalf("restore returned %v, want ErrStopCancelled", err)
	}
	r.requireNothingStopped(id, cid)
	u.check("cancelled restore")
	for _, s := range r.snapshots(id) {
		if s.Reason == world.ReasonBeforeRestore {
			t.Errorf("a cancelled restore took the undo snapshot %q", s.Name)
		}
	}
}

// A switch cancelled during its countdown ends with the old world still
// active and running, no snapshots, and no switch record left.
func TestCancelSwitch(t *testing.T) {
	r := newRig(t, rigOptions{})
	a, b := r.twoWorlds()
	r.start(a)
	r.joinBob(a)
	cid := r.container(a).ID
	u := r.record(a, true)

	done := r.inCountdown(a, func() error {
		return r.c.Switch(context.Background(), a, b, admin, StopOptions{Countdown: MaxStopCountdown}, StartOptions{}, true)
	})
	// Both worlds show the switch; the countdown belongs to the game being stopped.
	if err := r.c.CancelStop(context.Background(), a, admin); err != nil {
		t.Fatal(err)
	}
	if err := r.waitDone(done); !errors.Is(err, ErrStopCancelled) {
		t.Fatalf("switch returned %v, want ErrStopCancelled", err)
	}
	r.requireNothingStopped(a, cid)
	u.check("cancelled switch")
	if got := r.active(); got != a {
		t.Errorf("active world %q, want %q", got, a)
	}
	r.noRecord()
	if n := len(r.switchSnapshots(a)) + len(r.switchSnapshots(b)); n != 0 {
		t.Errorf("%d switch snapshots after a cancel", n)
	}
	if r.hasContainer(b) {
		t.Error("the new world was started")
	}
}

// An update cancelled during its countdown changes no version and takes no
// snapshot; an image it had already downloaded stays.
func TestCancelUpdateKeepsTheDownload(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.versionWorld("Update", "2.0.77", "2.0.77-no-space-age.zip")
	target := ImageRef("2.1.20")
	r.game.SetImage(target, false)
	r.start(id)
	r.joinBob(id)
	cid := r.container(id).ID
	files := r.record(id, true)

	done := r.inCountdown(id, func() error {
		return r.c.Update(context.Background(), id, "2.1.20", admin, StopOptions{Countdown: MaxStopCountdown}, StartOptions{})
	})
	if err := r.c.CancelStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := r.waitDone(done); !errors.Is(err, ErrStopCancelled) {
		t.Fatalf("update returned %v, want ErrStopCancelled", err)
	}
	r.requireNothingStopped(id, cid)
	if v := r.metaVersion(id); v != "2.0.77" {
		t.Errorf("world at %s after a cancelled update", v)
	}
	if len(r.snapshots(id)) != files.snapshots {
		t.Error("a cancelled update took a snapshot")
	}
	if !slices.Contains(r.game.Daemon().Pulls(), target) {
		t.Errorf("pulls %v: the finished download was lost", r.game.Daemon().Pulls())
	}
}

// A forced stop still interrupts a countdown, and a cancel that arrives
// after it has nothing to do.
func TestCancelAfterForceStop(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Force then cancel")
	r.start(id)
	r.joinBob(id)
	done := r.inCountdown(id, func() error {
		return r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: MaxStopCountdown})
	})
	if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := r.waitDone(done); !errors.Is(err, ErrCancelled) {
		t.Errorf("got %v, want ErrCancelled", err)
	}
	if err := r.c.CancelStop(context.Background(), id, admin); !errors.Is(err, ErrNoCountdown) {
		t.Errorf("got %v", err)
	}
}
