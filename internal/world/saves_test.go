package world

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// madeWorld makes a world and returns its id and folder.
func madeWorld(t *testing.T, m *Manager) (id, dir string) {
	t.Helper()
	id, err := m.Create(context.Background(), "Family base", "2.0.72", true)
	if err != nil {
		t.Fatal(err)
	}
	return id, filepath.Join(m.Dirs().Worlds, id)
}

func putSave(t *testing.T, dir, name, content string, at time.Time) {
	t.Helper()
	p := filepath.Join(dir, SavesDir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

func saveNames(saves []Save) []string {
	var out []string
	for _, s := range saves {
		out = append(out, s.Name)
	}
	return out
}

func TestValidSaveName(t *testing.T) {
	good := []string{"a.zip", "My Save (2).zip", "_autosave1.zip", "ünï.zip", "a..zip"}
	bad := []string{"", ".zip", ".hidden.zip", "../x.zip", "a/b.zip", `a\b.zip`, "/abs.zip", "x.ZIP", "x.zip.txt",
		"x", "..", "a\x00b.zip", "a\nb.zip", strings.Repeat("a", 252) + ".zip"}
	for _, n := range good {
		if !ValidSaveName(n) {
			t.Errorf("%q refused", n)
		}
	}
	for _, n := range bad {
		if ValidSaveName(n) {
			t.Errorf("%q allowed", n)
		}
	}
}

func TestSavesListNewestFirst(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)

	list, err := m.Saves(id)
	if err != nil || len(list.Saves) != 0 || list.WorldName != "Family base" {
		t.Fatalf("empty world: %+v, %v", list, err)
	}

	putSave(t, dir, "old.zip", "1", t0.Add(-2*time.Hour))
	putSave(t, dir, "new.zip", "twelve bytes", t0)
	putSave(t, dir, "mid.zip", "22", t0.Add(-time.Hour))
	putSave(t, dir, "also-mid.zip", "3", t0.Add(-time.Hour))
	// Not saves: hidden, wrong extension, a folder, a link, a half-written upload.
	putSave(t, dir, ".hidden.zip", "x", t0.Add(time.Hour))
	putSave(t, dir, "notes.txt", "x", t0.Add(time.Hour))
	putSave(t, dir, PrefixPartial+"abc", "x", t0.Add(time.Hour))
	if err := os.Mkdir(filepath.Join(dir, SavesDir, "dir.zip"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("new.zip", filepath.Join(dir, SavesDir, "link.zip")); err != nil {
		t.Fatal(err)
	}

	list, err = m.Saves(id)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new.zip", "also-mid.zip", "mid.zip", "old.zip"}
	if got := saveNames(list.Saves); !slices.Equal(got, want) {
		t.Errorf("saves %v, want %v", got, want)
	}
	if s := list.Saves[0]; s.Size != 12 || !s.ModTime.Equal(t0) {
		t.Errorf("newest: %+v", s)
	}
}

func TestSavesNeedsAWorld(t *testing.T) {
	m, _ := newTestManager(t)
	for _, id := range []string{"missing", "../x", ""} {
		if _, err := m.Saves(id); err == nil {
			t.Errorf("Saves(%q) succeeded", id)
		}
	}
}

func TestNewestSave(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	if _, found, err := m.NewestSave(id); err != nil || found {
		t.Fatalf("no saves: found %v, err %v", found, err)
	}
	putSave(t, dir, "a.zip", "1", t0.Add(-time.Hour))
	putSave(t, dir, "b.zip", "22", t0)
	putSave(t, dir, ".c.zip", "333", t0.Add(time.Hour))
	s, found, err := m.NewestSave(id)
	if err != nil || !found || s.Name != "b.zip" {
		t.Fatalf("newest: %+v, %v, %v", s, found, err)
	}
	// An explicit choice does not change which save is newest.
	if err := m.SetLoadSave(id, "a.zip"); err != nil {
		t.Fatal(err)
	}
	if s, _, _ := m.NewestSave(id); s.Name != "b.zip" {
		t.Errorf("newest after choice: %s", s.Name)
	}
	if _, _, err := m.NewestSave("missing"); err == nil {
		t.Error("NewestSave in a missing world succeeded")
	}
}

func TestOpenSave(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "a.zip", "hello", t0)

	f, info, err := m.OpenSave(id, "a.zip")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(f)
	_ = f.Close()
	if string(b) != "hello" || info.Size() != 5 {
		t.Errorf("read %q, size %d", b, info.Size())
	}

	// A file outside saves/, and a link out of the world, are not reachable.
	if err := os.WriteFile(filepath.Join(dir, "world-secret.zip"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.zip")
	if err := os.WriteFile(outside, []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, SavesDir, "escape.zip")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, SavesDir, "folder.zip"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing.zip", "../world-secret.zip", "a/../a.zip", "escape.zip", "folder.zip", ".a.zip", "world.json"} {
		if f, _, err := m.OpenSave(id, name); err == nil {
			_ = f.Close()
			t.Errorf("OpenSave(%q) succeeded", name)
		}
	}
	if _, _, err := m.OpenSave("missing", "a.zip"); err == nil {
		t.Error("OpenSave in a missing world succeeded")
	}
}

// uploadTemp puts a partial file in the saves folder as the upload helper does.
func uploadTemp(t *testing.T, m *Manager, id, content string) string {
	t.Helper()
	root, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	f, temp, err := CreateTemp(root, SavesDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return temp
}

func assertSavesFolder(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, SavesDir))
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if want == nil {
		want = []string{}
	}
	if !slices.Equal(got, want) {
		t.Errorf("saves folder holds %v, want %v", got, want)
	}
}

func TestAddSave(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)

	temp := uploadTemp(t, m, id, "new data")
	if err := m.AddSave(id, "up.zip", temp); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, SavesDir, "up.zip")) //nolint:gosec // a test temp file
	if err != nil || string(b) != "new data" {
		t.Fatalf("saved %q, %v", b, err)
	}
	if mode(t, filepath.Join(dir, SavesDir, "up.zip")) != FileMode {
		t.Error("wrong file mode")
	}
	assertSavesFolder(t, dir, "up.zip")

	// A name that exists is refused, the old save is untouched, and the
	// temp file is gone.
	temp = uploadTemp(t, m, id, "replacement")
	if err := m.AddSave(id, "up.zip", temp); !errors.Is(err, ErrSaveExists) {
		t.Errorf("duplicate: %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(dir, SavesDir, "up.zip")) //nolint:gosec // a test temp file
	if string(b) != "new data" {
		t.Errorf("save overwritten: %q", b)
	}
	assertSavesFolder(t, dir, "up.zip")
}

func TestAddSaveRefusesBadNames(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	for _, name := range []string{"../x.zip", "a/b.zip", `a\b.zip`, ".hidden.zip", ".zip", "x.txt", "", "x.ZIP"} {
		temp := uploadTemp(t, m, id, "data")
		if err := m.AddSave(id, name, temp); !errors.Is(err, ErrSaveName) {
			t.Errorf("AddSave(%q): %v", name, err)
		}
	}
	assertSavesFolder(t, dir) // nothing written, no temp left
	if _, err := os.Lstat(filepath.Join(filepath.Dir(dir), "x.zip")); err == nil {
		t.Error("a file escaped the world")
	}
	if err := m.AddSave("missing", "a.zip", "saves/.partial-x"); err == nil {
		t.Error("AddSave in a missing world succeeded")
	}
}

func rewriteMeta(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, MetaFile), []byte(content), 0o600); err != nil { //nolint:gosec // a test temp file
		t.Fatal(err)
	}
}

