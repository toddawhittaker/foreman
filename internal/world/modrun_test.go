package world

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// modRunWorld is a stopped world with one mod and a save.
func modRunWorld(t *testing.T) (*Manager, string, string) {
	t.Helper()
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.1", "2.0")
	writeAt(t, filepath.Join(dir, SavesDir, "a.zip"), "save", t0)
	return m, id, dir
}

// stoppedChange is one mod change on a stopped world, as the game layer
// makes it: the snapshot, the change, then the record of the run.
func stoppedChange(t *testing.T, m *Manager, id string, change ModChange) Snapshot {
	t.Helper()
	s, err := m.ModSnapshotHeld(id, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); err != nil {
		t.Fatal(err)
	}
	m.ContinueModRunHeld(id, s)
	return s
}

func modSnapshots(t *testing.T, m *Manager, id string) []Snapshot {
	t.Helper()
	list, err := m.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []Snapshot
	for _, s := range list {
		if s.Reason == ReasonMods {
			out = append(out, s)
		}
	}
	return out
}

func settingsChange(body string) ModChange { return ModChange{Settings: []byte(body)} }

func TestThreeStoppedModChangesTakeOneSnapshotAndUndoGoesBeforeTheFirst(t *testing.T) {
	m, id, dir := modRunWorld(t)
	first := stoppedChange(t, m, id, settingsChange("one"))
	if s := stoppedChange(t, m, id, ModChange{Remove: []string{"flib_0.16.1.zip"}}); s.Name != first.Name {
		t.Fatalf("second change used %s, want %s", s.Name, first.Name)
	}
	if s := stoppedChange(t, m, id, settingsChange("three")); s.Name != first.Name {
		t.Fatalf("third change used %s, want %s", s.Name, first.Name)
	}
	if got := modSnapshots(t, m, id); len(got) != 1 {
		t.Fatalf("%d mod snapshots, want 1", len(got))
	}
	run, ok := m.ModRunSnapshot(id)
	if !ok || run.Name != first.Name {
		t.Fatalf("run snapshot %v %v", run.Name, ok)
	}
	if _, err := m.Restore(t.Context(), id, first.Name, "alice"); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(dir, ModsDir, ModSettingsFile)) || !exists(filepath.Join(dir, ModsDir, "flib_0.16.1.zip")) {
		t.Fatal("undo did not return the world to before the first change")
	}
	if _, ok := m.ModRunSnapshot(id); ok {
		t.Fatal("a restore must end the run")
	}
}

