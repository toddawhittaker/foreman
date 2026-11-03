package world

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fileState struct {
	body    string
	modTime time.Time
}

// fileTree reads every regular file under dir with its contents and time.
func fileTree(t *testing.T, dir string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // a test temp file
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[rel] = fileState{string(b), info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, got, want map[string]fileState) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%d files, want %d: %v", len(got), len(want), got)
	}
	for p, w := range want {
		g, ok := got[p]
		if !ok {
			t.Errorf("%s missing", p)
			continue
		}
		if g.body != w.body || !g.modTime.Equal(w.modTime) {
			t.Errorf("%s: got %q at %v, want %q at %v", p, g.body, g.modTime, w.body, w.modTime)
		}
	}
}

// SPEC 6.5 and 6.9: restore gives back exact contents and modification
// times, and the snapshot taken first is the undo.
func TestRestoreGivesBackExactContentsAndTimes(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	world := filepath.Join(m.dirs.Worlds, id)
	want := fileTree(t, world)

	now := t0
	m.now = func() time.Time { now = now.Add(time.Minute); return now }
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Play on: a new save, a changed config, a removed mod, and the old
	// save's time touched (it shares an inode with the snapshot).
	writeAt(t, filepath.Join(world, "saves/later.zip"), "later", t0.Add(48*time.Hour))
	writeAt(t, filepath.Join(world, "config/server.json"), `{"a":2}`, t0.Add(48*time.Hour))
	if err := os.Remove(filepath.Join(world, "mods/flib_0.15.0.zip")); err != nil {
		t.Fatal(err)
	}
	later := t0.Add(72 * time.Hour)
	if err := os.Chtimes(filepath.Join(world, "saves/base.zip"), later, later); err != nil {
		t.Fatal(err)
	}
	played := fileTree(t, world)

	before, err := m.Restore(id, snap.Name, "bob")
	if err != nil {
		t.Fatal(err)
	}
	sameTree(t, fileTree(t, world), want)
	if before.Reason != ReasonBeforeRestore || before.Label != LabelBeforeRestore || before.TakenBy != "bob" {
		t.Errorf("before-restore snapshot %+v", before)
	}

	// Changing a time on the world afterwards must not change what the
	// snapshot records, so a second restore is exact too.
	if err := os.Chtimes(filepath.Join(world, "saves/base.zip"), later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(id, snap.Name, "bob"); err != nil {
		t.Fatal(err)
	}
	sameTree(t, fileTree(t, world), want)

	// Undo: restoring the first before-restore snapshot brings back play.
	if _, err := m.Restore(id, before.Name, "bob"); err != nil {
		t.Fatal(err)
	}
	sameTree(t, fileTree(t, world), played)

	entries, _ := os.ReadDir(m.dirs.Worlds)
	if len(entries) != 1 {
		t.Errorf("worlds folder holds %v", entries)
	}
}

func TestRestoreRefusesBusyMissingAndDamaged(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(id, snap.Name, "a"); !errors.Is(err, ErrBusy) {
		t.Errorf("locked: %v", err)
	}
	if _, err := m.TakeSnapshot(id, "", "a"); !errors.Is(err, ErrBusy) {
		t.Errorf("locked snapshot: %v", err)
	}
	unlock()
	for _, name := range []string{"../x", "20991231T000000Z"} {
		if _, err := m.Restore(id, name, "a"); !errors.Is(err, ErrNoSnapshot) {
			t.Errorf("%q: %v", name, err)
		}
	}
	p := filepath.Join(m.dirs.Snapshots, id, snap.Name, "config/server.json")
	if err := os.WriteFile(p, []byte("longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(id, snap.Name, "a"); !errors.Is(err, ErrDamaged) {
		t.Errorf("damaged: %v", err)
	}
	list, _ := m.Snapshots(id)
	if len(list) != 1 {
		t.Errorf("a refused restore took a snapshot: %d", len(list))
	}
}

// R9: a crash between the swap's renames is finished or reversed at startup.
func TestRecoverHalfDoneSwap(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, liveThere := range []bool{false, true} {
		m, _ := newTestManager(t)
		id := seedWorld(t, m)
		world := filepath.Join(m.dirs.Worlds, id)
		oldState := fileTree(t, world)
		old := filepath.Join(m.dirs.Worlds, PrefixRestoreOld+id+"-0123456789abcdef")
		if err := os.Rename(world, old); err != nil {
			t.Fatal(err)
		}
		staging := filepath.Join(m.dirs.Worlds, PrefixStaging+id+"-0123456789abcdef")
		if err := os.MkdirAll(filepath.Join(staging, "saves"), 0o750); err != nil {
			t.Fatal(err)
		}
		writeAt(t, filepath.Join(staging, "saves/new.zip"), "new", t0)
		if liveThere {
			if err := os.Rename(staging, world); err != nil {
				t.Fatal(err)
			}
		}
		if err := m.Recover(quiet); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(m.dirs.Worlds)
		if len(entries) != 1 || entries[0].Name() != id {
			t.Errorf("liveThere=%v: worlds folder holds %v", liveThere, entries)
		}
		if liveThere {
			sameTree(t, fileTree(t, world), map[string]fileState{"saves/new.zip": {"new", t0}})
		} else {
			sameTree(t, fileTree(t, world), oldState)
		}
	}
}

func TestRestoreOldID(t *testing.T) {
	for name, want := range map[string]string{
		".restore-old-base-0123456789abcdef":   "base",
		".restore-old-a-b-0123456789abcdef":    "a-b",
		".restore-old-0123456789abcdef":        "",
		".restore-old-BAD-0123456789abcdef":    "",
		".staging-base-0123456789abcdef":       "",
		".restore-old-base_0123456789abcdef":   "",
		".restore-old-../x-0123456789abcdefff": "",
	} {
		got, ok := restoreOldID(name)
		if ok != (want != "") || got != want && ok {
			t.Errorf("%q: %q %v", name, got, ok)
		}
	}
}
