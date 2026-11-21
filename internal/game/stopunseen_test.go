package game

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/fakegame"
)

// #646, SPEC 8.7: between Docker restarting a crashed game and the
// monitor's next look, the container's restart count is above the one
// Foreman saw. The monitor does not run here, so no look ever comes: the
// stop itself must find out whether the new run is in game.

// crashUnseen crashes the world's game and waits for Docker to restart it,
// with no look by the monitor in between.
func crashUnseen(t *testing.T, r *rig, id string) {
	t.Helper()
	cid := r.container(id).ID
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		c, ok := r.game.Daemon().Container(cid)
		return ok && c.RestartCount > 0 && c.Status == "running"
	}, "Docker to restart the crashed game")
	w := r.c.world(id)
	w.mu.Lock()
	seen := w.seenRestarts
	w.mu.Unlock()
	if seen != 0 {
		t.Fatalf("the restart was seen (%d) before the stop", seen)
	}
}

func saves(r *rig, id string) int {
	n := 0
	for _, c := range r.commands(id) {
		if c == "/server-save" {
			n++
		}
	}
	return n
}

// A run already back in game is a running game: its stop warns and saves.
func TestStopWarnsAGameBackInGameBeforeAnyLook(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Back unseen")
	r.start(id)
	crashUnseen(t, r, id)
	eventually(t, func() bool {
		return inGameRuns(r, id) >= 2
	}, "the new run to be in game")
	before := saves(r, id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatalf("stop = %v, want it to run", err)
	}
	if saves(r, id) == before {
		t.Error("the game back in game was stopped without a save")
	}
	if cs := r.container(id); cs.Status != "exited" {
		t.Errorf("container %s, want exited", cs.Status)
	}
}

// A Restart with OnlyRunning runs for a game back in game, before any look.
func TestOnlyRunningRestartsAGameBackInGameBeforeAnyLook(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Restart unseen")
	r.start(id)
	crashUnseen(t, r, id)
	eventually(t, func() bool {
		return inGameRuns(r, id) >= 2
	}, "the new run to be in game")
	before := saves(r, id)
	err := r.c.Restart(context.Background(), id, admin, StopOptions{OnlyRunning: true}, StartOptions{})
	if err != nil {
		t.Fatalf("restart = %v, want it to run", err)
	}
	if !slices.Contains(r.commands(id), "/server-save") || saves(r, id) == before {
		t.Error("the running game was not saved")
	}
}

// A run still loading is not in game: the stop is never refused, and
// there is nothing to save.
func TestStopsAGameNotYetBackInGameBeforeAnyLook(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true, fake: fakegame.Config{LoadDelay: 2 * time.Second}})
	id := r.newWorld("Loading unseen")
	r.start(id)
	crashUnseen(t, r, id)
	before := saves(r, id)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatalf("stop = %v, want it to run", err)
	}
	if saves(r, id) != before {
		t.Error("tried to save a game that is not in game")
	}
	if cs := r.container(id); cs.Status != "exited" {
		t.Errorf("container %s, want exited", cs.Status)
	}
}

// inGameRuns counts the runs of the world's container that reached InGame.
func inGameRuns(r *rig, id string) int {
	c, _ := r.game.Daemon().Container(r.container(id).ID)
	n := 0
	for _, l := range c.Log {
		if strings.Contains(l.Text, "to(InGame)") {
			n++
		}
	}
	return n
}

// Every action that stops the game sees the missed restart too (ruling 10):
// Restore stops a game back in game with a warning and a save.
func TestRestoreSavesAGameBackInGameBeforeAnyLook(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Restore unseen")
	r.start(id)
	snap, err := r.c.Snapshot(context.Background(), id, admin, "good")
	if err != nil {
		t.Fatal(err)
	}
	crashUnseen(t, r, id)
	eventually(t, func() bool { return inGameRuns(r, id) >= 2 }, "the new run to be in game")
	before := saves(r, id)
	if _, err := r.c.Restore(context.Background(), id, admin, snap.Name, StopOptions{}); err != nil {
		t.Fatalf("restore = %v, want it to run", err)
	}
	if saves(r, id) == before {
		t.Error("the game back in game was stopped without a save")
	}
}

// Switch does too, for the world it stops.
func TestSwitchSavesAGameBackInGameBeforeAnyLook(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	from := r.newWorld("Switch unseen")
	to := r.newWorld("Next")
	r.start(from)
	crashUnseen(t, r, from)
	eventually(t, func() bool { return inGameRuns(r, from) >= 2 }, "the new run to be in game")
	before := saves(r, from)
	if err := r.c.Switch(context.Background(), from, to, admin, StopOptions{}, StartOptions{}, true); err != nil {
		t.Fatalf("switch = %v, want it to run", err)
	}
	if saves(r, from) == before {
		t.Error("the game back in game was stopped without a save")
	}
}

// SPEC 8.7: an action's own look at the restart (catchUp) is newer than a
// monitor look read before it, which may still be reading the crash log.
// That older look is dropped: it would set the restart count back, end the
// new run's state, and let the next poll count the crash a second time.
func TestCatchUpDropsAnOlderMonitorLook(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Catch up race")
	r.start(id)
	ctx := context.Background()
	w := r.c.world(id)
	stale, ops := lookAt(t, r, w, r.container(id).ID)
	crashUnseen(t, r, id)
	r.c.catchUpWorld(ctx, id)
	type seen struct {
		crashes, restarts int
		inGame            bool
	}
	look := func() seen {
		w.mu.Lock()
		defer w.mu.Unlock()
		return seen{len(w.crashes), w.seenRestarts, w.run.inGame}
	}
	eventually(t, func() bool { return look().inGame }, "the new run to be in game")
	want := look()
	if want.crashes != 1 || want.restarts != 1 {
		t.Fatalf("after the catch-up: %+v, want one crash and one restart seen", want)
	}
	if r.c.observe(w, &stale, ops, nil) {
		t.Error("the older look was taken in")
	}
	if got := look(); got != want {
		t.Errorf("after the older look: %+v, want %+v", got, want)
	}
	r.c.tick(ctx)
	if got := look(); got != want {
		t.Errorf("after the next poll: %+v, want %+v", got, want)
	}
}
