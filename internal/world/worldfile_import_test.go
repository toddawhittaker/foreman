package world

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var importLimits = WorldFileLimits{Save: 1 << 20, Mod: 1 << 20}

type importEntry struct {
	name   string
	body   []byte
	method uint16
	mode   os.FileMode
}

const importFormat = `{"format_version":1,"exported_at":"2026-11-22T10:00:00Z","foreman_version":"1.0.0","saves":"all","game_password_set":true}`

func importMeta(extra string) string {
	return `{"format_version":1,"name":"Old name","game_version":"2.0.77","space_age":true,"load_save":"b.zip","created_at":"2025-01-01T00:00:00Z",` +
		`"imported_from":{"folder":"x","save":"a.zip","save_size":1,"save_mtime":"2025-01-01T00:00:00Z","at":"2025-01-01T00:00:00Z"},` +
		`"archived":true,"use_whitelist":true,"future_field":{"k":1}` + extra + `}`
}

// goodWorldFile is a world file as an export writes it.
func goodWorldFile() []importEntry {
	return []importEntry{
		{name: WorldFileFormatFile, body: []byte(importFormat)},
		{name: MetaFile, body: []byte(importMeta(""))},
		{name: "saves/a.zip", body: []byte("save a"), method: zip.Store},
		{name: "saves/b.zip", body: []byte("save b"), method: zip.Store},
		{name: "mods/mod-list.json", body: []byte(`{"mods":[]}`)},
		{name: "mods/folder-mod/", mode: os.ModeDir | 0o755},
		{name: "mods/folder-mod/info.json", body: []byte(`{}`)},
		{name: "config/server-settings.json", body: []byte(`{"name":"x","game_password":"hunter2","token":"tok"}`)},
		{name: "config/server-whitelist.json", body: []byte(`["a"]`)},
	}
}

var importTime = time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)

func importZip(t *testing.T, entries []importEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: e.method, Modified: importTime}
		if h.Method == 0 {
			h.Method = zip.Deflate
		}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// stageImport writes data as a staged world file and returns its name.
func stageImport(t *testing.T, m *Manager, data []byte) string {
	t.Helper()
	f, name, err := m.CreateWorldFileTemp()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return name
}

func staged(m *Manager, name string) bool {
	_, err := os.Stat(filepath.Join(m.dirs.Worlds, name))
	return err == nil
}

func TestWorldFileRoundTrip(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	temp := stageImport(t, m, importZip(t, goodWorldFile()))
	if !strings.HasPrefix(temp, PrefixWorldFile) {
		t.Fatalf("staged name %q", temp)
	}

	info, err := m.ReadWorldFile(temp, importLimits)
	if err != nil {
		t.Fatal(err)
	}
	want := WorldFileInfo{Name: "Old name", GameVersion: "2.0.77", SpaceAge: true, Saves: 2, ModFiles: 2,
		LoadSave: "b.zip", ForemanVersion: "1.0.0", GamePasswordSet: true}
	info.Bytes = 0
	if info != want {
		t.Fatalf("info %+v, want %+v", info, want)
	}

	id, err := m.ImportWorldFile(ctx, temp, "New name", importLimits)
	if err != nil {
		t.Fatal(err)
	}
	if staged(m, temp) {
		t.Error("staged file kept after a good import")
	}
	dir := filepath.Join(m.dirs.Worlds, id)
	for _, p := range []string{"saves/a.zip", "saves/b.zip", "mods/mod-list.json", "mods/folder-mod/info.json", "config/server-whitelist.json"} {
		info, err := os.Stat(filepath.Join(dir, p))
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(importTime) {
			t.Errorf("%s time %v, want %v", p, info.ModTime(), importTime)
		}
	}
	if got := mode(t, filepath.Join(dir, "config/server-whitelist.json")); got != rconPasswordMode {
		t.Errorf("config mode %v", got)
	}
	ss, err := os.ReadFile(filepath.Join(dir, "config/server-settings.json")) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ss), "hunter2") || strings.Contains(string(ss), "tok") {
		t.Errorf("secrets kept: %s", ss)
	}

	raw, err := os.ReadFile(filepath.Join(dir, MetaFile)) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	var meta Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Name != "New name" || meta.ImportedFrom != nil || meta.Archived || !meta.UseWhitelist ||
		meta.LoadSave != "b.zip" || !meta.CreatedAt.Equal(t0) || !meta.SpaceAge {
		t.Errorf("meta %+v", meta)
	}
	if !strings.Contains(string(raw), `"future_field"`) {
		t.Errorf("unknown field dropped: %s", raw)
	}
	if id == "old-name" || !strings.HasPrefix(id, "new-name") {
		t.Errorf("id %q", id)
	}
}

