package world

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
)

// ErrActive is returned when deleting the active world (SPEC 5.10).
var ErrActive = errors.New("this is the active world, and the active world cannot be deleted")

// ErrUnreadable is returned when a world's world.json cannot be read. Fail
// safe: the world is not deleted.
var ErrUnreadable = errors.New("this world's world.json cannot be read, so Foreman will not delete it")

// ErrWrongName is returned when the name typed to confirm a delete does not
// match the world's name.
var ErrWrongName = errors.New("the name typed does not match the world's name")

// Delete removes a world's folder (SPEC 5.10). typedName must equal the
// world's name exactly. With finalSnapshot set, the snapshot comes first
// (SPEC 6.2): if it fails the world is left untouched. The world's earlier
// snapshots stay, and so does the folder snapshots/<id>, so the id is never
// handed to another world (R2).
func (m *Manager) Delete(ctx context.Context, id, typedName string, finalSnapshot bool, by string) error {
	unlock, err := m.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	w, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	meta, err := readChangeable(w)
	_ = w.Close()
	if errors.Is(err, ErrNewerFormat) {
		return err
	}
	if err != nil {
		return ErrUnreadable
	}
	name := displayName(id, meta, nil)
	activeID, _, err := m.active.ActiveWorld(ctx)
	if err != nil {
		return err
	}
	if activeID == id {
		return ErrActive
	}
	if typedName != name {
		return ErrWrongName
	}

	if finalSnapshot {
		if _, err := m.AutoSnapshot(id, ReasonBeforeDelete, LabelBeforeDelete, by); err != nil {
			return fmt.Errorf("take the final snapshot: %w", err)
		}
	} else if err := m.keepID(id); err != nil {
		return err
	}
	return m.removeWorld(id)
}

// keepID makes snapshots/<id> if it is missing, so the id stays taken.
func (m *Manager) keepID(id string) error {
	snaps, err := m.openSnapshots(id, true)
	if err != nil {
		return err
	}
	return snaps.Close()
}

// removeWorld renames the folder out of sight and removes it. A crash
// between the two leaves a .deleting-* folder that startup recovery clears.
func (m *Manager) removeWorld(id string) error {
	worlds, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		return err
	}
	defer func() { _ = worlds.Close() }()
	gone := PrefixDeleting + id + "-" + randomSuffix()
	if err := worlds.Rename(id, gone); err != nil {
		return err
	}
	if err := syncDir(worlds, "."); err != nil {
		return err
	}
	return worlds.RemoveAll(gone)
}

// DeletedWorld is a world whose folder is gone but whose snapshots remain.
type DeletedWorld struct {
	ID string
	// Name is the world's name in its newest snapshot, or the id when that
	// cannot be read.
	Name      string
	Snapshots []Snapshot
}

// Deleted lists the worlds that have snapshots but no folder, by name
// ignoring case (SPEC 6.6).
func (m *Manager) Deleted() ([]DeletedWorld, error) {
	all, err := os.OpenRoot(m.dirs.Snapshots)
	if err != nil {
		return nil, err
	}
	defer func() { _ = all.Close() }()
	entries, err := fs.ReadDir(all.FS(), ".")
	if err != nil {
		return nil, err
	}
	worlds, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = worlds.Close() }()
	var out []DeletedWorld
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		id := e.Name()
		if _, err := worlds.Lstat(id); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		list, err := m.Snapshots(id)
		if err != nil {
			return nil, err
		}
		if len(list) == 0 {
			continue
		}
		out = append(out, DeletedWorld{ID: id, Name: m.snapshotWorldName(id, list[0]), Snapshots: list})
	}
	slices.SortFunc(out, func(a, b DeletedWorld) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

// SnapshotWorldName returns the world's name as snapshot name recorded it,
// or the id when that cannot be read.
func (m *Manager) SnapshotWorldName(id, name string) string {
	snap, err := m.Snapshot(id, name)
	if err != nil {
		return id
	}
	return m.snapshotWorldName(id, snap)
}

func (m *Manager) snapshotWorldName(id string, snap Snapshot) string {
	meta, err := m.snapshotMeta(id, snap.Name)
	return displayName(id, meta, err)
}

// snapshotMeta reads the world.json a snapshot holds.
func (m *Manager) snapshotMeta(id, name string) (Meta, error) {
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return Meta{}, err
	}
	defer func() { _ = snaps.Close() }()
	src, err := openDir(snaps, name)
	if err != nil {
		return Meta{}, err
	}
	defer func() { _ = src.Close() }()
	return ReadMeta(src)
}
