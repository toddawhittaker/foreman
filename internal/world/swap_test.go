package world

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// SPEC 14.5: the worlds folder is checked on every open, not once at
// startup, so a link swapped in for it later is refused by every operation,
// and the link's target stays as it was.
func TestWorldsFolderSwappedForALinkAfterStartupIsRefused(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "a.zip", "save a", t0)
	snap, err := m.TakeSnapshot(id, "before", "tester")
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(m.dirs.Imports, "old", "b.zip"), "save b", t0)
	temp := worldsTemp(t, m, "uploaded")

	// Swap: the real worlds folder moves out of the way and a link to it
	// takes its place, so everything is reachable through the link.
	elsewhere := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Rename(m.dirs.Worlds, elsewhere); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, m.dirs.Worlds); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(elsewhere, 0o700); err != nil { //nolint:gosec // a test folder
		t.Fatal(err)
	}
	before := diskTree(t, elsewhere)

	ops := map[string]func() error{
		"List":         func() error { _, err := m.List(t.Context()); return err },
		"Get":          func() error { _, err := m.Get(t.Context(), id); return err },
		"OpenWorld":    func() error { _, err := m.OpenWorld(id); return err },
		"Create":       func() error { _, err := m.Create(t.Context(), "New", "2.0.72", false); return err },
		"Rename":       func() error { return m.Rename(t.Context(), id, "Renamed") },
		"Delete":       func() error { return m.Delete(t.Context(), id, "Family base", false, "tester") },
		"Restore":      func() error { _, err := m.Restore(t.Context(), id, snap.Name, "tester"); return err },
		"ImportFolder": func() error { _, err := m.ImportFolder(t.Context(), "old", "Old", "2.0.72", false); return err },
		"ImportSave": func() error {
			_, err := m.ImportSave(t.Context(), "Up", "2.0.72", false, "up.zip", temp)
			return err
		},
		"RCONPassword":  func() error { _, err := m.RCONPassword(id); return err },
		"Saves":         func() error { _, err := m.Saves(id); return err },
		"AddSave":       func() error { return m.AddSave(id, "c.zip", temp) },
		"TakeSnapshot":  func() error { _, err := m.TakeSnapshot(id, "again", "tester"); return err },
		"Recover":       func() error { return m.Recover(m.log) },
		"EnsureModList": func() error { return m.EnsureModList(id) },
	}
	for name, op := range ops {
		if err := op(); err == nil {
			t.Errorf("%s worked through a swapped link", name)
		}
	}
	sameDisk(t, "after the swap", diskTree(t, elsewhere), before)
	if got := mode(t, elsewhere); got != 0o700 {
		t.Errorf("the link's target mode is %o, want 700", got)
	}
}

// SPEC 14.5: the startup chmod goes through the opened folder, so a link
// there is refused and its target keeps its mode.
func TestChmodFolderRefusesALink(t *testing.T) {
	data, elsewhere := t.TempDir(), t.TempDir()
	if err := os.Chmod(elsewhere, 0o700); err != nil { //nolint:gosec // a test folder
		t.Fatal(err)
	}
	link := filepath.Join(data, "worlds")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	if err := chmodFolder(link, DirMode); !errors.Is(err, ErrNotFolder) {
		t.Fatalf("err %v, want ErrNotFolder", err)
	}
	if got := mode(t, elsewhere); got != 0o700 {
		t.Errorf("the link's target was chmodded to %o", got)
	}
}

// SPEC 14.8: a password file that is a named pipe must be refused at once,
// not waited on until something writes to it.
func TestReadRCONPasswordRefusesAPipe(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	p := filepath.Join(dir, RCONPasswordFile)
	if err := syscall.Mkfifo(p, 0o640); err != nil {
		t.Fatal(err)
	}
	root, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()

	done := make(chan error, 1)
	go func() {
		_, err := readRCONPassword(root)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a pipe was accepted as the password file")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a pipe as the password file hangs")
	}
}
