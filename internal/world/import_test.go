package world

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

func write(t *testing.T, p, body string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// tree lists every path under dir with its mode, size and modification time.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, fmt.Sprintf("%s %v %d %s", rel, info.Mode(), info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func linkCount(t *testing.T, p string) uint64 {
	t.Helper()
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Sys().(*syscall.Stat_t).Nlink
}

func assertNoStaging(t *testing.T, m *Manager) {
	t.Helper()
	entries, _ := os.ReadDir(m.dirs.Worlds)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("left behind %s", e.Name())
		}
	}
}

func seedInbox(t *testing.T, m *Manager) string {
	t.Helper()
	src := filepath.Join(m.dirs.Imports, "saves-2025")
	old := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	write(t, filepath.Join(src, "base.zip"), "save-one", old)
	write(t, filepath.Join(src, "later.zip"), "save-two!", old.Add(time.Hour))
	write(t, filepath.Join(src, "notes.txt"), "not a save", old)
	write(t, filepath.Join(src, ".hidden.zip"), "x", old)
	write(t, filepath.Join(src, "old", "nested.zip"), "nested", old)
	write(t, filepath.Join(src, "mods", "mod-list.json"), "{}", old)
	write(t, filepath.Join(src, "mods", "sub", "a.zip"), "mod", old)
	outside := filepath.Join(t.TempDir(), "secret.zip")
	write(t, outside, "secret", old)
	if err := os.Symlink(outside, filepath.Join(src, "link.zip")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "mods", "link.zip")); err != nil {
		t.Fatal(err)
	}
	return src
}

func TestImportFolderCopiesSavesAndModsAndLeavesSourceAlone(t *testing.T) {
	m, active := newTestManager(t)
	src := seedInbox(t, m)
	before := tree(t, src)

	id, err := m.ImportFolder(context.Background(), "saves-2025", "Old game", "2.0.72", true)
	if err != nil {
		t.Fatal(err)
	}
	if active.id != id {
		t.Errorf("first world not active: %q", active.id)
	}
	if after := tree(t, src); !slices.Equal(before, after) {
		t.Errorf("source changed:\nbefore %v\nafter  %v", before, after)
	}

	w := filepath.Join(m.dirs.Worlds, id)
	for p, body := range map[string]string{
		"saves/base.zip":     "save-one",
		"saves/later.zip":    "save-two!",
		"mods/mod-list.json": "{}",
		"mods/sub/a.zip":     "mod",
	} {
		got, err := os.ReadFile(filepath.Join(w, p)) //nolint:gosec // a test temp file
		if err != nil || string(got) != body {
			t.Errorf("%s: %q, %v", p, got, err)
			continue
		}
		if mode(t, filepath.Join(w, p)) != FileMode {
			t.Errorf("%s mode %v", p, mode(t, filepath.Join(w, p)))
		}
		// A copy, never a hardlink.
		if n := linkCount(t, filepath.Join(w, p)); n != 1 {
			t.Errorf("%s has %d links", p, n)
		}
	}
	if mode(t, filepath.Join(w, "mods", "sub")) != DirMode {
		t.Errorf("mods/sub mode %v", mode(t, filepath.Join(w, "mods", "sub")))
	}
	for _, p := range []string{"saves/notes.txt", "saves/.hidden.zip", "saves/link.zip", "mods/link.zip", "saves/nested.zip", "saves/old"} {
		if _, err := os.Lstat(filepath.Join(w, p)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s should not be copied: %v", p, err)
		}
	}
	info, _ := os.Stat(filepath.Join(w, "saves", "later.zip"))
	if want := time.Date(2025, 6, 1, 13, 0, 0, 0, time.UTC); !info.ModTime().Equal(want) {
		t.Errorf("modification time not kept: %v", info.ModTime())
	}
	list, _ := m.List(context.Background())
	if len(list) != 1 || list[0].Name != "Old game" || list[0].GameVersion != "2.0.72" || !list[0].SpaceAge {
		t.Errorf("list %+v", list)
	}
	assertNoStaging(t, m)
}

func TestImportFolderRefusals(t *testing.T) {
	m, _ := newTestManager(t)
	seedInbox(t, m)
	write(t, filepath.Join(m.dirs.Imports, "empty", "readme.txt"), "x", t0)
	write(t, filepath.Join(m.dirs.Imports, ".hidden", "a.zip"), "x", t0)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "a.zip"), "x", t0)
	if err := os.Symlink(outside, filepath.Join(m.dirs.Imports, "escape")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := m.Create(ctx, "Taken", "2.0.72", false); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		folder, name, version string
		want                  error
	}{
		{"saves-2025", "Good", "2.0", ErrBadGameVersion},
		{"saves-2025", "Good", "2.2.0", ErrBadGameVersion},
		{"saves-2025", "Good", "0.17.79", ErrBadGameVersion},
		{"saves-2025", "Good", "3.0.1", ErrBadGameVersion},
		{"saves-2025", "  ", "2.0.72", ErrNameEmpty},
		{"saves-2025", "TAKEN", "2.0.72", ErrNameTaken},
		{"empty", "Good", "2.0.72", ErrNoSaves},
		{"missing", "Good", "2.0.72", ErrNoFolder},
		{"../import/saves-2025", "Good", "2.0.72", ErrNoFolder},
		{"escape", "Good", "2.0.72", ErrNoFolder},
		{".hidden", "Good", "2.0.72", ErrNoFolder},
	} {
		if _, err := m.ImportFolder(ctx, c.folder, c.name, c.version, false); !errors.Is(err, c.want) {
			t.Errorf("%q %q %q: got %v, want %v", c.folder, c.name, c.version, err, c.want)
		}
	}
	if list, _ := m.List(ctx); len(list) != 1 {
		t.Errorf("refusals made worlds: %+v", list)
	}
	assertNoStaging(t, m)
}

