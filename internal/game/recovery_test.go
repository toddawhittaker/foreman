package game

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/store"
)

// These tests pin down that no failed or interrupted action leaves the
// game in a state the admin cannot leave (SPEC 7.1, 7.5 to 7.7, 8.9).

// SPEC 7.3: a start that Docker refuses (here, the port is taken) leaves a
// created container that never ran. That is a stopped game that can be
// started again, not a crash.
func TestStartContainerRefusedShowsStopped(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Port taken")
	r.game.Daemon().SetCallFailure("POST", "start", 500, "port is already allocated")
	if err := r.c.Start(context.Background(), id, admin, StartOptions{}); err == nil {
		t.Fatal("the start reported success")
	}
	if cs := r.container(id); cs.Status != "created" {
		t.Fatalf("container %s, want created", cs.Status)
	}
	time.Sleep(100 * time.Millisecond) // a few monitor looks
	if st := r.c.Status(id); st.State != StateStopped {
		t.Fatalf("state %s after a refused start, want stopped", st.State)
	}
	r.game.Daemon().SetCallFailure("POST", "start", 0, "")
	r.start(id)
}

// SPEC 7.6: a stop Docker refuses leaves the game running, and shown so,
// not stuck in Stopping.
func TestStopRefusedByDockerKeepsRunning(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Stop refused")
	r.start(id)
	r.game.Daemon().SetCallFailure("POST", "stop", 500, "daemon busy")
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err == nil {
		t.Fatal("the stop reported success")
	}
	r.game.Daemon().SetCallFailure("POST", "stop", 0, "")
	r.waitState(id, StateRunning)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatalf("a second stop: %v", err)
	}
	r.waitState(id, StateStopped)
}

// SPEC 7.7: a forced stop Docker refuses leaves the game running and its
// crashes still noticed.
func TestForceStopRefusedByDockerKeepsRunning(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Kill refused")
	r.start(id)
	r.game.Daemon().SetCallFailure("POST", "kill", 500, "daemon busy")
	if err := r.c.ForceStop(context.Background(), id, admin); err == nil {
		t.Fatal("the forced stop reported success")
	}
	r.game.Daemon().SetCallFailure("POST", "kill", 0, "")
	r.waitState(id, StateRunning)
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.crashCount(id) == 1 }, "the crash to be counted")
}

// A stop that Docker never carried out is over once the same container is
// seen running a new run: the game is no longer shown as stopping.
func TestStoppingClearedWhenTheGameRunsAgain(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Stuck stopping")
	r.start(id)
	w := r.c.world(id)
	w.mu.Lock()
	w.stopping, w.expectRunning = true, false
	w.mu.Unlock()
	r.waitState(id, StateStopping)
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	r.waitState(id, StateRunning)
}

// A crashed game that Docker already restarted, still loading, has no game
// to save: Stop stops it without a save, as it does while Docker restarts.
func TestStopCrashedGameDockerRestarted(t *testing.T) {
	r := newRig(t, rigOptions{fake: fakegame.Config{LoadDelay: 2 * time.Second}})
	id := r.newWorld("Crashed reloading")
	r.start(id)
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return r.container(id).Status == "running" && r.c.Status(id).State == StateCrashed
	}, "Docker to restart the crashed game")
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if slices.Contains(r.commands(id), "/server-save") {
		t.Error("tried to save a game that is not in game")
	}
	if cs := r.container(id); cs.Status != "exited" {
		t.Errorf("container %s", cs.Status)
	}
	r.waitState(id, StateStopped)
}

// A game still loading cannot be warned or saved yet: Stop says to wait,
// not that RCON is unreachable.
func TestStopStartingGameNotReady(t *testing.T) {
	r := newRig(t, rigOptions{fake: fakegame.Config{LoadDelay: 2 * time.Second}})
	id := r.newWorld("Loading")
	r.start(id)
	r.stop()
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.container(id).Status == "running" }, "Docker to restart it")
	c2 := r.newController(rigOptions{})
	r.run(c2)
	eventually(t, func() bool { return c2.Status(id).State == StateStarting }, "the re-attached game to be starting")
	if err := c2.Stop(context.Background(), id, admin, StopOptions{}); !errors.Is(err, ErrNotReady) {
		t.Fatalf("got %v, want ErrNotReady", err)
	}
	if r.container(id).Status != "running" {
		t.Error("the loading game was stopped")
	}
}

// SPEC 6.5: a paused game is still a live game; its folder is never
// swapped under it, and Stop does not pretend it is stopped.
func TestPausedGameRefusesStopAndRestore(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Paused")
	snap, err := r.c.Snapshot(context.Background(), id, admin, "good")
	if err != nil {
		t.Fatal(err)
	}
	r.game.Daemon().Seed(dockertest.Container{Name: ContainerName(id), Status: "paused",
		Spec: docker.ContainerSpec{Labels: map[string]string{LabelWorld: id, LabelInstall: r.data}}})
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); !errors.Is(err, ErrPaused) {
		t.Errorf("stop got %v, want ErrPaused", err)
	}
	if _, err := r.c.Restore(context.Background(), id, admin, snap.Name, StopOptions{}); !errors.Is(err, ErrPaused) {
		t.Errorf("restore got %v, want ErrPaused", err)
	}
	snaps, err := r.worlds.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 {
		t.Errorf("%d snapshots: the restore went ahead", len(snaps))
	}
}

