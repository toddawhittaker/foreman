package world

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SPEC 5.5: a failed import leaves no world at all, not an empty one that
// may even have become active.
func TestImportThatFailsLeavesNoWorld(t *testing.T) {
	m, active := newTestManager(t)
	src := filepath.Join(m.dirs.Imports, "old")
	write(t, filepath.Join(src, "a.zip"), "save a", t0)
	write(t, filepath.Join(src, "mods", "m.zip"), "mod", t0)
	// An unreadable mod makes the copy fail partway.
	if err := os.Chmod(filepath.Join(src, "mods", "m.zip"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(src, "mods", "m.zip"), 0o600) })
	if _, err := m.ImportFolder(t.Context(), "old", "Old", "2.0.72", false); err == nil {
		t.Fatal("import worked")
	}
	if list, err := m.List(t.Context()); err != nil || len(list) != 0 {
		t.Errorf("worlds after a failed import: %+v %v", list, err)
	}
	if active.id != "" {
		t.Errorf("active world %q after a failed import", active.id)
	}
	noLeftovers(t, m)
}

// SPEC 5.5: an uploaded save becomes a new world in one step, and a
// refused one leaves no world and no temp file.
func TestImportSave(t *testing.T) {
	m, _ := newTestManager(t)
	temp := worldsTemp(t, m, "uploaded")
	id, err := m.ImportSave(t.Context(), "Uploaded", "1.1.110", false, "up.zip", temp)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(m.dirs.Worlds, id, "saves", "up.zip")); err != nil || string(b) != "uploaded" { //nolint:gosec // a test file
		t.Errorf("save not moved in: %q %v", b, err)
	}
	if meta, err := readMetaAt(m, id); err != nil || meta.Name != "Uploaded" || meta.GameVersion != "1.1.110" {
		t.Errorf("world.json %+v %v", meta, err)
	}
	for _, c := range []struct{ name, save string }{{"uploaded", "up.zip"}, {"Fresh", ".bad.zip"}} {
		temp := worldsTemp(t, m, "x")
		if _, err := m.ImportSave(t.Context(), c.name, "1.1.110", false, c.save, temp); err == nil {
			t.Errorf("ImportSave(%q, %q) worked", c.name, c.save)
		}
	}
	if list, _ := m.List(t.Context()); len(list) != 1 {
		t.Errorf("%d worlds, want 1", len(list))
	}
	noLeftovers(t, m)
}

