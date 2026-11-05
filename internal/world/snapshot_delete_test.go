package world

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// SPEC 6.12: deleting a snapshot removes only its folder. Zips it shares by
// hard link with the world and other snapshots stay, and the world is not
// touched.
func TestDeleteSnapshotRemovesOnlyThatSnapshot(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	first, err := m.TakeSnapshot(id, "one", "a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.TakeSnapshot(id, "two", "a")
	if err != nil {
		t.Fatal(err)
	}
	worldDir := filepath.Join(m.dirs.Worlds, id)
	before := diskTree(t, worldDir)

	if err := m.DeleteSnapshot(id, first.Name, noHook); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(m.dirs.Snapshots, id, first.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("snapshot folder still there: %v", err)
	}
	sameDisk(t, "world", diskTree(t, worldDir), before)
	if b, err := os.ReadFile(filepath.Join(m.dirs.Snapshots, id, second.Name, "saves/base.zip")); err != nil || string(b) != "save one" { //nolint:gosec // a test temp file
		t.Errorf("other snapshot's linked zip: %q %v", b, err)
	}
	list, err := m.Snapshots(id)
	if err != nil || len(list) != 1 || list[0].Name != second.Name {
		t.Errorf("left %+v %v", list, err)
	}
	if err := m.DeleteSnapshot(id, first.Name, noHook); !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("deleting twice: %v", err)
	}
	if _, err := m.Restore(t.Context(), id, second.Name, "a"); err != nil {
		t.Errorf("the other snapshot no longer restores: %v", err)
	}
}

func TestDeleteSnapshotRefusesBadNamesAndBusyWorlds(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	s, err := m.TakeSnapshot(id, "one", "a")
	if err != nil {
		t.Fatal(err)
	}
	snapsBefore := diskTree(t, m.dirs.Snapshots)
	worldsBefore := diskTree(t, m.dirs.Worlds)
	for _, bad := range []struct{ id, name string }{
		{id, "../" + s.Name}, {id, ".."}, {id, "."}, {id, ""}, {id, s.Name + "/saves"},
		{"..", id}, {"../worlds", id}, {id, "x" + s.Name}, {"nope", s.Name},
		{id, "20261103T142501Z-99"},
	} {
		if err := m.DeleteSnapshot(bad.id, bad.name, noHook); !errors.Is(err, ErrNoSnapshot) {
			t.Errorf("DeleteSnapshot(%q, %q) = %v, want ErrNoSnapshot", bad.id, bad.name, err)
		}
	}
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteSnapshot(id, s.Name, noHook); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
	unlock()
	stop := errors.New("audit failed")
	if err := m.DeleteSnapshot(id, s.Name, func(got Snapshot) error {
		if got.Label != "one" {
			t.Errorf("hook saw %+v", got)
		}
		return stop
	}); !errors.Is(err, stop) {
		t.Errorf("a failing hook: %v", err)
	}
	sameDisk(t, "snapshots", diskTree(t, m.dirs.Snapshots), snapsBefore)
	sameDisk(t, "worlds", diskTree(t, m.dirs.Worlds), worldsBefore)
}

// SPEC 6.12: a snapshot's own share is what no other link holds; the total
// counts each hard-linked zip once.
func TestAllSnapshotsCountsSharedZipsOnce(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	a, err := m.TakeSnapshot(id, "one", "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.TakeSnapshot(id, "two", "a"); err != nil {
		t.Fatal(err)
	}
	list, total, err := m.AllSnapshots()
	if err != nil || len(list) != 2 {
		t.Fatalf("list %+v %v", list, err)
	}
	// The zips (8 + 3 bytes) are shared with the world, so they free
	// nothing; the copied small files free their size.
	small := a.Size() - 11
	for _, u := range list {
		if u.WorldID != id || u.Frees != small {
			t.Errorf("%s: world %q frees %d, want %d", u.Name, u.WorldID, u.Frees, small)
		}
	}
	if want := 2*small + 11; total != want {
		t.Errorf("total %d, want %d", total, want)
	}
	frees, err := m.SnapshotFrees(id, a)
	if err != nil || frees != small {
		t.Errorf("SnapshotFrees %d %v", frees, err)
	}
}

func noHook(Snapshot) error { return nil }
