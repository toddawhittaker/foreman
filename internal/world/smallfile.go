package world

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// Size limits for the small files the game container can write. Real files
// are far below these.
const (
	maxMetaFile = 1 << 20
	// MaxModList is the most bytes read of a mod-list.json.
	MaxModList = 4 << 20
)

// openRegular opens a file inside root that the game container can write,
// for reading. A link or anything but a regular file is refused. The open
// is non-blocking so a named pipe swapped in after the check is not waited
// on, and the opened file is compared with the checked one so a swap is
// caught. Reads of the regular file that results never block. The caller
// closes it. Errors name the file only.
func openRegular(root *os.Root, name string) (*os.File, fs.FileInfo, error) {
	for try := 1; ; try++ {
		f, info, replaced, err := openRegularOnce(root, name)
		// An atomic replace (WriteFileAtomic) between the check and the open
		// swaps one regular file for another; check the new one.
		if replaced && try < openRegularTries {
			continue
		}
		return f, info, err
	}
}

// openRegularTries bounds the re-checks when the file keeps being replaced.
const openRegularTries = 5

// openRegularOnce is one check and open. replaced reports that the opened
// file is regular but not the one Lstat saw.
func openRegularOnce(root *os.Root, name string) (f *os.File, info fs.FileInfo, replaced bool, err error) {
	linfo, err := root.Lstat(name)
	if err != nil {
		return nil, nil, false, err
	}
	if !linfo.Mode().IsRegular() {
		return nil, nil, false, fmt.Errorf("%s is not a regular file", name)
	}
	f, err = root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, false, err
	}
	info, err = f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, false, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, linfo) {
		_ = f.Close()
		return nil, nil, info.Mode().IsRegular(), fmt.Errorf("%s is not a regular file", name)
	}
	return f, info, false, nil
}

// ReadSmallFile reads a file the game container can write (world.json,
// mod-list.json, the RCON password). A link or anything but a regular file
// is refused, the file is opened without blocking so a named pipe put there
// is not waited on, the opened file is checked again so a swap after the
// first check is caught, and a file over limit bytes is refused unread
// (SPEC 14.5, 14.8). Errors name the file, never its contents.
func ReadSmallFile(root *os.Root, name string, limit int64) ([]byte, error) {
	f, info, err := openRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if info.Size() > limit {
		return nil, fmt.Errorf("%s is too big to be a real file", name)
	}
	// The file can grow after the size check, so the read is capped too.
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is too big to be a real file", name)
	}
	return b, nil
}