func TestWorldFileWritesServerSettingsWhenMissing(t *testing.T) {
	m, _ := newTestManager(t)
	var entries []importEntry
	for _, e := range goodWorldFile() {
		if e.name != "config/server-settings.json" {
			entries = append(entries, e)
		}
	}
	id, err := m.ImportWorldFile(context.Background(), stageImport(t, m, importZip(t, entries)), "W", importLimits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.dirs.Worlds, id, "config", ServerSettingsFile)); err != nil {
		t.Fatal(err)
	}
}

func TestWorldFileDropsMissingLoadSave(t *testing.T) {
	m, _ := newTestManager(t)
	entries := goodWorldFile()
	entries[1].body = []byte(strings.Replace(importMeta(""), `"b.zip"`, `"gone.zip"`, 1))
	info, err := m.ReadWorldFile(stageImport(t, m, importZip(t, entries)), importLimits)
	if err != nil {
		t.Fatal(err)
	}
	if info.LoadSave != "" {
		t.Errorf("load save %q", info.LoadSave)
	}
}

// A refusal the form can fix keeps the staged file; a bad file removes it.
func TestWorldFileKeepsStagedOnNameClash(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, "Taken", "2.0.77", false); err != nil {
		t.Fatal(err)
	}
	temp := stageImport(t, m, importZip(t, goodWorldFile()))
	if _, err := m.ImportWorldFile(ctx, temp, "taken", importLimits); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("err %v", err)
	}
	if !staged(m, temp) {
		t.Fatal("staged file removed on a name clash")
	}
	if _, err := m.ImportWorldFile(ctx, temp, "Free", importLimits); err != nil {
		t.Fatal(err)
	}
}

func TestWorldFileKeepsStagedOnNoSpace(t *testing.T) {
	m, _ := newTestManager(t)
	defer SetFreeBytesForTest(0)()
	temp := stageImport(t, m, importZip(t, goodWorldFile()))
	if _, err := m.ImportWorldFile(context.Background(), temp, "W", importLimits); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("err %v", err)
	}
	if !staged(m, temp) {
		t.Fatal("staged file removed on too little space")
	}
}

