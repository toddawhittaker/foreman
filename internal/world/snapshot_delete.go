package world

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
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
// deleting it alone would free (SPEC 6.12).
type SnapshotUse struct {
	WorldID string
	Snapshot
	Frees uint64
	// files are the inodes the snapshot's listed files point at, one per file.
	files []inodeKey
}

// SnapshotRef names one snapshot.
type SnapshotRef struct{ WorldID, Name string }

type inodeKey struct{ dev, ino uint64 }

// inodeUse is a file's size and how many names it has on disk, in
// snapshots, worlds or anywhere else.
type inodeUse struct {
	size  uint64
	links uint64
}

// SnapshotCount is one pass over snapshot files by inode. A deletion frees
// a file only when every name the file has on disk belongs to a deleted
// snapshot: a zip a live world also holds by hard link frees nothing, and
// a zip held only by two snapshots frees its size once both are gone
// (SPEC 6.8, 6.12). The space a selection frees, each world's "snapshots
// add" and the total all come from free, so they always agree.
type SnapshotCount struct {
	uses   []SnapshotUse
	inodes map[inodeKey]inodeUse
}

// CountSnapshots counts every world's snapshots, deleted worlds included,
// each world's newest first and the worlds in id order.
func (m *Manager) CountSnapshots() (*SnapshotCount, error) {
	c := &SnapshotCount{inodes: map[inodeKey]inodeUse{}}
	all, err := openFolder(m.dirs.Snapshots)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(all.FS(), ".")
	_ = all.Close()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() && ValidID(e.Name()) {
			if err := m.countWorld(c, e.Name()); err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

// CountWorldSnapshots counts one world's snapshots. Its figures equal that
// world's part of CountSnapshots.
func (m *Manager) CountWorldSnapshots(id string) (*SnapshotCount, error) {
	c := &SnapshotCount{inodes: map[inodeKey]inodeUse{}}
	if !ValidID(id) {
		return c, nil
	}
	if err := m.countWorld(c, id); err != nil {
		return nil, err
	}
	return c, nil
}

func (m *Manager) countWorld(c *SnapshotCount, id string) error {
	snaps, err := m.openSnapshots(id, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = snaps.Close() }()
	list, err := m.listSnapshots(snaps)
	if err != nil {
		return err
	}
	for _, s := range list {
		u := SnapshotUse{WorldID: id, Snapshot: s}
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
			k := inodeKey{uint64(st.Dev), st.Ino}                                      //nolint:gosec,unconvert // types differ by platform
			c.inodes[k] = inodeUse{size: uint64(info.Size()), links: uint64(st.Nlink)} //nolint:gosec,unconvert // a size is never negative; Nlink's type differs by platform
			u.files = append(u.files, k)
		}
		u.Frees = c.free(u.files)
		c.uses = append(c.uses, u)
	}
	return nil
}

// free is the bytes of the files whose every name is among files, which
// lists a file once per name it has in the deleted snapshots.
func (c *SnapshotCount) free(files []inodeKey) uint64 {
	held := map[inodeKey]uint64{}
	for _, k := range files {
		held[k]++
	}
	var n uint64
	for k, h := range held {
		if h >= c.inodes[k].links {
			n += c.inodes[k].size
		}
	}
	return n
}

// Snapshots lists what was counted.
func (c *SnapshotCount) Snapshots() []SnapshotUse { return c.uses }

// Frees is the bytes deleting exactly the named snapshots would free.
func (c *SnapshotCount) Frees(refs []SnapshotRef) uint64 {
	return c.free(c.filesOf(func(u SnapshotUse) bool {
		return slices.Contains(refs, SnapshotRef{u.WorldID, u.Name})
	}))
}

// Total is the space the snapshots add: what deleting all of them would free.
func (c *SnapshotCount) Total() uint64 {
	return c.free(c.filesOf(func(SnapshotUse) bool { return true }))
}

// WorldAdds is, for each world with snapshots, the space its snapshots add:
// what deleting all of that world's snapshots would free. A file held only
// by snapshots of two worlds counts for neither, so the worlds can add up to
// less than Total.
func (c *SnapshotCount) WorldAdds() map[string]uint64 {
	out := map[string]uint64{}
	for _, u := range c.uses {
		if _, done := out[u.WorldID]; !done {
			out[u.WorldID] = c.free(c.filesOf(func(o SnapshotUse) bool { return o.WorldID == u.WorldID }))
		}
	}
	return out
}

func (c *SnapshotCount) filesOf(pick func(SnapshotUse) bool) []inodeKey {
	var files []inodeKey
	for _, u := range c.uses {
		if pick(u) {
			files = append(files, u.files...)
		}
	}
	return files
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
