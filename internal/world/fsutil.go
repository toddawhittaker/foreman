package world

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
)

// Modes for everything Foreman writes into a world or snapshot, so the
// game's group can use it whatever the process umask is (SPEC 3.6).
const (
	FileMode fs.FileMode = 0o664
	DirMode  fs.FileMode = 0o775
)

// Name prefixes of work in progress. Startup recovery clears them, so a
// crash never leaves one behind as if it were real.
const (
	PrefixPartial  = ".partial-"  // a file being written
	PrefixStaging  = ".staging-"  // a folder being built
	PrefixDeleting = ".deleting-" // a folder being removed
)

// randomSuffix returns 16 hex characters for temp names.
func randomSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Mkdir makes one folder inside root with DirMode.
func Mkdir(root *os.Root, name string) error {
	if err := root.Mkdir(name, DirMode); err != nil {
		return err
	}
	return root.Chmod(name, DirMode)
}

// CreateTemp creates an empty file with FileMode beside where its final
// name will go (in dir inside root), so the final rename stays on one
// filesystem. It returns the open file and its name relative to root.
func CreateTemp(root *os.Root, dir string) (*os.File, string, error) {
	return createTempMode(root, dir, FileMode)
}

// createTempMode is CreateTemp with a mode of the caller's choice. The file
// has that mode from the moment it exists, since anyone who opens it while
// it is wider can read what is written later (SPEC 14.8).
func createTempMode(root *os.Root, dir string, mode fs.FileMode) (*os.File, string, error) {
	return createTempNamed(root, path.Join(dir, PrefixPartial+randomSuffix()), mode)
}

// createTempNamed creates the new file name inside root with mode.
func createTempNamed(root *os.Root, name string, mode fs.FileMode) (*os.File, string, error) {
	f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return nil, "", err
	}
	// The umask may have narrowed it; the game's group still needs to read.
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		_ = root.Remove(name)
		return nil, "", err
	}
	return f, name, nil
}

// Commit renames a finished temp file to its final name and syncs the
// folder, so the rename survives a crash. The temp file must already be
// synced and closed.
func Commit(root *os.Root, temp, final string) error {
	if err := root.Rename(temp, final); err != nil {
		return err
	}
	return syncDir(root, path.Dir(final))
}

// WriteFileAtomic writes data to name inside root: temp file, fsync,
// rename, fsync the folder. Readers see the old file or the new one, never
// half of either.
func WriteFileAtomic(root *os.Root, name string, data []byte) error {
	f, temp, err := CreateTemp(root, path.Dir(name))
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = Commit(root, temp, name)
	}
	if err != nil {
		_ = root.Remove(temp)
	}
	return err
}