func TestWorldFileRefusals(t *testing.T) {
	replace := func(name string, body []byte) []importEntry {
		entries := goodWorldFile()
		for i := range entries {
			if entries[i].name == name {
				entries[i].body = body
			}
		}
		return entries
	}
	add := func(e ...importEntry) []importEntry { return append(goodWorldFile(), e...) }
	without := func(name string) []importEntry {
		var out []importEntry
		for _, e := range goodWorldFile() {
			if e.name != name {
				out = append(out, e)
			}
		}
		return out
	}
	big := bytes.Repeat([]byte{0}, 2<<20)
	cases := map[string]struct {
		entries []importEntry
		want    error
	}{
		"zip slip":           {add(importEntry{name: "mods/../../x"}), ErrWorldFileBad},
		"absolute":           {add(importEntry{name: "/etc/x"}), ErrWorldFileBad},
		"rconpw":             {add(importEntry{name: "config/rconpw", body: []byte("pw")}), ErrWorldFileBad},
		"nested save":        {add(importEntry{name: "saves/x/a.zip"}), ErrWorldFileBad},
		"repeated name":      {add(importEntry{name: "saves/a.zip", body: []byte("again")}), ErrWorldFileBad},
		"symlink":            {add(importEntry{name: "mods/link", body: []byte("/etc"), mode: os.ModeSymlink | 0o777}), ErrWorldFileBad},
		"file under file":    {add(importEntry{name: "mods/mod-list.json/x"}), ErrWorldFileBad},
		"no format file":     {without(WorldFileFormatFile), ErrWorldFileBad},
		"no world.json":      {without(MetaFile), ErrWorldFileBad},
		"newer format":       {replace(WorldFileFormatFile, []byte(`{"format_version":2,"new":true}`)), ErrWorldFileNewer},
		"format unknown key": {replace(WorldFileFormatFile, []byte(strings.Replace(importFormat, "{", `{"x":1,`, 1))), ErrWorldFileBad},
		"format two values":  {replace(WorldFileFormatFile, []byte(importFormat+importFormat)), ErrWorldFileBad},
		"newer world.json":   {replace(MetaFile, []byte(strings.Replace(importMeta(""), `"format_version":1`, `"format_version":2`, 1))), ErrWorldFileNewer},
		"bad game version":   {replace(MetaFile, []byte(strings.Replace(importMeta(""), "2.0.77", "9.9.9", 1))), ErrWorldFileBad},
		"space age on 1.1":   {replace(MetaFile, []byte(strings.Replace(importMeta(""), "2.0.77", "1.1.110", 1))), ErrWorldFileBad},
		"world.json array":   {replace(MetaFile, []byte(`[]`)), ErrWorldFileBad},
		"server not object":  {replace("config/server-settings.json", []byte(`[1]`)), ErrWorldFileBad},
		"save over limit":    {replace("saves/a.zip", big), ErrWorldFileBad},
		"no saves": {func() []importEntry {
			var out []importEntry
			for _, e := range goodWorldFile() {
				if !strings.HasPrefix(e.name, "saves/") {
					out = append(out, e)
				}
			}
			return out
		}(), ErrWorldFileBad},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := newTestManager(t)
			temp := stageImport(t, m, importZip(t, c.entries))
			if _, err := m.ReadWorldFile(temp, importLimits); !errors.Is(err, c.want) {
				t.Fatalf("read: err %v, want %v", err, c.want)
			}
			if _, err := m.ImportWorldFile(context.Background(), temp, "W", importLimits); !errors.Is(err, c.want) {
				t.Fatalf("import: err %v, want %v", err, c.want)
			}
			if staged(m, temp) {
				t.Error("staged file kept after a bad file")
			}
			if worlds, _ := m.List(context.Background()); len(worlds) != 0 {
				t.Errorf("a world was made: %v", worlds)
			}
		})
	}
}

// A world with new_map needs no save.
func TestWorldFileNewMapNeedsNoSave(t *testing.T) {
	m, _ := newTestManager(t)
	entries := []importEntry{
		{name: WorldFileFormatFile, body: []byte(importFormat)},
		{name: MetaFile, body: []byte(`{"format_version":1,"name":"n","game_version":"2.0.77","created_at":"2025-01-01T00:00:00Z","new_map":{"preset":""}}`)},
	}
	if _, err := m.ImportWorldFile(context.Background(), stageImport(t, m, importZip(t, entries)), "W", importLimits); err != nil {
		t.Fatal(err)
	}
}

func TestWorldFileRatio(t *testing.T) {
	m, _ := newTestManager(t)
	entries := goodWorldFile()
	entries[2].body = bytes.Repeat([]byte{0}, 1<<20-1)
	entries[2].method = zip.Deflate
	temp := stageImport(t, m, importZip(t, entries))
	if _, err := m.ReadWorldFile(temp, importLimits); !errors.Is(err, ErrWorldFileBad) {
		t.Fatalf("err %v", err)
	}
}

// Damage to a stored entry is caught by its CRC while writing, and no world
// is left behind.
func TestWorldFileDamagedEntry(t *testing.T) {
	m, _ := newTestManager(t)
	data := importZip(t, goodWorldFile())
	i := bytes.Index(data, []byte("save a"))
	data[i] = 'X'
	temp := stageImport(t, m, data)
	if _, err := m.ImportWorldFile(context.Background(), temp, "W", importLimits); !errors.Is(err, ErrWorldFileBad) {
		t.Fatalf("err %v", err)
	}
	if staged(m, temp) {
		t.Error("staged file kept")
	}
	entries, _ := os.ReadDir(m.dirs.Worlds)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), PrefixStaging) || !strings.HasPrefix(e.Name(), ".") {
			t.Errorf("left behind: %s", e.Name())
		}
	}
}

func TestWorldFileTooManyEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i <= maxWorldFileEntries; i++ {
		if _, err := zw.CreateHeader(&zip.FileHeader{Name: "mods/a" + strconv.Itoa(i), Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	m, _ := newTestManager(t)
	if _, err := m.ReadWorldFile(stageImport(t, m, buf.Bytes()), importLimits); !errors.Is(err, ErrWorldFileBad) {
		t.Fatalf("err %v", err)
	}
}

func TestWorldFileTempNames(t *testing.T) {
	m, _ := newTestManager(t)
	for _, name := range []string{"../x", PrefixWorldFile + "a/../../x", "world.json", ".partial-other"} {
		if _, err := m.ReadWorldFile(name, importLimits); err == nil {
			t.Errorf("%q read", name)
		}
	}
}

// At most two staged world files wait at once, and one older than an hour
// is removed when a new one is staged.
func TestWorldFileStagedPrune(t *testing.T) {
	m, _ := newTestManager(t)
	a := stageImport(t, m, []byte("a"))
	old := t0.Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(m.dirs.Worlds, a), old, old); err != nil {
		t.Fatal(err)
	}
	b := stageImport(t, m, []byte("b"))
	if staged(m, a) {
		t.Error("an hour-old staged file was kept")
	}
	_ = os.Chtimes(filepath.Join(m.dirs.Worlds, b), t0.Add(-10*time.Minute), t0.Add(-10*time.Minute))
	c := stageImport(t, m, []byte("c"))
	_ = os.Chtimes(filepath.Join(m.dirs.Worlds, c), t0.Add(-5*time.Minute), t0.Add(-5*time.Minute))
	d := stageImport(t, m, []byte("d"))
	if staged(m, b) || !staged(m, c) || !staged(m, d) {
		t.Errorf("b %v c %v d %v; want only the two newest", staged(m, b), staged(m, c), staged(m, d))
	}
}

// worldsHoldOnly fails unless the worlds folder holds exactly the staged
// files named, so nothing else was written.
func worldsHoldOnly(t *testing.T, m *Manager, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(m.dirs.Worlds)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if strings.Join(got, ",") != strings.Join(names, ",") {
		t.Errorf("worlds folder holds %v, want %v", got, names)
	}
}

// A path under mods/ deeper than maxModDepth segments is refused by the
// classifier.
func TestWorldFileEntryDepth(t *testing.T) {
	ok := "mods/" + strings.Repeat("a/", maxModDepth-1) + "x"
	if _, good := worldFileEntry(ok); !good {
		t.Errorf("%d segments refused", maxModDepth)
	}
	deep := "mods/" + strings.Repeat("a/", maxModDepth) + "x"
	if _, good := worldFileEntry(deep); good {
		t.Errorf("%d segments taken", maxModDepth+1)
	}
}

// 100,000 entries 500 folders deep are refused before anything is written.
func TestWorldFileDeepFolders(t *testing.T) {
	m, _ := newTestManager(t)
	f, temp, err := m.CreateWorldFileTemp()
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	deep := strings.Repeat("d/", 500)
	for i := 0; i < maxWorldFileEntries; i++ {
		if _, err := zw.CreateHeader(&zip.FileHeader{Name: "mods/" + deep + strconv.Itoa(i), Method: zip.Store}); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ImportWorldFile(context.Background(), temp, "Deep", importLimits); !errors.Is(err, ErrWorldFileBad) {
		t.Fatalf("err %v", err)
	}
	worldsHoldOnly(t, m)
}

// The folders an import would create count against the entry cap, so a
// file of few entries cannot make many folders.
func TestWorldFileImpliedFolders(t *testing.T) {
	entries := goodWorldFile()
	for i := 0; i < maxWorldFileEntries/maxModDepth+1; i++ {
		entries = append(entries, importEntry{name: "mods/" + strconv.Itoa(i) + strings.Repeat("/f", maxModDepth-2) + "/x"})
	}
	m, _ := newTestManager(t)
	temp := stageImport(t, m, importZip(t, entries))
	if _, err := m.ImportWorldFile(context.Background(), temp, "Folders", importLimits); !errors.Is(err, ErrWorldFileBad) {
		t.Fatalf("err %v", err)
	}
	worldsHoldOnly(t, m)
}

// A new_map preset must be one the game knows on the world's line.
func TestWorldFileNewMapPreset(t *testing.T) {
	file := func(preset string) []importEntry {
		return []importEntry{
			{name: WorldFileFormatFile, body: []byte(importFormat)},
			{name: MetaFile, body: []byte(`{"format_version":1,"name":"n","game_version":"2.0.77","created_at":"2025-01-01T00:00:00Z","new_map":{"preset":"` + preset + `"}}`)},
		}
	}
	for preset, want := range map[string]error{"": nil, "rich-resources": nil, "no-such-preset": ErrWorldFileBad, "--seed": ErrWorldFileBad} {
		m, _ := newTestManager(t)
		if _, err := m.ReadWorldFile(stageImport(t, m, importZip(t, file(preset))), importLimits); !errors.Is(err, want) {
			t.Errorf("preset %q: err %v, want %v", preset, err, want)
		}
	}
}

// Two uploads staged at once leave at most two staged files, and neither
// removes the other's.
func TestWorldFileConcurrentStaging(t *testing.T) {
	for range 50 {
		m, _ := newTestManager(t)
		old := stageImport(t, m, []byte("old"))
		at := t0.Add(-30 * time.Minute)
		_ = os.Chtimes(filepath.Join(m.dirs.Worlds, old), at, at)
		var wg sync.WaitGroup
		names := make([]string, 2)
		for i := range names {
			wg.Add(1)
			go func() {
				defer wg.Done()
				f, name, err := m.CreateWorldFileTemp()
				if err != nil {
					t.Error(err)
					return
				}
				_ = f.Close()
				names[i] = name
			}()
		}
		wg.Wait()
		if !staged(m, names[0]) || !staged(m, names[1]) || staged(m, old) {
			t.Fatalf("new %v %v, old %v; want both new kept and the old one gone",
				staged(m, names[0]), staged(m, names[1]), staged(m, old))
		}
	}
}

// A file and a folder of the same path refuse the file, in either order.
func TestWorldFileFileAndFolderSamePath(t *testing.T) {
	a := importEntry{name: "mods/a", body: []byte("a")}
	ab := importEntry{name: "mods/a/b", body: []byte("b")}
	for _, extra := range [][]importEntry{{a, ab}, {ab, a}} {
		m, _ := newTestManager(t)
		temp := stageImport(t, m, importZip(t, append(goodWorldFile(), extra...)))
		if _, err := m.ImportWorldFile(context.Background(), temp, "W", importLimits); !errors.Is(err, ErrWorldFileBad) {
			t.Errorf("%s then %s: err %v", extra[0].name, extra[1].name, err)
		}
		worldsHoldOnly(t, m)
	}
}

// A failure after some entries were written leaves no world and no
// staging folder.
func TestWorldFileFailsPartway(t *testing.T) {
	m, _ := newTestManager(t)
	entries := append(goodWorldFile(), importEntry{name: "mods/last_1.0.0.zip", body: []byte("last entry"), method: zip.Store})
	data := importZip(t, entries)
	data[bytes.Index(data, []byte("last entry"))] = 'X'
	temp := stageImport(t, m, data)
	if _, err := m.ImportWorldFile(context.Background(), temp, "W", importLimits); !errors.Is(err, ErrWorldFileBad) {
		t.Fatalf("err %v", err)
	}
	worldsHoldOnly(t, m)
	if list, _ := m.List(context.Background()); len(list) != 0 {
		t.Errorf("worlds %v", list)
	}
}
