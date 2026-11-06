package world

import (
	"context"
	"errors"
	"os"
	"path"
	"slices"
	"time"
)

// ErrCloneNoSave is returned when a clone is asked to keep a save and the
// source has none.
var ErrCloneNoSave = errors.New("this world has no save to keep. Choose a fresh map instead")

// loadedSave returns the save the game would load from a world: the one
// world.json chooses, else the newest (SPEC 7.11). found is false when the
// world has none. A chosen save that is gone is ErrSaveNotFound.
func loadedSave(root *os.Root, meta Meta) (save Save, found bool, err error) {
	saves, err := listSaves(root)
	if err != nil {
		return Save{}, false, err
	}
	if meta.LoadSave == "" {
		if len(saves) == 0 {
			return Save{}, false, nil
		}
		return saves[0], true, nil
	}
	for _, s := range saves {
		if s.Name == meta.LoadSave {
			return s, true, nil
		}
	}
	return Save{}, false, ErrSaveNotFound
}

// CloneSource names the save "keep the save" would copy from a world, and
// its modification time, for the page to show before it clones (SPEC 5.7).
// name is empty when the world has no save. It changes nothing.
func (m *Manager) CloneSource(srcID string) (name string, modTime time.Time, err error) {
	root, err := m.OpenWorld(srcID)
	if err != nil {
		return "", time.Time{}, err
	}
	defer func() { _ = root.Close() }()
	meta, err := ReadMeta(root)
	if err != nil {
		return "", time.Time{}, err
	}
	save, found, err := loadedSave(root, meta)
	if err != nil || !found {
		return "", time.Time{}, err
	}
	return save.Name, save.ModTime, nil
}

// Clone makes a new world from srcID: its mods folder (zips, mod-list.json,
// mod-settings.dat) and config files (never rconpw), with the same game
// version and Space Age flag (SPEC 5.7). With keepSave it also copies the
// save that would load and records it as the new world's load_save. Without,
// the new world has no save and its first start makes a map with seed (nil
// lets the game choose); seed is ignored when keepSave is set. The source is
// left as it was. It refuses a source a newer Foreman wrote, and one that
// is busy.
func (m *Manager) Clone(ctx context.Context, srcID, name string, keepSave bool, seed *uint32) (string, error) {
	unlock, err := m.Lock(srcID)
	if err != nil {
		return "", err
	}
	defer unlock()

	src, err := m.OpenWorld(srcID)
	if err != nil {
		return "", err
	}
	defer func() { _ = src.Close() }()
	meta, err := readChangeable(src)
	if err != nil {
		return "", err
	}
	if err := m.checkNew(ctx, name, meta.GameVersion, meta.SpaceAge); err != nil {
		return "", err
	}

	_, mods, config, err := importPlan(src)
	if err != nil {
		return "", err
	}
	files := slices.Concat(mods, config)
	opts := createOptions{}
	if keepSave {
		save, found, err := loadedSave(src, meta)
		if err != nil {
			return "", err
		}
		if !found {
			return "", ErrCloneNoSave
		}
		opts.loadSave = save.Name
		files = append(files, importFile{path: path.Join(SavesDir, save.Name), size: uint64(save.Size)}) //nolint:gosec // a file size is never negative
	} else {
		opts.newMap = &NewMap{Seed: seed}
	}
	var need uint64
	for _, f := range files {
		need += f.size
	}
	release, err := Reserve(m.dirs.Worlds, need)
	if err != nil {
		return "", err
	}
	defer release()

	return m.create(ctx, name, meta.GameVersion, meta.SpaceAge, opts, func(worlds *os.Root, dir string) error {
		dst, err := worlds.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer func() { _ = dst.Close() }()
		for _, f := range files {
			if err := copyFile(src, f.path, dst, f.path); err != nil {
				return err
			}
		}
		return nil
	})
}
