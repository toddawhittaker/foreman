package world

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// SPEC 14.5, 6.2: the game can write its world, so it can swap a file for a named pipe
// after the snapshot listed it. The snapshot must fail promptly, not hold
// the world's lock on a blocked open, and leave nothing half-written.
func TestSnapshotRefusesAFileSwappedForAPipe(t *testing.T) {
	for _, name := range []string{"saves/a.zip", "saves/a.txt", "config/rconpw", "mods/mod-list.json"} {
		t.Run(name, func(t *testing.T) {
			m, _ := newTestManager(t)
			id, dir := madeWorld(t, m)
			p := filepath.Join(dir, name)
			writeAt(t, p, "real", t0)

			w, err := m.OpenWorld(id)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = w.Close() }()
			// The listing saw a regular file; then it became a pipe.
			files, err := worldFiles(w)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(p, 0o640); err != nil {
				t.Fatal(err)
			}

			snaps, err := m.openSnapshots(id, true)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = snaps.Close() }()
			man := Manifest{Files: files}
			partial := PrefixPartial + "x"
			err = withTimeout(t, func() error { return m.fillSnapshot(w, snaps, partial, &man) })
			if err == nil {
				t.Fatal("a pipe was snapshotted as a file")
			}
			// snapshot() removes the partial folder on any error.
			if err := snaps.RemoveAll(partial); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Lstat(name); err != nil {
				t.Errorf("the world's file is gone: %v", err)
			}
		})
	}
}

// The public path: a pipe that is already there is refused at the listing,
// and no snapshot folder is left.
func TestTakeSnapshotWithAPipeLeavesNothing(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	if err := syscall.Mkfifo(filepath.Join(dir, SavesDir, "a.zip"), 0o640); err != nil {
		t.Fatal(err)
	}
	err := withTimeout(t, func() error { _, err := m.TakeSnapshot(id, "x", "tester"); return err })
	if err == nil {
		t.Fatal("a snapshot was taken with a pipe in the world")
	}
	list, err := m.Snapshots(id)
	if err != nil || len(list) != 0 {
		t.Errorf("snapshots %v, %v; want none", list, err)
	}
}

func TestCopyFileRefusesAPipeAndALink(t *testing.T) {
	root, dir := openTestWorld(t)
	dst, ddir := openTestWorld(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o640); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "real"), "x")
	if err := os.Symlink("real", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pipe", "link"} {
		err := withTimeout(t, func() error { return copyFile(root, name, dst, "out/"+name, FileMode) })
		if err == nil {
			t.Errorf("copyFile accepted %s", name)
		}
		if _, err := os.Lstat(filepath.Join(ddir, "out", name)); err == nil {
			t.Errorf("%s left a copy", name)
		}
	}
}

// SPEC 14.5, 6.2: the game can also swap the save's parent folder for a
// pipe after the listing. The link step must not wait on it.
func TestSnapshotRefusesAFolderSwappedForAPipe(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	writeAt(t, filepath.Join(dir, "saves", "a.zip"), "real", t0)

	w, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	files, err := worldFiles(w)
	if err != nil {
		t.Fatal(err)
	}
	saves := filepath.Join(dir, SavesDir)
	if err := os.Rename(saves, saves+".real"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(saves, 0o640); err != nil {
		t.Fatal(err)
	}

	snaps, err := m.openSnapshots(id, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snaps.Close() }()
	man := Manifest{Files: files}
	partial := PrefixPartial + "x"
	err = withTimeout(t, func() error { return m.fillSnapshot(w, snaps, partial, &man) })
	if err == nil {
		t.Fatal("a pipe was snapshotted as a folder")
	}
	if err := snaps.RemoveAll(partial); err != nil {
		t.Fatal(err)
	}
}
