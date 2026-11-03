package world

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// seedWorld makes a world with a save, a mod, a config file and world.json,
// each with its own nanosecond modification time.
func seedWorld(t *testing.T, m *Manager) string {
	t.Helper()
	id, err := m.Create(t.Context(), "Base", "2.0.72", true)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.dirs.Worlds, id)
	files := map[string]string{
		"saves/base.zip":       "save one",
		"mods/flib_0.15.0.zip": "mod",
		"config/server.json":   `{"a":1}`,
	}
	for p, body := range files {
		writeAt(t, filepath.Join(dir, p), body, t0.Add(time.Duration(len(p))*time.Hour+123456789))
	}
	return id
}

func writeAt(t *testing.T, p, body string, mt time.Time) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func statT(t *testing.T, p string) *syscall.Stat_t {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t)
}

func TestSnapshotLinksZipsCopiesTheRestAndRecordsTimes(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	s, err := m.TakeSnapshot(id, "My label", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "20261103T142501Z" || s.Reason != ReasonManual || s.TakenBy != "alice" || s.Copied {
		t.Errorf("snapshot %+v", s)
	}
	world := filepath.Join(m.dirs.Worlds, id)
	snap := filepath.Join(m.dirs.Snapshots, id, s.Name)
	if statT(t, filepath.Join(world, "saves/base.zip")).Ino != statT(t, filepath.Join(snap, "saves/base.zip")).Ino {
		t.Error("save zip was not hard-linked")
	}
	if statT(t, filepath.Join(world, "config/server.json")).Ino == statT(t, filepath.Join(snap, "config/server.json")).Ino {
		t.Error("config file was linked, not copied")
	}
	if got := mode(t, filepath.Join(snap, "config/server.json")); got != FileMode {
		t.Errorf("copied file mode %v", got)
	}
	got, err := m.Snapshot(id, s.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 4 {
		t.Fatalf("manifest files %+v", got.Files)
	}
	for _, f := range got.Files {
		info, err := os.Stat(filepath.Join(world, f.Path))
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(f.ModTime) || info.Size() != f.Size {
			t.Errorf("%s: manifest %v %d, file %v %d", f.Path, f.ModTime, f.Size, info.ModTime(), info.Size())
		}
	}

	// A second snapshot in the same second gets -2 (R4).
	s2, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if s2.Name != "20261103T142501Z-2" {
		t.Errorf("second name %q", s2.Name)
	}
}

func TestFailedSnapshotLeavesNothingAndBlocksRestore(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads any file")
	}
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	good, err := m.TakeSnapshot(id, "good", "alice")
	if err != nil {
		t.Fatal(err)
	}
	// A file the snapshot cannot read makes it fail partway.
	bad := filepath.Join(m.dirs.Worlds, id, "config", "zz-unreadable.json")
	writeAt(t, bad, "x", t0)
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(m.dirs.Worlds, id, "saves/base.zip"), "changed", t0)

	if _, err := m.TakeSnapshot(id, "", "alice"); err == nil {
		t.Fatal("snapshot of an unreadable file succeeded")
	}
	if _, err := m.Restore(context.Background(), id, good.Name, "alice"); err == nil {
		t.Fatal("restore ran although its snapshot failed")
	}
	entries, err := os.ReadDir(filepath.Join(m.dirs.Snapshots, id))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != good.Name {
		t.Errorf("snapshot folder holds %v, want only %s", entries, good.Name)
	}
	worlds, _ := os.ReadDir(m.dirs.Worlds)
	if len(worlds) != 1 {
		t.Errorf("worlds folder holds %v", worlds)
	}
	b, _ := os.ReadFile(filepath.Join(m.dirs.Worlds, id, "saves/base.zip")) //nolint:gosec // a test temp file
	if string(b) != "changed" {
		t.Errorf("world was changed: %q", b)
	}
}

func TestSnapshotRefusedWhenDiskIsShort(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	free, err := FreeBytes(m.dirs.Snapshots)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(m.dirs.Worlds, id, "config", "big.dat"), os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // a test temp file
	if err != nil {
		t.Fatal(err)
	}
	// A sparse file larger than the free space costs nothing to make.
	if err := f.Truncate(int64(free) + 1<<30); err != nil { //nolint:gosec // test size
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := m.TakeSnapshot(id, "", "alice"); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("err %v, want ErrNoSpace", err)
	}
	entries, _ := os.ReadDir(filepath.Join(m.dirs.Snapshots, id))
	if len(entries) != 0 {
		t.Errorf("left %v", entries)
	}
}