// worldsTemp writes body to an upload temp file in the worlds folder, as
// the import upload does, and returns its name there.
func worldsTemp(t *testing.T, m *Manager, body string) string {
	t.Helper()
	root, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	f, temp, err := CreateTemp(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return temp
}

func readMetaAt(m *Manager, id string) (Meta, error) {
	w, err := m.OpenWorld(id)
	if err != nil {
		return Meta{}, err
	}
	defer func() { _ = w.Close() }()
	return ReadMeta(w)
}

// SPEC 14.8: a save that is a symlink to another file inside the world,
// such as the RCON password, is never served.
func TestOpenSaveRefusesASymlinkInsideTheWorld(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	write(t, filepath.Join(dir, "config", "rconpw"), "hunter2", t0)
	if err := os.Symlink("../config/rconpw", filepath.Join(dir, "saves", "x.zip")); err != nil {
		t.Fatal(err)
	}
	if f, _, err := m.OpenSave(id, "x.zip"); !errors.Is(err, ErrSaveNotFound) {
		if f != nil {
			_ = f.Close()
		}
		t.Fatalf("got %v, want ErrSaveNotFound", err)
	}
}

// A world's snapshots folder that is a symlink is refused, as is a
// snapshot folder that is one.
func TestSnapshotFolderSymlinkIsRefused(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(m.dirs.Snapshots, id, "20000101T000000Z")
	if err := os.Symlink(snap.Name, link); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Snapshot(id, "20000101T000000Z"); err == nil {
		t.Error("read a snapshot through a symlinked folder")
	}
	if _, err := m.Restore(t.Context(), id, "20000101T000000Z", "a"); err == nil {
		t.Error("restored from a symlinked snapshot folder")
	}
}

// AddSave tells the caller the world changed under the upload rather than
// failing with a bare "no such file".
func TestAddSaveAfterTheWorldWasSwapped(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	snap, err := m.TakeSnapshot(id, "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	temp := uploadTemp(t, m, id, "new save")
	if _, err := m.Restore(t.Context(), id, snap.Name, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := m.AddSave(id, "new.zip", temp); !errors.Is(err, ErrWorldChanged) {
		t.Errorf("after a restore: got %v, want ErrWorldChanged", err)
	}
	temp = uploadTemp(t, m, id, "new save")
	if err := os.RemoveAll(filepath.Join(m.dirs.Worlds, id)); err != nil {
		t.Fatal(err)
	}
	if err := m.AddSave(id, "new.zip", temp); !errors.Is(err, ErrWorldChanged) {
		t.Errorf("after a delete: got %v, want ErrWorldChanged", err)
	}
}

// SPEC 5.4: Get returns one world's row.
func TestGet(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	s, err := m.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != id || s.Name != "Base" || !s.Active || s.DiskBytes == 0 || s.SizeUnknown {
		t.Errorf("Get = %+v", s)
	}
	for _, bad := range []string{"nope", "..", ".staging-x"} {
		if _, err := m.Get(t.Context(), bad); err == nil {
			t.Errorf("Get(%q) worked", bad)
		}
	}
}

// SPEC 5.4: a world whose disk use cannot be measured is listed with its
// size unknown rather than breaking the list.
func TestListShowsSizeUnknown(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	lockDown(t, filepath.Join(m.dirs.Worlds, id, ConfigDir))
	if err := os.Chmod(filepath.Join(m.dirs.Worlds, id, ConfigDir), 0o000); err != nil {
		t.Fatal(err)
	}
	list, err := m.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || !list[0].SizeUnknown {
		t.Errorf("list = %+v, want size unknown", list)
	}
}

// R19: writes in flight share one count of promised bytes, so two that
// each fit cannot together eat into the reserve.
func TestReserveCountsWritesInFlight(t *testing.T) {
	dir := t.TempDir()
	free, err := FreeBytes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if free < SpaceReserve+2 {
		t.Skip("needs more free space than the reserve")
	}
	half := (free - SpaceReserve) / 2
	r1, err := Reserve(dir, half)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Reserve(dir, half)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Reserve(dir, half); !errors.Is(err, ErrNoSpace) {
		t.Errorf("third reserve: %v, want ErrNoSpace", err)
	}
	r1()
	r1() // releasing twice is harmless
	r3, err := Reserve(dir, half)
	if err != nil {
		t.Errorf("after a release: %v", err)
	} else {
		r3()
	}
	r2()
}

// R19: the reserve is a fixed 2 GB, whatever the disk's size.
func TestCheckSpaceReserveIsFixed(t *testing.T) {
	cases := []struct {
		free, inFlight, need uint64
		ok                   bool
	}{
		{2_000_000_000, 0, 0, true},
		{1_999_999_999, 0, 0, false},
		{4_000_000_000, 0, 2_000_000_000, true},
		{4_000_000_000, 1, 2_000_000_000, false},
		{1, 5, 0, false},
	}
	for _, c := range cases {
		err := checkSpace(c.free, c.inFlight, c.need)
		if (err == nil) != c.ok {
			t.Errorf("%+v: %v", c, err)
		}
		if err != nil && !errors.Is(err, ErrNoSpace) {
			t.Errorf("not ErrNoSpace: %v", err)
		}
	}
	err := checkSpace(3_999_999_999, 0, 2_000_000_000)
	if err == nil || !strings.Contains(err.Error(), "needs 4.0 GB, including 2.0 GB kept free") {
		t.Errorf("message should give the need with the reserve: %v", err)
	}
}
