package game

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 10.3: a change the mods folder would refuse is refused after the
// download but before the stop, so it costs no downtime.
func TestApplyModsRefusesABadChangeBeforeTheStop(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Bad change")
	r.putModZip(id, "oldmod", "2.1")
	r.start(id)
	first := r.container(id).ID
	f := newFetcher()
	dl := r.offer(f, "flib", "0.16.0", "2.1")
	r.c.SetModFetcher(f)
	op := r.modOp(id, "install flib", dl)
	// A file outside the mods folder, which ApplyModsHeld refuses to remove.
	op.Change.Remove = []string{"../" + world.MetaFile}
	if err := r.applyMods(id, op); !errors.Is(err, world.ErrModFile) {
		t.Fatalf("ApplyMods = %v, want ErrModFile", err)
	}
	if cs := r.container(id); cs.ID != first || cs.Status != "running" {
		t.Errorf("container %s %s, want the first one still running", cs.ID, cs.Status)
	}
	if slices.Contains(r.commands(id), "/server-save") {
		t.Error("the game was stopped for a change that was refused")
	}
	if snaps := r.snapshots(id); len(snaps) != 0 {
		t.Errorf("snapshots %+v for a refused change", snaps)
	}
	if left := r.stagedFiles(); len(left) != 0 {
		t.Errorf("staged files left: %v", left)
	}
}

// SPEC 7.4: a world started anyway, past a problem, comes back after a mod
// change that leaves that problem as it was. A new problem still refuses.
func TestApplyModsOnAWorldStartedAnyway(t *testing.T) {
	startAnyway := func(r *rig) string {
		id := r.newWorld("Anyway")
		r.writeFile(id, filepath.Join(world.SavesDir, "broken.zip"), []byte("not a zip"))
		var pe *PrecheckError
		if err := r.c.Start(context.Background(), id, admin, StartOptions{}); !errors.As(err, &pe) {
			t.Fatalf("start = %v, want a refusal", err)
		}
		if err := r.c.Start(context.Background(), id, admin, StartOptions{Anyway: true, Seen: AnywayFingerprint(pe.Check, false)}); err != nil {
			t.Fatal(err)
		}
		return id
	}

	t.Run("same problem", func(t *testing.T) {
		r := newRig(t, rigOptions{})
		id := startAnyway(r)
		f := newFetcher()
		dl := r.offer(f, "flib", "0.16.0", "2.1")
		r.c.SetModFetcher(f)
		if err := r.applyMods(id, r.modOp(id, "install flib", dl)); err != nil {
			t.Fatalf("ApplyMods = %v, want the world running again", err)
		}
		if st := r.c.Status(id); st.State != StateRunning {
			t.Errorf("state %s, want running", st.State)
		}
	})

	t.Run("new problem", func(t *testing.T) {
		r := newRig(t, rigOptions{})
		id := startAnyway(r)
		f := newFetcher()
		dl := r.offer(f, "oldline", "1.0.0", "1.1") // made for another game line
		r.c.SetModFetcher(f)
		kinds := r.problemKinds(r.applyMods(id, r.modOp(id, "install oldline", dl)))
		if !slices.Contains(kinds, world.ProblemModGameVersion) {
			t.Errorf("problems %v, want the new mod version problem", kinds)
		}
		if st := r.c.Status(id); st.State == StateRunning {
			t.Error("the game was started past a new problem")
		}
	})
}

// A problem passes only when the game ran with one of its kind that named
// every mod it names, and that a start anyway allows.
func TestRanWith(t *testing.T) {
	off := func(mods ...string) world.Problem {
		return world.Problem{Kind: world.ProblemModDisabled, StartAnyway: true, Mods: mods}
	}
	cases := []struct {
		p      world.Problem
		before []world.Problem
		want   bool
	}{
		{off("a"), []world.Problem{off("a", "b")}, true},
		{off("a", "c"), []world.Problem{off("a", "b")}, false},
		{off("a"), nil, false},
		{off("a"), []world.Problem{{Kind: world.ProblemModMissing, StartAnyway: true, Mods: []string{"a"}}}, false},
		{world.Problem{Kind: world.ProblemModGameVersion}, []world.Problem{{Kind: world.ProblemModGameVersion}}, false},
		{world.Problem{Kind: world.ProblemSaveUnreadable, StartAnyway: true}, []world.Problem{{Kind: world.ProblemSaveUnreadable, StartAnyway: true}}, true},
	}
	for _, c := range cases {
		if got := ranWith(c.p, c.before); got != c.want {
			t.Errorf("ranWith(%+v, %+v) = %v, want %v", c.p, c.before, got, c.want)
		}
	}
}
