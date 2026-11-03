package world

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// PrefixRestoreOld names a live world moved aside during a restore's swap
// (R9). Startup recovery finishes or reverses the swap.
const PrefixRestoreOld = ".restore-old-"

// Restore puts a world back exactly as snapshot name recorded it, file
// contents and modification times both (SPEC 6.5, 6.9). It returns the
// "Before restore" snapshot, which is the undo.
//
// The restored world is built beside the live one first, then swapped in
// by two renames, so the path worlds/<id> only ever holds a whole world.
func (m *Manager) Restore(id, name, by string) (Snapshot, error) {
	unlock, err := m.Lock(id)
	if err != nil {
		return Snapshot{}, err
	}
	defer unlock()

	snap, err := m.Snapshot(id, name)
	if err != nil {
		return Snapshot{}, err
	}
	live, err := m.OpenWorld(id)
	if err != nil {
		return Snapshot{}, err
	}
	meta, err := ReadMeta(live)
	_ = live.Close()
	if err == nil && meta.Newer() {
		return Snapshot{}, ErrNewerFormat
	}
	if err := CheckSpace(m.dirs.Worlds, copyBytes(snap.Files)); err != nil {
		return Snapshot{}, err
	}

	worlds, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = worlds.Close() }()

	if err := m.checkFiles(id, snap); err != nil {
		return Snapshot{}, err
	}

	// The automatic snapshot comes first: staging sets times on zips that
	// share an inode with the live world (SPEC 6.2, 6.9).
	before, err := m.snapshot(id, ReasonBeforeRestore, LabelBeforeRestore, by)
	if err != nil {
		return Snapshot{}, fmt.Errorf("take the snapshot before restoring: %w", err)
	}
	staging := PrefixStaging + id + "-" + randomSuffix()
	if err := m.stage(worlds, staging, id, snap); err != nil {
		_ = worlds.RemoveAll(staging)
		return before, err
	}
	if err := swapIn(worlds, staging, id); err != nil {
		return before, err
	}
	// Retention runs last, so it can never remove the snapshot being
	// restored while its files are still needed.
	if err := m.prune(id); err != nil {
		return before, fmt.Errorf("remove old automatic snapshots: %w", err)
	}
	return before, nil
}

// checkFiles refuses a snapshot whose files no longer match its manifest,
// before anything is written.
func (m *Manager) checkFiles(id string, snap Snapshot) error {
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return err
	}
	defer func() { _ = snaps.Close() }()
	src, err := snaps.OpenRoot(snap.Name)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	for _, f := range snap.Files {
		if !fs.ValidPath(f.Path) || f.Path == "." || f.Path == ManifestFile {
			return fmt.Errorf("%w: bad path %q", ErrDamaged, f.Path)
		}
		info, err := src.Lstat(f.Path)
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %s is missing", ErrDamaged, f.Path)
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != f.Size {
			return fmt.Errorf("%w: %s has changed", ErrDamaged, f.Path)
		}
	}
	return nil
}

// stage builds the restored world in dir from the snapshot's files and sets
// every file's modification time from the manifest.
func (m *Manager) stage(worlds *os.Root, dir, id string, snap Snapshot) error {
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return err
	}
	defer func() { _ = snaps.Close() }()
	src, err := snaps.OpenRoot(snap.Name)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	if err := Mkdir(worlds, dir); err != nil {
		return err
	}
	dst, err := worlds.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()
	for _, sub := range []string{SavesDir, ModsDir, ConfigDir} {
		if err := Mkdir(dst, sub); err != nil {
			return err
		}
	}
	for _, f := range snap.Files {
		if _, err := placeFile(src, dst, f); err != nil {
			return fmt.Errorf("restore %s: %w", f.Path, err)
		}
		// The time goes on after placing: a hard-linked zip shares its time
		// with every other link, so the manifest is the record (SPEC 6.9).
		if err := dst.Chtimes(f.Path, f.ModTime, f.ModTime); err != nil {
			return err
		}
	}
	return syncDir(dst, ".")
}

// swapIn moves the live world aside, renames the staged one into its place,
// then removes the old one.
func swapIn(worlds *os.Root, staging, id string) error {
	old := PrefixRestoreOld + id + "-" + randomSuffix()
	if err := worlds.Rename(id, old); err != nil {
		_ = worlds.RemoveAll(staging)
		return err
	}
	if err := worlds.Rename(staging, id); err != nil {
		// Put the live world back; recovery does the same after a crash.
		if berr := worlds.Rename(old, id); berr != nil {
			return fmt.Errorf("swap in the restored world: %w; put the old one back: %w", err, berr)
		}
		_ = worlds.RemoveAll(staging)
		return err
	}
	if err := syncDir(worlds, "."); err != nil {
		return err
	}
	return worlds.RemoveAll(old)
}
