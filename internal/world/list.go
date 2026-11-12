package world

import (
	"cmp"
	"context"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"
)

// Summary is one row of the world list (SPEC 5.4).
type Summary struct {
	ID          string
	Name        string
	GameVersion string
	SpaceAge    bool
	// LastPlayed is the newest save's modification time; zero when the
	// world has no saves or LastPlayedUnknown is set.
	LastPlayed time.Time
	DiskBytes  uint64
	// SizeUnknown is true when DiskBytes could not be measured, for one
	// because a folder in the world cannot be read.
	SizeUnknown bool
	// LastPlayedUnknown is true when the saves folder cannot be read.
	LastPlayedUnknown bool
	Active            bool
	// Newer is true when a newer Foreman wrote world.json; the world is
	// listed but cannot be changed.
	Newer bool
	// Unreadable is true when world.json is missing or broken. Name is then
	// the id.
	Unreadable bool
	// ImportedFrom is where the world was imported from; nil when it was
	// not imported from a folder or world.json cannot be read (SPEC 5.5).
	ImportedFrom *ImportedFrom
	// UseWhitelist is the world's "only whitelisted players can join"
	// switch (SPEC 8.3).
	UseWhitelist bool
}

// List returns every world in the worlds folder, the active one first, then
// by name ignoring case. Folders whose names start with a dot are work in
// progress and are skipped.
func (m *Manager) List(ctx context.Context) ([]Summary, error) {
	activeID, _, err := m.active.ActiveWorld(ctx)
	if err != nil {
		return nil, err
	}
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = worlds.Close() }()
	entries, err := fs.ReadDir(worlds.FS(), ".")
	if err != nil {
		return nil, err
	}
	var out []Summary
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		s, err := summarize(worlds, e.Name())
		if err != nil {
			return nil, err
		}
		s.Active = s.ID == activeID
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Summary) int {
		if a.Active != b.Active {
			if a.Active {
				return -1
			}
			return 1
		}
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.ID, b.ID))
	})
	return out, nil
}

// Get returns one world's row, as List would show it (SPEC 5.4).
func (m *Manager) Get(ctx context.Context, id string) (Summary, error) {
	if !ValidID(id) {
		return Summary{}, fs.ErrNotExist
	}
	activeID, _, err := m.active.ActiveWorld(ctx)
	if err != nil {
		return Summary{}, err
	}
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return Summary{}, err
	}
	defer func() { _ = worlds.Close() }()
	s, err := summarize(worlds, id)
	if err != nil {
		return Summary{}, err
	}
	s.Active = id == activeID
	return s, nil
}

func summarize(worlds *os.Root, id string) (Summary, error) {
	w, err := openDir(worlds, id)
	if err != nil {
		return Summary{}, err
	}
	defer func() { _ = w.Close() }()

	meta, err := ReadMeta(w)
	s := Summary{ID: id, Name: displayName(id, meta, err), Unreadable: err != nil}
	if err == nil {
		s.GameVersion = meta.GameVersion
		s.SpaceAge = meta.SpaceAge
		s.Newer = meta.Newer()
		s.ImportedFrom = meta.ImportedFrom
		s.UseWhitelist = meta.UseWhitelist
	}
	if s.DiskBytes, err = diskUse(w); err != nil {
		s.DiskBytes, s.SizeUnknown = 0, true
	}
	if s.LastPlayed, err = lastPlayed(w); err != nil {
		s.LastPlayed, s.LastPlayedUnknown = time.Time{}, true
	}
	return s, nil
}

// diskUse adds up the sizes of the regular files under root. Symlinks are
// not followed.
func diskUse(root *os.Root) (uint64, error) {
	var total uint64
	err := fs.WalkDir(root.FS(), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += uint64(info.Size()) //nolint:gosec // a file size is never negative
		return nil
	})
	return total, err
}

// lastPlayed returns the newest save's modification time, the game's own
// record of when the world was last saved.
func lastPlayed(root *os.Root) (time.Time, error) {
	save, found, err := newestSave(root)
	if err != nil || !found {
		return time.Time{}, err
	}
	return save.ModTime, nil
}
