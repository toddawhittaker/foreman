package world

import (
	"errors"
	"fmt"
)

// The automatic snapshot taken before a game version update (SPEC 6.2,
// 10.3). Rollback restores it (SPEC 10.4).
const (
	ReasonGameUpdate  = "game-update"
	LabelBeforeUpdate = "Before update"
)

// Update and rollback refusals. The text is shown to the admin.
var (
	ErrVersionNotNewer    = errors.New("the version to update to must be newer than the world's current version")
	ErrVersionUnsupported = errors.New("enter an exact Factorio release of 1.1, 2.0 or 2.1, such as 2.0.72")
	ErrNoRollback         = errors.New("there is no snapshot from before an update to an older version to roll back to")
)

// CheckUpdateTarget refuses a target version Foreman does not run, or one
// that is not newer than the world's (SPEC 10.3, 10.7).
func CheckUpdateTarget(meta Meta, target string) error {
	if !SupportedGameVersion(target) {
		return ErrVersionUnsupported
	}
	if CompareGameVersions(target, meta.GameVersion) <= 0 {
		return ErrVersionNotNewer
	}
	return nil
}

// SetGameVersionHeld pins the world to a game version (SPEC 10.3). The
// caller holds Lock(id) and has taken the snapshot the update needs.
func (m *Manager) SetGameVersionHeld(id, version string) error {
	if !SupportedGameVersion(version) {
		return ErrVersionUnsupported
	}
	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	meta, err := readChangeable(root)
	if err != nil {
		return err
	}
	meta.GameVersion = version
	return WriteMeta(root, meta)
}

// RollbackTarget finds the snapshot a rollback restores: the newest
// "Before update" snapshot whose world.json has a version older than the
// world's current one, and that version (SPEC 10.4).
func (m *Manager) RollbackTarget(id string) (Snapshot, string, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return Snapshot{}, "", err
	}
	meta, err := ReadMeta(root)
	_ = root.Close()
	if err != nil {
		return Snapshot{}, "", err
	}
	list, err := m.Snapshots(id)
	if err != nil {
		return Snapshot{}, "", err
	}
	for _, s := range list {
		if s.Reason != ReasonGameUpdate {
			continue
		}
		v, err := m.snapshotGameVersion(id, s.Name)
		if err != nil {
			m.log.Warn("skipped an update snapshot whose world.json cannot be read", "world", id, "snapshot", s.Name, "error", err)
			continue
		}
		if CompareGameVersions(v, meta.GameVersion) < 0 {
			return s, v, nil
		}
	}
	return Snapshot{}, "", ErrNoRollback
}

// snapshotGameVersion reads the game version from a snapshot's world.json.
func (m *Manager) snapshotGameVersion(id, name string) (string, error) {
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return "", err
	}
	defer func() { _ = snaps.Close() }()
	dir, err := openDir(snaps, name)
	if err != nil {
		return "", err
	}
	defer func() { _ = dir.Close() }()
	meta, err := ReadMeta(dir)
	if err != nil {
		return "", err
	}
	if !SupportedGameVersion(meta.GameVersion) {
		return "", fmt.Errorf("snapshot %s has game version %q", name, meta.GameVersion)
	}
	return meta.GameVersion, nil
}
