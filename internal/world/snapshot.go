package world

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
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
	"time"

	"golang.org/x/sys/unix"
)

// ManifestFile is the snapshot's record of what it holds (SPEC 6.1, 6.7).
const ManifestFile = "snapshot.json"

// Reasons a snapshot was taken (SPEC 6.7). Any other reason names the risky
// operation it came before.
const (
	ReasonManual        = "manual"
	ReasonBeforeDelete  = "before-delete"
	ReasonBeforeRestore = "before-restore"
	ReasonSwitch        = "switch"
)

// Labels of the automatic snapshots (SPEC 6.5, 5.10).
const (
	LabelBeforeRestore = "Before restore"
	LabelBeforeDelete  = "Before delete"
	LabelBeforeSwitch  = "Before switch"
)

// KeepAutomatic is how many automatic snapshots each world keeps (SPEC 6.6).
const KeepAutomatic = 10

// snapshotTimeFormat names a snapshot folder by its UTC time (SPEC 6.7).
const snapshotTimeFormat = "20060102T150405Z"

var snapshotNameRE = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z(-[0-9]{1,4})?$`)

// ValidSnapshotName reports whether name has the shape of a snapshot folder.
func ValidSnapshotName(name string) bool { return snapshotNameRE.MatchString(name) }

// ErrNoSnapshot is returned for a snapshot that does not exist.
var ErrNoSnapshot = errors.New("there is no such snapshot")

// ErrUnsupportedFile is returned when a world holds a file a snapshot
// cannot keep, such as a symlink.
var ErrUnsupportedFile = errors.New("this world holds a file Foreman cannot snapshot")

// UnsupportedFileError names the file a snapshot cannot keep. It matches
// ErrUnsupportedFile.
type UnsupportedFileError struct{ Path string }

func (e *UnsupportedFileError) Error() string {
	return fmt.Sprintf("%s: %s is a symlink or special file, which a snapshot cannot keep. Remove it or replace it with a copy, then try again", ErrUnsupportedFile, e.Path)
}

func (*UnsupportedFileError) Is(target error) bool { return target == ErrUnsupportedFile }

// ErrDamaged is returned when a snapshot's files do not match its manifest.
var ErrDamaged = errors.New("this snapshot's files do not match its snapshot.json, so Foreman will not restore it")

// DamageKind says how a snapshot's files differ from its manifest.
type DamageKind int

// The ways a snapshot can be damaged (SPEC 6.5).
const (
	DamageBadPath      DamageKind = iota // the manifest names a path that is not allowed
	DamageMissing                        // a file is gone
	DamageChanged                        // a file is not the regular file of the size recorded
	DamageChangedSince                   // a file's content no longer matches its hash
)

// DamagedError says which file of a snapshot no longer matches. It matches
// ErrDamaged.
type DamagedError struct {
	Kind DamageKind
	Path string
}

func (e *DamagedError) Error() string {
	switch e.Kind {
	case DamageBadPath:
		return fmt.Sprintf("%s: bad path %q", ErrDamaged, e.Path)
	case DamageMissing:
		return fmt.Sprintf("%s: %s is missing", ErrDamaged, e.Path)
	case DamageChangedSince:
		return fmt.Sprintf("%s: %s has changed since the snapshot was taken", ErrDamaged, e.Path)
	}
	return fmt.Sprintf("%s: %s has changed", ErrDamaged, e.Path)
}

func (*DamagedError) Is(target error) bool { return target == ErrDamaged }

// ManifestEntry is one file in a snapshot, with the time to put back on
// restore (SPEC 6.9).
type ManifestEntry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	// SHA256 is the hex hash of a zip, which is hard-linked and so could be
	// changed in place through the live world (SPEC 6.7). Other files are
	// copies and have none; snapshots from before hashes were recorded have none either.
	SHA256 string `json:"sha256,omitempty"`
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
	// manifestBytes is the size of snapshot.json, set only by a count.
	manifestBytes uint64
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

// DiskSize is the size of every file in the snapshot, snapshot.json
// included (SPEC 6.12). It is set by CountSnapshots and CountWorldSnapshots;
// a snapshot listed any other way reports Size.
func (s Snapshot) DiskSize() uint64 { return s.Size() + s.manifestBytes }

// TakeSnapshot takes a manual snapshot of a world (SPEC 6.3).
func (m *Manager) TakeSnapshot(id, label, by string) (Snapshot, error) {
	unlock, err := m.Lock(id)
	if err != nil {
		return Snapshot{}, err
	}
	defer unlock()
	return m.TakeSnapshotHeld(id, label, by)
}

// TakeSnapshotHeld is TakeSnapshot for a caller that already holds
// Lock(id), such as server control after a /server-save (SPEC 6.10).
func (m *Manager) TakeSnapshotHeld(id, label, by string) (Snapshot, error) {
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
	m.pruneOrWarn(id)
	return s, nil
}

// pruneOrWarn applies retention after a snapshot that worked. A failure
// leaves an extra snapshot behind, which harms nothing, so it is only
// logged rather than failing the operation the snapshot came before.
func (m *Manager) pruneOrWarn(id string) {
	if err := m.prune(id); err != nil {
		m.log.Warn("could not remove old automatic snapshots", "world", id, "error", err)
	}
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
	release, err := Reserve(m.dirs.Snapshots, copyBytes(files))
	if err != nil {
		return Snapshot{}, err
	}
	defer release()

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
	prev := m.previousHashes(snaps)
	for i, f := range man.Files {
		copied, err := placeFile(w, dst, f)
		if err != nil {
			return fmt.Errorf("snapshot %s: %w", f.Path, err)
		}
		man.Copied = man.Copied || copied
		if isZip(f.Path) {
			if man.Files[i].SHA256, err = prev.hash(snaps, dst, f); err != nil {
				return fmt.Errorf("snapshot %s: %w", f.Path, err)
			}
		}
	}
	b, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(dst, ManifestFile, append(b, '\n'))
}

// prevSnapshot is the newest earlier snapshot, whose hashes a new one may
// reuse (SPEC 6.7).
type prevSnapshot struct {
	name  string
	files map[string]ManifestEntry
}

// previousHashes returns the newest readable snapshot in snaps, or an empty
// one when there is none or the list cannot be read: then every zip is
// hashed afresh.
func (m *Manager) previousHashes(snaps *os.Root) prevSnapshot {
	list, err := m.listSnapshots(snaps)
	if err != nil || len(list) == 0 {
		return prevSnapshot{}
	}
	p := prevSnapshot{name: list[0].Name, files: map[string]ManifestEntry{}}
	for _, f := range list[0].Files {
		p.files[f.Path] = f
	}
	return p
}

// hash returns the SHA-256 of f as placed in dst. The previous snapshot's
// hash is reused when its copy is the same inode with the same size and
// time, so an unchanged save is not read again (SPEC 6.7).
func (p prevSnapshot) hash(snaps, dst *os.Root, f ManifestEntry) (string, error) {
	if old, ok := p.files[f.Path]; ok && old.SHA256 != "" && old.Size == f.Size && old.ModTime.Equal(f.ModTime) {
		a, aerr := snaps.Lstat(path.Join(p.name, f.Path))
		b, berr := dst.Lstat(f.Path)
		if aerr == nil && berr == nil && os.SameFile(a, b) {
			return old.SHA256, nil
		}
	}
	return hashFile(dst, f.Path)
}

// hashFile returns the hex SHA-256 of a regular file inside root. A zip
// is hard-linked from the world, so a pipe swapped in for it there would
// arrive here as a link; openRegular refuses it rather than wait on it.
func hashFile(root *os.Root, name string) (string, error) {
	f, _, err := openRegular(root, name)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// freeSnapshotName returns the folder name for time t, with -2, -3 and so
// on when it is taken (SPEC 6.7).
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
	all, err := openFolder(m.dirs.Snapshots)
	if err != nil {
		return nil, err
	}
	defer func() { _ = all.Close() }()
	if create {
		if err := Mkdir(all, id); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	return openDir(all, id)
}

// isZip reports whether a file is hard-linked rather than copied (SPEC 6.8).
func isZip(p string) bool { return strings.HasSuffix(p, ".zip") }

// worldFiles lists the files of a world with their sizes and times, hidden
// ones included. Foreman's own unfinished work is left out. A symlink or
// other special file cannot be kept by a snapshot, so rather than lose it
// silently the snapshot is refused, naming it.
func worldFiles(w *os.Root) ([]ManifestEntry, error) { return filesUnder(w, ".") }

// filesUnder is worldFiles for the part of w at start, with paths
// relative to w.
func filesUnder(w *os.Root, start string) ([]ManifestEntry, error) {
	var out []ManifestEntry
	err := fs.WalkDir(w.FS(), start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != start && (isLeftover(d.Name()) || strings.HasPrefix(d.Name(), PrefixRestoreOld)) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return &UnsupportedFileError{Path: p}
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
// which are hard-linked (SPEC 6.11).
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
	if err := mkdirAll(dst, path.Dir(f.Path)); err != nil {
		return false, err
	}
	if f.Path == RCONPasswordFile {
		// Its own copy, at 0640 before the password is written, whatever
		// mode the source had (SPEC 14.8).
		return false, copyRCONPassword(src, dst)
	}
	if isZip(f.Path) {
		if linkFile(src, dst, f.Path) == nil {
			return false, nil
		}
		zipCopied = true
	}
	if zipCopied {
		// The caller reserved space for the copies it expected; only a
		// failed link makes a zip cost bytes too (SPEC 6.11).
		release, err := Reserve(dst.Name(), uint64(f.Size)) //nolint:gosec // a file size is never negative
		if err != nil {
			return false, err
		}
		defer release()
	}
	if err := copyFile(src, f.Path, dst, f.Path); err != nil {
		return false, err
	}
	return zipCopied, nil
}

// linkFile hardlinks name from one root into another. Each side's folder is
// opened through its root, and linkat is given one name in each folder
// without following symlinks, so the link can never reach outside a root.
// os.Root.Link only links within one root, hence golang.org/x/sys.
func linkFile(src, dst *os.Root, name string) error {
	sd, err := openDirFile(src, path.Dir(name))
	if err != nil {
		return err
	}
	defer func() { _ = sd.Close() }()
	dd, err := openDirFile(dst, path.Dir(name))
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
	return m.listSnapshots(snaps)
}

// listSnapshots reads every snapshot in snaps, newest first. A folder
// whose snapshot.json cannot be read is logged and skipped, so one damaged
// snapshot does not hide the rest or block new ones.
func (m *Manager) listSnapshots(snaps *os.Root) ([]Snapshot, error) {
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
			m.log.Warn("skipped a snapshot that cannot be read", "folder", path.Join(snaps.Name(), e.Name()), "error", err)
			continue
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
	dir, err := openDir(snaps, name)
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = dir.Close() }()
	b, err := dir.ReadFile(ManifestFile)
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
	updateKept := false
	for _, s := range list {
		if !s.Automatic() {
			continue
		}
		// Rollback restores the newest "Before update" snapshot (SPEC 10.4).
		if s.Reason == ReasonGameUpdate && !updateKept {
			updateKept = true
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