func TestAddSaveRefusals(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)

	// Busy: another operation holds the world past the wait.
	m.addSaveWait = 20 * time.Millisecond
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.AddSave(id, "a.zip", uploadTemp(t, m, id, "x")); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
	if err := m.SetLoadSave(id, ""); !errors.Is(err, ErrBusy) {
		t.Errorf("busy load: %v", err)
	}
	unlock()
	assertSavesFolder(t, dir)

	// A world a newer Foreman wrote cannot be changed, but its saves list.
	rewriteMeta(t, dir, `{"format_version": 9, "name": "Future"}`)
	if err := m.AddSave(id, "a.zip", uploadTemp(t, m, id, "x")); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("newer: %v", err)
	}
	assertSavesFolder(t, dir)
	putSave(t, dir, "s.zip", "x", t0)
	list, err := m.Saves(id)
	if err != nil || !list.Newer || len(list.Saves) != 1 {
		t.Errorf("newer list: %+v, %v", list, err)
	}

	// A world.json that cannot be read also stops the change.
	rewriteMeta(t, dir, "{not json")
	if err := m.AddSave(id, "b.zip", uploadTemp(t, m, id, "x")); err == nil {
		t.Error("AddSave with a broken world.json succeeded")
	}
}

func TestSetLoadSave(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "a.zip", "1", t0.Add(-time.Hour))
	putSave(t, dir, "b.zip", "2", t0)
	old, err := os.ReadFile(filepath.Join(dir, MetaFile)) //nolint:gosec // a test temp file
	if err != nil {
		t.Fatal(err)
	}
	rewriteMeta(t, dir, strings.TrimRight(string(old), "\n }")+`, "future_field": {"x": [1, 2]}}`)

	if err := m.SetLoadSave(id, "a.zip"); err != nil {
		t.Fatal(err)
	}
	list, _ := m.Saves(id)
	if list.LoadSave != "a.zip" {
		t.Errorf("load save %q", list.LoadSave)
	}
	// Other fields, known and unknown, survive the rewrite (SPEC 5.1).
	raw, err := os.ReadFile(filepath.Join(dir, MetaFile)) //nolint:gosec // a test temp file
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"future_field"`, `"Family base"`, `"2.0.72"`, `"space_age": true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s lost from world.json: %s", want, raw)
		}
	}

	// Empty goes back to the newest.
	if err := m.SetLoadSave(id, ""); err != nil {
		t.Fatal(err)
	}
	if list, _ = m.Saves(id); list.LoadSave != "" {
		t.Errorf("load save %q after reset", list.LoadSave)
	}

	// A name with no save is refused and changes nothing.
	if err := m.SetLoadSave(id, "a.zip"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, SavesDir, "d.zip"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"nope.zip", "../a.zip", "a/b.zip", ".a.zip", "saves", "world.json", "d.zip"} {
		if err := m.SetLoadSave(id, name); !errors.Is(err, ErrSaveNotFound) {
			t.Errorf("SetLoadSave(%q): %v", name, err)
		}
	}
	if list, _ = m.Saves(id); list.LoadSave != "a.zip" {
		t.Errorf("load save changed to %q", list.LoadSave)
	}

	// Newer format: refused, world.json untouched.
	const future = `{"format_version": 9, "name": "F"}`
	rewriteMeta(t, dir, future)
	if err := m.SetLoadSave(id, "a.zip"); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("newer: %v", err)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, MetaFile)); string(raw) != future { //nolint:gosec // a test temp file
		t.Errorf("newer world.json rewritten: %s", raw)
	}
	if err := m.SetLoadSave("missing", ""); err == nil {
		t.Error("SetLoadSave in a missing world succeeded")
	}
}

func TestListLastPlayedIsNewestSave(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "a.zip", "1", t0.Add(-time.Hour))
	putSave(t, dir, "b.zip", "2", t0)
	putSave(t, dir, ".z.zip", "3", t0.Add(time.Hour))
	list, err := m.List(context.Background())
	if err != nil || len(list) != 1 || list[0].ID != id || !list[0].LastPlayed.Equal(t0) {
		t.Fatalf("list: %+v, %v", list, err)
	}
}

// SPEC 7.11: LoadChoice reads the chosen save from world.json alone, and is
// empty when none is chosen or the world is not there.
func TestLoadChoice(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	putSave(t, dir, "a.zip", "1", t0.Add(-time.Hour))
	putSave(t, dir, "b.zip", "2", t0)
	if got := m.LoadChoice(id); got != "" {
		t.Errorf("no choice: %q", got)
	}
	if err := m.SetLoadSave(id, "a.zip"); err != nil {
		t.Fatal(err)
	}
	if got := m.LoadChoice(id); got != "a.zip" {
		t.Errorf("chosen: %q", got)
	}
	if got := m.LoadChoice("nope"); got != "" {
		t.Errorf("unknown world: %q", got)
	}
}
