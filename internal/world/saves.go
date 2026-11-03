package world

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Save is one save zip in a world's saves folder (SPEC 5.11).
type Save struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// SaveList is what the saves page shows for one world.
type SaveList struct {
	WorldName string
	// LoadSave is the save chosen to load, or empty for the newest
	// (SPEC 7.11).
	LoadSave string
	// Newer is true when a newer Foreman wrote world.json, so saves can be
	// downloaded but nothing can be changed.
	Newer bool
	// Saves are newest first.
	Saves []Save
}

// Refusals the saves page shows in words.
var (
	ErrSaveName     = errors.New("a save's name must end in .zip, must not start with a dot, and must not contain a slash")
	ErrSaveExists   = errors.New("this world already has a save with that name. Rename the file and upload it again")
	ErrSaveNotFound = errors.New("this world has no save with that name")
)

// maxSaveNameLength is the longest file name most filesystems allow.
const maxSaveNameLength = 255

// ValidSaveName reports whether name is a plain file name of a save: a base
// name that ends in .zip and does not start with a dot (SPEC 5.11).
func ValidSaveName(name string) bool {
	if name == "" || len(name) > maxSaveNameLength || !strings.HasSuffix(name, ".zip") || name[0] == '.' {
		return false
	}
	if strings.ContainsAny(name, `/\`) || path.Base(name) != name {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

// listSaves returns the saves in a world's root, newest first, with equal
// times in name order. Anything else in the folder, such as a partial
// upload, is not a save.
func listSaves(root *os.Root) ([]Save, error) {
	entries, err := fs.ReadDir(root.FS(), SavesDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Save
	for _, e := range entries {
		if !e.Type().IsRegular() || !ValidSaveName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, Save{Name: e.Name(), Size: info.Size(), ModTime: info.ModTime()})
	}
	slices.SortFunc(out, func(a, b Save) int {
		return cmp.Or(b.ModTime.Compare(a.ModTime), cmp.Compare(a.Name, b.Name))
	})
	return out, nil
}

// newestSave is the single decision of which save the game will load: the
// one with the newest modification time (SPEC 7.11).
func newestSave(root *os.Root) (Save, bool, error) {
	saves, err := listSaves(root)
	if err != nil || len(saves) == 0 {
		return Save{}, false, err
	}
	return saves[0], true, nil
}

// NewestSave returns the world's newest save, the one the game loads. The
// pre-start check must use it (SPEC 7.4, 7.11). found is false when the
// world has no saves.
func (m *Manager) NewestSave(id string) (save Save, found bool, err error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return Save{}, false, err
	}
	defer func() { _ = root.Close() }()
	return newestSave(root)
}

// Saves lists a world's saves with the choice of which loads (SPEC 5.11).
// A world whose world.json cannot be read still lists its saves, under its
// id.
func (m *Manager) Saves(id string) (SaveList, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return SaveList{}, err
	}
	defer func() { _ = root.Close() }()

	out := SaveList{WorldName: id}
	if meta, err := ReadMeta(root); err == nil {
		out.WorldName = meta.Name
		out.LoadSave = meta.LoadSave
		out.Newer = meta.Newer()
	}
	out.Saves, err = listSaves(root)
	return out, err
}

// OpenSave opens a save for download. The caller closes the file.
func (m *Manager) OpenSave(id, name string) (*os.File, fs.FileInfo, error) {
	if !ValidSaveName(name) {
		return nil, nil, ErrSaveNotFound
	}
	root, err := m.OpenWorld(id)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(path.Join(SavesDir, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrSaveNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = ErrSaveNotFound
	}
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// changeable opens a world for a change: its root and world.json, refusing
// one a newer Foreman wrote. The caller closes the root.
func (m *Manager) changeable(id string) (*os.Root, Meta, error) {
	root, err := m.OpenWorld(id)
	if err != nil {
		return nil, Meta{}, err
	}
	meta, err := ReadMeta(root)
	if err == nil && meta.Newer() {
		err = ErrNewerFormat
	}
	if err != nil {
		_ = root.Close()
		return nil, Meta{}, err
	}
	return root, meta, nil
}

// AddSave moves an uploaded temp file, named relative to the world's folder
// as the upload helper returns it, into the saves folder as name. It refuses
// a name that already exists: no upload overwrites a save (SPEC 5.11). The
// temp file is removed whatever happens, so the caller has nothing to clean
// up.
func (m *Manager) AddSave(id, name, temp string) error {
	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	defer func() { _ = root.Remove(temp) }() // gone already after a good commit

	if !ValidSaveName(name) {
		return ErrSaveName
	}
	unlock, err := m.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	meta, err := ReadMeta(root)
	if err != nil {
		return err
	}
	if meta.Newer() {
		return ErrNewerFormat
	}
	final := path.Join(SavesDir, name)
	if _, err := root.Lstat(final); err == nil {
		return ErrSaveExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return Commit(root, temp, final)
}

// SetLoadSave sets which save loads: a save's name, or empty for the newest
// (SPEC 7.11). It refuses a name the world has no save of.
func (m *Manager) SetLoadSave(id, name string) error {
	unlock, err := m.Lock(id)
	if err != nil {
		return err
	}
	defer unlock()

	root, meta, err := m.changeable(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	if name != "" {
		if !ValidSaveName(name) {
			return ErrSaveNotFound
		}
		info, err := root.Lstat(path.Join(SavesDir, name))
		if errors.Is(err, fs.ErrNotExist) || (err == nil && !info.Mode().IsRegular()) {
			return ErrSaveNotFound
		}
		if err != nil {
			return fmt.Errorf("check save: %w", err)
		}
	}
	meta.LoadSave = name
	return WriteMeta(root, meta)
}
