package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/datadir"
	"github.com/toddawhittaker/foreman/internal/gamedir"
	"github.com/toddawhittaker/foreman/internal/world"
)

// unadoptedFolder lays out a game folder the image made itself, with real
// saves, mods and config (SPEC 4.8).
func unadoptedFolder(t *testing.T) func(dir string) {
	return func(dir string) {
		for _, sub := range gamedir.FixedLinks {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o750); err != nil {
				t.Fatal(err)
			}
		}
		b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", "2.1.24-no-space-age.zip"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "saves", "base.zip"), b, 0o600); err != nil { //nolint:gosec // a test's own temp folder
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config", "rconpw"), []byte("pw-original\n"), 0o640); err != nil { //nolint:gosec // the mode the image's group needs
			t.Fatal(err)
		}
	}
}

func TestAdoptionOfARunningGame(t *testing.T) {
	ctx := context.Background()
	r, dir := newAdoptedRig(t, adoptedOptions{running: true, prepare: unadoptedFolder(t)})
	eventually(t, func() bool {
		d, err := r.c.AdoptionDefaults(ctx)
		return err == nil && d.Running
	}, "the container to run")
	d, err := r.c.AdoptionDefaults(ctx)
	if err != nil || !d.Unadopted || d.GameVersion != "2.1.24" || d.SpaceAge {
		t.Fatalf("defaults = %+v, %v", d, err)
	}

	a, err := r.c.ReserveAdoption(ctx, "Gate", d.GameVersion, d.SpaceAge)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.c.Adopt(ctx, a, admin, StopOptions{}); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	st := r.waitState(a.ID, StateRunning)
	presents(t, dir, a.ID)
	if r.active() != a.ID {
		t.Errorf("active = %q", r.active())
	}
	for _, s := range st.Operation.Steps {
		if s.State != StepDone && s.State != StepSkipped {
			t.Errorf("step %q is %s", s.Name, s.State)
		}
	}
	if !slices.Contains(r.adoptedState().Commands, "/server-save") {
		t.Error("the game was not saved before the adoption")
	}
	if !slices.Contains(r.actions(), AuditAdopt) {
		t.Errorf("audit = %v", r.actions())
	}
	snaps, err := r.worlds.Snapshots(a.ID)
	if err != nil || !slices.ContainsFunc(snaps, func(s world.Snapshot) bool { return s.Reason == world.ReasonAdoption }) {
		t.Errorf("snapshots = %+v, %v", snaps, err)
	}
	// The originals are kept, and the save is the same file (SPEC 4.8).
	pre, _ := filepath.Glob(filepath.Join(dir, gamedir.PreForemanPrefix+"*"))
	if len(pre) != 1 {
		t.Fatalf("pre-foreman folders = %v", pre)
	}
	kept, err1 := os.Stat(filepath.Join(pre[0], "saves", "base.zip"))
	adopted, err2 := os.Stat(filepath.Join(r.worldDir(a.ID), "saves", "base.zip"))
	if err1 != nil || err2 != nil || !os.SameFile(kept, adopted) {
		t.Errorf("save not linked into the world: %v, %v", err1, err2)
	}
	if _, err := os.Lstat(filepath.Join(dir, adoptionMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("adoption record left behind: %v", err)
	}
	if _, err := r.c.ReserveAdoption(ctx, "Again", "2.1.24", false); !errors.Is(err, ErrAlreadyAdopted) {
		t.Errorf("second adoption = %v", err)
	}
}

func TestAdoptionOfAStoppedGameLeavesItStopped(t *testing.T) {
	ctx := context.Background()
	r, dir := newAdoptedRig(t, adoptedOptions{prepare: unadoptedFolder(t)})
	a, err := r.c.ReserveAdoption(ctx, "Gate", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.c.Adopt(ctx, a, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	presents(t, dir, a.ID)
	if cs := r.adoptedState(); cs.Status != "exited" {
		t.Errorf("container status %q", cs.Status)
	}
	r.start(a.ID)
}

func TestAdoptionRefusalsChangeNothing(t *testing.T) {
	ctx := context.Background()
	r, dir := newAdoptedRig(t, adoptedOptions{running: true, env: []string{"UPDATE_MODS_ON_START=true"}, prepare: unadoptedFolder(t)})
	if _, err := r.c.ReserveAdoption(ctx, "Gate", "2.1.24", false); !errors.Is(err, ErrContainerRefused) {
		t.Fatalf("reserve = %v", err)
	}
	if _, err := r.c.ReserveAdoption(ctx, "Gate", "2.1.24", true); err == nil {
		t.Fatal("a Space Age world on a container with it off was accepted")
	}
	if st, _ := gamedir.Inspect(dir); st.Kind != gamedir.Unadopted {
		t.Errorf("folder is %s", st.Kind)
	}
	if cs := r.adoptedState(); cs.Status != "running" {
		t.Errorf("container status %q", cs.Status)
	}
}

// TestAdoptionRecoveryAtEachStep stops an adoption, as a crash would,
// after each change it makes on disk, and checks that startup finishes it
// with the snapshot first and the originals kept (SPEC 3.8, 4.8).
func TestAdoptionRecoveryAtEachStep(t *testing.T) {
	ctx := context.Background()
	build := func(t *testing.T, r *rig, dir, id string) {
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = root.Close() }()
		if _, err := r.worlds.Adopt(ctx, root, id, "Gate", "2.1.24", false, admin.Username); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name  string
		after func(t *testing.T, r *rig, dir, id string)
		made  bool // the world exists after recovery
	}{
		{"record written", func(*testing.T, *rig, string, string) {}, false},
		{"world built, no snapshot", func(t *testing.T, r *rig, dir, id string) {
			build(t, r, dir, id)
			snapDir := filepath.Join(datadir.Snapshots(r.data), id)
			entries, err := os.ReadDir(snapDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if err := os.RemoveAll(filepath.Join(snapDir, e.Name())); err != nil {
					t.Fatal(err)
				}
			}
			if snaps, _ := r.worlds.Snapshots(id); len(snaps) != 0 {
				t.Fatalf("snapshots left: %+v", snaps)
			}
		}, true},
		{"snapshot taken", build, true},
		{"cut-over part way", func(t *testing.T, r *rig, dir, id string) {
			build(t, r, dir, id)
			if err := os.Mkdir(filepath.Join(dir, gamedir.PreForemanPrefix+"20260101T000000Z"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("worlds/"+id, filepath.Join(dir, "current.new")); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(dir, "saves"), filepath.Join(dir, gamedir.PreForemanPrefix+"20260101T000000Z", "saves")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("current/saves", filepath.Join(dir, "saves")); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"cut over, not active", func(t *testing.T, r *rig, dir, id string) {
			build(t, r, dir, id)
			if _, err := gamedir.CutOver(dir, id, time.Now()); err != nil {
				t.Fatal(err)
			}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, dir := newAdoptedRig(t, adoptedOptions{noRun: true, prepare: unadoptedFolder(t)})
			a, err := r.c.ReserveAdoption(ctx, "Gate", "2.1.24", false)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.c.writeAdoptionMarker(a.ID); err != nil {
				t.Fatal(err)
			}
			tc.after(t, r, dir, a.ID)

			if err := r.c.Recover(ctx); err != nil {
				t.Fatalf("recover: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(dir, adoptionMarker)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("adoption record left behind: %v", err)
			}
			if !tc.made {
				if st, _ := gamedir.Inspect(dir); st.Kind != gamedir.Unadopted {
					t.Errorf("folder is %s", st.Kind)
				}
				return
			}
			presents(t, dir, a.ID)
			if r.active() != a.ID {
				t.Errorf("active = %q", r.active())
			}
			snaps, err := r.worlds.Snapshots(a.ID)
			if err != nil || !slices.ContainsFunc(snaps, func(s world.Snapshot) bool { return s.Reason == world.ReasonAdoption }) {
				t.Errorf("no adoption snapshot: %+v, %v", snaps, err)
			}
			kept, _ := filepath.Glob(filepath.Join(dir, gamedir.PreForemanPrefix+"*", "saves", "base.zip"))
			if len(kept) != 1 {
				t.Errorf("original save kept at %v", kept)
			}
			if cs := r.adoptedState(); cs.Status != "exited" {
				t.Errorf("recovery started the game: %q", cs.Status)
			}
		})
	}
}
