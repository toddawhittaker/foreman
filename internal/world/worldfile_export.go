package world

import (
	"context"
	"errors"
	"io"
)

// WorldExport is a world prepared for export: its files hard-linked or copied
// into a staging folder, so the download runs without the world's lock (SPEC
// 5.13). It is built by T1 (the plan's task table).
type WorldExport struct{}

// PrepareExport takes the world's lock only while it stages the files of a
// world file, and refuses a world a newer Foreman wrote, a busy world and a
// world with a link or special file (SPEC 5.13).
func (m *Manager) PrepareExport(ctx context.Context, id string, saves ExportSaves) (*WorldExport, error) {
	return nil, errors.New("not built")
}

// Name is the world's name, for the file name.
func (e *WorldExport) Name() string { return "" }

// Bytes is the unpacked size of the export, for the card.
func (e *WorldExport) Bytes() uint64 { return 0 }

// GamePasswordSet is whether the world had a game password, which the file
// does not hold.
func (e *WorldExport) GamePasswordSet() bool { return false }

// WriteZip streams the staged files as a world file into w.
func (e *WorldExport) WriteZip(w io.Writer, foremanVersion string) error {
	return errors.New("not built")
}

// Close removes the staging folder.
func (e *WorldExport) Close() error { return nil }
