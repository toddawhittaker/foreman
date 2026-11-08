package world

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// modZip is a mod zip whose info.json is info.
func modZip(t *testing.T, folder, info string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(folder + "/info.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(info)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// stage stages a mod zip for name and version and returns its staged name.
func stage(t *testing.T, m *Manager, name, version string) string {
	t.Helper()
	return stageBytes(t, m, modZip(t, name+"_"+version,
		fmt.Sprintf(`{"name":%q,"version":%q,"factorio_version":"2.0"}`, name, version)))
}

func stageBytes(t *testing.T, m *Manager, b []byte) string {
	t.Helper()
	f, name, err := m.StageMod()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func fingerprint(t *testing.T, m *Manager, id string) string {
	t.Helper()
	fp, err := m.ModsFingerprint(id)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func readModListFile(t *testing.T, dir string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ModsDir, ModListFile)) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	return parseModList(t, b)
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestApplyModsOnAGeneratedWorldWritesTheDefaultListFirst(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	if exists(filepath.Join(dir, ModsDir, ModListFile)) {
		t.Fatal("a generated world should start with no mod list")
	}
	s := stage(t, m, "flib", "0.16.2")
	if err := m.ApplyModsHeld(id, ModChange{Add: []string{s}}, fingerprint(t, m, id)); err != nil {
		t.Fatal(err)
	}
	got := readModListFile(t, dir)
	want := map[string]bool{"base": true, "elevated-rails": false, "quality": false, "space-age": false, "flib": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if !exists(filepath.Join(dir, ModsDir, "flib_0.16.2.zip")) {
		t.Fatal("the zip was not placed")
	}
	if exists(m.StagedPath(s)) {
		t.Fatal("the staged file was left")
	}
}

func TestApplyModsKeepsUnknownModListKeys(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.2", "2.0")
	writeFile(t, filepath.Join(dir, ModsDir, ModListFile),
		`{"version":7,"mods":[{"name":"base","enabled":true,"version":"2.0.77"},{"name":"flib","enabled":true,"extra":[1]}]}`)
	if err := m.ApplyModsHeld(id, ModChange{Enabled: map[string]bool{"flib": false}}, fingerprint(t, m, id)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ModsDir, ModListFile)) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int `json:"version"`
		Mods    []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
			Version string `json:"version"`
			Extra   []int  `json:"extra"`
		} `json:"mods"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 7 || len(doc.Mods) != 2 || doc.Mods[0].Version != "2.0.77" || len(doc.Mods[1].Extra) != 1 {
		t.Fatalf("lost keys: %s", b)
	}
	if !doc.Mods[0].Enabled || doc.Mods[1].Enabled {
		t.Fatalf("enabled states wrong: %s", b)
	}
}

func TestApplyModsRefusesBuiltinMods(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", true)
	if err := m.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	before := readModListFile(t, dir)
	for _, change := range []ModChange{
		{Enabled: map[string]bool{"quality": false}},
		{Enabled: map[string]bool{"base": false}},
		{Add: []string{stage(t, m, "space-age", "2.0.77")}},
	} {
		if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); !errors.Is(err, ErrBuiltinMod) {
			t.Fatalf("%+v: got %v, want ErrBuiltinMod", change, err)
		}
	}
	after := readModListFile(t, dir)
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("built-in state changed: %v to %v", before, after)
		}
	}
	if exists(filepath.Join(dir, ModsDir, "space-age_2.0.77.zip")) {
		t.Fatal("a built-in mod zip was placed")
	}
}

func TestApplyModsNeverReplacesALocalMod(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	folder := filepath.Join(dir, ModsDir, "mine_1.0.0")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(folder, "info.json"), `{"name":"mine","version":"1.0.0"}`)
	s := stage(t, m, "mine", "1.1.0")
	if err := m.ApplyModsHeld(id, ModChange{Add: []string{s}}, fingerprint(t, m, id)); !errors.Is(err, ErrLocalMod) {
		t.Fatalf("got %v, want ErrLocalMod", err)
	}
	if exists(filepath.Join(dir, ModsDir, "mine_1.1.0.zip")) || !exists(filepath.Join(folder, "info.json")) {
		t.Fatal("the folder mod was touched")
	}
	if exists(m.StagedPath(s)) {
		t.Fatal("a refused staged file was left")
	}
	mods, err := m.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, mod := range mods {
		if mod.Name == "mine" {
			found = mod.Local && mod.File == "" && mod.Version == "1.0.0" && mod.Enabled
		}
	}
	if !found {
		t.Fatalf("folder mod not listed as local: %+v", mods)
	}
}

func TestApplyModsRefusesWhenTheFolderChanged(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	fp := fingerprint(t, m, id)
	putModFor(t, dir, "other", "1.0.0", "2.0")
	s := stage(t, m, "flib", "0.16.2")
	if err := m.ApplyModsHeld(id, ModChange{Add: []string{s}}, fp); !errors.Is(err, ErrModsChanged) {
		t.Fatalf("got %v, want ErrModsChanged", err)
	}
	if exists(filepath.Join(dir, ModsDir, "flib_0.16.2.zip")) || exists(filepath.Join(dir, ModsDir, ModListFile)) {
		t.Fatal("something was written")
	}
}

func TestModsFingerprintSeesTheListAndFiles(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	a := fingerprint(t, m, id)
	if a != fingerprint(t, m, id) {
		t.Fatal("fingerprint not stable")
	}
	if err := m.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	b := fingerprint(t, m, id)
	putModFor(t, dir, "x", "1.0.0", "2.0")
	c := fingerprint(t, m, id)
	if a == b || b == c {
		t.Fatalf("fingerprint missed a change: %s %s %s", a, b, c)
	}
}

func TestApplyModsRefusesBeforeWritingWhenTheDiskIsFull(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	if err := m.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	list, _ := os.ReadFile(filepath.Join(dir, ModsDir, ModListFile)) //nolint:gosec // a test temp folder
	s := stage(t, m, "flib", "0.16.2")
	fp := fingerprint(t, m, id)
	t.Cleanup(SetSpaceReserveForTest(1 << 62))
	err := m.ApplyModsHeld(id, ModChange{Add: []string{s}, Settings: []byte("x")}, fp)
	if !errors.Is(err, ErrNoSpace) {
		t.Fatalf("got %v, want ErrNoSpace", err)
	}
	now, _ := os.ReadFile(filepath.Join(dir, ModsDir, ModListFile)) //nolint:gosec // a test temp folder
	if !bytes.Equal(list, now) || exists(filepath.Join(dir, ModsDir, "flib_0.16.2.zip")) || exists(filepath.Join(dir, ModsDir, ModSettingsFile)) {
		t.Fatal("something was written")
	}
}

func TestApplyModsReplacesAndRemovesAndWritesSettings(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.1", "2.0")
	putModFor(t, dir, "gone", "1.0.0", "2.0")
	s := stage(t, m, "flib", "0.16.2")
	change := ModChange{Add: []string{s}, Remove: []string{"flib_0.16.1.zip", "gone_1.0.0.zip"}, Settings: []byte("settings")}
	if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(dir, ModsDir)
	if !exists(filepath.Join(mods, "flib_0.16.2.zip")) || exists(filepath.Join(mods, "flib_0.16.1.zip")) || exists(filepath.Join(mods, "gone_1.0.0.zip")) {
		t.Fatal("zips not as asked")
	}
	if b, _ := os.ReadFile(filepath.Join(mods, ModSettingsFile)); string(b) != "settings" { //nolint:gosec // a test temp folder
		t.Fatalf("settings: %q", b)
	}
}

func TestApplyModsRefusesARemoveOutsideTheModsFolder(t *testing.T) {
	m, id, _ := checkWorld(t, "2.0.77", false)
	for _, f := range []string{"../world.json", "x.txt", ".partial-1.zip", "missing_1.0.0.zip"} {
		if err := m.ApplyModsHeld(id, ModChange{Remove: []string{f}}, fingerprint(t, m, id)); !errors.Is(err, ErrModFile) {
			t.Fatalf("%s: got %v, want ErrModFile", f, err)
		}
	}
}

func TestStagedModsAreNotInSnapshotsAndRecoveryRemovesThem(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	s := stage(t, m, "flib", "0.16.2")
	// A crash mid-apply: one zip placed, the mod list's temp file half written.
	placedZip := filepath.Join(dir, ModsDir, "other_1.0.0.zip")
	if err := os.WriteFile(placedZip, modZip(t, "other_1.0.0", `{"name":"other","version":"1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	half := filepath.Join(dir, ModsDir, PrefixPartial+"abcd")
	writeFile(t, half, `{"mods":[`)
	snap, err := m.AutoSnapshot(id, ReasonMods, LabelBeforeMods, "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range snap.Files {
		if strings.Contains(f.Path, "flib") {
			t.Fatalf("snapshot holds a staged file: %s", f.Path)
		}
	}
	if err := m.Recover(slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	if exists(m.StagedPath(s)) || exists(half) {
		t.Fatal("recovery left a staged or half-written file")
	}
	if !exists(placedZip) {
		t.Fatal("recovery removed a placed zip")
	}
	// The game can load what is left: the world still lists its mods.
	if _, err := m.Mods(id); err != nil {
		t.Fatal(err)
	}
}

func TestReadModZipRefusesBadZips(t *testing.T) {
	dir := t.TempDir()
	cases := map[string][]byte{
		"not a zip":    []byte("hello"),
		"empty file":   {},
		"no name":      modZip(t, "x_1.0.0", `{"version":"1.0.0"}`),
		"path in name": modZip(t, "x_1.0.0", `{"name":"../x","version":"1.0.0"}`),
		"slash":        modZip(t, "x_1.0.0", `{"name":"a/b","version":"1.0.0"}`),
		"dot first":    modZip(t, "x_1.0.0", `{"name":".partial-x","version":"1.0.0"}`),
		"control char": modZip(t, "x_1.0.0", `{"name":"a\u0007b","version":"1.0.0"}`),
		"bad version":  modZip(t, "x_1.0.0", `{"name":"x","version":"1.0/../.."}`),
		"top-level only": func() []byte {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			w, _ := zw.Create("info.json")
			_, _ = w.Write([]byte(`{"name":"x","version":"1.0.0"}`))
			_ = zw.Close()
			return buf.Bytes()
		}(),
	}
	for name, b := range cases {
		p := filepath.Join(dir, "up.zip")
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadModZip(p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	big := filepath.Join(dir, "big.zip")
	f, err := os.Create(big) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxModZip + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := ReadModZip(big); !errors.Is(err, ErrModTooBig) {
		t.Fatalf("big: got %v", err)
	}
}

func TestReadModZipReadsInfo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "up.zip")
	b := modZip(t, "flib_0.16.2", `{"name":"flib","version":"0.16.2","factorio_version":"2.0","dependencies":["base >= 2.0","? other"]}`)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := ReadModZip(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "flib" || info.Version != "0.16.2" || info.FactorioVersion != "2.0" || len(info.Dependencies) != 2 || ModFileName(info) != "flib_0.16.2.zip" {
		t.Fatalf("%+v", info)
	}
}

func TestModsListsBuiltinsAndZips(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	if err := m.EnsureModList(id); err != nil {
		t.Fatal(err)
	}
	// The file name disagrees with info.json; info.json wins.
	putModZip(t, dir, "Odd_Name_1.2.3.zip", "x", `{"name":"Odd_Name","version":"1.2.4"}`)
	mods, err := m.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]InstalledMod{}
	for _, mod := range mods {
		byName[mod.Name] = mod
	}
	if b := byName["base"]; !b.BuiltIn || !b.Enabled {
		t.Fatalf("base: %+v", b)
	}
	if q := byName["quality"]; !q.BuiltIn || q.Enabled {
		t.Fatalf("quality: %+v", q)
	}
	if o := byName["Odd_Name"]; o.Version != "1.2.4" || o.File != "Odd_Name_1.2.3.zip" || !o.Enabled || o.BuiltIn || o.Local {
		t.Fatalf("zip: %+v", o)
	}
}

func TestSaveModsReadsTheSave(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putFixture(t, dir, "2.0.77-no-space-age.zip", "a.zip", t0)
	save, err := m.SaveMods(id, "a.zip")
	if err != nil {
		t.Fatal(err)
	}
	if save.Version.String() != "2.0.77" || len(save.Mods) == 0 {
		t.Fatalf("%+v", save)
	}
}
