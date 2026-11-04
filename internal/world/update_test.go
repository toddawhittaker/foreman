package world

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// These tests pin what an update and a rollback must hold at the world
// level (SPEC 10.3 to 10.5, 6.2, 6.6):
//
//   - a version only moves up, to an exact release of a supported line;
//   - the "Before update" snapshot keeps the old version, and rollback
//     goes back one update at a time, version and save together;
//   - pruning never removes a world's newest "Before update" snapshot.

// tickingManager is a test manager whose clock moves a second per read,
// so snapshots taken in a row are ordered.
func tickingManager(t *testing.T) *Manager {
	t.Helper()
	data := t.TempDir()
	var mu sync.Mutex
	now := t0
	m, err := NewManager(Dirs{
		Worlds:    filepath.Join(data, "worlds"),
		Snapshots: filepath.Join(data, "snapshots"),
		Imports:   filepath.Join(data, "import"),
	}, &fakeActive{}, func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Second)
		return now
	})
	if err != nil {
		t.Fatal(err)
	}
	m.log = slog.New(slog.NewTextHandler(io.Discard, nil))
	return m
}

// updateWorld makes a world at version with one save holding saveBody.
func updateWorld(t *testing.T, m *Manager, version, saveBody string) string {
	t.Helper()
	id, err := m.Create(t.Context(), "Update", version, false)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(m.dirs.Worlds, id, SavesDir, "base.zip"), saveBody)
	return id
}

func worldVersion(t *testing.T, m *Manager, id string) string {
	t.Helper()
	root, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	return meta.GameVersion
}

