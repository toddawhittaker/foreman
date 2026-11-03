package world

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ManifestFile is the snapshot's record of what it holds (SPEC 6.1, 6.7).
const ManifestFile = "snapshot.json"

// Reasons a snapshot was taken (R4). Any other reason names the risky
// operation it came before.
const (
	ReasonManual        = "manual"
	ReasonBeforeDelete  = "before-delete"
	ReasonBeforeRestore = "before-restore"
)

// Labels of the automatic snapshots (R14).
const (
	LabelBeforeRestore = "Before restore"
	LabelBeforeDelete  = "Before delete"
)

// KeepAutomatic is how many automatic snapshots each world keeps (SPEC 6.6).
const KeepAutomatic = 10

// snapshotTimeFormat names a snapshot folder by its UTC time (R4).
const snapshotTimeFormat = "20060102T150405Z"

var snapshotNameRE = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z(-[0-9]{1,4})?$`)

// ValidSnapshotName reports whether name has the shape of a snapshot folder.
func ValidSnapshotName(name string) bool { return snapshotNameRE.MatchString(name) }

// ErrNoSnapshot is returned for a snapshot that does not exist.
var ErrNoSnapshot = errors.New("there is no such snapshot")

// ErrDamaged is returned when a snapshot's files do not match its manifest.
var ErrDamaged = errors.New("this snapshot's files do not match its snapshot.json, so Foreman will not restore it")

// ManifestEntry is one file in a snapshot, with the time to put back on
// restore (SPEC 6.9).
type ManifestEntry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// Manifest is snapshot.json.
type Manifest struct {
	Label   string    `json:"label"`
	Reason  string    `json:"reason"`
	TakenAt time.Time `json:"taken_at"`
	TakenBy string    `json:"taken_by"`
	// Copied is true when a zip could not be hard-linked and was copied
	// whole (SPEC 6.8).
	Copied bool            `json:"copied,omitempty"`
	Files  []ManifestEntry `json:"files"`
}

// Snapshot is one listed snapshot.
type Snapshot struct {
	Name string
	Manifest
}

// Automatic reports whether retention may remove the snapshot (SPEC 6.6).
func (s Snapshot) Automatic() bool {
	return s.Reason != ReasonManual && s.Reason != ReasonBeforeDelete
}

// Size is the total size of the snapshot's files.
func (s Snapshot) Size() uint64 {
	var n uint64
	for _, f := range s.Files {
		n += uint64(f.Size) //nolint:gosec // a file size is never negative
	}
	return n
}

// TakeSnapshot takes a manual snapshot of a world (SPEC 6.3).
func (m *Manager) TakeSnapshot(id, label, by string) (Snapshot, error) {
	unlock, err := m.Lock(id)
	if err != nil {
		return Snapshot{}, err
	}
	defer unlock()
	return m.snapshot(id, ReasonManual, label, by)
}

// AutoSnapshot takes the automatic snapshot that comes before a risky
// operation (SPEC 6.2), then applies retention. The caller holds Lock(id)
// and must not run the operation if this returns an error.
func (m *Manager) AutoSnapshot(id, reason, label, by string) (Snapshot, error) {
	s, err := m.snapshot(id, reason, label, by)
	if err != nil {
		return Snapshot{}, err
	}
	if err := m.prune(id); err != nil {
		return s, fmt.Errorf("remove old automatic snapshots: %w", err)
	}
	return s, nil
}

// snapshot copies a world into snapshots/<id>/<time>. It builds in a
// partial folder and renames it into place, so a failure or crash leaves
// nothing that looks like a snapshot.
func (m *Manager) snapshot(id, reason, label, by string) (Snapshot, error) {
	w, err := m.OpenWorld(id)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = w.Close() }()

	files, err := worldFiles(w)
	if err != nil {
		return Snapshot{}, fmt.Errorf("list the world's files: %w", err)
	}
	if err := CheckSpace(m.dirs.Snapshots, copyBytes(files)); err != nil {
		return Snapshot{}, err
	}

	snaps, err := m.openSnapshots(id, true)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = snaps.Close() }()

	man := Manifest{Label: label, Reason: reason, TakenAt: m.now().UTC(), TakenBy: by, Files: files}
	partial := PrefixPartial + randomSuffix()
	if err := m.fillSnapshot(w, snaps, partial, &man); err != nil {
		_ = snaps.RemoveAll(partial)
		return Snapshot{}, err
	}
	name, err := freeSnapshotName(snaps, man.TakenAt)
	if err == nil {
		err = snaps.Rename(partial, name)
	}
	if err == nil {
		err = syncDir(snaps, ".")
	}
	if err != nil {
		_ = snaps.RemoveAll(partial)
		return Snapshot{}, err
	}
	return Snapshot{Name: name, Manifest: man}, nil
}

func (m *Manager) fillSnapshot(w, snaps *os.Root, dir string, man *Manifest) error {
	if err := Mkdir(snaps, dir); err != nil {
		return err
	}
	dst, err := snaps.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()
	for _, sub := range []string{SavesDir, ModsDir, ConfigDir} {
		if err := Mkdir(dst, sub); err != nil {
			return err
		}
	}
	for _, f := range man.Files {
		copied, err := placeFile(w, dst, f)
		if err != nil {
			return fmt.Errorf("snapshot %s: %w", f.Path, err)
		}
		man.Copied = man.Copied || copied
	}
	b, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(dst, ManifestFile, append(b, '\n'))
}

// freeSnapshotName returns the folder name for time t, with -2, -3 and so
// on when it is taken (R4).
func freeSnapshotName(snaps *os.Root, t time.Time) (string, error) {
	base := t.UTC().Format(snapshotTimeFormat)
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name += "-" + strconv.Itoa(n)
		}
		_, err := snaps.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
	}
}

// openSnapshots opens snapshots/<id>, making it when create is set.
func (m *Manager) openSnapshots(id string, create bool) (*os.Root, error) {
	if !ValidID(id) {
		return nil, fs.ErrNotExist
	}
	all, err := os.OpenRoot(m.dirs.Snapshots)
	if err != nil {
		return nil, err
	}
	defer func() { _ = all.Close() }()
	if create {
		if err := Mkdir(all, id); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	return all.OpenRoot(id)
}

// isZip reports whether a file is hard-linked rather than copied (SPEC 6.8).
func isZip(p string) bool { return strings.HasSuffix(p, ".zip") }

// worldFiles lists the regular files of a world with their sizes and times.
// Symlinks and other special files are left out, as is unfinished work.
func worldFiles(w *os.Root) ([]ManifestEntry, error) {
	var out []ManifestEntry
	err := fs.WalkDir(w.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != "." && (strings.HasPrefix(d.Name(), ".") || isLeftover(d.Name())) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, ManifestEntry{Path: p, Size: info.Size(), ModTime: info.ModTime().UTC()})
		return nil
	})
	return out, err
}

// copyBytes is what placing files will write: everything but the zips,
// which are hard-linked (R19).
func copyBytes(files []ManifestEntry) uint64 {
	var n uint64
	for _, f := range files {
		if !isZip(f.Path) {
			n += uint64(f.Size) //nolint:gosec // a file size is never negative
		}
	}
	return n
}

// placeFile puts f from src into dst: a hardlink for a zip, a copy for
// anything else or when the link fails. It reports whether a zip was
// copied. The copy gets f's modification time so the folder reads right
// to a person (SPEC 6.7).
func placeFile(src, dst *os.Root, f ManifestEntry) (zipCopied bool, err error) {
	if !fs.ValidPath(f.Path) {
		return false, fmt.Errorf("bad path %q", f.Path)
	}
	if err := mkdirParents(dst, path.Dir(f.Path)); err != nil {
		return false, err
	}
	if isZip(f.Path) {
		if linkFile(src, dst, f.Path) == nil {
			return false, nil
		}
		zipCopied = true
	}
	if err := copyFile(src, dst, f); err != nil {
		return false, err
	}
	return zipCopied, nil
}

// mkdirParents makes dir and its parents inside root with DirMode.
func mkdirParents(root *os.Root, dir string) error {
	if dir == "." {
		return nil
	}
	if err := mkdirParents(root, path.Dir(dir)); err != nil {
		return err
	}
	if err := Mkdir(root, dir); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return nil
}

// linkFile hardlinks name from one root into another. Each side's folder is
// opened through its root, and linkat is given one name in each folder
// without following symlinks, so the link can never reach outside a root.
// os.Root.Link only links within one root, hence golang.org/x/sys.
func linkFile(src, dst *os.Root, name string) error {
	sd, err := src.Open(path.Dir(name))
	if err != nil {
		return err
	}
	defer func() { _ = sd.Close() }()
	dd, err := dst.Open(path.Dir(name))
	if err != nil {
		return err
	}
	defer func() { _ = dd.Close() }()
	base := path.Base(name)
	info, err := src.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", name)
	}
	return unix.Linkat(int(sd.Fd()), base, int(dd.Fd()), base, 0) //nolint:gosec // an fd fits in an int
}

// copyFile copies f from src to dst with FileMode, syncs it and sets its
// modification time. It refuses the copy if the disk is too full (R19).
func copyFile(src, dst *os.Root, f ManifestEntry) error {
	in, err := src.OpenFile(f.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := CheckSpace(dst.Name(), uint64(f.Size)); err != nil { //nolint:gosec // a file size is never negative
		return err
	}
	out, err := dst.OpenFile(f.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, FileMode)
	if err != nil {
		return err
	}
	err = out.Chmod(FileMode)
	if err == nil {
		_, err = io.Copy(out, in)
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = dst.Chtimes(f.Path, f.ModTime, f.ModTime)
	}
	return err
}

// Snapshots lists a world's snapshots, newest first (SPEC 6.4). It works
// for a deleted world too, since its snapshots stay.
func (m *Manager) Snapshots(id string) ([]Snapshot, error) {
	snaps, err := m.openSnapshots(id, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = snaps.Close() }()
	entries, err := fs.ReadDir(snaps.FS(), ".")
	if err != nil {
		return nil, err
	}
	var out []Snapshot
	for _, e := range entries {
		if !e.IsDir() || !ValidSnapshotName(e.Name()) {
			continue
		}
		man, err := readManifest(snaps, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, Snapshot{Name: e.Name(), Manifest: man})
	}
	slices.SortFunc(out, func(a, b Snapshot) int {
		return cmp.Or(b.TakenAt.Compare(a.TakenAt), cmp.Compare(b.Name, a.Name))
	})
	return out, nil
}

// Snapshot returns one snapshot of a world.
func (m *Manager) Snapshot(id, name string) (Snapshot, error) {
	if !ValidSnapshotName(name) {
		return Snapshot{}, ErrNoSnapshot
	}
	snaps, err := m.openSnapshots(id, false)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, ErrNoSnapshot
	}
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = snaps.Close() }()
	man, err := readManifest(snaps, name)
	if errors.Is(err, fs.ErrNotExist) {
		return Snapshot{}, ErrNoSnapshot
	}
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Name: name, Manifest: man}, nil
}

func readManifest(snaps *os.Root, name string) (Manifest, error) {
	b, err := snaps.ReadFile(path.Join(name, ManifestFile))
	if err != nil {
		return Manifest{}, err
	}
	var man Manifest
	if err := json.Unmarshal(b, &man); err != nil {
		return Manifest{}, fmt.Errorf("read %s of snapshot %s: %w", ManifestFile, name, err)
	}
	return man, nil
}

// prune removes a world's automatic snapshots beyond the newest
// KeepAutomatic. Manual and before-delete snapshots are never removed.
func (m *Manager) prune(id string) error {
	list, err := m.Snapshots(id)
	if err != nil {
		return err
	}
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return err
	}
	defer func() { _ = snaps.Close() }()
	kept := 0
	for _, s := range list {
		if !s.Automatic() {
			continue
		}
		if kept++; kept <= KeepAutomatic {
			continue
		}
		// Renamed first, so a crash mid-removal leaves a leftover, not a
		// half snapshot.
		gone := PrefixDeleting + randomSuffix()
		if err := snaps.Rename(s.Name, gone); err != nil {
			return err
		}
		if err := snaps.RemoveAll(gone); err != nil {
			return err
		}
	}
	return nil
}

// SnapshotDiskUse is the space a world's snapshots take, counting each
// file once by inode since zips are hard-linked between them (R11), and the
// free space on the snapshots folder's filesystem (R19).
func (m *Manager) SnapshotDiskUse(id string, list []Snapshot) (used, free uint64, err error) {
	free, err = FreeBytes(m.dirs.Snapshots)
	if err != nil || len(list) == 0 {
		return 0, free, err
	}
	snaps, err := m.openSnapshots(id, false)
	if err != nil {
		return 0, free, err
	}
	defer func() { _ = snaps.Close() }()
	type inode struct{ dev, ino uint64 }
	seen := map[inode]bool{}
	for _, s := range list {
		for _, f := range s.Files {
			info, err := snaps.Lstat(path.Join(s.Name, f.Path))
			if err != nil {
				return 0, free, err
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return 0, free, fmt.Errorf("no inode for %s", f.Path)
			}
			k := inode{uint64(st.Dev), st.Ino} //nolint:gosec,unconvert // Dev's type differs by platform
			if !seen[k] {
				seen[k] = true
				used += uint64(info.Size()) //nolint:gosec // a file size is never negative
			}
		}
	}
	return used, free, nil
}
