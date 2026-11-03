package world

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R12: restoring a live world to a snapshot whose name another world now
// has would give two worlds one name, so it is refused before anything is
// written, the "Before restore" snapshot included.
func TestRestoreLiveWorldRefusesANameAnotherWorldNowHas(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Rename(t.Context(), id, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(t.Context(), "BASE", "2.0.72", false); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(t.Context(), id, snap.Name, "bob"); !errors.Is(err, ErrRestoreNameTaken) {
		t.Fatalf("got %v, want ErrRestoreNameTaken", err)
	}
	if list, _ := m.Snapshots(id); len(list) != 1 {
		t.Errorf("%d snapshots, want 1: a refused restore took a snapshot", len(list))
	}
}

// The world's own name in the snapshot is not a clash.
func TestRestoreLiveWorldKeepsItsOwnName(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(t.Context(), id, snap.Name, "bob"); err != nil {
		t.Fatal(err)
	}
}

// lockDown makes dir unwritable until the test ends.
func lockDown(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // a test folder
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // a test folder
}

// SPEC 6.5: once the swap is done the restore has worked, even when
// retention afterwards fails; that failure is only a warning.
func TestRestoreSucceedsWhenRetentionFailsAfterTheSwap(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	var first Snapshot
	for i := range KeepAutomatic {
		s, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "a")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = s
		}
	}
	// The oldest automatic snapshot cannot be emptied, so removing it fails.
	lockDown(t, filepath.Join(m.dirs.Snapshots, id, first.Name, SavesDir))
	t.Cleanup(func() { unlockAll(t, filepath.Join(m.dirs.Snapshots, id)) })
	list, _ := m.Snapshots(id)
	before, err := m.Restore(t.Context(), id, list[0].Name, "bob")
	if err != nil {
		t.Fatalf("restore after a good swap reported %v", err)
	}
	if before.Name == "" {
		t.Error("no before-restore snapshot returned")
	}
}

// unlockAll makes every folder under dir writable again so the test's temp
// folder can be removed.
func unlockAll(t *testing.T, dir string) {
	t.Helper()
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o755) //nolint:gosec // a test folder
		}
		return nil
	})
}

// SPEC 6.2: a restore that fails after its "Before restore" snapshot still
// returns that snapshot, so the admin can be told an undo exists.
func TestFailedRestoreStillReturnsTheBeforeSnapshot(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	// Staging is built in the worlds folder; make that impossible.
	lockDown(t, m.dirs.Worlds)
	before, err := m.Restore(t.Context(), id, snap.Name, "bob")
	if err == nil {
		t.Fatal("restore worked")
	}
	if before.Name == "" || before.Reason != ReasonBeforeRestore {
		t.Errorf("before = %+v, want the before-restore snapshot", before)
	}
}

// SPEC 6.4: one snapshot folder whose snapshot.json is unreadable does not
// hide the world's other snapshots.
func TestSnapshotsSkipsAnUnreadableSnapshot(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	good, err := m.TakeSnapshot(id, "good", "alice")
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(m.dirs.Snapshots, id, "20000101T000000Z")
	if err := os.MkdirAll(broken, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, ManifestFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := m.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != good.Name {
		t.Errorf("list = %+v, want only %s", list, good.Name)
	}
	// Taking another snapshot (which runs retention) still works.
	if _, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "a"); err != nil {
		t.Errorf("snapshot with a broken neighbour: %v", err)
	}
	if _, err := m.Deleted(); err != nil {
		t.Errorf("Deleted: %v", err)
	}
}

// SPEC 6.6: a retention failure after a good snapshot is a warning; the
// snapshot was taken and the risky operation may go on.
func TestAutoSnapshotSucceedsWhenRetentionFails(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	var first Snapshot
	for i := range KeepAutomatic {
		s, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "a")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = s
		}
	}
	lockDown(t, filepath.Join(m.dirs.Snapshots, id, first.Name, SavesDir))
	t.Cleanup(func() { unlockAll(t, filepath.Join(m.dirs.Snapshots, id)) })
	s, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "a")
	if err != nil || s.Name == "" {
		t.Fatalf("AutoSnapshot = %+v, %v; want the snapshot and no error", s, err)
	}
}

