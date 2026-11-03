package world

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// leftovers lists work-in-progress folders in the worlds folder.
func leftovers(t *testing.T, m *Manager) []string {
	t.Helper()
	entries, err := os.ReadDir(m.dirs.Worlds)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out
}

// R12: a restore in progress must not stop other worlds from being renamed,
// made or imported; the create lock is held only for the swap.
func TestRestoreDoesNotHoldTheCreateLockWhileItRuns(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	other, err := m.Create(t.Context(), "Other", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	m.afterStage = func() {
		go func() { done <- m.Rename(t.Context(), other, "Other renamed") }()
		select {
		case err := <-done:
			done <- err
		case <-time.After(3 * time.Second):
		}
	}
	if _, err := m.Restore(t.Context(), id, snap.Name, "bob"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("rename: %v", err)
		}
	default:
		t.Fatal("renaming another world did not finish while the restore ran")
	}
}

// R12: a name taken after the restore's first check but before its swap is
// still refused, and the live world is left as it was.
func TestRestoreRefusesANameTakenWhileItRuns(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	other, err := m.Create(t.Context(), "Other", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Rename(t.Context(), id, "Renamed"); err != nil {
		t.Fatal(err)
	}
	m.afterStage = func() {
		if err := m.Rename(t.Context(), other, "base"); err != nil {
			t.Errorf("rename during the restore: %v", err)
		}
	}
	if _, err := m.Restore(t.Context(), id, snap.Name, "bob"); !errors.Is(err, ErrRestoreNameTaken) {
		t.Fatalf("got %v, want ErrRestoreNameTaken", err)
	}
	if meta, err := readMetaAt(m, id); err != nil || meta.Name != "Renamed" {
		t.Errorf("live world after a refused restore: %+v, %v; want it still named Renamed", meta, err)
	}
	if left := leftovers(t, m); len(left) != 0 {
		t.Errorf("left behind in the worlds folder: %v", left)
	}
}

// The same race for a deleted world, which is recreated rather than swapped.
func TestRestoreDeletedRefusesANameTakenWhileItRuns(t *testing.T) {
	m, _ := newTestManager(t)
	seedWorld(t, m)
	id := seedSecond(t, m)
	third, err := m.Create(t.Context(), "Third", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(t.Context(), id, "Second", true, "a"); err != nil {
		t.Fatal(err)
	}
	list, err := m.Snapshots(id)
	if err != nil || len(list) != 1 {
		t.Fatalf("snapshots %v, %v", list, err)
	}
	m.afterStage = func() {
		if err := m.Rename(t.Context(), third, "second"); err != nil {
			t.Errorf("rename during the restore: %v", err)
		}
	}
	if _, err := m.Restore(t.Context(), id, list[0].Name, "bob"); !errors.Is(err, ErrRestoreNameTaken) {
		t.Fatalf("got %v, want ErrRestoreNameTaken", err)
	}
	if _, err := os.Lstat(filepath.Join(m.dirs.Worlds, id)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused restore created the world: %v", err)
	}
	if left := leftovers(t, m); len(left) != 0 {
		t.Errorf("left behind in the worlds folder: %v", left)
	}
}
