package world

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// SPEC 6.5, 6.9, 6.6: a deleted world comes back exactly from its
// final snapshot, with no "Before restore" snapshot, and does not become
// active while another world is.
func TestRestoreDeletedWorldIsExact(t *testing.T) {
	m, active := newTestManager(t)
	seedWorld(t, m)
	id := seedSecond(t, m)
	want := fileTree(t, filepath.Join(m.dirs.Worlds, id))
	if err := m.Delete(t.Context(), id, "Second", true, "a"); err != nil {
		t.Fatal(err)
	}
	list, _ := m.Snapshots(id)
	if len(list) != 1 {
		t.Fatalf("snapshots %v", list)
	}

	before, err := m.Restore(context.Background(), id, list[0].Name, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if before.Name != "" {
		t.Errorf("a deleted world has nothing to snapshot, got %+v", before)
	}
	sameTree(t, fileTree(t, filepath.Join(m.dirs.Worlds, id)), want)
	if again, _ := m.Snapshots(id); len(again) != 1 {
		t.Errorf("snapshots after restore: %d", len(again))
	}
	if active.id == id {
		t.Error("restoring a deleted world displaced the active world")
	}
	entries, _ := os.ReadDir(m.dirs.Worlds)
	if len(entries) != 2 {
		t.Errorf("worlds folder holds %v", entries)
	}
	if d, _ := m.Deleted(); len(d) != 0 {
		t.Errorf("still listed as deleted: %v", d)
	}
}

func TestRestoreDeletedWorldBecomesActiveWhenNoneIs(t *testing.T) {
	m, active := newTestManager(t)
	id := seedWorld(t, m)
	active.id = ""
	if err := m.Delete(t.Context(), id, "Base", true, "a"); err != nil {
		t.Fatal(err)
	}
	list, _ := m.Snapshots(id)
	if _, err := m.Restore(context.Background(), id, list[0].Name, "a"); err != nil {
		t.Fatal(err)
	}
	if active.id != id {
		t.Errorf("active = %q, want %q", active.id, id)
	}
}

func TestRestoreDeletedWorldRefusals(t *testing.T) {
	m, _ := newTestManager(t)
	seedWorld(t, m)
	id := seedSecond(t, m)
	if err := m.Delete(t.Context(), id, "Second", true, "a"); err != nil {
		t.Fatal(err)
	}
	list, _ := m.Snapshots(id)
	ctx := context.Background()

	// Another world took the name meanwhile.
	other, err := m.Create(t.Context(), "second", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(ctx, id, list[0].Name, "a"); !errors.Is(err, ErrRestoreNameTaken) {
		t.Errorf("name taken: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(m.dirs.Worlds, id)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused restore made the world: %v", err)
	}
	if err := m.Delete(t.Context(), other, "second", false, "a"); err != nil {
		t.Fatal(err)
	}

	// A snapshot whose files no longer match its manifest.
	p := filepath.Join(m.dirs.Snapshots, id, list[0].Name, "config/x.json")
	if err := os.WriteFile(p, []byte("longer than before"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(ctx, id, list[0].Name, "a"); !errors.Is(err, ErrDamaged) {
		t.Errorf("damaged: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(m.dirs.Worlds, id)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a refused restore made the world: %v", err)
	}
	entries, _ := os.ReadDir(m.dirs.Worlds)
	for _, e := range entries {
		if e.Name()[0] == '.' {
			t.Errorf("leftover %s", e.Name())
		}
	}
}
