package world

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
