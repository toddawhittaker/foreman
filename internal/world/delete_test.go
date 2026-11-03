package world

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedSecond makes a second world, so the first (active) one stays.
func seedSecond(t *testing.T, m *Manager) string {
	t.Helper()
	id, err := m.Create(t.Context(), "Second", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.dirs.Worlds, id)
	writeAt(t, filepath.Join(dir, "saves/second.zip"), "second save", t0.Add(time.Hour+987654321))
	writeAt(t, filepath.Join(dir, "config/x.json"), "{}", t0.Add(2*time.Hour))
	return id
}

// SPEC 5.10 and 6.6: delete with a final snapshot keeps it, through
// retention, and the world's folder and files are gone.
func TestDeleteTakesFinalSnapshotAndKeepsItThroughRetention(t *testing.T) {
	m, _ := newTestManager(t)
	seedWorld(t, m)
	id := seedSecond(t, m)
	want := fileTree(t, filepath.Join(m.dirs.Worlds, id))

	now := t0
	m.now = func() time.Time { now = now.Add(time.Minute); return now }
	for range KeepAutomatic + 2 {
		if _, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "a"); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Delete(t.Context(), id, "Second", true, "bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(m.dirs.Worlds, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("world folder still there: %v", err)
	}
	list, err := m.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	var final *Snapshot
	automatic := 0
	for i, s := range list {
		if s.Reason == ReasonBeforeDelete {
			final = &list[i]
		}
		if s.Automatic() {
			automatic++
		}
	}
	if final == nil || final.Label != LabelBeforeDelete || final.TakenBy != "bob" {
		t.Fatalf("no final snapshot in %+v", list)
	}
	if automatic != KeepAutomatic {
		t.Errorf("%d automatic snapshots kept, want %d", automatic, KeepAutomatic)
	}

	// The final snapshot holds the whole world, and retention running
	// again never removes it.
	got := map[string]time.Time{}
	for _, f := range final.Files {
		got[f.Path] = f.ModTime
	}
	for p, w := range want {
		if g, ok := got[p]; !ok || !g.Equal(w.modTime) {
			t.Errorf("final snapshot lacks %s at %v", p, w.modTime)
		}
	}
	if err := m.prune(id); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Snapshot(id, final.Name); err != nil {
		t.Errorf("final snapshot pruned: %v", err)
	}
}

// Without the box ticked no snapshot is taken, but the id stays taken.
func TestDeleteWithoutSnapshotKeepsTheIDTaken(t *testing.T) {
	m, _ := newTestManager(t)
	seedWorld(t, m)
	id := seedSecond(t, m)
	if err := m.Delete(t.Context(), id, "Second", false, "bob"); err != nil {
		t.Fatal(err)
	}
	if list, _ := m.Snapshots(id); len(list) != 0 {
		t.Errorf("snapshots %v", list)
	}
	again, err := m.Create(t.Context(), "Second", "2.0.72", false)
	if err != nil || again == id {
		t.Errorf("new world got id %q (%v); the deleted world's id was reused", again, err)
	}
	deleted, err := m.Deleted()
	if err != nil || len(deleted) != 0 {
		t.Errorf("deleted = %v, %v; a world with no snapshots has nothing to restore", deleted, err)
	}
}

func TestDeleteRefusals(t *testing.T) {
	m, active := newTestManager(t)
	activeID := seedWorld(t, m)
	id := seedSecond(t, m)
	ctx := t.Context()

	if err := m.Delete(ctx, activeID, "Base", true, "a"); !errors.Is(err, ErrActive) {
		t.Errorf("active: %v", err)
	}
	for _, typed := range []string{"second", "Second ", "", "other"} {
		if err := m.Delete(ctx, id, typed, true, "a"); !errors.Is(err, ErrWrongName) {
			t.Errorf("typed %q: %v", typed, err)
		}
	}
	unlock, _ := m.Lock(id)
	if err := m.Delete(ctx, id, "Second", true, "a"); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
	unlock()
	if err := m.Delete(ctx, "nope", "x", true, "a"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}

	// A world a newer Foreman wrote cannot be changed.
	p := filepath.Join(m.dirs.Worlds, id, MetaFile)
	if err := os.WriteFile(p, []byte(`{"format_version":99,"name":"Second"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, id, "Second", true, "a"); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("newer: %v", err)
	}

	// Nothing was changed by any refusal.
	if _, err := os.Stat(filepath.Join(m.dirs.Worlds, id, "saves/second.zip")); err != nil {
		t.Error(err)
	}
	if list, _ := m.Snapshots(id); len(list) != 0 {
		t.Errorf("a refusal took a snapshot: %v", list)
	}

	// Once another world is active, the first is deletable.
	active.id = id
	if err := m.Delete(ctx, activeID, "Base", false, "a"); err != nil {
		t.Errorf("former active world: %v", err)
	}
}

// SPEC 6.2: if the final snapshot fails, the world is untouched.
func TestDeleteStopsWhenFinalSnapshotFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads any file")
	}
	m, _ := newTestManager(t)
	seedWorld(t, m)
	id := seedSecond(t, m)
	bad := filepath.Join(m.dirs.Worlds, id, "config", "zz-unreadable.json")
	writeAt(t, bad, "x", t0)
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}

	if err := m.Delete(t.Context(), id, "Second", true, "a"); err == nil {
		t.Fatal("delete went ahead although the snapshot failed")
	}
	for _, p := range []string{"saves/second.zip", MetaFile, "config/x.json"} {
		if _, err := os.Stat(filepath.Join(m.dirs.Worlds, id, p)); err != nil {
			t.Errorf("world was changed: %v", err)
		}
	}
	if list, _ := m.Snapshots(id); len(list) != 0 {
		t.Errorf("snapshots %v", list)
	}
	entries, _ := os.ReadDir(m.dirs.Worlds)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

// A crash after the rename leaves a .deleting folder that startup removes.
func TestRecoverClearsHalfDeletedWorld(t *testing.T) {
	m, _ := newTestManager(t)
	seedWorld(t, m)
	left := filepath.Join(m.dirs.Worlds, PrefixDeleting+"second-0123456789abcdef")
	if err := os.MkdirAll(filepath.Join(left, "saves"), 0o750); err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(left, "saves/a.zip"), "x", t0)
	if err := m.Recover(slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(left); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("leftover remains: %v", err)
	}
	list, err := m.List(t.Context())
	if err != nil || len(list) != 1 {
		t.Errorf("list %v, %v", list, err)
	}
}

func TestDeletedListsWorldsWithSnapshotsAndNoFolder(t *testing.T) {
	m, _ := newTestManager(t)
	live := seedWorld(t, m)
	if _, err := m.TakeSnapshot(live, "", "a"); err != nil {
		t.Fatal(err)
	}
	id := seedSecond(t, m)
	if got, err := m.Deleted(); err != nil || len(got) != 0 {
		t.Fatalf("before delete: %v, %v", got, err)
	}
	if err := m.Delete(t.Context(), id, "Second", true, "a"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Deleted()
	if err != nil || len(got) != 1 {
		t.Fatalf("deleted = %v, %v", got, err)
	}
	if got[0].ID != id || got[0].Name != "Second" || len(got[0].Snapshots) != 1 {
		t.Errorf("deleted[0] = %+v", got[0])
	}
}