func TestImportFolderRefusesWhenDiskTooFull(t *testing.T) {
	m, _ := newTestManager(t)
	src := filepath.Join(m.dirs.Imports, "huge")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(src, "huge.zip")) //nolint:gosec // a test temp file
	if err != nil {
		t.Fatal(err)
	}
	// Sparse: a stated size far beyond any test disk, using no space.
	if err := f.Truncate(1 << 60); err != nil {
		t.Skip("filesystem cannot hold a sparse file:", err)
	}
	_ = f.Close()
	if _, err := m.ImportFolder(context.Background(), "huge", "Huge", "2.0.72", false); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("got %v, want ErrNoSpace", err)
	}
	assertNoStaging(t, m)
}

func TestInboxListsRealFoldersOnly(t *testing.T) {
	m, _ := newTestManager(t)
	seedInbox(t, m)
	write(t, filepath.Join(m.dirs.Imports, ".partial", "a.zip"), "x", t0)
	write(t, filepath.Join(m.dirs.Imports, "loose.zip"), "x", t0)
	if err := os.Symlink(t.TempDir(), filepath.Join(m.dirs.Imports, "escape")); err != nil {
		t.Fatal(err)
	}
	got, err := m.Inbox()
	if err != nil {
		t.Fatal(err)
	}
	want := []InboxFolder{{Name: "saves-2025", Saves: 2, HasMods: true, Bytes: 8 + 9 + 2 + 3}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestBadGameVersionNamesTheSupportedLines(t *testing.T) {
	for _, line := range []string{"1.1", "2.0", "2.1"} {
		if !strings.Contains(ErrBadGameVersion.Error(), line) {
			t.Errorf("%q does not name line %s", ErrBadGameVersion, line)
		}
	}
}

func TestImportRefusesAnUnsupportedLineBeforeAnyWork(t *testing.T) {
	m, _ := newTestManager(t)
	seedInbox(t, m)
	if _, err := m.ImportFolder(context.Background(), "saves-2025", "New line", "2.2.1", false); !errors.Is(err, ErrBadGameVersion) {
		t.Fatalf("folder import: got %v", err)
	}
	if _, err := m.ImportSave(context.Background(), "New line", "2.2.1", false, "a.zip", PrefixPartial+"x"); !errors.Is(err, ErrBadGameVersion) {
		t.Fatalf("upload import: got %v", err)
	}
	if list, _ := m.List(context.Background()); len(list) != 0 {
		t.Errorf("refusal made a world: %+v", list)
	}
	assertNoStaging(t, m)
}

func TestValidNames(t *testing.T) {
	for v, ok := range map[string]bool{"2.0.72": true, "1.1.110": true, "2.0": false, "v2.0.72": false, "2.0.72 ": false, "": false} {
		if ValidGameVersion(v) != ok {
			t.Errorf("ValidGameVersion(%q) != %v", v, ok)
		}
	}
	for n, ok := range map[string]bool{"a.zip": true, ".a.zip": false, ".zip": false, "a/b.zip": false, `a\b.zip`: false, "a.txt": false, "..": false} {
		if ValidSaveName(n) != ok {
			t.Errorf("ValidSaveName(%q) != %v", n, ok)
		}
	}
}

func TestRename(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	id, _ := m.Create(ctx, "First", "2.0.72", false)
	if _, err := m.Create(ctx, "Second", "2.0.72", false); err != nil {
		t.Fatal(err)
	}
	if err := m.Rename(ctx, id, "  New name "); err != nil {
		t.Fatal(err)
	}
	// Changing only the case of its own name is allowed.
	if err := m.Rename(ctx, id, "NEW NAME"); err != nil {
		t.Fatal(err)
	}
	list, _ := m.List(ctx)
	if list[0].ID != id || list[0].Name != "NEW NAME" {
		t.Errorf("id or name wrong: %+v", list[0])
	}
	if err := m.Rename(ctx, id, "second"); !errors.Is(err, ErrNameTaken) {
		t.Errorf("duplicate: %v", err)
	}
	if err := m.Rename(ctx, id, " "); !errors.Is(err, ErrNameEmpty) {
		t.Errorf("blank: %v", err)
	}
	unlock, _ := m.Lock(id)
	if err := m.Rename(ctx, id, "Busy"); !errors.Is(err, ErrBusy) {
		t.Errorf("locked: %v", err)
	}
	unlock()
	if err := m.Rename(ctx, "missing", "X"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}

	// A world a newer Foreman wrote is not changed.
	w := filepath.Join(m.dirs.Worlds, id, MetaFile)
	if err := os.WriteFile(w, []byte(`{"format_version":9,"name":"Future","created_at":"2026-11-01T10:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Rename(ctx, id, "Changed"); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("newer: %v", err)
	}
	b, _ := os.ReadFile(w) //nolint:gosec // a test temp file
	if !strings.Contains(string(b), `"Future"`) {
		t.Errorf("newer world.json changed: %s", b)
	}
}