// openDirFile opens folder dir inside root as a file, for a caller that
// needs its descriptor. The game can swap a folder for a named pipe at any
// time, so the open is non-blocking and directory-only, and the opened
// file must be the folder that Lstat saw (SPEC 14.5). The caller closes it.
func openDirFile(root *os.Root, dir string) (*os.File, error) {
	linfo, err := root.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !linfo.IsDir() {
		return nil, fmt.Errorf("%s: %w", dir, ErrNotFolder)
	}
	f, err := root.OpenFile(dir, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.IsDir() || !os.SameFile(info, linfo) {
		_ = f.Close()
		if err == nil {
			err = fmt.Errorf("%s: %w", dir, ErrNotFolder)
		}
		return nil, err
	}
	return f, nil
}

// syncDir flushes folder dir inside root to disk, so a rename or new file
// in it survives a crash.
func syncDir(root *os.Root, dir string) error {
	d, err := openDirFile(root, dir)
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	return err
}

// FreeBytes returns the space an unprivileged process can still use on the
// filesystem holding dir.
func FreeBytes(dir string) (uint64, error) {
	if n := fakeFree.Load(); n != nil {
		return *n, nil
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("read free space: %w", err)
	}
	return st.Bavail * uint64(st.Bsize), nil //nolint:gosec // Bsize is a positive block size
}

// fakeFree, when set, is what FreeBytes reports instead of the disk.
var fakeFree atomic.Pointer[uint64]

// SetFreeBytesForTest makes FreeBytes report n whatever the disk holds, for
// a test that must not change with other processes using the same disk. It
// returns a function that puts the real reading back. Never call it outside
// tests.
func SetFreeBytesForTest(n uint64) (restore func()) {
	fakeFree.Store(&n)
	return func() { fakeFree.Store(nil) }
}

// SpaceReserve is the free space every write must leave for the game's
// own autosaves (SPEC 6.11).
const SpaceReserve uint64 = 2_000_000_000

// reserveBytes is SpaceReserve; tests on a tiny filesystem lower it.
var reserveBytes atomic.Uint64

func init() { reserveBytes.Store(SpaceReserve) }

// SetSpaceReserveForTest sets the reserve for a test that runs on a
// filesystem smaller than SpaceReserve. It returns a function that puts
// the reserve back. Never call it outside tests.
func SetSpaceReserveForTest(n uint64) (restore func()) {
	reserveBytes.Store(n)
	return func() { reserveBytes.Store(SpaceReserve) }
}

// ErrNoSpace is wrapped by Reserve and CheckSpace when the disk is too full.
var ErrNoSpace = errors.New("not enough free disk space")

// NoSpaceError carries the sizes behind an ErrNoSpace, in bytes: what the
// write needs including the reserve, the reserve itself, and what is free.
// It matches ErrNoSpace.
type NoSpaceError struct{ Need, Reserve, Free uint64 }

func (e *NoSpaceError) Error() string {
	return fmt.Sprintf("%s: this needs %s, including %s kept free for the game, but only %s is free. Free some space and try again",
		ErrNoSpace, FormatBytes(e.Need), FormatBytes(e.Reserve), FormatBytes(e.Free))
}

func (*NoSpaceError) Is(target error) bool { return target == ErrNoSpace }

// promised counts bytes that writes in flight said they will write, so
// writes running side by side cannot together eat into the reserve (SPEC 6.11).
// One count for the process: the data folder is one filesystem.
var promised struct {
	mu sync.Mutex
	n  uint64
}

// Reserve refuses a write of n bytes on dir's filesystem unless the free
// space, less what other writes in flight have promised, covers n plus
// SpaceReserve. On success n stays promised until release is called;
// release is safe to call more than once.
func Reserve(dir string, n uint64) (release func(), err error) {
	free, err := FreeBytes(dir)
	if err != nil {
		return nil, err
	}
	return reserveFree(free, n)
}

// reserveFree is Reserve given the free space already read.
func reserveFree(free, n uint64) (release func(), err error) {
	promised.mu.Lock()
	defer promised.mu.Unlock()
	if err := checkSpace(free, promised.n, n); err != nil {
		return nil, err
	}
	promised.n += n
	var once sync.Once
	return func() {
		once.Do(func() {
			promised.mu.Lock()
			promised.n -= n
			promised.mu.Unlock()
		})
	}, nil
}

// CheckSpace is Reserve for a caller that only wants to refuse up front.
func CheckSpace(dir string, need uint64) error {
	release, err := Reserve(dir, need)
	if err != nil {
		return err
	}
	release()
	return nil
}

func checkSpace(free, inFlight, need uint64) error {
	avail := uint64(0)
	if free > inFlight {
		avail = free - inFlight
	}
	reserve := reserveBytes.Load()
	if want := need + reserve; avail < want {
		return &NoSpaceError{Need: want, Reserve: reserve, Free: avail}
	}
	return nil
}

// FormatBytes shows a size with one decimal and a unit, as in "2.1 GB".
func FormatBytes(n uint64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "kMGTPE"[exp])
}

// openFolder opens the folder at p as a root, refusing a link at p itself.
// It checks on every open, not once at startup, so a link swapped in for
// the folder after startup is refused too (SPEC 14.5). The parent is
// opened by path.
func openFolder(p string) (*os.Root, error) {
	parent, err := os.OpenRoot(filepath.Dir(p))
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	return openDir(parent, filepath.Base(p))
}

// chmodFolder sets the mode of the folder at p through the opened folder,
// so a link swapped in after the check is not followed.
func chmodFolder(p string, mode fs.FileMode) error {
	r, err := openFolder(p)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	f, err := r.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Chmod(mode)
}

// ErrNotFolder is returned by openDir for a name that is not a real
// folder, such as a symlink. To a visitor such a world is not there.
var ErrNotFolder = errors.New("not a folder")

// openDir opens the folder name inside parent as a root, refusing a
// symlink even one that stays inside parent, so one world's folder can
// never stand in for another's (SPEC 14.5).
func openDir(parent *os.Root, name string) (*os.Root, error) {
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() {
		return nil, fmt.Errorf("%s: %w", name, ErrNotFolder)
	}
	r, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	// The name could be swapped for a link between the Lstat and the open.
	after, err := r.Stat(".")
	if err == nil && !os.SameFile(before, after) {
		err = fmt.Errorf("%s: %w", name, ErrNotFolder)
	}
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}
