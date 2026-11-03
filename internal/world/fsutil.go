package world

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
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
	name := path.Join(dir, PrefixPartial+randomSuffix())
	f, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, FileMode)
	if err != nil {
		return nil, "", err
	}
	if err := f.Chmod(FileMode); err != nil {
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

func syncDir(root *os.Root, dir string) error {
	d, err := root.Open(dir)
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
	free, _, err := diskSpace(dir)
	return free, err
}

// diskSpace returns the free bytes and the total size of dir's filesystem.
func diskSpace(dir string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, 0, fmt.Errorf("read free space: %w", err)
	}
	bsize := uint64(st.Bsize) //nolint:gosec // Bsize is a positive block size
	return st.Bavail * bsize, st.Blocks * bsize, nil
}

// ErrNoSpace is wrapped by CheckSpace when the disk is too full.
var ErrNoSpace = errors.New("not enough free disk space")

// CheckSpace refuses an operation that will write need bytes on dir's
// filesystem unless that much plus 1% of the filesystem would stay free.
// The reserve leaves room for the game's own autosaves, and refusing up
// front beats failing halfway.
func CheckSpace(dir string, need uint64) error {
	free, total, err := diskSpace(dir)
	if err != nil {
		return err
	}
	return checkSpace(free, total, need)
}

func checkSpace(free, total, need uint64) error {
	reserve := total / 100
	if want := need + reserve; free < want {
		return fmt.Errorf("%w: this needs %s, including %s kept free for the game, but only %s is free. Free some space and try again",
			ErrNoSpace, FormatBytes(want), FormatBytes(reserve), FormatBytes(free))
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
