package game

import (
	"context"
	"errors"
	"testing"
	"time"
)

// In adopted mode the container decides the game version, so a world's
// recorded version does not gate its save (SPEC 4.9, 7.4): only a save newer
// than the container's version is refused, before anything stops.
func TestAdoptedSaveIsCheckedAgainstTheContainerNotTheWorld(t *testing.T) {
	t.Run("a save newer than the world but not the container starts and switches", func(t *testing.T) {
		r, _ := newAdoptedRig(t, adoptedOptions{})
		a := r.newWorld("Alpha")
		old, err := r.worlds.Create(context.Background(), "Old", "2.1.9", false)
		if err != nil {
			t.Fatal(err)
		}
		r.putSave(old, "2.1.24-no-space-age.zip", "base.zip", time.Now().Add(-time.Hour))
		r.start(a)
		r.waitState(a, StateRunning)
		if err := r.switchWorlds(a, old); err != nil {
			t.Fatalf("switch = %v", err)
		}
		r.waitState(old, StateRunning)
		if err := r.c.Stop(context.Background(), old, admin, StopOptions{}); err != nil {
			t.Fatal(err)
		}
		r.start(old)
	})
	t.Run("a save newer than the container is refused before stopping", func(t *testing.T) {
		r, _ := newAdoptedRig(t, adoptedOptions{env: []string{"VERSION=2.0.77"}})
		a, err := r.worlds.Create(context.Background(), "Alpha", "2.0.77", false)
		if err != nil {
			t.Fatal(err)
		}
		r.putSave(a, "2.0.77-no-space-age.zip", "base.zip", time.Now().Add(-time.Hour))
		b := r.newWorld("Beta") // a 2.1.24 save
		r.start(a)
		r.waitState(a, StateRunning)
		if err := r.switchWorlds(a, b); !errors.Is(err, ErrSaveNewerThanImage) {
			t.Fatalf("switch = %v", err)
		}
		r.waitState(a, StateRunning)
		if snaps := r.switchSnapshots(a); len(snaps) != 0 {
			t.Errorf("snapshots taken before a refusal: %+v", snaps)
		}
	})
}