// SPEC 7.7: a forced stop that interrupted an operation did what was asked
// even when nothing was left running.
func TestForceStopOfOperationWithNothingRunning(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Cancelled")
	ctx, o := r.c.beginOp(context.Background(), r.c.world(id), AuditStart, r.c.startSteps(StartOptions{}))
	go func() {
		<-ctx.Done()
		_ = o.end(ctx.Err())
	}()
	if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
		t.Fatalf("got %v, want nil", err)
	}
}

// SPEC 8.9: a crash-loop stop that Docker refuses is tried again at later
// polls until Docker accepts it; the state stays crash-looping throughout.
func TestCrashLoopStopRetriedAfterRefusal(t *testing.T) {
	for _, refusals := range []int{1, 3} {
		t.Run(fmt.Sprintf("refused %d times", refusals), func(t *testing.T) {
			r := newRig(t, rigOptions{})
			id := r.newWorld("Loop refused")
			r.start(id)
			stops := func() int { return len(r.calls("POST /containers/" + r.container(id).ID + "/stop")) }
			before := stops()
			r.game.Daemon().SetCallFailure("POST", "stop", 500, "daemon busy")
			r.game.SetCrashLoop(ContainerName(id), true)
			r.waitState(id, StateCrashLooping)
			eventually(t, func() bool { return stops()-before >= refusals }, "%d refused stops", refusals)
			if st := r.c.Status(id); st.State != StateCrashLooping {
				t.Fatalf("state %s while the stop was refused", st.State)
			}
			r.game.Daemon().SetCallFailure("POST", "stop", 0, "")
			eventually(t, func() bool { return r.container(id).Status == "exited" }, "the retried stop to work")
			n := stops()
			time.Sleep(200 * time.Millisecond)
			if stops() != n {
				t.Error("the stop was tried again after Docker accepted it")
			}
			if st := r.c.Status(id); st.State != StateCrashLooping {
				t.Errorf("state %s after the stop", st.State)
			}
		})
	}
}

// faultyStore fails the calls a test names, to show that recovery goes on.
type faultyStore struct {
	SwitchStore
	failList, failAdoption bool
	failEndFrom            string
}

func (s faultyStore) ListSwitches(ctx context.Context) ([]store.Switch, error) {
	if s.failList {
		return nil, errDatabase
	}
	return s.SwitchStore.ListSwitches(ctx)
}

func (s faultyStore) Adoption(ctx context.Context) (store.AdoptionRecord, bool, error) {
	if s.failAdoption {
		return store.AdoptionRecord{}, false, errDatabase
	}
	return s.SwitchStore.Adoption(ctx)
}

func (s faultyStore) EndSwitch(ctx context.Context, from string) error {
	if from == s.failEndFrom {
		return errDatabase
	}
	return s.SwitchStore.EndSwitch(ctx, from)
}

// SPEC 7.1, 3.8: a fault during startup recovery is logged and recovery
// goes on, so Foreman still serves pages; only a cancelled startup is an
// error.
func TestRecoveryFaultsDoNotStopStartup(t *testing.T) {
	ctx := context.Background()
	t.Run("the records cannot be read", func(t *testing.T) {
		r, _ := newAdoptedRig(t, adoptedOptions{noRun: true, prepare: unadoptedFolder(t)})
		r.c.switches = faultyStore{SwitchStore: r.store, failList: true, failAdoption: true}
		if err := r.c.Recover(ctx); err != nil {
			t.Fatalf("recover = %v", err)
		}
	})
	t.Run("one switch record fails and the next is still recovered", func(t *testing.T) {
		r := newRig(t, rigOptions{noRun: true})
		a, b := r.twoWorlds()
		c, d := r.newWorld("Gamma"), r.newWorld("Delta")
		r.recordSwitch(a, b, store.StepStopped)
		r.recordSwitch(c, d, store.StepStopped)
		r.c.switches = faultyStore{SwitchStore: r.store, failEndFrom: a}
		if err := r.c.Recover(ctx); err != nil {
			t.Fatalf("recover = %v", err)
		}
		if r.c.Status(c).Notice == nil {
			t.Error("the second record was not recovered")
		}
	})
	t.Run("a cancelled startup", func(t *testing.T) {
		r := newRig(t, rigOptions{noRun: true})
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if err := r.c.Recover(cancelled); !errors.Is(err, context.Canceled) {
			t.Errorf("recover = %v", err)
		}
	})
}