func TestALaunchEndsTheRun(t *testing.T) {
	m, id, _ := modRunWorld(t)
	first := stoppedChange(t, m, id, settingsChange("one"))
	if _, err := m.PrepareLoad(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.ModRunSnapshot(id); ok {
		t.Fatal("run still open after a launch")
	}
	if s := stoppedChange(t, m, id, settingsChange("two")); s.Name == first.Name {
		t.Fatal("change after a launch reused the old snapshot")
	}
}

func TestAnyOtherFileChangeEndsTheRun(t *testing.T) {
	for name, edit := range map[string]func(dir string){
		"save written":    func(dir string) { writeAt(t, filepath.Join(dir, SavesDir, "a.zip"), "later", t0.Add(time.Hour)) },
		"save added":      func(dir string) { writeAt(t, filepath.Join(dir, SavesDir, "b.zip"), "new", t0) },
		"settings change": func(dir string) { writeAt(t, filepath.Join(dir, ConfigDir, "server-settings.json"), "{}", t0) },
	} {
		t.Run(name, func(t *testing.T) {
			m, id, dir := modRunWorld(t)
			first := stoppedChange(t, m, id, settingsChange("one"))
			edit(dir)
			if _, ok := m.ModRunSnapshot(id); ok {
				t.Fatal("run still open")
			}
			if s := stoppedChange(t, m, id, settingsChange("two")); s.Name == first.Name {
				t.Fatal("reused the old snapshot")
			}
		})
	}
}

func TestADeletedSnapshotEndsTheRun(t *testing.T) {
	m, id, _ := modRunWorld(t)
	first := stoppedChange(t, m, id, settingsChange("one"))
	if err := os.RemoveAll(filepath.Join(m.Dirs().Snapshots, id, first.Name)); err != nil {
		t.Fatal(err)
	}
	// The test clock is fixed, so the new snapshot may reuse the name; what
	// matters is that one exists again.
	stoppedChange(t, m, id, settingsChange("two"))
	if got := modSnapshots(t, m, id); len(got) != 1 {
		t.Fatalf("%d mod snapshots, want 1", len(got))
	}
}

func TestRetentionRemovingTheSnapshotEndsTheRun(t *testing.T) {
	m, id, _ := modRunWorld(t)
	// Snapshots sort by time, so each one needs its own.
	clock := t0
	m.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	stoppedChange(t, m, id, settingsChange("one"))
	for range DefaultKeepAutomatic {
		if _, err := m.AutoSnapshot(id, ReasonSettings, LabelBeforeSettings, "bob"); err != nil {
			t.Fatal(err)
		}
	}
	if len(modSnapshots(t, m, id)) != 0 {
		t.Fatal("retention kept the mod snapshot; the test needs it gone")
	}
	stoppedChange(t, m, id, settingsChange("two"))
	if len(modSnapshots(t, m, id)) != 1 {
		t.Fatal("no new snapshot after retention removed the run's")
	}
}

func TestAMissingOrUnreadableMarkerTakesANewSnapshot(t *testing.T) {
	for name, body := range map[string]string{"missing": "", "garbage": "{not json", "other snapshot": `{"snapshot":"20200101T000000Z","files":[]}`} {
		t.Run(name, func(t *testing.T) {
			m, id, _ := modRunWorld(t)
			first := stoppedChange(t, m, id, settingsChange("one"))
			p := filepath.Join(m.Dirs().Snapshots, id, modRunFile)
			if body == "" {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if s := stoppedChange(t, m, id, settingsChange("two")); s.Name == first.Name {
				t.Fatal("reused the snapshot without a good marker")
			}
		})
	}
}

func TestARunningWorldSnapshotsEveryChange(t *testing.T) {
	m, id, _ := modRunWorld(t)
	first := stoppedChange(t, m, id, settingsChange("one"))
	s, err := m.ModSnapshotHeld(id, "alice", true)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name == first.Name {
		t.Fatal("a running world reused the snapshot")
	}
	if _, ok := m.ModRunSnapshot(id); ok {
		t.Fatal("a running change must end the run")
	}
}

// The players record is Foreman's own file and a read of the save can write
// it between two mod changes. It is not a mod change, so it must neither
// break the run nor cost the next change a snapshot (SPEC 8.14, 9.4).
func TestAPlayersRecordWrittenBetweenModChangesKeepsTheRun(t *testing.T) {
	m, id, dir := modRunWorld(t)
	first := stoppedChange(t, m, id, settingsChange("one"))

	if err := m.WritePlayersRecord(id, []byte(`{"format_version":1}`)); err != nil {
		t.Fatal(err)
	}
	run, ok := m.ModRunSnapshot(id)
	if !ok || run.Name != first.Name {
		t.Fatalf("a new players record ended the run: %q %v", run.Name, ok)
	}
	if s := stoppedChange(t, m, id, settingsChange("two")); s.Name != first.Name {
		t.Fatalf("second change used %s, want %s", s.Name, first.Name)
	}

	// A later rewrite and a removal of the record are no different.
	if err := m.WritePlayersRecord(id, []byte(`{"format_version":1,"inexact":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := m.RemovePlayersRecord(id); err != nil {
		t.Fatal(err)
	}
	if s := stoppedChange(t, m, id, settingsChange("three")); s.Name != first.Name {
		t.Fatalf("third change used %s, want %s", s.Name, first.Name)
	}
	if got := modSnapshots(t, m, id); len(got) != 1 {
		t.Fatalf("%d mod snapshots, want 1", len(got))
	}
	if !exists(filepath.Join(dir, ModsDir, ModSettingsFile)) {
		t.Fatal("the changes did not land")
	}
}

// Leaving the record out must not blind the run to a real change.
func TestAPlayersRecordDoesNotHideARealChangeFromTheRun(t *testing.T) {
	m, id, dir := modRunWorld(t)
	stoppedChange(t, m, id, settingsChange("one"))
	if err := m.WritePlayersRecord(id, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	writeAt(t, filepath.Join(dir, SavesDir, "b.zip"), "newer save", t0.Add(time.Hour))
	if _, ok := m.ModRunSnapshot(id); ok {
		t.Fatal("a new save must still end the run")
	}
}
