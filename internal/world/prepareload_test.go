package world

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrepareLoadMakesTheChosenSaveNewest(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "chosen.zip", "c", t0.Add(-3*time.Hour))
	putSave(t, dir, "_autosave1.zip", "a", t0.Add(-time.Hour))
	if err := m.SetLoadSave(id, "chosen.zip"); err != nil {
		t.Fatal(err)
	}
	name, err := m.PrepareLoad(id)
	if err != nil {
		t.Fatal(err)
	}
	newest, _, err := m.NewestSave(id)
	if err != nil {
		t.Fatal(err)
	}
	if name != "chosen.zip" || newest.Name != "chosen.zip" {
		t.Fatalf("prepared %q, newest %q", name, newest.Name)
	}
	// Set to now, never later, so a later autosave wins after a crash.
	if !newest.ModTime.Equal(t0) {
		t.Fatalf("time %v, want %v", newest.ModTime, t0)
	}
}

func TestPrepareLoadWithNoChoiceChangesNothing(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	old := t0.Add(-time.Hour)
	putSave(t, dir, "a.zip", "a", old)
	putSave(t, dir, "b.zip", "b", old.Add(-time.Hour))
	name, err := m.PrepareLoad(id)
	if err != nil || name != "a.zip" {
		t.Fatalf("%q %v", name, err)
	}
	info, err := os.Stat(filepath.Join(dir, SavesDir, "a.zip"))
	if err != nil || !info.ModTime().Equal(old) {
		t.Fatalf("time changed: %v %v", info, err)
	}
}

func TestPrepareLoadRefusesWhenAnotherSaveIsInTheFuture(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "chosen.zip", "c", t0.Add(-time.Hour))
	putSave(t, dir, "future.zip", "f", t0.Add(time.Hour))
	if err := m.SetLoadSave(id, "chosen.zip"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrepareLoad(id); !errors.Is(err, ErrSaveInFuture) {
		t.Fatalf("err %v, want ErrSaveInFuture", err)
	}
}

func TestPrepareLoadChosenSaveGone(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "chosen.zip", "c", t0.Add(-time.Hour))
	if err := m.SetLoadSave(id, "chosen.zip"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, SavesDir, "chosen.zip")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PrepareLoad(id); !errors.Is(err, ErrSaveNotFound) {
		t.Fatalf("err %v, want ErrSaveNotFound", err)
	}
}

func TestClearLoadSaveAppliesOnce(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "a.zip", "a", t0)
	putSave(t, dir, "b.zip", "b", t0)
	if err := m.SetLoadSave(id, "a.zip"); err != nil {
		t.Fatal(err)
	}
	// A choice made after the start is kept.
	if err := m.SetLoadSave(id, "b.zip"); err != nil {
		t.Fatal(err)
	}
	if err := m.ClearLoadSave(id, "a.zip"); err != nil {
		t.Fatal(err)
	}
	list, _ := m.Saves(id)
	if list.LoadSave != "b.zip" {
		t.Fatalf("load save %q, want b.zip", list.LoadSave)
	}
	if err := m.ClearLoadSave(id, "b.zip"); err != nil {
		t.Fatal(err)
	}
	list, _ = m.Saves(id)
	if list.LoadSave != "" {
		t.Fatalf("load save %q, want newest", list.LoadSave)
	}
	b, err := os.ReadFile(filepath.Join(dir, MetaFile)) //nolint:gosec // a test temp file
	if err != nil || strings.Contains(string(b), "load_save") {
		t.Fatalf("world.json still names a save: %s %v", b, err)
	}
}

func TestHeldVariantsWorkUnderTheCallersLock(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "a.zip", "first", t0)
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	// The locking forms refuse while the lock is held.
	if _, err := m.TakeSnapshot(id, "x", "admin"); !errors.Is(err, ErrBusy) {
		t.Fatalf("TakeSnapshot err %v, want ErrBusy", err)
	}
	snap, err := m.TakeSnapshotHeld(id, "x", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(context.Background(), id, snap.Name, "admin"); !errors.Is(err, ErrBusy) {
		t.Fatalf("Restore err %v, want ErrBusy", err)
	}
	_ = os.Remove(filepath.Join(dir, SavesDir, "a.zip")) // the snapshot hardlinks it
	putSave(t, dir, "a.zip", "changed", t0)
	before, err := m.RestoreHeld(context.Background(), id, snap.Name, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if before.Reason != ReasonBeforeRestore {
		t.Fatalf("undo snapshot reason %q", before.Reason)
	}
	b, err := os.ReadFile(filepath.Join(dir, SavesDir, "a.zip")) //nolint:gosec // a test temp file
	if err != nil || string(b) != "first" {
		t.Fatalf("restored %q %v", b, err)
	}
}