// R20: a save hard-linked into a snapshot and rewritten in place changes
// the snapshot too. Restore notices by the recorded hash and refuses,
// naming the file, before it writes anything.
func TestRestoreRefusesASnapshotRewrittenInPlace(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range snap.Files {
		if strings.HasSuffix(f.Path, ".zip") && len(f.SHA256) != 64 {
			t.Errorf("%s has hash %q", f.Path, f.SHA256)
		}
	}
	save := filepath.Join(m.dirs.Worlds, id, "saves", "base.zip")
	info, err := os.Stat(save)
	if err != nil {
		t.Fatal(err)
	}
	// Same size, same time, new bytes, same inode.
	f, err := os.OpenFile(save, os.O_WRONLY, 0) //nolint:gosec // a test file
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("SAVE ONE"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := os.Chtimes(save, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	world := diskTree(t, filepath.Join(m.dirs.Worlds, id))
	_, err = m.Restore(t.Context(), id, snap.Name, "bob")
	if !errors.Is(err, ErrDamaged) || !strings.Contains(err.Error(), "saves/base.zip") {
		t.Fatalf("got %v, want ErrDamaged naming saves/base.zip", err)
	}
	sameDisk(t, "world", diskTree(t, filepath.Join(m.dirs.Worlds, id)), world)
	if list, _ := m.Snapshots(id); len(list) != 1 {
		t.Errorf("%d snapshots, want 1", len(list))
	}
}

// R20: a hash is reused when the file's inode, size and time are
// unchanged, and read afresh when they are not.
func TestSnapshotHashReuse(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	first, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	hash := func(s Snapshot, p string) string {
		for _, f := range s.Files {
			if f.Path == p {
				return f.SHA256
			}
		}
		return ""
	}
	// A planted wrong hash in the previous manifest is reused only when
	// nothing about the file changed, which proves the file was not read.
	man := filepath.Join(m.dirs.Snapshots, id, first.Name, ManifestFile)
	b, err := os.ReadFile(man) //nolint:gosec // a test file
	if err != nil {
		t.Fatal(err)
	}
	fake := strings.Repeat("0", 64)
	b = []byte(strings.Replace(string(b), hash(first, "saves/base.zip"), fake, 1))
	if err := os.WriteFile(man, b, 0o600); err != nil { //nolint:gosec // a test file
		t.Fatal(err)
	}
	second, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got := hash(second, "saves/base.zip"); got != fake {
		t.Errorf("hash %s, want the reused one", got)
	}
	writeAt(t, filepath.Join(m.dirs.Worlds, id, "saves", "base.zip"), "save two", t0)
	third, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got := hash(third, "saves/base.zip"); got == fake || got == hash(first, "saves/base.zip") || len(got) != 64 {
		t.Errorf("hash %s after a change", got)
	}
}

// R20: a snapshot from before hashes were recorded still restores.
func TestRestoreSnapshotWithoutHashes(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	man := filepath.Join(m.dirs.Snapshots, id, snap.Name, ManifestFile)
	b, err := os.ReadFile(man) //nolint:gosec // a test file
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range snap.Files {
		if f.SHA256 != "" {
			b = []byte(strings.Replace(string(b), f.SHA256, "", 1))
		}
	}
	if err := os.WriteFile(man, b, 0o600); err != nil { //nolint:gosec // a test file
		t.Fatal(err)
	}
	if _, err := m.Restore(t.Context(), id, snap.Name, "bob"); err != nil {
		t.Fatal(err)
	}
}

// Hidden files are part of a world: a snapshot keeps them, so restore and
// delete do not lose them. Foreman's own temp files are left out.
func TestSnapshotKeepsHiddenFiles(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	w := filepath.Join(m.dirs.Worlds, id)
	write(t, filepath.Join(w, "config", ".hidden"), "keep me", t0)
	write(t, filepath.Join(w, ".dotdir", "x"), "keep me too", t0)
	write(t, filepath.Join(w, "saves", PrefixPartial+"0123456789abcdef"), "upload", t0)
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range snap.Files {
		paths = append(paths, f.Path)
	}
	got := strings.Join(paths, " ")
	for _, want := range []string{"config/.hidden", ".dotdir/x"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot lacks %s: %s", want, got)
		}
	}
	if strings.Contains(got, PrefixPartial) {
		t.Errorf("snapshot holds a temp file: %s", got)
	}
	if err := os.Remove(filepath.Join(w, "config", ".hidden")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore(t.Context(), id, snap.Name, "bob"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(w, "config", ".hidden")); err != nil || string(b) != "keep me" { //nolint:gosec // a test file
		t.Errorf("hidden file after restore: %q %v", b, err)
	}
}

// Fail safe: a symlink in a world would be lost by a snapshot, so the
// snapshot refuses, naming it, and nothing is written.
func TestSnapshotRefusesASymlinkInTheWorld(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	if err := os.Symlink("server.json", filepath.Join(m.dirs.Worlds, id, "config", "link.json")); err != nil {
		t.Fatal(err)
	}
	_, err := m.TakeSnapshot(id, "", "alice")
	if err == nil || !strings.Contains(err.Error(), "config/link.json") {
		t.Fatalf("got %v, want a refusal naming config/link.json", err)
	}
	if dirs := snapshotDirs(t, m, id); len(dirs) != 0 {
		t.Errorf("left %v", dirs)
	}
}
