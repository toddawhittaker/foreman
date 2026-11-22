package game

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// autoWorld makes an automatic world that the loop has seen stopped for two
// minutes, so the next tick may draw it.
func (r *mapRig) autoWorld(name string) string {
	r.t.Helper()
	id := r.newWorld(name)
	r.setMode(id, world.FactoryMapAutomatic, true)
	return id
}

// tickTwice is a first look, two minutes, and a second look.
func (r *mapRig) tickTwice() {
	r.c.factoryMapTick()
	r.clock.Advance(FactoryMapStoppedFor)
	r.c.factoryMapTick()
}

// expectRun waits for one run of id to begin and end.
func (r *mapRig) expectRun(id string) {
	r.t.Helper()
	select {
	case got := <-r.started:
		if got != id {
			r.t.Fatalf("drew %s, want %s", got, id)
		}
	case <-time.After(5 * time.Second):
		r.t.Fatal("no automatic run")
	}
	r.waitIdle(id)
}

func (r *mapRig) expectNoRun() {
	r.t.Helper()
	select {
	case got := <-r.started:
		r.t.Fatalf("drew %s, want no run", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// SPEC 5.16: automatic draws once a world has been stopped for 2 minutes,
// and not again until the save that loads changes.
func TestFactoryMapAutoDrawsAfterTwoMinutes(t *testing.T) {
	r := newMapRig(t)
	r.waitListed()
	id := r.autoWorld("Base")

	r.c.factoryMapTick()
	r.clock.Advance(FactoryMapStoppedFor - time.Second)
	r.c.factoryMapTick()
	r.expectNoRun()

	r.clock.Advance(time.Second)
	r.c.factoryMapTick()
	r.expectRun(id)
	if _, kept, _ := r.store(id).get(); !kept {
		t.Fatal("no picture kept")
	}

	r.c.factoryMapTick()
	r.expectNoRun()

	r.putSave(id, "2.1.24-no-space-age.zip", "newer.zip", time.Now())
	r.c.factoryMapTick()
	r.expectRun(id)
}

func TestFactoryMapAutoSkips(t *testing.T) {
	cases := map[string]func(r *mapRig, id string){
		"manual":   func(r *mapRig, id string) { r.setMode(id, world.FactoryMapManual, true) },
		"off":      func(r *mapRig, id string) { r.setMode(id, world.FactoryMapOff, true) },
		"archived": func(r *mapRig, id string) { r.setMeta(id, func(m *world.Meta) { m.Archived = true }) },
		"no save": func(r *mapRig, id string) {
			if err := os.Remove(filepath.Join(r.worldDir(id), world.SavesDir, "base.zip")); err != nil {
				r.t.Fatal(err)
			}
		},
		"picture of this save": func(r *mapRig, id string) {
			target, _, err := r.worlds.LoadTarget(id)
			if err != nil {
				r.t.Fatal(err)
			}
			r.store(id).drawing, r.store(id).kept = factorymapDrawing(basisOf(target)), true
		},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			r := newMapRig(t)
			r.waitListed()
			id := r.autoWorld("Base")
			set(r, id)
			r.tickTwice()
			r.expectNoRun()
		})
	}
}

// SPEC 5.16, rule 19: no automatic run while any game runs on this Foreman,
// and the stopped time of a world that ran starts again when it stops.
func TestFactoryMapAutoWaitsForEveryGame(t *testing.T) {
	r := newMapRig(t)
	r.waitListed()
	id := r.autoWorld("Auto")
	other := r.newWorld("Other")
	r.startAsActive(other)
	r.tickTwice()
	r.expectNoRun()

	if err := r.c.Stop(context.Background(), other, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	r.waitState(other, StateStopped)
	r.c.factoryMapTick()
	r.expectRun(id)
}

func TestFactoryMapAutoWaitsWhileItsGameRan(t *testing.T) {
	r := newMapRig(t)
	r.waitListed()
	id := r.autoWorld("Auto")
	r.c.factoryMapTick()
	r.clock.Advance(FactoryMapStoppedFor)
	r.start(id)
	r.c.factoryMapTick()
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	r.waitState(id, StateStopped)
	r.c.factoryMapTick() // first seen stopped again
	r.expectNoRun()
	r.clock.Advance(FactoryMapStoppedFor)
	r.c.factoryMapTick()
	r.expectRun(id)
}

// A failed run is not tried again for the same save; a new save is tried.
func TestFactoryMapAutoDoesNotRetryAFailedSave(t *testing.T) {
	r := newMapRig(t)
	r.waitListed()
	id := r.autoWorld("Base")
	r.runErr = fmt.Errorf("run: %w", ErrSaveCopyTimeout)
	r.tickTwice()
	r.expectRun(id)
	r.c.factoryMapTick()
	r.expectNoRun()
	r.putSave(id, "2.1.24-no-space-age.zip", "newer.zip", time.Now())
	r.c.factoryMapTick()
	r.expectRun(id)
}

// Before Docker is listed nothing can be known to be stopped, so nothing is
// drawn (fail safe).
func TestFactoryMapAutoNeedsDockerListed(t *testing.T) {
	r := newMapRig(t)
	r.waitListed()
	r.c.mu.Lock()
	r.c.listed = false
	r.c.mu.Unlock()
	if !r.c.anyGameRunning() {
		t.Error("an unlisted Docker counts as no game running")
	}
	r.c.mu.Lock()
	r.c.listed = true
	r.c.dockerErr = fmt.Errorf("docker is gone")
	r.c.mu.Unlock()
	if !r.c.anyGameRunning() {
		t.Error("a failed listing counts as no game running")
	}
}

// One run per look: the runs share the one preview slot.
func TestFactoryMapAutoOneRunPerTick(t *testing.T) {
	r := newMapRig(t)
	r.waitListed()
	a := r.autoWorld("A")
	b := r.autoWorld("B")
	r.block = make(chan struct{})
	r.tickTwice()
	first := <-r.started
	r.c.factoryMapTick()
	r.expectNoRun()
	close(r.block)
	r.waitIdle(first)
	r.c.factoryMapTick()
	second := <-r.started
	r.waitIdle(second)
	if first == second || (first != a && first != b) || (second != a && second != b) {
		t.Errorf("drew %s then %s, want %s and %s", first, second, a, b)
	}
}

// SPEC 5.16: at startup, pictures of worlds deleted while Foreman was down
// are removed.
func TestFactoryMapSweepRemovesDeletedWorlds(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Kept")
	gone := "0123456789abcdef0123456789abcdef"
	if !world.ValidID(gone) {
		t.Skip("the id format changed")
	}
	maps := filepath.Join(r.c.dataDir(), "factorymaps")
	for _, d := range []string{id, gone} {
		if err := os.MkdirAll(filepath.Join(maps, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	r.c.factoryMaps.every = time.Hour
	r.stop = r.run(r.c)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(maps, gone)); os.IsNotExist(err) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(maps, id)); err != nil {
		t.Errorf("a world's pictures were removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(maps, gone)); !os.IsNotExist(err) {
		t.Errorf("a deleted world's pictures are still there: %v", err)
	}
}

// The loop ends with the controller, or shutdown would hang.
func TestFactoryMapLoopEnds(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	r.c.factoryMaps.every = time.Millisecond
	stop := r.run(r.c)
	time.Sleep(20 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the controller did not stop")
	}
}

func factorymapDrawing(b saveplayers.Basis) factorymap.Drawing {
	return factorymap.Drawing{Basis: b, DrawnAt: time.Now()}
}
