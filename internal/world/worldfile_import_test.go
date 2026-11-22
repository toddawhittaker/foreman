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
	"testing"
	"time"
)

var wfLimits = WorldFileLimits{Save: 1 << 20, Mod: 1 << 20}

type wfEntry struct {
	name   string
	body   []byte
	method uint16
	mode   os.FileMode
}

const wfFormat = `{"format_version":1,"exported_at":"2026-11-22T10:00:00Z","foreman_version":"1.0.0","saves":"all","game_password_set":true}`

func wfMeta(extra string) string {
	return `{"format_version":1,"name":"Old name","game_version":"2.0.77","space_age":true,"load_save":"b.zip","created_at":"2025-01-01T00:00:00Z",` +
		`"imported_from":{"folder":"x","save":"a.zip","save_size":1,"save_mtime":"2025-01-01T00:00:00Z","at":"2025-01-01T00:00:00Z"},` +
		`"archived":true,"use_whitelist":true,"future_field":{"k":1}` + extra + `}`
}

// goodWorldFile is a world file as an export writes it.
func goodWorldFile() []wfEntry {
	return []wfEntry{
		{name: WorldFileFormatFile, body: []byte(wfFormat)},
		{name: MetaFile, body: []byte(wfMeta(""))},
		{name: "saves/a.zip", body: []byte("save a"), method: zip.Store},
		{name: "saves/b.zip", body: []byte("save b"), method: zip.Store},
		{name: "mods/mod-list.json", body: []byte(`{"mods":[]}`)},
		{name: "mods/folder-mod/", mode: os.ModeDir | 0o755},
		{name: "mods/folder-mod/info.json", body: []byte(`{}`)},
		{name: "config/server-settings.json", body: []byte(`{"name":"x","game_password":"hunter2","token":"tok"}`)},
		{name: "config/server-whitelist.json", body: []byte(`["a"]`)},
	}
}

var wfTime = time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)

func wfZip(t *testing.T, entries []wfEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: e.method, Modified: wfTime}
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

// stageWorldFile writes data as a staged world file and returns its name.
func stageWorldFile(t *testing.T, m *Manager, data []byte) string {
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
	temp := stageWorldFile(t, m, wfZip(t, goodWorldFile()))
	if !strings.HasPrefix(temp, PrefixWorldFile) {
		t.Fatalf("staged name %q", temp)
	}

	info, err := m.ReadWorldFile(temp, wfLimits)
	if err != nil {
		t.Fatal(err)
	}
	want := WorldFileInfo{Name: "Old name", GameVersion: "2.0.77", SpaceAge: true, Saves: 2, ModFiles: 2,
		LoadSave: "b.zip", ForemanVersion: "1.0.0", GamePasswordSet: true}
	info.Bytes = 0
	if info != want {
		t.Fatalf("info %+v, want %+v", info, want)
	}

	id, err := m.ImportWorldFile(ctx, temp, "New name", wfLimits)
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
		if !info.ModTime().Equal(wfTime) {
			t.Errorf("%s time %v, want %v", p, info.ModTime(), wfTime)
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
	var entries []wfEntry
	for _, e := range goodWorldFile() {
		if e.name != "config/server-settings.json" {
			entries = append(entries, e)
		}
	}
	id, err := m.ImportWorldFile(context.Background(), stageWorldFile(t, m, wfZip(t, entries)), "W", wfLimits)
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
	entries[1].body = []byte(strings.Replace(wfMeta(""), `"b.zip"`, `"gone.zip"`, 1))
	info, err := m.ReadWorldFile(stageWorldFile(t, m, wfZip(t, entries)), wfLimits)
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
	temp := stageWorldFile(t, m, wfZip(t, goodWorldFile()))
	if _, err := m.ImportWorldFile(ctx, temp, "taken", wfLimits); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("err %v", err)
	}
	if !staged(m, temp) {
		t.Fatal("staged file removed on a name clash")
	}
	if _, err := m.ImportWorldFile(ctx, temp, "Free", wfLimits); err != nil {
		t.Fatal(err)
	}
}

func TestWorldFileKeepsStagedOnNoSpace(t *testing.T) {
	m, _ := newTestManager(t)
	defer SetFreeBytesForTest(0)()
	temp := stageWorldFile(t, m, wfZip(t, goodWorldFile()))
	if _, err := m.ImportWorldFile(context.Background(), temp, "W", wfLimits); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("err %v", err)
	}
	if !staged(m, temp) {
		t.Fatal("staged file removed on too little space")
	}
}

