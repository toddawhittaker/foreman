package world

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// The three game lines Foreman supports, by a version of each.
var gameLines = []string{"1.1.110", "2.0.77", "2.1.24"}

func readWorldConfig(t *testing.T, m *Manager, id, file string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(m.Dirs().Worlds, id, ConfigDir, file)) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// slotsOf reads autosave_slots; ok is false when the key is not there.
func slotsOf(t *testing.T, b []byte) (n int, ok bool) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	raw, ok := m["autosave_slots"]
	if !ok {
		return 0, false
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatal(err)
	}
	return n, true
}

// SPEC 11.1: a server-settings.json Foreman creates holds 20 autosave slots
// and passes the checks of the world's game line.
func TestGenerateCreatesServerSettingsWith20Slots(t *testing.T) {
	for _, version := range gameLines {
		m, _ := newTestManager(t)
		id, err := m.Generate(t.Context(), NewWorld{Name: " Fresh ", GameVersion: version})
		if err != nil {
			t.Fatal(err)
		}
		b := readWorldConfig(t, m, id, ServerSettingsFile)
		line, _ := mapgen.Line(version)
		if errs := gamesettings.Validate(gamesettings.FileServerSettings, line, b); len(errs) != 0 {
			t.Errorf("%s: %+v", version, errs)
		}
		if n, ok := slotsOf(t, b); !ok || n != 20 {
			t.Errorf("%s: autosave_slots = %d, %v; want 20", version, n, ok)
		}
		var name struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(b, &name); err != nil || name.Name != "Fresh" {
			t.Errorf("%s: name = %q, %v", version, name.Name, err)
		}
	}
}

func TestGenerateFromAFavoriteAddsSlotsOnlyWhenMissing(t *testing.T) {
	m, _ := newTestManager(t)
	without := `{"name": "Fav", "description": "", "visibility": {"public": false, "lan": true}, "max_players": 7}`
	id, err := m.Generate(t.Context(), NewWorld{Name: "A", GameVersion: "2.0.77", ServerSettings: []byte(without)})
	if err != nil {
		t.Fatal(err)
	}
	b := readWorldConfig(t, m, id, ServerSettingsFile)
	if n, ok := slotsOf(t, b); !ok || n != 20 {
		t.Errorf("favorite without slots: autosave_slots = %d, %v; want 20", n, ok)
	}
	if v, ok := gamesettings.ReadVisibility(b); !ok || v != gamesettings.VisibilityLAN {
		t.Errorf("the favorite's visibility was changed: %q %v", v, ok)
	}
	with := `{"name": "Fav", "description": "", "visibility": {"public": false, "lan": true}, "autosave_slots": 3}`
	id, err = m.Generate(t.Context(), NewWorld{Name: "B", GameVersion: "2.0.77", ServerSettings: []byte(with)})
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := slotsOf(t, readWorldConfig(t, m, id, ServerSettingsFile)); n != 3 {
		t.Errorf("favorite with slots: autosave_slots = %d, want its own 3", n)
	}
}

func TestImportSaveCreatesServerSettingsWith20Slots(t *testing.T) {
	m, _ := newTestManager(t)
	temp := worldsTemp(t, m, "uploaded")
	id, err := m.ImportSave(t.Context(), "Uploaded", "2.1.24", false, "up.zip", temp)
	if err != nil {
		t.Fatal(err)
	}
	b := readWorldConfig(t, m, id, ServerSettingsFile)
	if n, ok := slotsOf(t, b); !ok || n != 20 {
		t.Errorf("autosave_slots = %d, %v; want 20", n, ok)
	}
	if errs := gamesettings.Validate(gamesettings.FileServerSettings, gamesettings.Line21, b); len(errs) != 0 {
		t.Errorf("%+v", errs)
	}
}

// SPEC 11.1: an imported folder's own file is copied as it is, with no slots
// added; a folder with none gets the file Foreman creates.
func TestImportFolderKeepsItsOwnServerSettingsAndCreatesOneWhenMissing(t *testing.T) {
	m, _ := newTestManager(t)
	own := `{"name":"Mine","description":"","visibility":{"public":false,"lan":true}}`
	write(t, filepath.Join(m.dirs.Imports, "own", "saves", "a.zip"), "a", t0)
	write(t, filepath.Join(m.dirs.Imports, "own", "config", "server-settings.json"), own, t0)
	id, err := m.ImportFolder(t.Context(), "own", "Own", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(readWorldConfig(t, m, id, ServerSettingsFile)); got != own {
		t.Errorf("the folder's file was changed: %q", got)
	}
	write(t, filepath.Join(m.dirs.Imports, "bare", "saves", "a.zip"), "a", t0)
	id, err = m.ImportFolder(t.Context(), "bare", "Bare", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := slotsOf(t, readWorldConfig(t, m, id, ServerSettingsFile)); !ok || n != 20 {
		t.Errorf("a folder with no file: autosave_slots = %d, %v; want 20", n, ok)
	}
}
