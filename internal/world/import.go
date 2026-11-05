package world

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// InboxFolder is one subfolder of the import inbox (SPEC 5.5).
type InboxFolder struct {
	Name    string
	Saves   int
	HasMods bool
	// Bytes is what an import would copy.
	Bytes uint64
	// Read is true when the newest save's header was read; GameVersion and
	// SpaceAge are set only then.
	Read        bool
	GameVersion string
	SpaceAge    bool
	// Clash is set when a save name is in two places, which an import
	// refuses; the folder is listed as not importable (SPEC 5.5).
	Clash error
	// Newest is the folder's newest save, by modification time; its path
	// is empty when the folder has no saves.
	Newest importFile
	// ServerName is the name in the folder's config/server-settings.json,
	// or "" when it has none or the file cannot be read.
	ServerName string
	// Imported is the world this folder, with the same newest save, was
	// already imported as; nil when it was not (SPEC 5.5).
	Imported *ImportRecord
}

// ImportRecord names a world made from an inbox folder, and when.
type ImportRecord struct {
	WorldID, WorldName string
	At                 time.Time
}

// SuggestedName is the name a new world from the folder starts with: the
// server's name from its settings when there is one, else the folder name.
func (f InboxFolder) SuggestedName() string {
	if f.ServerName != "" {
		return f.ServerName
	}
	return f.Name
}

// sameSave reports whether rec was made from this folder and its newest save
// as it is now: same folder and save name, size, and time to the nanosecond.
func (f InboxFolder) sameSave(rec *ImportedFrom) bool {
	return rec != nil && f.Newest.path != "" && rec.Folder == f.Name &&
		rec.Save == path.Base(f.Newest.path) && rec.SaveSize == f.Newest.size &&
		rec.SaveMtime.Equal(f.Newest.mtime)
}

// Importable reports whether an import of the folder can go ahead.
func (f InboxFolder) Importable() bool { return f.Saves > 0 && f.Clash == nil }

// ErrBadGameVersion is returned for a game version that is not an exact
// release of a supported line (SPEC 10.8), such as 2.0.77.
var ErrBadGameVersion = errors.New("enter the game version as three numbers from a supported line (1.1, 2.0 or 2.1), such as 2.0.77")

// ErrSpaceAgeOld is returned for Space Age on a game version before 2.0,
// which has no Space Age.
var ErrSpaceAgeOld = errors.New("space Age needs Factorio 2.0 or later. Untick Space Age for a Factorio 1.1 world")

// ErrNoSaves is returned when an inbox folder holds no save zips.
var ErrNoSaves = errors.New("this folder holds no save files (.zip) at its top level or in a saves folder, so there is nothing to import")

// ErrNoFolder is returned when the chosen inbox folder does not exist.
var ErrNoFolder = errors.New("choose a folder from the import folder")

// ErrSaveClash is returned when a save name is in both the folder's top
// level and its saves/ folder, so Foreman cannot tell which one is meant.
var ErrSaveClash = errors.New("save name in two places")

// saveClashError names the clashing file. It matches ErrSaveClash.
type saveClashError struct{ name string }

func (e saveClashError) Error() string {
	return fmt.Sprintf("the save %s is in both the folder and its saves folder, so it is not clear which to import. Remove or rename one of them", e.name)
}

func (saveClashError) Is(target error) bool { return target == ErrSaveClash }

var gameVersionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// ValidGameVersion reports whether v looks like x.y.z.
func ValidGameVersion(v string) bool { return gameVersionRE.MatchString(v) }

// importFile is a regular file to copy, by its path inside the source folder.
type importFile struct {
	path  string
	size  uint64
	mtime time.Time
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
	records, err := m.importRecords()
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
		for _, r := range records {
			if f.sameSave(r.from) {
				f.Imported = &r.ImportRecord
			}
		}
		out = append(out, f)
	}
	return out, nil
}

type importRecord struct {
	ImportRecord
	from *ImportedFrom
}

// importRecords reads where each world was imported from. A world whose
// world.json cannot be read has no record.
func (m *Manager) importRecords() ([]importRecord, error) {
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = worlds.Close() }()
	entries, err := fs.ReadDir(worlds.FS(), ".")
	if err != nil {
		return nil, err
	}
	var out []importRecord
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		meta, err := readMetaIn(worlds, e.Name())
		if err != nil || meta.ImportedFrom == nil {
			continue
		}
		out = append(out, importRecord{ImportRecord{e.Name(), displayName(e.Name(), meta, nil), meta.ImportedFrom.At}, meta.ImportedFrom})
	}
	return out, nil
}

