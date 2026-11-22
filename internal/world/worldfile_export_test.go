package world

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const exportSettings = `{"name":"My game","game_password":"hunter2-secret","username":"factorio-user","password":"portal-pass","token":"portal-token","visibility":{"public":false}}`

// exportWorld makes a world with every kind of file an export must keep or
// leave out. It returns the id and the world's folder.
func exportWorld(t *testing.T, m *Manager) (string, string) {
	t.Helper()
	id, err := m.Create(t.Context(), "Export me", "2.0.72", true)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.dirs.Worlds, id)
	mt := t0.Add(-48 * time.Hour)
	files := map[string]string{
		"saves/a.zip":                  "save a",
		"saves/b.zip":                  "save b, the newest",
		"saves/c.tmp.zip":              "the game's partial save",
		"saves/notes.txt":              "not a save",
		"mods/flib_0.15.0.zip":         "mod zip",
		"mods/mod-list.json":           `{"mods":[]}`,
		"mods/mod-settings.dat":        "settings",
		"mods/folder-mod/info.json":    `{"name":"x"}`,
		"config/server-settings.json":  exportSettings,
		"config/server-adminlist.json": `["ann"]`,
		"config/rconpw":                "rcon-secret",
		"players.json":                 `{"format":1}`,
		".hidden":                      "hidden",
		"saves/.partial-xyz":           "leftover",
	}
	for p, body := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o750); err != nil {
			t.Fatal(err)
		}
		writeAt(t, filepath.Join(dir, p), body, mt.Add(time.Duration(len(p))*time.Minute))
	}
	// The newest save is b.zip.
	writeAt(t, filepath.Join(dir, "saves/b.zip"), "save b, the newest", t0)
	root, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	meta.ImportedFrom = &ImportedFrom{Folder: "old-host-folder", Save: "a.zip"}
	meta.Archived = true
	meta.UseWhitelist = true
	meta.unknown = map[string]json.RawMessage{"future_field": json.RawMessage(`{"k":1}`)}
	if err := WriteMeta(root, meta); err != nil {
		t.Fatal(err)
	}
	return id, dir
}

// exportZip prepares, writes and closes an export, returning the ZIP bytes.
func exportZip(t *testing.T, m *Manager, id string, saves ExportSaves) []byte {
	t.Helper()
	e, err := m.PrepareExport(t.Context(), id, saves)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := e.WriteZip(&buf, "9.9.9"); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func openZip(t *testing.T, b []byte) (*zip.Reader, map[string]*zip.File) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		byName[f.Name] = f
	}
	return zr, byName
}

