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
	// ErrWorldChanged's text is the wording shown to the user as it is.
	ErrWorldChanged = errors.New("This world was changed or deleted while your upload was running. Nothing was saved; upload it again.") //nolint:staticcheck // shown to the user as written
)

// AddSaveWait is how long AddSave waits for a busy world before giving up.
const AddSaveWait = 60 * time.Second

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

	meta, err := ReadMeta(root)
	out := SaveList{WorldName: displayName(id, meta, err)}
	if err == nil {
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
	// Lstat first: a symlink, even one that stays inside the world, could
	// serve another file such as the RCON password (SPEC 14.8).
	p := path.Join(SavesDir, name)
	linfo, err := root.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && !linfo.Mode().IsRegular()) {
		return nil, nil, ErrSaveNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	f, err := root.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrSaveNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	// The name could be swapped for a link between the Lstat and the open.
	info, err := f.Stat()
	if err == nil && !os.SameFile(info, linfo) {
		err = ErrSaveNotFound
	}
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// AddSave moves an uploaded temp file, named relative to the world's folder
// as the upload helper returns it, into the saves folder as name. It refuses
// a name that already exists: no upload overwrites a save (SPEC 5.11). The
// temp file is removed whatever happens, so the caller has nothing to clean
// up. A busy world is waited for, up to AddSaveWait, so a finished upload is
// not thrown away.
func (m *Manager) AddSave(id, name, temp string) error {
	root, err := m.OpenWorld(id)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrWorldChanged
	}
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	defer func() { _ = root.Remove(temp) }() // gone already after a good commit

	if !ValidSaveName(name) {
		return ErrSaveName
	}
	unlock, err := m.lockWait(id, m.addSaveWait)
	if err != nil {
		return err
	}
	defer unlock()

	// A restore or delete while the upload streamed or while this waited
	// replaced the folder the temp file was in; root is then the old one.
	if _, err := root.Lstat(temp); errors.Is(err, fs.ErrNotExist) {
		return ErrWorldChanged
	} else if err != nil {
		return err
	}
	if _, err := readChangeable(root); err != nil {
		return err
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

	root, err := m.OpenWorld(id)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	meta, err := readChangeable(root)
	if err != nil {
		return err
	}

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
