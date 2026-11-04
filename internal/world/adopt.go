package world

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"syscall"
)

// Adopt builds a world from the real saves, mods and config folders of an
// adopted game folder (SPEC 4.8, 5.1). game is the game folder opened as a
// root. Zips are hard-linked, everything else is copied, and the originals
// are never changed or moved: the caller does that only after Adopt has
// returned without an error. id comes from ReserveID. The caller holds
// Lock(id), the game is stopped, and the world is not made active here.
//
// The world is built in a staging folder and renamed into place, then a
// "Before adoption" snapshot is taken (SPEC 6.2). If the snapshot fails
// the new world is removed again, so a failed Adopt leaves the folder as
// it found it.
func (m *Manager) Adopt(ctx context.Context, game *os.Root, id, name, gameVersion string, spaceAge bool, by string) (Snapshot, error) {
	if !ValidGameVersion(gameVersion) {
		return Snapshot{}, ErrBadGameVersion
	}
	files, err := adoptionFiles(game)
	if err != nil {
		return Snapshot{}, err
	}
	// Zips are linked, so only the rest costs space (SPEC 6.11).
	release, err := Reserve(m.dirs.Worlds, copyBytes(files))
	if err != nil {
		return Snapshot{}, err
	}
	defer release()

	if _, err := m.build(ctx, id, name, gameVersion, spaceAge, func(worlds *os.Root, dir string) error {
		dst, err := worlds.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer func() { _ = dst.Close() }()
		for _, f := range files {
			if err := placeAdopted(game, dst, f); err != nil {
				return fmt.Errorf("adopt %s: %w", f.Path, err)
			}
		}
		return nil
	}); err != nil {
		return Snapshot{}, err
	}
	snap, err := m.AutoSnapshot(id, ReasonAdoption, LabelBeforeAdoption, by)
	if err != nil {
		if rerr := m.removeWorld(id); rerr != nil {
			err = errors.Join(err, rerr)
		}
		return Snapshot{}, fmt.Errorf("take the snapshot before adoption: %w", err)
	}
	return snap, nil
}

// adoptionFiles lists the files under the game folder's saves, mods and
// config, by path from the game folder. A missing folder adds nothing; one
// that is not a real folder is refused.
func adoptionFiles(game *os.Root) ([]ManifestEntry, error) {
	var all []ManifestEntry
	for _, sub := range []string{SavesDir, ModsDir, ConfigDir} {
		info, err := game.Lstat(sub)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s in the game folder is not a real folder, so there is nothing to adopt", sub)
		}
		files, err := filesUnder(game, sub)
		if err != nil {
			return nil, fmt.Errorf("list the files in %s: %w", sub, err)
		}
		all = slices.Concat(all, files)
	}
	return all, nil
}

// placeAdopted puts one file into the new world. A zip is hard-linked and
// copied only when the link crosses filesystems (EXDEV), as when saves is
// a separate mount. Any other link failure is an error: a silent copy of
// many gigabytes is not what the disk check promised.
func placeAdopted(src, dst *os.Root, f ManifestEntry) error {
	if !isZip(f.Path) {
		_, err := placeFile(src, dst, f)
		return err
	}
	if err := mkdirAll(dst, path.Dir(f.Path)); err != nil {
		return err
	}
	err := linkFile(src, dst, f.Path)
	if !errors.Is(err, syscall.EXDEV) {
		return err
	}
	release, err := Reserve(dst.Name(), uint64(f.Size)) //nolint:gosec // a file size is never negative
	if err != nil {
		return err
	}
	defer release()
	return copyFile(src, f.Path, dst, f.Path)
}
