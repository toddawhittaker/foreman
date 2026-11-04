package world

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// Size limits for the small files the game container can write. Real files
// are far below these.
const (
	maxMetaFile = 1 << 20
	maxModList  = 4 << 20
)

// ReadSmallFile reads a file the game container can write (world.json,
// mod-list.json, the RCON password). A link or anything but a regular file
// is refused, the file is opened without blocking so a named pipe put there
// is not waited on, the opened file is checked again so a swap after the
// first check is caught, and a file over limit bytes is refused unread
// (SPEC 14.5, 14.8). Errors name the file, never its contents.
func ReadSmallFile(root *os.Root, name string, limit int64) ([]byte, error) {
	linfo, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !linfo.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, linfo) {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
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