func TestWorldFileRefusals(t *testing.T) {
	replace := func(name string, body []byte) []wfEntry {
		entries := goodWorldFile()
		for i := range entries {
			if entries[i].name == name {
				entries[i].body = body
			}
		}
		return entries
	}
	add := func(e ...wfEntry) []wfEntry { return append(goodWorldFile(), e...) }
	without := func(name string) []wfEntry {
		var out []wfEntry
		for _, e := range goodWorldFile() {
			if e.name != name {
				out = append(out, e)
			}
		}
		return out
	}
	big := bytes.Repeat([]byte{0}, 2<<20)
	cases := map[string]struct {
		entries []wfEntry
		want    error
	}{
		"zip slip":           {add(wfEntry{name: "mods/../../x"}), ErrWorldFileBad},
		"absolute":           {add(wfEntry{name: "/etc/x"}), ErrWorldFileBad},
		"rconpw":             {add(wfEntry{name: "config/rconpw", body: []byte("pw")}), ErrWorldFileBad},
		"nested save":        {add(wfEntry{name: "saves/x/a.zip"}), ErrWorldFileBad},
		"repeated name":      {add(wfEntry{name: "saves/a.zip", body: []byte("again")}), ErrWorldFileBad},
		"symlink":            {add(wfEntry{name: "mods/link", body: []byte("/etc"), mode: os.ModeSymlink | 0o777}), ErrWorldFileBad},
		"file under file":    {add(wfEntry{name: "mods/mod-list.json/x"}), ErrWorldFileBad},
		"no format file":     {without(WorldFileFormatFile), ErrWorldFileBad},
		"no world.json":      {without(MetaFile), ErrWorldFileBad},
		"newer format":       {replace(WorldFileFormatFile, []byte(`{"format_version":2,"new":true}`)), ErrWorldFileNewer},
		"format unknown key": {replace(WorldFileFormatFile, []byte(strings.Replace(wfFormat, "{", `{"x":1,`, 1))), ErrWorldFileBad},
		"format two values":  {replace(WorldFileFormatFile, []byte(wfFormat+wfFormat)), ErrWorldFileBad},
		"newer world.json":   {replace(MetaFile, []byte(strings.Replace(wfMeta(""), `"format_version":1`, `"format_version":2`, 1))), ErrWorldFileNewer},
		"bad game version":   {replace(MetaFile, []byte(strings.Replace(wfMeta(""), "2.0.77", "9.9.9", 1))), ErrWorldFileBad},
		"space age on 1.1":   {replace(MetaFile, []byte(strings.Replace(wfMeta(""), "2.0.77", "1.1.110", 1))), ErrWorldFileBad},
		"world.json array":   {replace(MetaFile, []byte(`[]`)), ErrWorldFileBad},
		"server not object":  {replace("config/server-settings.json", []byte(`[1]`)), ErrWorldFileBad},
		"save over limit":    {replace("saves/a.zip", big), ErrWorldFileBad},
		"no saves": {func() []wfEntry {
			var out []wfEntry
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
			temp := stageWorldFile(t, m, wfZip(t, c.entries))
			if _, err := m.ReadWorldFile(temp, wfLimits); !errors.Is(err, c.want) {
				t.Fatalf("read: err %v, want %v", err, c.want)
			}
			if _, err := m.ImportWorldFile(context.Background(), temp, "W", wfLimits); !errors.Is(err, c.want) {
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
	entries := []wfEntry{
		{name: WorldFileFormatFile, body: []byte(wfFormat)},
		{name: MetaFile, body: []byte(`{"format_version":1,"name":"n","game_version":"2.0.77","created_at":"2025-01-01T00:00:00Z","new_map":{"preset":""}}`)},
	}
	if _, err := m.ImportWorldFile(context.Background(), stageWorldFile(t, m, wfZip(t, entries)), "W", wfLimits); err != nil {
		t.Fatal(err)
	}
}

func TestWorldFileRatio(t *testing.T) {
	m, _ := newTestManager(t)
	entries := goodWorldFile()
	entries[2].body = bytes.Repeat([]byte{0}, 1<<20-1)
	entries[2].method = zip.Deflate
	temp := stageWorldFile(t, m, wfZip(t, entries))
	if _, err := m.ReadWorldFile(temp, wfLimits); !errors.Is(err, ErrWorldFileBad) {
		t.Fatalf("err %v", err)
	}
}

// Damage to a stored entry is caught by its CRC while writing, and no world
// is left behind.
func TestWorldFileDamagedEntry(t *testing.T) {
	m, _ := newTestManager(t)
	data := wfZip(t, goodWorldFile())
	i := bytes.Index(data, []byte("save a"))
	data[i] = 'X'
	temp := stageWorldFile(t, m, data)
	if _, err := m.ImportWorldFile(context.Background(), temp, "W", wfLimits); !errors.Is(err, ErrWorldFileBad) {
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
	if _, err := m.ReadWorldFile(stageWorldFile(t, m, buf.Bytes()), wfLimits); !errors.Is(err, ErrWorldFileBad) {
		t.Fatalf("err %v", err)
	}
}

func TestWorldFileTempNames(t *testing.T) {
	m, _ := newTestManager(t)
	for _, name := range []string{"../x", PrefixWorldFile + "a/../../x", "world.json", ".partial-other"} {
		if _, err := m.ReadWorldFile(name, wfLimits); err == nil {
			t.Errorf("%q read", name)
		}
	}
}

// At most two staged world files wait at once, and one older than an hour
// is removed when a new one is staged.
func TestWorldFileStagedPrune(t *testing.T) {
	m, _ := newTestManager(t)
	a := stageWorldFile(t, m, []byte("a"))
	old := t0.Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(m.dirs.Worlds, a), old, old); err != nil {
		t.Fatal(err)
	}
	b := stageWorldFile(t, m, []byte("b"))
	if staged(m, a) {
		t.Error("an hour-old staged file was kept")
	}
	_ = os.Chtimes(filepath.Join(m.dirs.Worlds, b), t0.Add(-10*time.Minute), t0.Add(-10*time.Minute))
	c := stageWorldFile(t, m, []byte("c"))
	_ = os.Chtimes(filepath.Join(m.dirs.Worlds, c), t0.Add(-5*time.Minute), t0.Add(-5*time.Minute))
	d := stageWorldFile(t, m, []byte("d"))
	if staged(m, b) || !staged(m, c) || !staged(m, d) {
		t.Errorf("b %v c %v d %v; want only the two newest", staged(m, b), staged(m, c), staged(m, d))
	}
}
