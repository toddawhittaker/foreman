package world

import (
	"path/filepath"
	"testing"
)

// SPEC 6.7, 8.14: Foreman rewrites players.json by itself (a session ends, a
// read of the save finishes) without the world's lock, so it can change
// between the snapshot's listing and its copy. The manifest must describe the
// copy it holds, or the snapshot can never be restored ("players.json has
// changed"), as a rollback right after an update found.
func TestSnapshotOfAFileChangedAfterTheListingCanBeRestored(t *testing.T) {
	m, _ := newTestManager(t)
	id, dir := madeWorld(t, m)
	p := filepath.Join(dir, PlayersFile)
	writeAt(t, p, `{"short":1}`, t0)

	w, err := m.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Close() }()
	files, err := worldFiles(w)
	if err != nil {
		t.Fatal(err)
	}
	// The listing saw one record; then a longer one replaced it.
	writeAt(t, p, `{"a longer record":12345}`, t0.Add(1))

	snaps, err := m.openSnapshots(id, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = snaps.Close() }()
	name := "2026-11-03T14-25-01Z"
	man := Manifest{Files: files, TakenAt: t0}
	if err := m.fillSnapshot(w, snaps, name, &man); err != nil {
		t.Fatal(err)
	}
	if err := m.checkFiles(id, Snapshot{Name: name, Manifest: man}); err != nil {
		t.Errorf("the snapshot cannot be restored: %v", err)
	}
	for _, f := range man.Files {
		if f.Path == PlayersFile && (f.Size != int64(len(`{"a longer record":12345}`)) || !f.ModTime.Equal(t0.Add(1))) {
			t.Errorf("manifest has %s as %d bytes at %v, want the copy's size and time", f.Path, f.Size, f.ModTime)
		}
	}
}
