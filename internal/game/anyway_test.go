package game

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// wantChanged fails unless err is a refusal because the plan changed.
func wantChanged(t *testing.T, err error) {
	t.Helper()
	var pe *PrecheckError
	if !errors.As(err, &pe) || !pe.Changed {
		t.Fatalf("got %v, want a refusal because the plan changed", err)
	}
}

// SPEC 7.4, 17.5: a go-ahead anyway confirms the problems and the running
// state the admin saw. A stale one is refused before anything changes; a
// matching one goes ahead.
func TestAnywayFingerprint(t *testing.T) {
	broken := func(r *rig, id string) {
		r.writeFile(id, filepath.Join(world.SavesDir, "broken.zip"), []byte("not a zip"))
	}
	// refused returns the check a refusal without anyway showed, which
	// is what the page's box draws from.
	refused := func(err error) world.PreCheck {
		var pe *PrecheckError
		if !errors.As(err, &pe) || !pe.Check.CanStartAnyway() {
			t.Fatalf("got %v, want a refusal that allows going ahead", err)
		}
		return pe.Check
	}

	t.Run("start", func(t *testing.T) {
		r := newRig(t, rigOptions{})
		id := r.newWorld("Odd")
		broken(r, id)
		c := refused(r.c.Start(context.Background(), id, admin, StartOptions{}))
		wantChanged(t, r.c.Start(context.Background(), id, admin, StartOptions{Anyway: true, Seen: AnywayFingerprint(c, true)}))
		wantChanged(t, r.c.Start(context.Background(), id, admin, StartOptions{Anyway: true}))
		if snaps, _ := r.worlds.Snapshots(id); len(snaps) != 0 {
			t.Errorf("a stale start anyway took snapshots: %+v", snaps)
		}
		if err := r.c.Start(context.Background(), id, admin, StartOptions{Anyway: true, Seen: AnywayFingerprint(c, false)}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("restart", func(t *testing.T) {
		r := newRig(t, rigOptions{})
		id := r.newWorld("Odd")
		r.start(id)
		broken(r, id)
		c := refused(r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{}))
		err := r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{Anyway: true, Seen: AnywayFingerprint(c, false)})
		wantChanged(t, err)
		if r.container(id).Status != "running" {
			t.Fatal("a stale restart anyway stopped the game")
		}
		err = r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{Anyway: true, Seen: AnywayFingerprint(c, true)})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("switch", func(t *testing.T) {
		r := newRig(t, rigOptions{})
		a, b := r.twoWorlds()
		r.start(a)
		broken(r, b)
		c := refused(r.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{}))
		err := r.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{Anyway: true, Seen: AnywayFingerprint(c, false)})
		wantChanged(t, err)
		if r.active() != a || r.container(a).Status != "running" {
			t.Fatal("a stale switch anyway changed something")
		}
		err = r.c.Switch(context.Background(), a, b, admin, StopOptions{}, StartOptions{Anyway: true, Seen: AnywayFingerprint(c, true)})
		if err != nil {
			t.Fatal(err)
		}
		if r.active() != b {
			t.Error("the switch anyway did not make the new world active")
		}
	})
}
