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

// InboxFolder is one subfolder of the import inbox (R5 in the Epic 2 plan,
// SPEC 5.5).
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
	inbox, err := os.OpenRoot(m.dirs.Imports)
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
// up front when the disk lacks room for the copy (R19).
func (m *Manager) ImportFolder(ctx context.Context, folder, name, gameVersion string, spaceAge bool) (string, error) {
	if err := m.checkNew(ctx, name, gameVersion); err != nil {
		return "", err
	}
	if folder == "" || path.Base(folder) != folder || strings.HasPrefix(folder, ".") {
		return "", ErrNoFolder
	}
	inbox, err := os.OpenRoot(m.dirs.Imports)
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
	if err := CheckSpace(m.dirs.Worlds, need); err != nil {
		return "", err
	}

	staging, err := m.NewStaging()
	if err != nil {
		return "", err
	}
	id, err := m.fillAndCreate(ctx, staging, name, gameVersion, spaceAge, func(dst *os.Root) error {
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
	if err != nil {
		return "", err
	}
	return id, nil
}

// checkNew refuses a new world's name or game version before any work is
// done. Create checks the name again when it makes the world.
func (m *Manager) checkNew(ctx context.Context, name, gameVersion string) error {
	if !ValidGameVersion(gameVersion) {
		return ErrBadGameVersion
	}
	m.createMu.Lock()
	defer m.createMu.Unlock()
	_, err := m.checkName(ctx, name)
	return err
}

// NewStaging makes an empty work folder in the worlds folder, holding saves/
// and mods/, for a new world's files to land in before the world exists.
// It returns the folder's name inside the worlds folder. Startup recovery
// removes one a crash leaves behind.
func (m *Manager) NewStaging() (string, error) {
	worlds, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		return "", err
	}
	defer func() { _ = worlds.Close() }()
	name := PrefixStaging + "import-" + randomSuffix()
	if err := Mkdir(worlds, name); err != nil {
		return "", err
	}
	for _, sub := range []string{SavesDir, ModsDir} {
		if err := Mkdir(worlds, path.Join(name, sub)); err != nil {
			_ = worlds.RemoveAll(name)
			return "", err
		}
	}
	return name, nil
}

// DiscardStaging removes a work folder NewStaging made.
func (m *Manager) DiscardStaging(staging string) error {
	if !strings.HasPrefix(staging, PrefixStaging) || path.Base(staging) != staging {
		return fmt.Errorf("not a staging folder: %q", staging)
	}
	worlds, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		return err
	}
	defer func() { _ = worlds.Close() }()
	return worlds.RemoveAll(staging)
}

// CreateFromStaging makes a new world with Create and moves the staged
// saves/ and mods/ into it. The staging folder is removed either way.
func (m *Manager) CreateFromStaging(ctx context.Context, staging, name, gameVersion string, spaceAge bool) (string, error) {
	return m.fillAndCreate(ctx, staging, name, gameVersion, spaceAge, func(*os.Root) error { return nil })
}

// fillAndCreate runs fill on the staging folder, where a failure leaves no
// world behind, then makes the world and moves the staged folders in with
// two renames on one filesystem.
func (m *Manager) fillAndCreate(ctx context.Context, staging, name, gameVersion string, spaceAge bool, fill func(*os.Root) error) (id string, err error) {
	defer func() {
		if derr := m.DiscardStaging(staging); err == nil {
			err = derr
		}
	}()
	worlds, err := os.OpenRoot(m.dirs.Worlds)
	if err != nil {
		return "", err
	}
	defer func() { _ = worlds.Close() }()
	st, err := worlds.OpenRoot(staging)
	if err != nil {
		return "", err
	}
	defer func() { _ = st.Close() }()
	if err := fill(st); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	id, err = m.Create(ctx, name, gameVersion, spaceAge)
	if err != nil {
		return "", err
	}
	unlock, err := m.Lock(id)
	if err != nil {
		return "", err
	}
	defer unlock()
	for _, sub := range []string{SavesDir, ModsDir} {
		// Remove refuses a folder that is not empty, so nothing is lost.
		if err := worlds.Remove(path.Join(id, sub)); err != nil {
			return "", err
		}
		if err := worlds.Rename(path.Join(staging, sub), path.Join(id, sub)); err != nil {
			return "", fmt.Errorf("move %s into world %s: %w", sub, id, err)
		}
	}
	return id, syncDir(worlds, id)
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
