package world

import (
	"context"
	"errors"
)

// ErrArchiveActive is returned when archiving the active world: it would
// leave a game that cannot start (SPEC 5.9).
var ErrArchiveActive = errors.New("this is the active world, and the active world cannot be archived. Switch to another world first")

// SetArchived archives or unarchives a world (SPEC 5.9). Only world.json
// changes, so no snapshot is taken, as for a rename. The active world
// cannot be archived; the check runs under the world's lock, which a
// switch holds too, so the world cannot become active meanwhile.
func (m *Manager) SetArchived(ctx context.Context, id string, on bool) error {
	unlock, err := m.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	w, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = w.Close() }()
	meta, err := readChangeable(w)
	if err != nil {
		return err
	}
	if on {
		active, _, err := m.active.ActiveWorld(ctx)
		if err != nil {
			return err
		}
		if active == id {
			return ErrArchiveActive
		}
	}
	if meta.Archived == on {
		return nil
	}
	meta.Archived = on
	return WriteMeta(w, meta)
}