// readMetaIn reads world.json of the world dir inside worlds.
func readMetaIn(worlds *os.Root, dir string) (Meta, error) {
	w, err := openDir(worlds, dir)
	if err != nil {
		return Meta{}, err
	}
	defer func() { _ = w.Close() }()
	return ReadMeta(w)
}

// ImportedFrom returns where the world id was imported from, or nil when it
// was not imported from a folder or its world.json cannot be read.
func (m *Manager) ImportedFrom(id string) *ImportedFrom {
	if !ValidID(id) {
		return nil
	}
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return nil
	}
	defer func() { _ = worlds.Close() }()
	meta, err := readMetaIn(worlds, id)
	if err != nil {
		return nil
	}
	return meta.ImportedFrom
}

// InboxEntry describes one inbox folder by name, as Inbox does, without
// reading the others. A name that is not a folder directly in the inbox is
// ErrNoFolder.
func (m *Manager) InboxEntry(name string) (InboxFolder, error) {
	if !inboxName(name) {
		return InboxFolder{}, ErrNoFolder
	}
	inbox, err := openFolder(m.dirs.Imports)
	if err != nil {
		return InboxFolder{}, err
	}
	defer func() { _ = inbox.Close() }()
	if info, err := inbox.Lstat(name); err != nil || !info.IsDir() {
		return InboxFolder{}, ErrNoFolder
	}
	return inboxFolder(inbox, name)
}

// inboxName reports whether name can be an inbox folder: one path element,
// not hidden.
func inboxName(name string) bool {
	return name != "" && path.Base(name) == name && !strings.HasPrefix(name, ".")
}

func inboxFolder(inbox *os.Root, name string) (InboxFolder, error) {
	src, err := inbox.OpenRoot(name)
	if err != nil {
		return InboxFolder{}, err
	}
	defer func() { _ = src.Close() }()
	saves, mods, config, err := importPlan(src)
	if err != nil {
		return InboxFolder{}, err
	}
	f := InboxFolder{Name: name, Saves: len(saves), HasMods: mods != nil}
	if n, clash := saveClash(saves); clash {
		f.Clash = saveClashError{n}
	}
	for _, x := range slices.Concat(saves, mods, config) {
		f.Bytes += x.size
	}
	if len(saves) > 0 {
		f.Newest = newestImport(saves)
		if info, ok := ReadSaveInfo(src, f.Newest.path); ok {
			f.Read, f.GameVersion, f.SpaceAge = true, info.GameVersion, info.SpaceAge
		}
	}
	f.ServerName = serverName(src)
	return f, nil
}

// newestImport returns the save with the latest modification time; saves must
// not be empty.
func newestImport(saves []importFile) importFile {
	newest := saves[0]
	for _, s := range saves[1:] {
		if s.mtime.After(newest.mtime) {
			newest = s
		}
	}
	return newest
}

// maxServerName is the world name field's length limit.
const maxServerName = 100

// serverName reads the name from src's config/server-settings.json, or ""
// when there is none usable. Nothing else in the file is kept: it may hold
// the game password.
func serverName(src *os.Root) string {
	b, err := ReadSmallFile(src, path.Join(ConfigDir, ServerSettingsFile), maxMetaFile)
	if err != nil {
		return ""
	}
	var doc struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return ""
	}
	name := strings.TrimSpace(doc.Name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxServerName {
		return ""
	}
	return name
}

