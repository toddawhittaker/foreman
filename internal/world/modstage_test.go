package world

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A plan reads an installed mod's dependencies from its own info.json, zip
// or folder, so it needs no portal for them (SPEC 9.2).
func TestModsReadsDependencies(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModZip(t, dir, "a_1.0.0.zip", "a_1.0.0", `{"name":"a","version":"1.0.0","dependencies":["base >= 2.0","? b"]}`)
	folder := filepath.Join(dir, ModsDir, "local")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "info.json"), []byte(`{"name":"local","version":"0.1.0","dependencies":["a"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mods, err := m.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	deps := map[string][]string{}
	for _, mod := range mods {
		deps[mod.Name] = mod.Dependencies
	}
	if got := deps["a"]; !slices.Equal(got, []string{"base >= 2.0", "? b"}) {
		t.Errorf("zip dependencies %q", got)
	}
	if got := deps["local"]; !slices.Equal(got, []string{"a"}) {
		t.Errorf("folder dependencies %q", got)
	}
}

// Staged mods have their own prefix, under the one startup recovery clears.
func TestStageModUsesTheModPrefix(t *testing.T) {
	m, _, _ := checkWorld(t, "2.0.77", false)
	f, name, err := m.StageMod()
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if !strings.HasPrefix(name, PrefixModStaging) || !strings.HasPrefix(PrefixModStaging, PrefixPartial) {
		t.Errorf("staged name %q, want the prefix %q", name, PrefixModStaging)
	}
}

// SPEC 6.11: a staged mod nobody applied is removed an hour after it was
// last written, when the next one is staged. Other temp files are left.
func TestStageModRemovesAbandonedStagedMods(t *testing.T) {
	m, _, _ := checkWorld(t, "2.0.77", false)
	top := m.Dirs().Worlds
	old := time.Now().Add(-2 * time.Hour)
	put := func(name string, at time.Time) {
		p := filepath.Join(top, name)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	put(PrefixModStaging+"old", old)
	put(PrefixModStaging+"recent", time.Now().Add(-10*time.Minute))
	put(PrefixPartial+"import-upload", old)
	f, _, err := m.StageMod()
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if exists(filepath.Join(top, PrefixModStaging+"old")) {
		t.Error("an abandoned staged mod was kept")
	}
	for _, kept := range []string{PrefixModStaging + "recent", PrefixPartial + "import-upload"} {
		if !exists(filepath.Join(top, kept)) {
			t.Errorf("%s was removed", kept)
		}
	}
}

// SPEC 6.11: an upload, which stages through CreateModTemp, removes
// abandoned staged mods too.
func TestCreateModTempRemovesAbandonedStagedMods(t *testing.T) {
	m, _, _ := checkWorld(t, "2.0.77", false)
	top := m.Dirs().Worlds
	p := filepath.Join(top, PrefixModStaging+"old")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	worlds, err := m.OpenWorlds()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worlds.Close() }()
	f, _, err := CreateModTemp(worlds)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if exists(p) {
		t.Error("an abandoned staged mod was kept")
	}
}

// Code N1: an info.json that lists no dependencies gives an empty list, so a
// plan knows it was read; nil is left for one that could not be read.
func TestModsTellsNoDependenciesFromUnreadable(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModZip(t, dir, "a_1.0.0.zip", "a_1.0.0", `{"name":"a","version":"1.0.0"}`)
	if err := os.WriteFile(filepath.Join(dir, ModsDir, "broken_1.0.0.zip"), []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, ModsDir, "local")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "info.json"), []byte(`{"name":"local","version":"0.1.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mods, err := m.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, mod := range mods {
		switch mod.Name {
		case "a", "local":
			if mod.Dependencies == nil || len(mod.Dependencies) != 0 {
				t.Errorf("%s: dependencies %#v, want an empty list", mod.Name, mod.Dependencies)
			}
		case "broken":
			if mod.Dependencies != nil {
				t.Errorf("unreadable zip: dependencies %#v, want nil", mod.Dependencies)
			}
		}
	}
}

