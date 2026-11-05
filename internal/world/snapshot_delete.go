package world

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"syscall"
	"time"
)

// DeleteSnapshot removes one snapshot of a world (SPEC 6.12). Only that
// snapshot's folder goes; a zip it shares by hard link with the world or
// another snapshot stays there. It refuses while another operation holds
// the world. before runs once the snapshot is found and the world is held,
// so the caller can record the delete; its error stops the delete.
func (m *Manager) DeleteSnapshot(id, name string, before func(Snapshot) error) error {
	if !ValidID(id) || !ValidSnapshotName(name) {
		return ErrNoSnapshot
	}
	unlock, err := m.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()
	snaps, err := m.openSnapshots(id, false)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNoSnapshot
	}
	if err != nil {
		return err
	}
	defer func() { _ = snaps.Close() }()
	info, err := snaps.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return ErrNoSnapshot
	}
	if err != nil {
		return err
	}
	man, err := readManifest(snaps, name)
	if err != nil {
		// A snapshot whose snapshot.json is unreadable can still be deleted.
		man = Manifest{}
	}
	if err := before(Snapshot{Name: name, Manifest: man}); err != nil {
		return err
	}
	// Renamed first, so a crash mid-removal leaves a leftover that startup
	// clears, not a half snapshot that looks whole.
	gone := PrefixDeleting + randomSuffix()
	if err := snaps.Rename(name, gone); err != nil {
		return err
	}
	if err := syncDir(snaps, "."); err != nil {
		return err
	}
	return snaps.RemoveAll(gone)
}

// SnapshotUse is one snapshot with the world it belongs to and the bytes
// deleting it would free (SPEC 6.12).
type SnapshotUse struct {
	WorldID string
	Snapshot
	// Frees counts the files no other snapshot or world shares by hard
	// link (SPEC 6.8); a shared zip stays on disk when this one goes.
	Frees uint64
}

type inodeKey struct{ dev, ino uint64 }

// snapshotFiles stats each file of s, calling fn with its inode, size and
// link count.
func snapshotFiles(snaps fsLstater, s Snapshot, fn func(k inodeKey, size int64, links uint64)) error {
	for _, f := range s.Files {
		info, err := snaps.Lstat(path.Join(s.Name, f.Path))
		if errors.Is(err, fs.ErrNotExist) {
			continue // a damaged snapshot still lists, so it can be deleted
		}
		if err != nil {
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("no inode for %s", f.Path)
		}
		fn(inodeKey{uint64(st.Dev), st.Ino}, info.Size(), uint64(st.Nlink)) //nolint:gosec,unconvert // types differ by platform
	}
	return nil
}

type fsLstater interface {
	Lstat(name string) (fs.FileInfo, error)
}

// SnapshotFrees is the bytes deleting one snapshot would free.
func (m *Manager) SnapshotFrees(id string, s Snapshot) (uint64, error) {
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return 0, err
	}
	defer func() { _ = snaps.Close() }()
	var n uint64
	err = snapshotFiles(snaps, s, func(_ inodeKey, size int64, links uint64) {
		if links <= 1 {
			n += uint64(size) //nolint:gosec // a file size is never negative
		}
	})
	return n, err
}

// AllSnapshots lists every world's snapshots, deleted worlds included,
// each world's newest first, and the disk they take together. The total
// counts each file once by inode, so a zip hard-linked into many
// snapshots counts once (SPEC 6.6, 6.8).
func (m *Manager) AllSnapshots() (list []SnapshotUse, total uint64, err error) {
	all, err := openFolder(m.dirs.Snapshots)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	entries, err := fs.ReadDir(all.FS(), ".")
	_ = all.Close()
	if err != nil {
		return nil, 0, err
	}
	seen := map[inodeKey]bool{}
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		id := e.Name()
		snaps, err := m.openSnapshots(id, false)
		if err != nil {
			return nil, 0, err
		}
		got, err := m.listSnapshots(snaps)
		for _, s := range got {
			if err != nil {
				break
			}
			u := SnapshotUse{WorldID: id, Snapshot: s}
			err = snapshotFiles(snaps, s, func(k inodeKey, size int64, links uint64) {
				if links <= 1 {
					u.Frees += uint64(size) //nolint:gosec // a file size is never negative
				}
				if !seen[k] {
					seen[k] = true
					total += uint64(size) //nolint:gosec // a file size is never negative
				}
			})
			list = append(list, u)
		}
		_ = snaps.Close()
		if err != nil {
			return nil, 0, err
		}
	}
	return list, total, nil
}

// SnapshotNameTime reads the UTC time a snapshot folder is named by
// (SPEC 6.7).
func SnapshotNameTime(name string) (time.Time, bool) {
	if !ValidSnapshotName(name) {
		return time.Time{}, false
	}
	t, err := time.Parse(snapshotTimeFormat, name[:len(snapshotTimeFormat)])
	return t.UTC(), err == nil
}