// SPEC 6.8: when a hardlink cannot be made, the zip is copied and the
// snapshot records it. /dev/shm is a second filesystem on most Linux hosts.
func TestCopyFallbackAcrossFilesystems(t *testing.T) {
	shm, err := os.MkdirTemp("/dev/shm", "foreman-test-")
	if err != nil {
		t.Skip("no /dev/shm")
	}
	t.Cleanup(func() { _ = os.RemoveAll(shm) })
	data := t.TempDir()
	if statT(t, shm).Dev == statT(t, data).Dev {
		t.Skip("/dev/shm is on the same filesystem")
	}
	m, err := NewManager(Dirs{
		Worlds:    filepath.Join(data, "worlds"),
		Snapshots: filepath.Join(shm, "snapshots"),
		Imports:   filepath.Join(data, "import"),
	}, &fakeActive{}, func() time.Time { return t0 })
	if err != nil {
		t.Fatal(err)
	}
	id := seedWorld(t, m)
	s, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Copied {
		t.Error("copies not recorded")
	}
	got, _ := m.Snapshot(id, s.Name)
	if !got.Copied {
		t.Error("copies not in snapshot.json")
	}
	b, err := os.ReadFile(filepath.Join(shm, "snapshots", id, s.Name, "saves/base.zip")) //nolint:gosec // a test temp file
	if err != nil || string(b) != "save one" {
		t.Errorf("copied save %q, %v", b, err)
	}
}

func TestRetentionKeepsManualAndBeforeDelete(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	now := t0
	m.now = func() time.Time { now = now.Add(time.Minute); return now }

	if _, err := m.TakeSnapshot(id, "mine", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AutoSnapshot(id, ReasonBeforeDelete, LabelBeforeDelete, "alice"); err != nil {
		t.Fatal(err)
	}
	var autos []string
	for range KeepAutomatic + 3 {
		s, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "alice")
		if err != nil {
			t.Fatal(err)
		}
		autos = append(autos, s.Name)
	}
	list, err := m.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != KeepAutomatic+2 {
		t.Fatalf("%d snapshots, want %d", len(list), KeepAutomatic+2)
	}
	have := map[string]bool{}
	reasons := map[string]int{}
	for i, s := range list {
		have[s.Name] = true
		reasons[s.Reason]++
		if i > 0 && s.TakenAt.After(list[i-1].TakenAt) {
			t.Error("not newest first")
		}
	}
	if reasons[ReasonManual] != 1 || reasons[ReasonBeforeDelete] != 1 {
		t.Errorf("reasons %v", reasons)
	}
	for i, n := range autos {
		if want := i >= 3; have[n] != want {
			t.Errorf("automatic %d (%s) kept=%v, want %v", i, n, have[n], want)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(m.dirs.Snapshots, id))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("left %s", e.Name())
		}
	}
}

func TestDiskUseCountsEachInodeOnce(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	now := t0
	m.now = func() time.Time { now = now.Add(time.Minute); return now }
	for range 3 {
		if _, err := m.TakeSnapshot(id, "", "alice"); err != nil {
			t.Fatal(err)
		}
	}
	list, err := m.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	used, free, err := m.SnapshotDiskUse(id, list)
	if err != nil {
		t.Fatal(err)
	}
	// The zips are shared by all three; world.json and the config file are
	// copied into each.
	var zips, small uint64
	for _, f := range list[0].Files {
		if isZip(f.Path) {
			zips += uint64(f.Size) //nolint:gosec // test size
		} else {
			small += uint64(f.Size) //nolint:gosec // test size
		}
	}
	if want := zips + 3*small; used != want {
		t.Errorf("used %d, want %d", used, want)
	}
	if free == 0 {
		t.Error("free space is zero")
	}
}

func TestValidSnapshotName(t *testing.T) {
	for name, want := range map[string]bool{
		"20261103T142501Z":   true,
		"20261103T142501Z-2": true,
		"../x":               false,
		".partial-abc":       false,
		"20261103T142501Z/x": false,
		"":                   false,
	} {
		if got := ValidSnapshotName(name); got != want {
			t.Errorf("%q: %v", name, got)
		}
	}
}
