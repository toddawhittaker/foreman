package world

import (
	"context"
	"errors"
	"io/fs"
)

// Refusals of SetNewMap.
var (
	// ErrMapMade means the world has a save, so its map exists and map
	// settings can no longer change it (SPEC 5.6).
	ErrMapMade = errors.New("this world already has a save, so its map is made and its map settings can no longer be changed")
	// ErrNotNewMap means the world is not waiting for its first map.
	ErrNotNewMap = errors.New("this world is not waiting for a new map, so it has no map settings to set")
	// ErrWorldArchived means an archived world cannot be changed (SPEC 5.9).
	ErrWorldArchived = errors.New("this world is archived. Unarchive it first")
)

// NewMapFiles is what step 3 of the guided flow saves (SPEC 5.6): the
// game's preset name and seed, and the two map settings files, complete
// under ADR 0010. Empty file data means no file, as for an unchanged
// preset.
type NewMapFiles struct {
	Preset         string
	Seed           *uint32
	MapGenSettings []byte
	MapSettings    []byte
}

// SetNewMap saves step 3 for a world that has new_map and no save: it
// writes or removes config/map-gen-settings.json and
// config/map-settings.json through the writer Generate uses, and sets the
// preset and seed in new_map. It takes the world's lock, so a busy world
// is refused with ErrBusy. It refuses a world with a save (ErrMapMade), one
// without new_map (ErrNotNewMap), an archived one (ErrWorldArchived) and
// one a newer Foreman wrote (ErrNewerFormat). It takes no snapshot, since
// the world has no save to lose (SPEC 5.6, 11.3). Nothing changes when it
// refuses.
func (m *Manager) SetNewMap(ctx context.Context, id string, files NewMapFiles) error {
	if err := checkMapFiles(files.Preset, files.MapGenSettings, files.MapSettings); err != nil {
		return err
	}
	unlock, err := m.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	meta, err := readChangeable(root)
	if err != nil {
		return err
	}
	if meta.Archived {
		return ErrWorldArchived
	}
	if _, found, err := newestSave(root); err != nil {
		return err
	} else if found {
		return ErrMapMade
	}
	if meta.NewMap == nil {
		return ErrNotNewMap
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := Mkdir(root, ConfigDir); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	// The files go first and world.json last, so a failure part way leaves
	// new_map as it was and a retry writes the same files again.
	if err := writeMapFiles(root, files.MapGenSettings, files.MapSettings); err != nil {
		return err
	}
	meta.NewMap = &NewMap{Preset: files.Preset, Seed: files.Seed}
	return WriteMeta(root, meta)
}

// unfinished is true when meta says the world waits for its first map
// and root holds no save: derived, never stored (SPEC 5.6).
func unfinished(meta Meta, hasSave bool) bool {
	return meta.NewMap != nil && !hasSave
}
