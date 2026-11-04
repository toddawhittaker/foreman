package world

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
)

// InboxFolder is one subfolder of the import inbox (SPEC 5.5).
type InboxFolder struct {
	Name    string
	Saves   int
	HasMods bool
	// Bytes is what an import would copy.
	Bytes uint64
}

// ErrBadGameVersion is returned for a game version not shaped like 2.0.72.
var ErrBadGameVersion = errors.New("enter the game version as three numbers, such as 2.0.72")

// ErrNoSaves is returned when an inbox folder holds no save zips.
var ErrNoSaves = errors.New("this folder holds no save files (.zip) at its top level, so there is nothing to import")

// ErrNoFolder is returned when the chosen inbox folder does not exist.
var ErrNoFolder = errors.New("choose a folder from the import folder")

var gameVersionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// ValidGameVersion reports whether v looks like x.y.z.
func ValidGameVersion(v string) bool { return gameVersionRE.MatchString(v) }

// importFile is a regular file to copy, by its path inside the source folder.
type importFile struct {
	path string
	size uint64
}

// Inbox lists the import inbox's subfolders. Hidden folders and anything
// that is not a real folder (a symlink, for one) are left out.
func (m *Manager) Inbox() ([]InboxFolder, error) {
	inbox, err := openFolder(m.dirs.Imports)
	if err != nil {
		return nil, err
	}
	defer func() { _ = inbox.Close() }()
	entries, err := fs.ReadDir(inbox.FS(), ".")
	if err != nil {
		return nil, err
	}
	var out []InboxFolder
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		f, err := inboxFolder(inbox, e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func inboxFolder(inbox *os.Root, name string) (InboxFolder, error) {
	src, err := inbox.OpenRoot(name)
	if err != nil {
		return InboxFolder{}, err
	}
	defer func() { _ = src.Close() }()
	saves, mods, err := importPlan(src)
	if err != nil {
		return InboxFolder{}, err
	}
	f := InboxFolder{Name: name, Saves: len(saves), HasMods: mods != nil}
	for _, x := range slices.Concat(saves, mods) {
		f.Bytes += x.size
	}
	return f, nil
}

// importPlan lists what an import copies from src: its top-level save zips
// and every regular file under mods/. mods is nil when there is no mods/
// folder. Symlinks and other special files are skipped, never followed.
func importPlan(src *os.Root) (saves, mods []importFile, err error) {
	entries, err := fs.ReadDir(src.FS(), ".")
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !ValidSaveName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, nil, err
		}
		saves = append(saves, importFile{e.Name(), uint64(info.Size())}) //nolint:gosec // a file size is never negative
	}
	info, err := src.Lstat(ModsDir)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return saves, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	mods = []importFile{}
	err = fs.WalkDir(src.FS(), ModsDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		mods = append(mods, importFile{p, uint64(info.Size())}) //nolint:gosec // a file size is never negative
		return nil
	})
	return saves, mods, err
}

// ImportFolder makes a new world from the inbox folder named folder: its
// top-level save zips and its mods/ folder are copied, never linked or
// moved, so the source is left exactly as it was (SPEC 5.5). It refuses
// up front when the disk lacks room for the copy (SPEC 6.11).
func (m *Manager) ImportFolder(ctx context.Context, folder, name, gameVersion string, spaceAge bool) (string, error) {
	if err := m.checkNew(ctx, name, gameVersion); err != nil {
		return "", err
	}
	if folder == "" || path.Base(folder) != folder || strings.HasPrefix(folder, ".") {
		return "", ErrNoFolder
	}
	inbox, err := openFolder(m.dirs.Imports)
	if err != nil {
		return "", err
	}
	defer func() { _ = inbox.Close() }()
	if info, err := inbox.Lstat(folder); err != nil || !info.IsDir() {
		return "", ErrNoFolder
	}
	src, err := inbox.OpenRoot(folder)
	if err != nil {
		return "", err
	}
	defer func() { _ = src.Close() }()

	saves, mods, err := importPlan(src)
	if err != nil {
		return "", err
	}
	if len(saves) == 0 {
		return "", ErrNoSaves
	}
	var need uint64
	for _, f := range slices.Concat(saves, mods) {
		need += f.size
	}
	release, err := Reserve(m.dirs.Worlds, need)
	if err != nil {
		return "", err
	}
	defer release()
	return m.create(ctx, name, gameVersion, spaceAge, func(worlds *os.Root, dir string) error {
		dst, err := worlds.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer func() { _ = dst.Close() }()
		for _, f := range saves {
			if err := copyFile(src, f.path, dst, path.Join(SavesDir, f.path)); err != nil {
				return err
			}
		}
		for _, f := range mods {
			if err := copyFile(src, f.path, dst, f.path); err != nil {
				return err
			}
		}
		return nil
	})
}

// ImportSave makes a new world from one uploaded save (SPEC 5.5). temp is
// the upload's temp file, made with CreateTemp in the worlds folder (its
// name relative to that folder), so moving it into the new world is a
// rename. temp is removed whatever happens.
func (m *Manager) ImportSave(ctx context.Context, name, gameVersion string, spaceAge bool, saveName, temp string) (string, error) {
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return "", err
	}
	defer func() { _ = worlds.Close() }()
	defer func() { _ = worlds.Remove(temp) }() // gone already after a good import

	if !strings.HasPrefix(temp, PrefixPartial) || path.Base(temp) != temp {
		return "", fmt.Errorf("not an upload temp file: %q", temp)
	}
	if !ValidSaveName(saveName) {
		return "", ErrSaveName
	}
	if err := m.checkNew(ctx, name, gameVersion); err != nil {
		return "", err
	}
	return m.create(ctx, name, gameVersion, spaceAge, func(worlds *os.Root, dir string) error {
		return worlds.Rename(temp, path.Join(dir, SavesDir, saveName))
	})
}

// checkNew refuses a new world's name or game version before any work is
// done. create checks the name again when it makes the world.
func (m *Manager) checkNew(ctx context.Context, name, gameVersion string) error {
	if !ValidGameVersion(gameVersion) {
		return ErrBadGameVersion
	}
	m.createMu.Lock()
	defer m.createMu.Unlock()
	_, err := m.checkName(ctx, "", name)
	return err
}

// copyFile copies one regular file from src to dst through a temp file,
// keeping its modification time so "last played" stays true. Parent
// folders are made as needed.
func copyFile(src *os.Root, from string, dst *os.Root, to string) error {
	if err := mkdirAll(dst, path.Dir(to)); err != nil {
		return err
	}
	in, err := src.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("copy %s: not a regular file", from)
	}
	out, temp, err := CreateTemp(dst, path.Dir(to))
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = dst.Chtimes(temp, info.ModTime(), info.ModTime())
	}
	if err == nil {
		err = Commit(dst, temp, to)
	}
	if err != nil {
		_ = dst.Remove(temp)
	}
	return err
}

// mkdirAll makes dir and its parents inside root with DirMode.
func mkdirAll(root *os.Root, dir string) error {
	if dir == "." || dir == "" {
		return nil
	}
	if info, err := root.Lstat(dir); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s is not a folder", dir)
		}
		return nil
	}
	if err := mkdirAll(root, path.Dir(dir)); err != nil {
		return err
	}
	return Mkdir(root, dir)
}
