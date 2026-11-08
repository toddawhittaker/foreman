package world

import (
	"encoding/json"
	"errors"
	"io/fs"
	"slices"
)

// modRunFile sits in snapshots/<id>, beside the snapshot folders, so it is
// never part of a snapshot or of the world.
const modRunFile = "mod-run.json"

// modRun records an open run of mod changes on a stopped world (SPEC 9.4,
// ADR 0014): the snapshot taken before its first change, and the world's
// files as the last change left them.
type modRun struct {
	Snapshot string          `json:"snapshot"`
	Files    []ManifestEntry `json:"files"`
}

// ModSnapshotHeld gives the snapshot a mod change needs (SPEC 9.4). On a
// stopped world whose run of mod changes is still open, it is the snapshot
// taken before the run's first change, and nothing new is taken. Otherwise
// it takes a new "Before mod change" snapshot. The caller holds Lock(id),
// has stopped the game, and must not change the mods if this fails.
func (m *Manager) ModSnapshotHeld(id, by string, running bool) (Snapshot, error) {
	if !running {
		if s, ok := m.ModRunSnapshot(id); ok {
			return s, nil
		}
	}
	m.endModRun(id)
	return m.AutoSnapshot(id, ReasonMods, LabelBeforeMods, by)
}

// ContinueModRunHeld records that a mod change on a stopped world worked,
// so the next one may reuse snap. A failure to record only costs the next
// change a fresh snapshot, so it is logged.
func (m *Manager) ContinueModRunHeld(id string, snap Snapshot) {
	if err := m.writeModRun(id, snap); err != nil {
		m.log.Warn("could not record the run of mod changes", "world", id, "error", err)
		m.endModRun(id)
	}
}

func (m *Manager) writeModRun(id string, snap Snapshot) error {
	w, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	files, err := worldFiles(w)
	if err != nil {
		return err
	}
	b, err := json.Marshal(modRun{Snapshot: snap.Name, Files: files})
	if err != nil {
		return err
	}
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return err
	}
	defer func() { _ = snaps.Close() }()
	return WriteFileAtomic(snaps, modRunFile, b)
}

// ModRunSnapshot returns the snapshot that undoes every mod change of the
// world's open run, and whether a run is open (SPEC 9.4). Its TakenAt is
// the time before the first change, and its Files list the mod zips the
// world had then. Whenever Foreman cannot be sure the run still holds, it
// reports none: the marker is missing or unreadable, the snapshot is gone
// or not a mod snapshot, or the world's files changed since the last mod
// change (a game ran, a save was added, settings or a restore).
func (m *Manager) ModRunSnapshot(id string) (Snapshot, bool) {
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return Snapshot{}, false
	}
	defer func() { _ = snaps.Close() }()
	b, err := snaps.ReadFile(modRunFile)
	if err != nil {
		return Snapshot{}, false
	}
	var run modRun
	if json.Unmarshal(b, &run) != nil || !ValidSnapshotName(run.Snapshot) {
		return Snapshot{}, false
	}
	man, err := readManifest(snaps, run.Snapshot)
	if err != nil || man.Reason != ReasonMods {
		return Snapshot{}, false
	}
	w, err := m.OpenWorld(id)
	if err != nil {
		return Snapshot{}, false
	}
	defer func() { _ = w.Close() }()
	files, err := worldFiles(w)
	if err != nil || !slices.EqualFunc(files, run.Files, sameEntry) {
		return Snapshot{}, false
	}
	return Snapshot{Name: run.Snapshot, Manifest: man}, true
}

func sameEntry(a, b ManifestEntry) bool {
	return a.Path == b.Path && a.Size == b.Size && a.ModTime.Equal(b.ModTime)
}

// endModRun closes the world's run of mod changes. A marker left behind by
// a failure here still fails the files check once anything changes.
func (m *Manager) endModRun(id string) {
	snaps, err := m.openSnapshots(id, false)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err == nil {
		err = snaps.Remove(modRunFile)
		_ = snaps.Close()
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.log.Warn("could not end the run of mod changes", "world", id, "error", err)
	}
}