// A zip's info.json is read once per name, size and modification time, so
// listing mods on each page view does not re-open every zip.
func TestModsCachesZipInfoUntilTheZipChanges(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	zipPath := filepath.Join(dir, ModsDir, "a_1.0.0.zip")
	putModZip(t, dir, "a_1.0.0.zip", "a_1.0.0", `{"name":"a","version":"1.0.0","dependencies":["x"]}`)
	depsOf := func() []string {
		t.Helper()
		mods, err := m.Mods(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, mod := range mods {
			if mod.File == "a_1.0.0.zip" {
				return mod.Dependencies
			}
		}
		t.Fatal("a_1.0.0.zip not listed")
		return nil
	}
	if got := depsOf(); !slices.Equal(got, []string{"x"}) {
		t.Fatalf("first read %q", got)
	}
	st, err := os.Stat(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	// Same size and time, other contents: the cached info is used.
	putModZip(t, dir, "a_1.0.0.zip", "a_1.0.0", `{"name":"a","version":"1.0.0","dependencies":["y"]}`)
	if err := os.Chtimes(zipPath, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if got := depsOf(); !slices.Equal(got, []string{"x"}) {
		t.Errorf("unchanged zip re-read: %q", got)
	}
	later := st.ModTime().Add(time.Second)
	if err := os.Chtimes(zipPath, later, later); err != nil {
		t.Fatal(err)
	}
	if got := depsOf(); !slices.Equal(got, []string{"y"}) {
		t.Errorf("changed zip not re-read: %q", got)
	}
}

// ReadStaged checks the name itself: only a staged file at the top of the
// worlds folder is read.
func TestReadStagedRefusesOtherNames(t *testing.T) {
	m, id, _ := checkWorld(t, "2.0.77", false)
	good := stage(t, m, "mine", "1.0.0")
	info, err := m.ReadStaged(good)
	if err != nil || info.Name != "mine" || info.Version != "1.0.0" {
		t.Fatalf("ReadStaged(%q) = %+v, %v", good, info, err)
	}
	for _, bad := range []string{"", "mine_1.0.0.zip", ".staging-x", id, "../" + good, id + "/" + PrefixModStaging + "x", `a\` + good} {
		if _, err := m.ReadStaged(bad); !errors.Is(err, ErrNotStaged) {
			t.Errorf("ReadStaged(%q) = %v, want ErrNotStaged", bad, err)
		}
	}
	if err := m.RemoveStaged(good); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadStaged(good); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadStaged of a removed file = %v, want fs.ErrNotExist", err)
	}
}

// Another upload in progress also starts with PrefixPartial. A posted name
// that points at it is refused by every staged-file call and the file stays.
func TestOtherUploadsTempFilesAreNotStagedMods(t *testing.T) {
	m, id, _ := checkWorld(t, "2.0.77", false)
	other := PrefixPartial + "import-upload"
	path := filepath.Join(m.Dirs().Worlds, other)
	if err := os.WriteFile(path, modZip(t, "a_1.0.0", `{"name":"a","version":"1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ReadStaged(other); !errors.Is(err, ErrNotStaged) {
		t.Errorf("ReadStaged = %v, want ErrNotStaged", err)
	}
	if err := m.RemoveStaged(other); !errors.Is(err, ErrNotStaged) {
		t.Errorf("RemoveStaged = %v, want ErrNotStaged", err)
	}
	if err := m.ApplyModsHeld(id, ModChange{Add: []string{other}}, fingerprint(t, m, id)); !errors.Is(err, ErrNotStaged) {
		t.Errorf("ApplyModsHeld = %v, want ErrNotStaged", err)
	}
	if !exists(path) {
		t.Error("another upload's temp file was removed")
	}
}

// CheckModChange refuses what ApplyModsHeld would and changes nothing.
func TestCheckModChangeRefusesWithoutChanging(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", true)
	putModZip(t, dir, "a_1.0.0.zip", "a_1.0.0", `{"name":"a","version":"1.0.0"}`)
	fp := fingerprint(t, m, id)
	staged := stage(t, m, "a", "1.0.0")
	for _, c := range []struct {
		change ModChange
		want   error
	}{
		{ModChange{Add: []string{stage(t, m, "space-age", "2.0.77")}}, ErrBuiltinMod},
		{ModChange{Remove: []string{"../meta.json"}}, ErrModFile},
		{ModChange{Add: []string{"a_1.0.0.zip"}}, ErrNotStaged},
	} {
		if err := m.CheckModChange(id, c.change); !errors.Is(err, c.want) {
			t.Errorf("%+v: %v, want %v", c.change, err, c.want)
		}
	}
	if err := m.CheckModChange(id, ModChange{Add: []string{staged}, Remove: []string{"a_1.0.0.zip"}}); err == nil {
		t.Error("a file both added and removed passed")
	}
	if err := m.CheckModChange(id, ModChange{Remove: []string{"a_1.0.0.zip"}, Enabled: map[string]bool{"a": false}}); err != nil {
		t.Errorf("a good change: %v", err)
	}
	if fingerprint(t, m, id) != fp || !exists(filepath.Join(m.Dirs().Worlds, staged)) {
		t.Error("the check changed the folder or used up the staged file")
	}
}
