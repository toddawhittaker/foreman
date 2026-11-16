package world

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"slices"
)

// ErrModFileNotFound is returned for a mod zip the world does not offer for
// public download (SPEC 14.18).
var ErrModFileNotFound = errors.New("this world offers no mod zip with that name")

// ShareableMods picks, from a world's mod list, the mods the public mods
// page offers: the enabled mods that are not built in, one entry per mod
// name, each the one the game loads, which is the highest version. A folder
// mod is kept, with Local set and no File (SPEC 9.16, 9.15). Where a zip and
// a folder have the same version, the zip wins because it can be downloaded.
func ShareableMods(list []InstalledMod) []InstalledMod {
	var out []InstalledMod
	for _, mod := range list {
		if !mod.Enabled || mod.BuiltIn {
			continue
		}
		i := slices.IndexFunc(out, func(o InstalledMod) bool { return o.Name == mod.Name })
		if i < 0 {
			out = append(out, mod)
			continue
		}
		c := CompareVersions(mod.Version, out[i].Version)
		if c > 0 || c == 0 && out[i].Local && !mod.Local {
			out[i] = mod
		}
	}
	return out
}

// ModShare is a world's shareable mods as they were when it was made, with
// the means to open their zips. Open holds the world to the same folder the
// list was read from, so a world replaced in between serves nothing.
type ModShare struct {
	// Mods are in name order.
	Mods   []InstalledMod
	m      *Manager
	id     string
	opened fs.FileInfo
}

// ModShare reads the world's mod list now (SPEC 14.18).
func (m *Manager) ModShare(id string) (*ModShare, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	opened, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	list, err := m.Mods(id)
	if err != nil {
		return nil, err
	}
	return &ModShare{Mods: ShareableMods(list), m: m, id: id, opened: opened}, nil
}

// Open opens one mod zip for download. file must equal, byte for byte, the
// file name of a zip in Mods; a path is never built from anything else. The
// file must be a regular file and not a link. It returns ErrModFileNotFound
// for anything else. The caller closes the file.
func (s *ModShare) Open(file string) (*os.File, fs.FileInfo, error) {
	if file == "" || !slices.ContainsFunc(s.Mods, func(mod InstalledMod) bool { return !mod.Local && mod.File == file }) {
		return nil, nil, ErrModFileNotFound
	}
	root, err := s.m.OpenWorld(s.id)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrModFileNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = root.Close() }()
	now, err := root.Stat(".")
	if err != nil {
		return nil, nil, err
	}
	if !os.SameFile(s.opened, now) {
		return nil, nil, ErrModFileNotFound
	}
	p := path.Join(ModsDir, file)
	// openRegular refuses a link too, but with an error that does not tell a
	// refusal from a read failure, so the refusal is told apart here.
	linfo, err := root.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !linfo.Mode().IsRegular()) {
		return nil, nil, ErrModFileNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	f, info, err := openRegular(root, p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrModFileNotFound
	}
	return f, info, err
}

// OpenModFile lists the world's mods and opens the zip named file: the
// single download.
func (m *Manager) OpenModFile(id, file string) (*os.File, fs.FileInfo, error) {
	share, err := m.ModShare(id)
	if err != nil {
		return nil, nil, err
	}
	return share.Open(file)
}
