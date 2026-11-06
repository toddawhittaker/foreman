package world

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cloneSource makes a world with mods, every config file, an rconpw and two
// saves, the newer one called b.zip.
func cloneSource(t *testing.T) (*Manager, string, string) {
	t.Helper()
	m, _ := newTestManager(t)
	id, err := m.Create(context.Background(), "Source", "2.0.77", true)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.Dirs().Worlds, id)
	old := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	write(t, filepath.Join(dir, SavesDir, "a.zip"), "save-a", old)
	write(t, filepath.Join(dir, SavesDir, "b.zip"), "save-b", old.Add(time.Hour))
	write(t, filepath.Join(dir, ModsDir, "cool_1.0.0.zip"), "mod", old)
	write(t, filepath.Join(dir, ModsDir, ModListFile), `{"mods":[{"name":"base","enabled":true}]}`, old)
	write(t, filepath.Join(dir, ModsDir, "mod-settings.dat"), "settings", old)
	for _, f := range ConfigFiles {
		write(t, filepath.Join(dir, ConfigDir, f), "body of "+f, old)
	}
	write(t, filepath.Join(dir, RCONPasswordFile), "hunter2", old)
	return m, id, dir
}

func TestCloneFreshMapCopiesModsAndConfigButNotSavesOrRCONPassword(t *testing.T) {
	m, src, srcDir := cloneSource(t)
	before := tree(t, srcDir)
	id, err := m.Clone(context.Background(), src, "Copy", false, seedPtr(99))
	if err != nil {
		t.Fatal(err)
	}
	if after := tree(t, srcDir); strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("source changed:\nbefore %v\nafter  %v", before, after)
	}
	dir := filepath.Join(m.Dirs().Worlds, id)
	want := map[string]string{
		"mods/cool_1.0.0.zip":         "mod",
		"mods/mod-list.json":          `{"mods":[{"name":"base","enabled":true}]}`,
		"mods/mod-settings.dat":       "settings",
		"config/server-settings.json": "body of server-settings.json",
	}
	for _, f := range ConfigFiles {
		want["config/"+f] = "body of " + f
	}
	for p, body := range want {
		b, err := os.ReadFile(filepath.Join(dir, p)) //nolint:gosec // a test temp folder
		if err != nil || string(b) != body {
			t.Errorf("%s: %q, %v", p, b, err)
		}
	}
	for _, f := range ConfigFiles {
		if got := mode(t, filepath.Join(dir, ConfigDir, f)); got != 0o640 {
			t.Errorf("%s mode %o, want 640", f, got)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, RCONPasswordFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("rconpw was copied: %v", err)
	}
	if saves, _ := os.ReadDir(filepath.Join(dir, SavesDir)); len(saves) != 0 {
		t.Errorf("saves copied: %v", saves)
	}
	meta := readWorldMetaFile(t, m, id)
	if meta.GameVersion != "2.0.77" || !meta.SpaceAge || meta.LoadSave != "" || meta.ImportedFrom != nil {
		t.Errorf("meta %+v", meta)
	}
	if meta.NewMap == nil || meta.NewMap.Preset != "" || meta.NewMap.Seed == nil || *meta.NewMap.Seed != 99 {
		t.Errorf("new_map %+v", meta.NewMap)
	}
	assertNoStaging(t, m)
}

func TestCloneKeepSaveCopiesOnlyTheNewestSaveAndRecordsIt(t *testing.T) {
	m, src, _ := cloneSource(t)
	id, err := m.Clone(context.Background(), src, "Copy", true, seedPtr(5))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.Dirs().Worlds, id)
	entries, _ := os.ReadDir(filepath.Join(dir, SavesDir))
	if len(entries) != 1 || entries[0].Name() != "b.zip" {
		t.Fatalf("saves %v, want only b.zip", entries)
	}
	info, _ := entries[0].Info()
	if !info.ModTime().Equal(time.Date(2025, 6, 1, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("save mtime not kept: %v", info.ModTime())
	}
	meta := readWorldMetaFile(t, m, id)
	if meta.LoadSave != "b.zip" || meta.NewMap != nil {
		t.Errorf("meta %+v, want load_save b.zip and no new_map", meta)
	}
}

