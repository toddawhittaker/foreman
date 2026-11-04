package world

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// The RCON password must stay unreadable to other users after a snapshot
// and after a restore (SPEC 14.8), not just when first created.
func TestRCONPasswordKeeps0640ThroughSnapshotAndRestore(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	pw, err := m.RCONPassword(id)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(m.dirs.Snapshots, id, snap.Name, RCONPasswordFile)
	if got := mode(t, copied); got != 0o640 {
		t.Errorf("snapshot copy has mode %v, want 0640", got)
	}

	if _, err := m.Restore(context.Background(), id, snap.Name, "bob"); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, filepath.Join(dir, RCONPasswordFile)); got != 0o640 {
		t.Errorf("restored world has mode %v, want 0640", got)
	}
	again, err := m.RCONPassword(id)
	if err != nil {
		t.Fatal(err)
	}
	if again.Reveal() != pw.Reveal() {
		t.Error("restore changed the password")
	}
}

// A snapshot made before the fix holds a 0664 copy; restoring it must
// still give the live world 0640.
func TestRestoreNarrowsAnOldWideRCONPasswordCopy(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	if _, err := m.RCONPassword(id); err != nil {
		t.Fatal(err)
	}
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(m.dirs.Snapshots, id, snap.Name, RCONPasswordFile)
	if err := os.Chmod(copied, 0o664); err != nil { //nolint:gosec // simulates an old, wider copy
		t.Fatal(err)
	}
	if _, err := m.Restore(context.Background(), id, snap.Name, "bob"); err != nil {
		t.Fatal(err)
	}
	if got := mode(t, filepath.Join(dir, RCONPasswordFile)); got != 0o640 {
		t.Errorf("restored world has mode %v, want 0640", got)
	}
}

// The file is linked into place from a finished temp file, so it is never
// empty and no temp file is left behind.
func TestRCONPasswordIsCompleteAndLeavesNoTemp(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	if _, err := m.RCONPassword(id); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ConfigDir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), PrefixPartial) {
			t.Errorf("temp file %s left behind", e.Name())
		}
	}
	info, err := os.Stat(filepath.Join(dir, RCONPasswordFile))
	if err != nil || info.Size() == 0 {
		t.Fatalf("password file %v, %v", info, err)
	}
}

// Two starts at once must agree on one password.
func TestRCONPasswordConcurrentCreatesAgree(t *testing.T) {
	m, _ := newTestManager(t)
	id, _ := madeWorld(t, m)
	const n = 8
	out := make(chan string, n)
	for range n {
		go func() {
			pw, err := m.RCONPassword(id)
			if err != nil {
				out <- "error: " + err.Error()
				return
			}
			out <- pw.Reveal()
		}()
	}
	first := <-out
	for range n - 1 {
		if got := <-out; got != first {
			t.Fatalf("passwords differ: %q vs %q", got, first)
		}
	}
}

// SPEC 14.8: the temp file a password is written into is 0640 from the
// moment it exists, so no other user can open it and read the password
// once it is written. umask 0 makes a wider creation mode show.
func TestPasswordTempIs0640BeforeAnythingIsWritten(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	f, name, err := createPasswordTemp(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	info, err := root.Lstat(name)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 || info.Size() != 0 {
		t.Errorf("new temp file: mode %v, %d bytes; want 0640 and empty", info.Mode().Perm(), info.Size())
	}
}

// SPEC 14.8: a live password file left wider than 0640 (a world restored
// before the restore narrowed it) is narrowed the next time it is read.
// A narrower one is left alone.
func TestRCONPasswordNarrowsAWideFile(t *testing.T) {
	for before, want := range map[os.FileMode]os.FileMode{0o664: 0o640, 0o644: 0o640, 0o606: 0o600, 0o600: 0o600, 0o640: 0o640} {
		t.Run(before.String(), func(t *testing.T) {
			m, _ := newTestManager(t)
			id, dir := madeWorld(t, m)
			pw, err := m.RCONPassword(id)
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(dir, RCONPasswordFile)
			if err := os.Chmod(p, before); err != nil {
				t.Fatal(err)
			}
			again, err := m.RCONPassword(id)
			if err != nil {
				t.Fatal(err)
			}
			if got := mode(t, p); got != want {
				t.Errorf("mode %v after reading, want %v", got, want)
			}
			if again.Reveal() != pw.Reveal() {
				t.Error("the password changed")
			}
		})
	}
}

// SPEC 14.8: password files copied before copies kept 0640 are narrowed at
// startup, in live worlds and in snapshots, and a symlink is not followed.
func TestRecoverNarrowsOldRCONPasswords(t *testing.T) {
	m, _ := newTestManager(t)
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(outside, 0o664); err != nil { // #nosec G302 -- the too-wide mode under test
		t.Fatal(err)
	}
	wide := []string{
		filepath.Join(m.dirs.Worlds, "base", RCONPasswordFile),
		filepath.Join(m.dirs.Snapshots, "base", "20261103T142501Z", RCONPasswordFile),
		filepath.Join(m.dirs.Snapshots, "base", "20261104T142501Z", RCONPasswordFile),
	}
	for _, p := range wide {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("pw"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0o664); err != nil { // #nosec G302 -- the too-wide mode under test
			t.Fatal(err)
		}
	}
	link := filepath.Join(m.dirs.Snapshots, "base", "20261105T142501Z", RCONPasswordFile)
	if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := m.Recover(slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	for _, p := range wide {
		if got := mode(t, p); got != 0o640 {
			t.Errorf("%s has mode %v, want 0640", p, got)
		}
	}
	if got := mode(t, outside); got != 0o664 {
		t.Errorf("the symlink was followed: target mode %v", got)
	}
}
