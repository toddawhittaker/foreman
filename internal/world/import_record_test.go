package world

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// inboxEntry returns the inbox folder named name, failing when it is missing.
func inboxEntry(t *testing.T, m *Manager, name string) InboxFolder {
	t.Helper()
	list, err := m.Inbox(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range list {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no inbox folder %s in %+v", name, list)
	return InboxFolder{}
}

// SPEC 5.5: world.json records the folder and its newest save, to the
// nanosecond, and the Import page counts the folder as already imported
// only while that save is unchanged and still the newest.
func TestImportRecordsWhereTheWorldCameFrom(t *testing.T) {
	m, _ := newTestManager(t)
	src := seedInbox(t, m)
	later := filepath.Join(src, "later.zip")
	mtime := time.Date(2025, 6, 1, 13, 0, 0, 123456789, time.UTC)
	if err := os.Chtimes(later, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if f := inboxEntry(t, m, "saves-2025"); f.Imported != nil {
		t.Fatalf("not yet imported, but Imported = %+v", f.Imported)
	}
	id, err := m.ImportFolder(context.Background(), "saves-2025", "Old game", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(m.dirs.Worlds, id, MetaFile)) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		ImportedFrom map[string]any `json:"imported_from"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"folder": "saves-2025", "save": "later.zip", "save_size": float64(9),
		"save_mtime": "2025-06-01T13:00:00.123456789Z", "at": t0.Format(time.RFC3339)}
	for k, v := range want {
		if doc.ImportedFrom[k] != v {
			t.Errorf("imported_from.%s = %v, want %v", k, doc.ImportedFrom[k], v)
		}
	}
	if sum, err := m.Get(t.Context(), id); err != nil || sum.ImportedFrom == nil || sum.ImportedFrom.Folder != "saves-2025" {
		t.Errorf("Get(%s).ImportedFrom = %+v, %v", id, sum.ImportedFrom, err)
	}

	f := inboxEntry(t, m, "saves-2025")
	if f.Imported == nil || f.Imported.WorldID != id || f.Imported.WorldName != "Old game" || !f.Imported.At.Equal(t0) {
		t.Fatalf("Imported = %+v, want world %s", f.Imported, id)
	}

	// One nanosecond later is a different save.
	moved := mtime.Add(time.Nanosecond)
	if err := os.Chtimes(later, moved, moved); err != nil {
		t.Fatal(err)
	}
	if f := inboxEntry(t, m, "saves-2025"); f.Imported != nil {
		t.Errorf("save changed, but Imported = %+v", f.Imported)
	}
	if err := os.Chtimes(later, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	// A newer save makes the folder wait to be imported again.
	write(t, filepath.Join(src, "newest.zip"), "third", mtime.Add(time.Hour))
	if f := inboxEntry(t, m, "saves-2025"); f.Imported != nil {
		t.Errorf("newer save, but Imported = %+v", f.Imported)
	}
}

func TestImportedFromSurvivesARewrite(t *testing.T) {
	from := &ImportedFrom{Folder: "f", Save: "s.zip", SaveSize: 3, SaveMtime: time.Date(2025, 1, 2, 3, 4, 5, 6, time.UTC), At: t0}
	b, err := json.Marshal(Meta{FormatVersion: 1, Name: "W", ImportedFrom: from})
	if err != nil {
		t.Fatal(err)
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.ImportedFrom == nil || *m.ImportedFrom != *from {
		t.Errorf("read back %+v, want %+v", m.ImportedFrom, from)
	}
	if m.unknown != nil {
		t.Errorf("imported_from kept as unknown: %v", m.unknown)
	}
	b, _ = json.Marshal(Meta{FormatVersion: 1, Name: "W"})
	if strings.Contains(string(b), "imported_from") {
		t.Errorf("a world not imported writes imported_from: %s", b)
	}
}

func TestImportSuggestsTheServerName(t *testing.T) {
	m, _ := newTestManager(t)
	src := seedInbox(t, m)
	if got := inboxEntry(t, m, "saves-2025").SuggestedName(); got != "saves-2025" {
		t.Errorf("no settings: suggested %q, want the folder name", got)
	}
	settings := filepath.Join(src, ConfigDir, ServerSettingsFile)
	write(t, settings, `{"name":"  Friday factory ","game_password":"hunter2"}`, t0)
	if got := inboxEntry(t, m, "saves-2025").SuggestedName(); got != "Friday factory" {
		t.Errorf("suggested %q, want the server name", got)
	}
	write(t, settings, `not json`, t0)
	if got := inboxEntry(t, m, "saves-2025").SuggestedName(); got != "saves-2025" {
		t.Errorf("broken settings: suggested %q, want the folder name", got)
	}
	write(t, settings, `{"name":"`+strings.Repeat("x", 101)+`"}`, t0)
	if got := inboxEntry(t, m, "saves-2025").SuggestedName(); got != "saves-2025" {
		t.Errorf("too long: suggested %q, want the folder name", got)
	}
}

// Factorio 1.1 has no Space Age, so a 1.1 world with it on is refused.
func TestImportRefusesSpaceAgeBefore2(t *testing.T) {
	m, _ := newTestManager(t)
	seedInbox(t, m)
	if _, err := m.ImportFolder(context.Background(), "saves-2025", "Old", "1.1.110", true); !errors.Is(err, ErrSpaceAgeOld) {
		t.Errorf("ImportFolder = %v, want ErrSpaceAgeOld", err)
	}
	assertNoStaging(t, m)
	if _, err := m.ImportFolder(context.Background(), "saves-2025", "Old", "1.1.110", false); err != nil {
		t.Errorf("without Space Age: %v", err)
	}
}