// snapshotVersion is the game version in a snapshot's copy of world.json.
func snapshotVersion(t *testing.T, m *Manager, id, name string) string {
	t.Helper()
	root, err := os.OpenRoot(filepath.Join(m.dirs.Snapshots, id, name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	return meta.GameVersion
}

// setVersion sets the version the way an update does, under the lock.
func setVersion(t *testing.T, m *Manager, id, version string) {
	t.Helper()
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := m.SetGameVersionHeld(id, version); err != nil {
		t.Fatalf("SetGameVersionHeld(%s): %v", version, err)
	}
}

// update does at the world level what an update of a stopped world does:
// the "Before update" snapshot, then the new version.
func update(t *testing.T, m *Manager, id, version string) Snapshot {
	t.Helper()
	snap, err := m.AutoSnapshot(id, ReasonGameUpdate, LabelBeforeUpdate, "alice")
	if err != nil {
		t.Fatal(err)
	}
	setVersion(t, m, id, version)
	return snap
}

func TestUpdateNames(t *testing.T) {
	if ReasonGameUpdate != "game-update" || LabelBeforeUpdate != "Before update" {
		t.Errorf("reason %q, label %q", ReasonGameUpdate, LabelBeforeUpdate)
	}
}

// SPEC 10.3: a world's version only moves up, to an exact release of a
// line Foreman runs. Each refusal says why with its own error.
func TestCheckUpdateTarget(t *testing.T) {
	meta := Meta{FormatVersion: FormatVersion, Name: "Base", GameVersion: "2.0.77"}
	for _, target := range []string{"2.0.78", "2.1.0", "2.1.24"} {
		if err := CheckUpdateTarget(meta, target); err != nil {
			t.Errorf("CheckUpdateTarget(2.0.77, %q) = %v, want nil", target, err)
		}
	}
	for _, target := range []string{"2.0.77", "2.0.76", "2.0.9", "1.1.110"} {
		if err := CheckUpdateTarget(meta, target); !errors.Is(err, ErrVersionNotNewer) {
			t.Errorf("CheckUpdateTarget(2.0.77, %q) = %v, want ErrVersionNotNewer", target, err)
		}
	}
	for _, target := range []string{"2.2.0", "3.0.0", "1.2.0", "0.18.47"} {
		if err := CheckUpdateTarget(meta, target); !errors.Is(err, ErrVersionUnsupported) {
			t.Errorf("CheckUpdateTarget(2.0.77, %q) = %v, want ErrVersionUnsupported", target, err)
		}
	}
	// A malformed version is refused; which of the two errors is not pinned.
	for _, target := range []string{"", "2.1", "latest", "2.1.24 ", "v2.1.24", "2.1.24.1", "../2.1.24", "2.1.x"} {
		err := CheckUpdateTarget(meta, target)
		if !errors.Is(err, ErrVersionNotNewer) && !errors.Is(err, ErrVersionUnsupported) {
			t.Errorf("CheckUpdateTarget(2.0.77, %q) = %v, want a refusal", target, err)
		}
	}
	// Numbers, not text: 2.0.100 is newer than 2.0.77.
	if err := CheckUpdateTarget(meta, "2.0.100"); err != nil {
		t.Errorf("CheckUpdateTarget(2.0.77, 2.0.100) = %v", err)
	}
}

// Setting the version changes world.json's version and nothing else.
func TestSetGameVersionHeldChangesOnlyTheVersion(t *testing.T) {
	m := tickingManager(t)
	id := updateWorld(t, m, "2.0.77", "save one")
	root, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ReadMeta(root)
	_ = root.Close()
	if err != nil {
		t.Fatal(err)
	}
	setVersion(t, m, id, "2.1.24")
	root, err = m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	after, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	if after.GameVersion != "2.1.24" {
		t.Errorf("version %q, want 2.1.24", after.GameVersion)
	}
	after.GameVersion = before.GameVersion
	if after.Name != before.Name || after.SpaceAge != before.SpaceAge || after.LoadSave != before.LoadSave ||
		!after.CreatedAt.Equal(before.CreatedAt) || after.FormatVersion != before.FormatVersion {
		t.Errorf("world.json changed beyond the version: %+v, was %+v", after, before)
	}
	b, err := os.ReadFile(filepath.Join(m.dirs.Worlds, id, SavesDir, "base.zip")) //nolint:gosec // a test temp folder
	if err != nil || string(b) != "save one" {
		t.Errorf("save changed: %q, %v", b, err)
	}
}

// SPEC 6.2: the "Before update" snapshot holds the old version.
func TestBeforeUpdateSnapshotHoldsTheOldVersion(t *testing.T) {
	m := tickingManager(t)
	id := updateWorld(t, m, "2.0.77", "save one")
	snap := update(t, m, id, "2.1.24")
	if snap.Reason != ReasonGameUpdate || snap.Label != LabelBeforeUpdate || !snap.Automatic() {
		t.Errorf("snapshot %+v", snap.Manifest)
	}
	if v := snapshotVersion(t, m, id, snap.Name); v != "2.0.77" {
		t.Errorf("snapshot's world.json says %s, want 2.0.77", v)
	}
	if v := worldVersion(t, m, id); v != "2.1.24" {
		t.Errorf("world at %s, want 2.1.24", v)
	}
}

// A world that was never updated has nothing to roll back to.
func TestRollbackTargetNone(t *testing.T) {
	m := tickingManager(t)
	id := updateWorld(t, m, "2.1.24", "save")
	if _, err := m.TakeSnapshot(id, "mine", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.RollbackTarget(id); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("RollbackTarget = %v, want ErrNoRollback", err)
	}
}

// A "Before update" snapshot at the current version (an update that was
// snapshotted but did not set the version) is not a step back.
func TestRollbackTargetSkipsSnapshotsAtTheCurrentVersion(t *testing.T) {
	m := tickingManager(t)
	id := updateWorld(t, m, "2.1.24", "save")
	if _, err := m.AutoSnapshot(id, ReasonGameUpdate, LabelBeforeUpdate, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.RollbackTarget(id); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("RollbackTarget = %v, want ErrNoRollback", err)
	}
}

// Ruling 5 of the plan, SPEC 10.4: rollback goes one update back at a time,
// and each step puts back that update's old version and save together.
func TestRollbackStepsBackOneUpdateAtATime(t *testing.T) {
	m := tickingManager(t)
	id := updateWorld(t, m, "2.0.77", "save at 2.0.77")
	first := update(t, m, id, "2.1.23")
	writeFile(t, filepath.Join(m.dirs.Worlds, id, SavesDir, "base.zip"), "save at 2.1.23")
	second := update(t, m, id, "2.1.24")
	writeFile(t, filepath.Join(m.dirs.Worlds, id, SavesDir, "base.zip"), "save at 2.1.24")
	// Other snapshots after the updates do not change the target.
	if _, err := m.TakeSnapshot(id, "mine", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "alice"); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		snap    Snapshot
		version string
		save    string
	}{{second, "2.1.23", "save at 2.1.23"}, {first, "2.0.77", "save at 2.0.77"}}
	for _, s := range steps {
		snap, version, err := m.RollbackTarget(id)
		if err != nil {
			t.Fatalf("RollbackTarget at %s: %v", worldVersion(t, m, id), err)
		}
		if snap.Name != s.snap.Name || version != s.version {
			t.Fatalf("RollbackTarget = %s at %s, want %s at %s", snap.Name, version, s.snap.Name, s.version)
		}
		if _, err := m.Restore(t.Context(), id, snap.Name, "alice"); err != nil {
			t.Fatal(err)
		}
		if v := worldVersion(t, m, id); v != s.version {
			t.Errorf("after rollback the world is at %s, want %s", v, s.version)
		}
		b, err := os.ReadFile(filepath.Join(m.dirs.Worlds, id, SavesDir, "base.zip")) //nolint:gosec // a test temp folder
		if err != nil || string(b) != s.save {
			t.Errorf("after rollback to %s the save is %q, %v; want %q", s.version, b, err, s.save)
		}
	}
	if _, _, err := m.RollbackTarget(id); !errors.Is(err, ErrNoRollback) {
		t.Fatalf("RollbackTarget after the first update = %v, want ErrNoRollback", err)
	}
}

// Ruling 5 of the plan: pruning keeps a world's newest "Before update"
// snapshot however many automatic snapshots follow it (SPEC 6.6, 10.4).
func TestPruneKeepsTheNewestBeforeUpdateSnapshot(t *testing.T) {
	m := tickingManager(t)
	id := updateWorld(t, m, "2.0.77", "save")
	update(t, m, id, "2.1.23")
	newest := update(t, m, id, "2.1.24")
	for range KeepAutomatic * 2 {
		if _, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "alice"); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.prune(id); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Snapshot(id, newest.Name); err != nil {
		t.Fatalf("the newest Before update snapshot was pruned: %v", err)
	}
	snap, version, err := m.RollbackTarget(id)
	if err != nil || snap.Name != newest.Name || version != "2.1.23" {
		t.Errorf("RollbackTarget = %s at %s, %v; want %s at 2.1.23", snap.Name, version, err, newest.Name)
	}
	// Pruning still runs: the other automatic snapshots stay within the cap
	// plus the one kept.
	list, err := m.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	automatic := 0
	for _, s := range list {
		if s.Automatic() {
			automatic++
		}
	}
	if automatic > KeepAutomatic+1 {
		t.Errorf("%d automatic snapshots kept, want at most %d", automatic, KeepAutomatic+1)
	}
}
