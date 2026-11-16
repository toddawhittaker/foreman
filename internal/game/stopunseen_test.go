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
