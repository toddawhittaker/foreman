package world

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// PrefixRestoreOld names a live world moved aside during a restore's swap
// (SPEC 6.5). Startup recovery finishes or reverses the swap.
const PrefixRestoreOld = ".restore-old-"

// ErrRestoreNameTaken is returned when restoring a deleted world whose name
// another world now has (SPEC 5.2).
var ErrRestoreNameTaken = errors.New("another world now has this world's name. Rename that world, then restore this snapshot")

// Restore puts a world back exactly as snapshot name recorded it, file
// contents and modification times both (SPEC 6.5, 6.9). It returns the
// "Before restore" snapshot, which is the undo. When the restore fails
// after that snapshot was taken, it is still returned with the error, so
// the admin can be told an undo exists.
//
// The restored world is built beside the live one first, then swapped in
// by two renames, so the path worlds/<id> only ever holds a whole world.
// Once the swap is done the restore has worked; cleanup that fails after
// it is logged, not returned.
//
// When the world's folder is gone (a deleted world) there is nothing to
// snapshot, so the returned snapshot is empty. The world is recreated and
// becomes active only if no world is.
func (m *Manager) Restore(ctx context.Context, id, name, by string) (Snapshot, error) {
	unlock, err := m.Lock(id)
	if err != nil {
		return Snapshot{}, err
	}
	defer unlock()
	return m.RestoreHeld(ctx, id, name, by)
}

// RestoreHeld is Restore for a caller that already holds Lock(id), such as
// server control restoring a running world: it stops the game, restores and
// starts it again under one lock (SPEC 6.5).
func (m *Manager) RestoreHeld(ctx context.Context, id, name, by string) (Snapshot, error) {
	snap, err := m.Snapshot(id, name)
	if err != nil {
		return Snapshot{}, err
	}
	live, err := m.OpenWorld(id)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, m.restoreDeleted(ctx, id, snap)
	}
	if err != nil {
		return Snapshot{}, err
	}
	_, err = readChangeable(live)
	_ = live.Close()
	if errors.Is(err, ErrNewerFormat) {
		return Snapshot{}, err
	}
	release, err := m.checkRestore(ctx, id, snap)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()

	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = worlds.Close() }()

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
	var old string
	err = m.commitStaged(ctx, worlds, staging, id, snap, func() (err error) {
		old, err = swapIn(worlds, staging, id)
		return err
	})
	if err != nil {
		return before, err
	}
	if err := SyncDir(worlds, "."); err != nil {
		m.log.Warn("restore: could not sync the worlds folder", "world", id, "error", err)
	}
	// Startup recovery removes the old copy if this fails.
	if err := worlds.RemoveAll(old); err != nil {
		m.log.Warn("restore: could not remove the replaced world", "world", id, "error", err)
	}
	// Retention runs last, so it can never remove the snapshot being
	// restored while its files are still needed.
	m.pruneOrWarn(id)
	return before, nil
}

// checkRestore runs every check a restore needs before it writes anything:
// the snapshot's world.json is not newer than this Foreman and its name is
// not another world's (SPEC 5.2), there is room for the copies (SPEC 6.11), and the
// snapshot's files match its manifest (SPEC 6.7). It takes no lock, so it never
// holds up other worlds; commitStaged checks the name again at the swap. On
// success the space stays reserved until release is called.
func (m *Manager) checkRestore(ctx context.Context, id string, snap Snapshot) (release func(), err error) {
	if err := m.checkRestoreName(ctx, id, snap); err != nil {
		return nil, err
	}
	release, err = Reserve(m.dirs.Worlds, copyBytes(snap.Files))
	if err != nil {
		return nil, err
	}
	if err := m.checkFiles(id, snap); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

// checkRestoreName refuses a snapshot written by a newer Foreman and one
// whose world name another world now has (SPEC 5.2).
func (m *Manager) checkRestoreName(ctx context.Context, id string, snap Snapshot) error {
	// A snapshot whose world.json cannot be read restores under the id.
	meta, err := m.snapshotMeta(id, snap.Name)
	if err != nil {
		return nil //nolint:nilerr // nothing to compare; the id is the name
	}
	if meta.Newer() {
		return ErrNewerFormat
	}
	if _, err := m.checkName(ctx, id, meta.Name); errors.Is(err, ErrNameTaken) {
		return ErrRestoreNameTaken
	} else if err != nil && !errors.Is(err, ErrNameEmpty) {
		return err
	}
	return nil
}

// commitStaged runs swap, which puts the staged world in place. The name is
// checked again first, with createMu held until swap is done, so no world
// made or renamed meanwhile can take the restored name (SPEC 5.2). The lock is
// held only for this step, not for the copying before it. A refused name
// removes the staged world.
func (m *Manager) commitStaged(ctx context.Context, worlds *os.Root, staging, id string, snap Snapshot, swap func() error) error {
	if m.afterStage != nil {
		m.afterStage()
	}
	m.createMu.Lock()
	defer m.createMu.Unlock()
	if err := m.checkRestoreName(ctx, id, snap); err != nil {
		_ = worlds.RemoveAll(staging)
		return err
	}
	return swap()
}

// restoreDeleted recreates a deleted world from a snapshot. The checks come
// first and nothing is written until they pass.
func (m *Manager) restoreDeleted(ctx context.Context, id string, snap Snapshot) error {
	release, err := m.checkRestore(ctx, id, snap)
	if err != nil {
		return err
	}
	defer release()
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return err
	}
	defer func() { _ = worlds.Close() }()
	staging := PrefixStaging + id + "-" + randomSuffix()
	if err := m.stage(worlds, staging, id, snap); err != nil {
		_ = worlds.RemoveAll(staging)
		return err
	}
	err = m.commitStaged(ctx, worlds, staging, id, snap, func() error {
		return worlds.Rename(staging, id)
	})
	if err != nil {
		_ = worlds.RemoveAll(staging)
		return err
	}
	if err := SyncDir(worlds, "."); err != nil {
		m.log.Warn("restore: could not sync the worlds folder", "world", id, "error", err)
	}
	return m.activateIfNone(ctx, id)
}

// checkFiles refuses a snapshot whose files no longer match its manifest,
// before anything is written.
func (m *Manager) checkFiles(id string, snap Snapshot) error {
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return err
	}
	defer func() { _ = snaps.Close() }()
	src, err := openDir(snaps, snap.Name)
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
		// Older snapshots have no hash, so there is nothing to check (SPEC 6.7).
		if f.SHA256 == "" {
			continue
		}
		sum, err := hashFile(src, f.Path)
		if err != nil {
			return err
		}
		if sum != f.SHA256 {
			return fmt.Errorf("%w: %s has changed since the snapshot was taken", ErrDamaged, f.Path)
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
	src, err := openDir(snaps, snap.Name)
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
	return SyncDir(dst, ".")
}

// swapIn moves the live world aside and renames the staged one into its
// place. It returns the old world's name for the caller to remove. On
// failure the live world is put back and the staged one removed.
func swapIn(worlds *os.Root, staging, id string) (old string, err error) {
	old = PrefixRestoreOld + id + "-" + randomSuffix()
	if err := worlds.Rename(id, old); err != nil {
		_ = worlds.RemoveAll(staging)
		return "", err
	}
	if err := worlds.Rename(staging, id); err != nil {
		// Put the live world back; recovery does the same after a crash.
		if berr := worlds.Rename(old, id); berr != nil {
			return "", fmt.Errorf("swap in the restored world: %w; put the old one back: %w", err, berr)
		}
		_ = worlds.RemoveAll(staging)
		return "", err
	}
	return old, nil
}