func TestCloneKeepSaveCopiesTheChosenSave(t *testing.T) {
	m, src, _ := cloneSource(t)
	if err := m.SetLoadSave(src, "a.zip"); err != nil {
		t.Fatal(err)
	}
	name, mtime, err := m.CloneSource(src)
	if err != nil || name != "a.zip" || !mtime.Equal(time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("CloneSource: %q %v %v", name, mtime, err)
	}
	id, err := m.Clone(context.Background(), src, "Copy", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(m.Dirs().Worlds, id, SavesDir))
	if len(entries) != 1 || entries[0].Name() != "a.zip" {
		t.Errorf("saves %v, want only a.zip", entries)
	}
	if meta := readWorldMetaFile(t, m, id); meta.LoadSave != "a.zip" {
		t.Errorf("load_save %q", meta.LoadSave)
	}
}

func TestCloneSourceNamesNewestSaveAndIsEmptyWithoutOne(t *testing.T) {
	m, src, _ := cloneSource(t)
	name, _, err := m.CloneSource(src)
	if err != nil || name != "b.zip" {
		t.Errorf("%q, %v", name, err)
	}
	empty, err := m.Create(context.Background(), "Empty", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	name, mtime, err := m.CloneSource(empty)
	if err != nil || name != "" || !mtime.IsZero() {
		t.Errorf("%q %v %v", name, mtime, err)
	}
	if _, _, err := m.CloneSource("nope"); err == nil {
		t.Error("an unknown world was accepted")
	}
}

func TestCloneRefusals(t *testing.T) {
	ctx := context.Background()
	m, src, srcDir := cloneSource(t)
	empty, err := m.Create(ctx, "Empty", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Clone(ctx, src, "source", false, nil); !errors.Is(err, ErrNameTaken) {
		t.Errorf("taken name: %v", err)
	}
	if _, err := m.Clone(ctx, src, " ", false, nil); !errors.Is(err, ErrNameEmpty) {
		t.Errorf("blank name: %v", err)
	}
	if _, err := m.Clone(ctx, empty, "Keep nothing", true, nil); !errors.Is(err, ErrCloneNoSave) {
		t.Errorf("keep with no save: %v", err)
	}
	if _, err := m.Clone(ctx, "missing", "X", false, nil); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unknown source: %v", err)
	}

	// The chosen save is gone: refuse, never copy another one instead.
	if err := m.SetLoadSave(src, "a.zip"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(srcDir, SavesDir, "a.zip")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Clone(ctx, src, "Gone", true, nil); !errors.Is(err, ErrSaveNotFound) {
		t.Errorf("chosen save gone: %v", err)
	}

	// An unsupported game version is refused before any work.
	if err := os.WriteFile(filepath.Join(srcDir, MetaFile), []byte(`{"format_version":1,"name":"Source","game_version":"3.0.1","space_age":false,"created_at":"2026-11-03T14:25:01Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Clone(ctx, src, "Old", false, nil); !errors.Is(err, ErrBadGameVersion) {
		t.Errorf("unsupported version: %v", err)
	}

	worlds, _ := m.List(ctx)
	if len(worlds) != 2 {
		t.Errorf("a refused clone left a world: %d worlds", len(worlds))
	}
	assertNoStaging(t, m)
}

func TestCloneRefusesANewerSourceAndABusyOne(t *testing.T) {
	ctx := context.Background()
	m, src, srcDir := cloneSource(t)
	unlock, err := m.Lock(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Clone(ctx, src, "Busy", false, nil); !errors.Is(err, ErrBusy) {
		t.Errorf("busy source: %v", err)
	}
	unlock()

	if err := os.WriteFile(filepath.Join(srcDir, MetaFile), []byte(`{"format_version":2,"name":"Source","game_version":"2.0.77","space_age":true,"created_at":"2026-11-03T14:25:01Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Clone(ctx, src, "Newer", false, nil); !errors.Is(err, ErrNewerFormat) {
		t.Errorf("newer source: %v", err)
	}
	// The lock is released after a refusal.
	if unlock, err := m.Lock(src); err != nil {
		t.Errorf("lock left held: %v", err)
	} else {
		unlock()
	}
	assertNoStaging(t, m)
}

func TestCloneIgnoresSymlinksInTheSource(t *testing.T) {
	m, src, srcDir := cloneSource(t)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(srcDir, ModsDir, "link.zip")); err != nil {
		t.Fatal(err)
	}
	id, err := m.Clone(context.Background(), src, "Copy", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(m.Dirs().Worlds, id, ModsDir, "link.zip")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("symlink was followed or copied: %v", err)
	}
}