// realFolder reports whether dir inside src is a real folder. A dir that
// is missing, or is not a real folder (a symlink, for one), is not, and
// that is not an error: there is nothing to copy from it.
func realFolder(src *os.Root, dir string) (bool, error) {
	info, err := src.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

// regularFiles lists the regular files directly in dir whose names pass
// keep. A dir that is missing, or is not a real folder, has none.
func regularFiles(src *os.Root, dir string, keep func(name string) bool) ([]importFile, error) {
	if dir != "." {
		if ok, err := realFolder(src, dir); err != nil || !ok {
			return nil, err
		}
	}
	entries, err := fs.ReadDir(src.FS(), dir)
	if err != nil {
		return nil, err
	}
	var out []importFile
	for _, e := range entries {
		if !e.Type().IsRegular() || !keep(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, importFile{path.Join(dir, e.Name()), uint64(info.Size()), info.ModTime()}) //nolint:gosec // a file size is never negative
	}
	return out, nil
}

// saveClash returns the first save name that appears twice in saves.
func saveClash(saves []importFile) (string, bool) {
	seen := map[string]bool{}
	for _, s := range saves {
		n := path.Base(s.path)
		if seen[n] {
			return n, true
		}
		seen[n] = true
	}
	return "", false
}

// importPlan lists what an import copies from src: save zips from its top
// level and from saves/, every regular file under mods/, and the SPEC 5.1
// files in config/. mods is nil when there is no mods/ folder. Symlinks and
// other special files are skipped, never followed.
func importPlan(src *os.Root) (saves, mods, config []importFile, err error) {
	saves, err = regularFiles(src, ".", ValidSaveName)
	if err != nil {
		return nil, nil, nil, err
	}
	inner, err := regularFiles(src, SavesDir, ValidSaveName)
	if err != nil {
		return nil, nil, nil, err
	}
	saves = append(saves, inner...)
	config, err = regularFiles(src, ConfigDir, func(n string) bool { return slices.Contains(ConfigFiles, n) })
	if err != nil {
		return nil, nil, nil, err
	}
	if ok, err := realFolder(src, ModsDir); err != nil || !ok {
		return saves, nil, config, err
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
		mods = append(mods, importFile{p, uint64(info.Size()), info.ModTime()}) //nolint:gosec // a file size is never negative
		return nil
	})
	return saves, mods, config, err
}

// ImportFolder makes a new world from the inbox folder named folder: its
// save zips (top level and saves/), mods/ folder and SPEC 5.1 config files
// are copied, never linked or moved, so the source is left exactly as it
// was (SPEC 5.5). It refuses
// up front when the disk lacks room for the copy (SPEC 6.11).
func (m *Manager) ImportFolder(ctx context.Context, folder, name, gameVersion string, spaceAge bool) (string, error) {
	if err := m.checkNew(ctx, name, gameVersion, spaceAge); err != nil {
		return "", err
	}
	if !inboxName(folder) {
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

	saves, mods, config, err := importPlan(src)
	if err != nil {
		return "", err
	}
	if len(saves) == 0 {
		return "", ErrNoSaves
	}
	if n, clash := saveClash(saves); clash {
		return "", saveClashError{n}
	}
	var need uint64
	for _, f := range slices.Concat(saves, mods, config) {
		need += f.size
	}
	release, err := Reserve(m.dirs.Worlds, need)
	if err != nil {
		return "", err
	}
	defer release()
	newest := newestImport(saves)
	from := &ImportedFrom{Folder: folder, Save: path.Base(newest.path), SaveSize: newest.size, SaveMtime: newest.mtime.UTC(), At: m.now().UTC()}
	return m.create(ctx, name, gameVersion, spaceAge, from, func(worlds *os.Root, dir string) error {
		dst, err := worlds.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer func() { _ = dst.Close() }()
		for _, f := range saves {
			if err := copyFile(src, f.path, dst, path.Join(SavesDir, path.Base(f.path))); err != nil {
				return err
			}
		}
		for _, f := range slices.Concat(mods, config) {
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
	if err := m.checkNew(ctx, name, gameVersion, spaceAge); err != nil {
		return "", err
	}
	return m.create(ctx, name, gameVersion, spaceAge, nil, func(worlds *os.Root, dir string) error {
		return worlds.Rename(temp, path.Join(dir, SavesDir, saveName))
	})
}

// checkNew refuses a new world's name or game version before any work is
// done. create checks the name again when it makes the world.
func (m *Manager) checkNew(ctx context.Context, name, gameVersion string, spaceAge bool) error {
	if !SupportedGameVersion(gameVersion) {
		return ErrBadGameVersion
	}
	if spaceAge && strings.HasPrefix(gameVersion, "1.") {
		return ErrSpaceAgeOld
	}
	m.createMu.Lock()
	defer m.createMu.Unlock()
	_, err := m.checkName(ctx, "", name)
	return err
}

// copyFile copies one regular file from src to dst through a temp file,
// keeping its modification time so "last played" stays true. The copy's
// mode comes from where it lands (copyMode). Parent folders are made as
// needed.
func copyFile(src *os.Root, from string, dst *os.Root, to string) error {
	if err := mkdirAll(dst, path.Dir(to)); err != nil {
		return err
	}
	in, info, err := openRegular(src, from)
	if err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	defer func() { _ = in.Close() }()
	out, temp, err := createTempMode(dst, path.Dir(to), copyMode(to))
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
