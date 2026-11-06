package world

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func settingsTestWorld(t *testing.T) (*Manager, string) {
	t.Helper()
	m, _ := newTestManager(t)
	id, err := m.Create(context.Background(), "Settings", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.Dirs().Worlds, id, ConfigDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for f, body := range map[string]string{ServerSettingsFile: `{"name": "old"}`, ServerAdminListFile: `["a"]`} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o640); err != nil { //nolint:gosec // a test temp folder
			t.Fatal(err)
		}
	}
	return m, id
}

func seenHash(t *testing.T, m *Manager, id, file string) string {
	t.Helper()
	r, err := m.ReadConfig(id, file)
	if err != nil {
		t.Fatal(err)
	}
	return r.Hash
}

func settingsSnapshotCount(t *testing.T, m *Manager, id string) int {
	t.Helper()
	snaps, err := m.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range snaps {
		if s.Reason == ReasonSettings {
			n++
		}
	}
	return n
}

// SPEC 11.3: a change of several files takes one snapshot and one audit
// call, and the audit call comes before any file is written.
func TestSaveSettingsHeldTakesOneSnapshotAndOneAuditForManyFiles(t *testing.T) {
	m, id := settingsTestWorld(t)
	changes := []ConfigChange{
		{File: ServerSettingsFile, Data: []byte(`{"name": "new"}`), SeenHash: seenHash(t, m, id, ServerSettingsFile)},
		{File: ServerAdminListFile, Data: []byte(`["a","b"]`), SeenHash: seenHash(t, m, id, ServerAdminListFile)},
	}
	audits := 0
	snap, err := m.SaveSettingsHeld(id, changes, "todd", func() error {
		audits++
		if r, _ := m.ReadConfig(id, ServerSettingsFile); string(r.Data) != `{"name": "old"}` {
			t.Error("a file was written before the audit entry")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if audits != 1 || snap.Name == "" || settingsSnapshotCount(t, m, id) != 1 {
		t.Errorf("audits %d, snapshot %q, settings snapshots %d", audits, snap.Name, settingsSnapshotCount(t, m, id))
	}
	for _, c := range changes {
		if r, _ := m.ReadConfig(id, c.File); string(r.Data) != string(c.Data) {
			t.Errorf("%s holds %q", c.File, r.Data)
		}
	}
}

// SPEC 11.3: one file out of date refuses the whole change before the
// snapshot, the audit entry or any write.
func TestSaveSettingsHeldRefusesAllWhenOneFileMoved(t *testing.T) {
	m, id := settingsTestWorld(t)
	changes := []ConfigChange{
		{File: ServerSettingsFile, Data: []byte(`{"name": "new"}`), SeenHash: seenHash(t, m, id, ServerSettingsFile)},
		{File: ServerAdminListFile, Data: []byte(`["a","b"]`), SeenHash: "stale"},
	}
	_, err := m.SaveSettingsHeld(id, changes, "todd", func() error {
		t.Error("audited")
		return nil
	})
	if !errors.Is(err, ErrSettingsChanged) {
		t.Fatalf("error %v, want ErrSettingsChanged", err)
	}
	if r, _ := m.ReadConfig(id, ServerSettingsFile); string(r.Data) != `{"name": "old"}` {
		t.Errorf("the settings file was written: %q", r.Data)
	}
	if settingsSnapshotCount(t, m, id) != 0 {
		t.Error("a snapshot was taken")
	}
}

// SPEC 12.9: when the audit entry cannot be recorded, nothing is written.
func TestSaveSettingsHeldWritesNothingWhenTheAuditFails(t *testing.T) {
	m, id := settingsTestWorld(t)
	changes := []ConfigChange{{File: ServerSettingsFile, Data: []byte(`{}`), SeenHash: seenHash(t, m, id, ServerSettingsFile)}}
	boom := errors.New("audit down")
	if _, err := m.SaveSettingsHeld(id, changes, "todd", func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("error %v", err)
	}
	if r, _ := m.ReadConfig(id, ServerSettingsFile); string(r.Data) != `{"name": "old"}` {
		t.Errorf("written: %q", r.Data)
	}
}

// SPEC 11.1: a NoWrite file is checked and covered by the snapshot but left
// as it is.
func TestSaveSettingsHeldLeavesANoWriteFileAlone(t *testing.T) {
	m, id := settingsTestWorld(t)
	changes := []ConfigChange{
		{File: ServerAdminListFile, Data: []byte(`["z"]`), SeenHash: seenHash(t, m, id, ServerAdminListFile), NoWrite: true},
	}
	if _, err := m.SaveSettingsHeld(id, changes, "todd", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if r, _ := m.ReadConfig(id, ServerAdminListFile); string(r.Data) != `["a"]` {
		t.Errorf("written: %q", r.Data)
	}
	changes[0].SeenHash = "stale"
	if _, err := m.SaveSettingsHeld(id, changes, "todd", func() error { return nil }); !errors.Is(err, ErrSettingsChanged) {
		t.Errorf("a NoWrite file's hash was not checked: %v", err)
	}
}