func readEntry(t *testing.T, f *zip.File) []byte {
	t.Helper()
	r, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestExportHoldsTheFormatSetAndNothingElse(t *testing.T) {
	m, _ := newTestManager(t)
	id, _ := exportWorld(t, m)
	zr, by := openZip(t, exportZip(t, m, id, ExportAllSaves))

	if zr.File[0].Name != WorldFileFormatFile {
		t.Fatalf("first entry is %q, want %q", zr.File[0].Name, WorldFileFormatFile)
	}
	want := []string{
		"foreman-world.json", "world.json",
		"config/server-adminlist.json", "config/server-settings.json",
		"saves/a.zip", "saves/b.zip",
		"mods/flib_0.15.0.zip", "mods/folder-mod/info.json", "mods/mod-list.json", "mods/mod-settings.dat",
	}
	var got []string
	for _, f := range zr.File {
		got = append(got, f.Name)
		// Every entry is one an import accepts (SPEC 5.13, 14.5).
		if _, ok := worldFileEntry(f.Name); !ok {
			t.Errorf("entry %q is refused by the world file classifier", f.Name)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("entries\n got %v\nwant %v", got, want)
	}
	for _, never := range []string{"config/rconpw", "players.json", ".hidden", "saves/c.tmp.zip", "saves/notes.txt"} {
		if by[never] != nil {
			t.Errorf("%s is in the export", never)
		}
	}
}

func TestExportStripsSecretsEverywhere(t *testing.T) {
	m, _ := newTestManager(t)
	id, _ := exportWorld(t, m)
	b := exportZip(t, m, id, ExportAllSaves)
	_, by := openZip(t, b)

	settings := readEntry(t, by["config/server-settings.json"])
	var obj map[string]any
	if err := json.Unmarshal(settings, &obj); err != nil {
		t.Fatalf("stripped settings are not JSON: %v", err)
	}
	for _, k := range []string{"game_password", "username", "password", "token"} {
		if _, ok := obj[k]; ok {
			t.Errorf("server-settings.json still has %s", k)
		}
	}
	if obj["name"] != "My game" {
		t.Errorf("the rest of server-settings.json changed: %s", settings)
	}
	// ZIP method Deflate hides bytes from a plain search, so check each entry
	// as read, and the format file for the password's value.
	for _, f := range by {
		body := readEntry(t, f)
		for _, secret := range []string{"hunter2-secret", "portal-pass", "portal-token", "rcon-secret"} {
			if bytes.Contains(body, []byte(secret)) {
				t.Errorf("%s holds a secret value", f.Name)
			}
		}
	}
	var doc WorldFileDoc
	if err := json.Unmarshal(readEntry(t, by[WorldFileFormatFile]), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.GamePasswordSet {
		t.Error("game_password_set is false for a world with a game password")
	}
}

func TestExportFormatFileAndWorldJSON(t *testing.T) {
	m, _ := newTestManager(t)
	id, _ := exportWorld(t, m)
	_, by := openZip(t, exportZip(t, m, id, ExportAllSaves))

	var doc WorldFileDoc
	if err := json.Unmarshal(readEntry(t, by[WorldFileFormatFile]), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.FormatVersion != 1 || doc.ForemanVer != "9.9.9" || doc.Saves != ExportAllSaves || !doc.ExportedAt.Equal(t0) {
		t.Errorf("format file = %+v", doc)
	}

	raw := readEntry(t, by[MetaFile])
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"imported_from", "archived"} {
		if _, ok := fields[gone]; ok {
			t.Errorf("world.json still has %s", gone)
		}
	}
	for _, kept := range []string{"future_field", "use_whitelist", "name", "game_version", "space_age"} {
		if _, ok := fields[kept]; !ok {
			t.Errorf("world.json lost %s", kept)
		}
	}
}

func TestExportMethodsAndTimes(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := exportWorld(t, m)
	_, by := openZip(t, exportZip(t, m, id, ExportAllSaves))

	for _, name := range []string{"saves/a.zip", "saves/b.zip", "mods/flib_0.15.0.zip"} {
		if by[name].Method != zip.Store {
			t.Errorf("%s is method %d, want stored", name, by[name].Method)
		}
	}
	for _, name := range []string{"world.json", "config/server-adminlist.json", "mods/mod-list.json", WorldFileFormatFile} {
		if by[name].Method != zip.Deflate {
			t.Errorf("%s is method %d, want deflate", name, by[name].Method)
		}
	}
	// "Last played" survives the move: each entry carries its file's time.
	info, err := os.Stat(filepath.Join(dir, "saves/b.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if got := by["saves/b.zip"].Modified; got.Unix() != info.ModTime().Unix() {
		t.Errorf("saves/b.zip time = %v, want %v", got, info.ModTime())
	}
	if got := string(readEntry(t, by["saves/b.zip"])); got != "save b, the newest" {
		t.Errorf("saves/b.zip content = %q", got)
	}
}

func TestExportStoresASaveWithoutRecompressing(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := exportWorld(t, m)
	big := make([]byte, 1<<20)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "saves/big.zip"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	_, by := openZip(t, exportZip(t, m, id, ExportAllSaves))
	f := by["saves/big.zip"]
	if f.CompressedSize64 != f.UncompressedSize64 || f.UncompressedSize64 != uint64(len(big)) {
		t.Errorf("sizes compressed %d, unpacked %d, want both %d", f.CompressedSize64, f.UncompressedSize64, len(big))
	}
	if !bytes.Equal(readEntry(t, f), big) {
		t.Error("the stored save differs from the original")
	}
}

func TestExportOnlyTheLoadedSave(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := exportWorld(t, m)

	// No load_save: the newest save loads.
	_, by := openZip(t, exportZip(t, m, id, ExportLoadedSave))
	if by["saves/a.zip"] != nil || by["saves/b.zip"] == nil {
		t.Errorf("newest-save export holds a.zip=%v b.zip=%v", by["saves/a.zip"] != nil, by["saves/b.zip"] != nil)
	}
	var doc WorldFileDoc
	_ = json.Unmarshal(readEntry(t, by[WorldFileFormatFile]), &doc)
	if doc.Saves != ExportLoadedSave {
		t.Errorf("format file saves = %q", doc.Saves)
	}
	var meta Meta
	if err := json.Unmarshal(readEntry(t, by[MetaFile]), &meta); err != nil || meta.LoadSave != "b.zip" {
		t.Errorf("world.json load_save = %q, %v", meta.LoadSave, err)
	}

	// A chosen save wins over the newest.
	root, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	cur, _ := ReadMeta(root)
	cur.LoadSave = "a.zip"
	if err := WriteMeta(root, cur); err != nil {
		t.Fatal(err)
	}
	_ = root.Close()
	_, by = openZip(t, exportZip(t, m, id, ExportLoadedSave))
	if by["saves/a.zip"] == nil || by["saves/b.zip"] != nil {
		t.Error("export of the chosen save holds the wrong save")
	}
	// The mods and config still travel.
	if by["mods/flib_0.15.0.zip"] == nil || by["config/server-settings.json"] == nil {
		t.Error("loaded-save export lost mods or config")
	}

	// A chosen save that is gone: no saves, and world.json names none.
	if err := os.Remove(filepath.Join(dir, "saves/a.zip")); err != nil {
		t.Fatal(err)
	}
	_, by = openZip(t, exportZip(t, m, id, ExportLoadedSave))
	for n := range by {
		if strings.HasPrefix(n, "saves/") {
			t.Errorf("unexpected %s", n)
		}
	}
	meta = Meta{}
	_ = json.Unmarshal(readEntry(t, by[MetaFile]), &meta)
	if meta.LoadSave != "" {
		t.Errorf("load_save = %q for a save that is not in the file", meta.LoadSave)
	}
}

func TestExportAllSavesDropsALoadSaveThatIsGone(t *testing.T) {
	m, _ := newTestManager(t)
	id, _ := exportWorld(t, m)
	root, _ := m.OpenWorld(id)
	cur, _ := ReadMeta(root)
	cur.LoadSave = "gone.zip"
	_ = WriteMeta(root, cur)
	_ = root.Close()
	_, by := openZip(t, exportZip(t, m, id, ExportAllSaves))
	var meta Meta
	_ = json.Unmarshal(readEntry(t, by[MetaFile]), &meta)
	if meta.LoadSave != "" {
		t.Errorf("load_save = %q", meta.LoadSave)
	}
}

func stagingFolders(t *testing.T, m *Manager) []string {
	t.Helper()
	es, err := os.ReadDir(m.dirs.Worlds)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		if strings.HasPrefix(e.Name(), PrefixStaging) {
			out = append(out, filepath.Join(m.dirs.Worlds, e.Name()))
		}
	}
	return out
}

func TestExportStagesWithHardLinksAndReleasesTheLock(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := exportWorld(t, m)
	e, err := m.PrepareExport(t.Context(), id, ExportAllSaves)
	if err != nil {
		t.Fatal(err)
	}
	// The lock is held only while staging (SPEC 5.13): a long download never
	// blocks a start, stop or mod change.
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatalf("the world is still locked after PrepareExport: %v", err)
	}
	unlock()

	folders := stagingFolders(t, m)
	if len(folders) != 1 {
		t.Fatalf("staging folders = %v", folders)
	}
	// Saves and mod zips are links to the world's own files; small files are copies.
	for _, p := range []string{"saves/a.zip", "mods/flib_0.15.0.zip"} {
		a, b := statT(t, filepath.Join(dir, p)), statT(t, filepath.Join(folders[0], p))
		if a.Ino != b.Ino {
			t.Errorf("%s is not hard-linked", p)
		}
	}
	a, b := statT(t, filepath.Join(dir, "mods/mod-list.json")), statT(t, filepath.Join(folders[0], "mods/mod-list.json"))
	if a.Ino == b.Ino {
		t.Error("mod-list.json is linked, not copied")
	}
	if e.Bytes() == 0 || e.Name() != "Export me" || !e.GamePasswordSet() {
		t.Errorf("Bytes %d Name %q GamePasswordSet %v", e.Bytes(), e.Name(), e.GamePasswordSet())
	}

	// The world keeps changing while the download runs: the file stays whole.
	writeAt(t, filepath.Join(dir, "mods/mod-list.json"), "changed after staging", t0)
	var buf bytes.Buffer
	if err := e.WriteZip(&buf, "1"); err != nil {
		t.Fatal(err)
	}
	_, by := openZip(t, buf.Bytes())
	if got := string(readEntry(t, by["mods/mod-list.json"])); got != `{"mods":[]}` {
		t.Errorf("the download changed with the world: %q", got)
	}

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if left := stagingFolders(t, m); len(left) != 0 {
		t.Errorf("Close left %v", left)
	}
	if err := e.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if err := e.WriteZip(io.Discard, "1"); err == nil {
		t.Error("WriteZip after Close succeeded")
	}
	// The world's own files are untouched.
	if _, err := os.Stat(filepath.Join(dir, "config/rconpw")); err != nil {
		t.Error(err)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "config/server-settings.json")); string(body) != exportSettings { //nolint:gosec // a test temp folder
		t.Error("the world's server-settings.json was changed by the export")
	}
}

func TestExportRefusals(t *testing.T) {
	t.Run("busy", func(t *testing.T) {
		m, _ := newTestManager(t)
		id, _ := exportWorld(t, m)
		unlock, err := m.Lock(id)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		if _, err := m.PrepareExport(t.Context(), id, ExportAllSaves); !errors.Is(err, ErrBusy) {
			t.Errorf("err = %v, want ErrBusy", err)
		}
	})
	t.Run("newer world", func(t *testing.T) {
		m, _ := newTestManager(t)
		id, dir := exportWorld(t, m)
		b, _ := os.ReadFile(filepath.Join(dir, MetaFile)) //nolint:gosec // a test temp folder
		b = regexp.MustCompile(`"format_version": *1`).ReplaceAll(b, []byte(`"format_version": 99`))
		if err := os.WriteFile(filepath.Join(dir, MetaFile), b, 0o600); err != nil { //nolint:gosec // a test temp folder
			t.Fatal(err)
		}
		if _, err := m.PrepareExport(t.Context(), id, ExportAllSaves); !errors.Is(err, ErrNewerFormat) {
			t.Errorf("err = %v, want ErrNewerFormat", err)
		}
	})
	t.Run("no such world", func(t *testing.T) {
		m, _ := newTestManager(t)
		if _, err := m.PrepareExport(t.Context(), "nope", ExportAllSaves); err == nil {
			t.Error("exported a world that is not there")
		}
	})
	t.Run("unknown choice", func(t *testing.T) {
		m, _ := newTestManager(t)
		id, _ := exportWorld(t, m)
		if _, err := m.PrepareExport(t.Context(), id, "some"); err == nil {
			t.Error("accepted an unknown choice of saves")
		}
	})
	t.Run("symlink", func(t *testing.T) {
		m, _ := newTestManager(t)
		id, dir := exportWorld(t, m)
		if err := os.Symlink("/etc/passwd", filepath.Join(dir, "mods/link")); err != nil {
			t.Fatal(err)
		}
		_, err := m.PrepareExport(t.Context(), id, ExportAllSaves)
		var fe *ExportFileError
		if !errors.As(err, &fe) || fe.Path != "mods/link" || !errors.Is(err, ErrExportFile) {
			t.Errorf("err = %v, want an ExportFileError for mods/link", err)
		}
	})
	t.Run("name a world file cannot hold", func(t *testing.T) {
		m, _ := newTestManager(t)
		id, dir := exportWorld(t, m)
		if err := os.WriteFile(filepath.Join(dir, "mods/a\\b.zip"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := m.PrepareExport(t.Context(), id, ExportAllSaves); !errors.Is(err, ErrExportFile) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("settings that cannot be stripped", func(t *testing.T) {
		m, _ := newTestManager(t)
		id, dir := exportWorld(t, m)
		if err := os.WriteFile(filepath.Join(dir, "config/server-settings.json"), []byte(`{"game_password": "x"`), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := m.PrepareExport(t.Context(), id, ExportAllSaves)
		if !errors.Is(err, ErrExportFile) {
			t.Fatalf("err = %v", err)
		}
		if strings.Contains(err.Error(), `"x"`) {
			t.Errorf("the error repeats the file: %v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		m, _ := newTestManager(t)
		id, _ := exportWorld(t, m)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if _, err := m.PrepareExport(ctx, id, ExportAllSaves); err == nil {
			t.Error("exported with a cancelled context")
		}
	})
	t.Run("every refusal cleans up and unlocks", func(t *testing.T) {
		m, _ := newTestManager(t)
		id, dir := exportWorld(t, m)
		_ = os.Symlink("/etc/passwd", filepath.Join(dir, "mods/link"))
		_, _ = m.PrepareExport(t.Context(), id, ExportAllSaves)
		if len(stagingFolders(t, m)) != 0 {
			t.Error("a refused export left a staging folder")
		}
		unlock, err := m.Lock(id)
		if err != nil {
			t.Fatal(err)
		}
		unlock()
	})
}

func TestExportWithTooManyFilesIsRefused(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := exportWorld(t, m)
	many := filepath.Join(dir, "mods/many")
	if err := os.MkdirAll(many, 0o750); err != nil {
		t.Fatal(err)
	}
	for i := range maxWorldFileEntries {
		f, err := os.Create(filepath.Join(many, "f"+strconv.Itoa(i))) //nolint:gosec // a test temp folder
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	if _, err := m.PrepareExport(t.Context(), id, ExportAllSaves); !errors.Is(err, ErrExportTooManyFiles) {
		t.Errorf("err = %v, want ErrExportTooManyFiles", err)
	}
	if len(stagingFolders(t, m)) != 0 {
		t.Error("left a staging folder")
	}
}

// failAfter is a writer that fails once n bytes have gone through.
type failAfter struct {
	n   int
	buf bytes.Buffer
}

func (w *failAfter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.n {
		return 0, errors.New("connection reset")
	}
	return w.buf.Write(p)
}

func TestExportFailurePartwayLeavesNoCompleteFile(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := exportWorld(t, m)
	big := make([]byte, 2<<20)
	if _, err := rand.Read(big); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "saves/big.zip"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := m.PrepareExport(t.Context(), id, ExportAllSaves)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	w := &failAfter{n: 1 << 20}
	if err := e.WriteZip(w, "1"); err == nil {
		t.Fatal("WriteZip hid the failure")
	}
	// No central directory was written, so no tool reads it as a world file.
	if _, err := zip.NewReader(bytes.NewReader(w.buf.Bytes()), int64(w.buf.Len())); err == nil {
		t.Error("a failed export is a readable ZIP")
	}
}

func TestExportOfAWorldWithNoSaveAndNoSettings(t *testing.T) {
	m, _ := newTestManager(t)
	id, err := m.Create(t.Context(), "Empty", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	zr, by := openZip(t, exportZip(t, m, id, ExportAllSaves))
	if zr.File[0].Name != WorldFileFormatFile || by[MetaFile] == nil {
		t.Errorf("entries: %v", zr.File)
	}
	var doc WorldFileDoc
	_ = json.Unmarshal(readEntry(t, by[WorldFileFormatFile]), &doc)
	if doc.GamePasswordSet {
		t.Error("game_password_set for a world with no password")
	}
}

// A world file never carries players.json (SPEC 8.16): the new host reads its
// own record of the save.
func TestExportLeavesOutThePlayersRecord(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := exportWorld(t, m)
	if _, err := os.Stat(filepath.Join(dir, PlayersFile)); err != nil {
		t.Fatal("the test world has no players.json:", err)
	}
	for _, saves := range []ExportSaves{ExportAllSaves, ExportLoadedSave} {
		zr, _ := openZip(t, exportZip(t, m, id, saves))
		for _, f := range zr.File {
			if f.Name == PlayersFile || strings.HasSuffix(f.Name, "/"+PlayersFile) {
				t.Errorf("%s export holds %s", saves, f.Name)
			}
		}
	}
}

// Hidden files are not exported, under mods/ too: a mod folder kept in git
// holds .git/config, which can hold a token.
func TestExportLeavesOutHiddenFilesUnderMods(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := exportWorld(t, m)
	for p, body := range map[string]string{
		"mods/my-mod/.git/config":    "[remote] url = https://user:token@example.com/x.git",
		"mods/my-mod/.git/HEAD":      "ref: refs/heads/main",
		"mods/.hidden-file":          "x",
		"mods/my-mod/.env":           "TOKEN=abc",
		"mods/my-mod/control.lua":    "-- kept",
		"mods/.hidden-dir/inner.lua": "x",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o750); err != nil {
			t.Fatal(err)
		}
		writeAt(t, filepath.Join(dir, p), body, t0)
	}
	zr, by := openZip(t, exportZip(t, m, id, ExportAllSaves))
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "mods/") && hasHiddenSegment(f.Name) {
			t.Errorf("the export holds hidden %s", f.Name)
		}
	}
	if by["mods/my-mod/control.lua"] == nil {
		t.Error("a visible file next to the hidden ones was left out")
	}
}

// panicCtx is a context whose Err panics, to stop staging partway.
type panicCtx struct{ context.Context }

func (panicCtx) Err() error { panic("stop staging") }

// A panic while staging still removes the staging folder, and goes on.
func TestExportPanicWhileStagingCleansUp(t *testing.T) {
	m, _ := newTestManager(t)
	id, _ := exportWorld(t, m)
	func() {
		defer func() {
			if r := recover(); r != "stop staging" {
				t.Errorf("recovered %v, want the panic to go on", r)
			}
		}()
		_, _ = m.PrepareExport(panicCtx{t.Context()}, id, ExportAllSaves)
	}()
	if left := stagingFolders(t, m); len(left) != 0 {
		t.Errorf("a panic left %v", left)
	}
	unlock, err := m.Lock(id)
	if err != nil {
		t.Fatalf("the world is still locked after the panic: %v", err)
	}
	unlock()
}
