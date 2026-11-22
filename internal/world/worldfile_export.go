package world

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
)

// Refusals of an export (SPEC 5.13). The text never repeats file contents.
var (
	// ErrExportFile is a world that holds a file a world file cannot carry:
	// a symlink or other special file, a name the format forbids, or a
	// server-settings.json whose secrets cannot be removed. The error that
	// wraps it names the path.
	ErrExportFile = errors.New("this world holds a file a world file cannot carry")
	// ErrExportTooManyFiles is a world with more files than a world file may
	// hold (SPEC 14.6), so the file could not be imported again.
	ErrExportTooManyFiles = errors.New("this world has more files than a world file can hold")
)

// ExportFileError names the file an export cannot carry. It matches
// ErrExportFile.
type ExportFileError struct {
	Path   string
	Reason string
}

func (e *ExportFileError) Error() string {
	return fmt.Sprintf("%s: %q %s", ErrExportFile, e.Path, e.Reason)
}

func (*ExportFileError) Is(target error) bool { return target == ErrExportFile }

// WorldExport is a world prepared for export: its files hard-linked or copied
// into a staging folder, so the download runs without the world's lock (SPEC
// 5.13). Close removes the folder.
type WorldExport struct {
	name        string
	saves       ExportSaves
	passwordSet bool
	bytes       uint64
	now         func() time.Time
	files       []exportFile

	worlds  *os.Root
	stage   *os.Root
	dirName string
	once    sync.Once
	closed  bool
	mu      sync.Mutex
}

// exportFile is one staged entry of the ZIP. Its name is the entry name and
// the path under the staging folder.
type exportFile struct {
	name string
	kind entryKind
}

// exportOrder is the order of the entries after foreman-world.json: the small
// files a person or an import reads first, then the saves, then the mods.
var exportOrder = []entryKind{entryMeta, entryConfig, entrySave, entryMod}

// PrepareExport takes the world's lock only while it stages the files of a
// world file, and refuses a world a newer Foreman wrote, a busy world and a
// world with a link or special file (SPEC 5.13). A running world is read as
// it is on disk: the last save the game finished.
func (m *Manager) PrepareExport(ctx context.Context, id string, saves ExportSaves) (*WorldExport, error) {
	if saves != ExportAllSaves && saves != ExportLoadedSave {
		return nil, errors.New("export: choose all saves or only the save that loads")
	}
	unlock, err := m.Lock(id)
	if err != nil {
		return nil, err
	}
	defer unlock()

	root, err := m.OpenWorld(id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	meta, err := readChangeable(root)
	if err != nil {
		return nil, err
	}
	plan, loaded, err := planExport(root, meta, saves)
	if err != nil {
		return nil, err
	}
	// The format file and world.json are two more entries.
	if len(plan)+2 > maxWorldFileEntries {
		return nil, ErrExportTooManyFiles
	}
	release, err := Reserve(m.dirs.Worlds, copyBytes(plan)+maxMetaFile+gamesettings.MaxBytes)
	if err != nil {
		return nil, err
	}
	defer release()

	return m.stageExport(ctx, id, root, meta, saves, loaded, plan)
}

// planExport lists the files an export holds: the classifier of the world
// file format (worldFileEntry) decides which of the world's files are in,
// the same one an import uses. loaded is the save that loads, "" when the
// world has none on disk.
func planExport(root *os.Root, meta Meta, saves ExportSaves) (plan []ManifestEntry, loaded string, err error) {
	files, err := worldFiles(root)
	if err != nil {
		var u *UnsupportedFileError
		if errors.As(err, &u) {
			return nil, "", &ExportFileError{Path: u.Path, Reason: "is a symlink or special file, which a world file cannot hold. Remove it or replace it with a copy, then try again"}
		}
		return nil, "", err
	}
	name, found, err := loadName(root, meta)
	if err != nil {
		return nil, "", err
	}
	// A chosen save that is gone from the folder is not "the save that loads".
	if found && slices.ContainsFunc(files, func(f ManifestEntry) bool { return f.Path == path.Join(SavesDir, name) }) {
		loaded = name
	}
	for _, f := range files {
		// Hidden files are not exported (SPEC 5.13), under mods/ too: a mod
		// folder kept in git holds .git/config, which can hold a token.
		if strings.HasPrefix(f.Path, ModsDir+"/") && hasHiddenSegment(f.Path) {
			continue
		}
		kind, ok := worldFileEntry(f.Path)
		if !ok {
			// Only the mods folder's names are Foreman's to refuse: they are
			// the files of mods that a person chose. Anything else outside
			// the format is simply not exported.
			if strings.HasPrefix(f.Path, ModsDir+"/") {
				return nil, "", &ExportFileError{Path: f.Path, Reason: "has a name a world file cannot hold. Rename or remove it, then try again"}
			}
			continue
		}
		switch kind {
		case entrySave:
			if saves == ExportLoadedSave && path.Base(f.Path) != loaded {
				continue
			}
		case entryConfig, entryMod:
		default:
			continue
		}
		plan = append(plan, f)
	}
	return plan, loaded, nil
}

// hasHiddenSegment reports whether any segment of p starts with a dot.
func hasHiddenSegment(p string) bool {
	return slices.ContainsFunc(strings.Split(p, "/"), func(s string) bool { return strings.HasPrefix(s, ".") })
}

// stageExport builds the staging folder and the WorldExport over it. The
// caller holds the world's lock.
func (m *Manager) stageExport(ctx context.Context, id string, root *os.Root, meta Meta, saves ExportSaves, loaded string, plan []ManifestEntry) (_ *WorldExport, err error) {
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return nil, err
	}
	dirName := PrefixStaging + "export-" + randomSuffix()
	if err := Mkdir(worlds, dirName); err != nil {
		_ = worlds.Close()
		return nil, err
	}
	var stage *os.Root
	// A failure or a panic removes the staging folder; the panic goes on.
	defer func() {
		r := recover()
		if err != nil || r != nil {
			if stage != nil {
				_ = stage.Close()
			}
			_ = worlds.RemoveAll(dirName)
			_ = worlds.Close()
		}
		if r != nil {
			panic(r)
		}
	}()
	if stage, err = openDir(worlds, dirName); err != nil {
		return nil, err
	}

	e := &WorldExport{
		name:    displayName(id, meta, nil),
		saves:   saves,
		now:     m.now,
		worlds:  worlds,
		stage:   stage,
		dirName: dirName,
	}
	keptSave := false
	for _, f := range plan {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		kind, _ := worldFileEntry(f.Path)
		switch {
		case f.Path == path.Join(ConfigDir, ServerSettingsFile):
			set, err := stageSettings(root, stage, f)
			if err != nil {
				return nil, err
			}
			e.passwordSet = set["game_password"]
		default:
			if _, err := placeFile(root, stage, f); err != nil {
				return nil, fmt.Errorf("stage %s: %w", f.Path, err)
			}
		}
		if kind == entrySave && path.Base(f.Path) == meta.LoadSave {
			keptSave = true
		}
		e.files = append(e.files, exportFile{name: f.Path, kind: kind})
	}

	// world.json says nothing about the old host: the folder it was imported
	// from, and the archive flag (an imported world arrives unarchived).
	out := meta
	out.ImportedFrom = nil
	out.Archived = false
	if saves == ExportLoadedSave {
		out.LoadSave = loaded
	} else if !keptSave {
		out.LoadSave = ""
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomicWith(stage, MetaFile, append(b, '\n'), FileMode); err != nil {
		return nil, err
	}
	if info, err := root.Lstat(MetaFile); err == nil {
		_ = stage.Chtimes(MetaFile, info.ModTime(), info.ModTime())
	}
	e.files = append(e.files, exportFile{name: MetaFile, kind: entryMeta})

	// What is staged is what the ZIP holds: size and times from the staged
	// files themselves.
	for _, f := range e.files {
		info, err := stage.Lstat(f.name)
		if err != nil {
			return nil, err
		}
		e.bytes += uint64(info.Size()) //nolint:gosec // a file size is never negative
	}
	e.sort()
	return e, nil
}

// stageSettings writes server-settings.json into the staging folder with
// every secret removed (SPEC 14.8) and reports which secrets it had. A file
// that cannot be read as one JSON object is refused: its secrets could not
// be found, so nothing of it may leave the host.
func stageSettings(root, stage *os.Root, f ManifestEntry) (map[string]bool, error) {
	b, err := ReadSmallFile(root, f.Path, gamesettings.MaxBytes)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.Path, err)
	}
	stripped, set := gamesettings.StripSecrets(b)
	if stripped == nil {
		return nil, &ExportFileError{Path: f.Path, Reason: "is not one JSON object Foreman can read, so its secrets cannot be removed. Fix or remove it, then try again"}
	}
	if err := mkdirAll(stage, ConfigDir); err != nil {
		return nil, err
	}
	if err := writeFileAtomicWith(stage, f.Path, stripped, copyMode(f.Path)); err != nil {
		return nil, err
	}
	_ = stage.Chtimes(f.Path, f.ModTime, f.ModTime)
	return set, nil
}

// sort puts the entries in the order of exportOrder, each group by name.
func (e *WorldExport) sort() {
	var out []exportFile
	for _, k := range exportOrder {
		var group []exportFile
		for _, f := range e.files {
			if f.kind == k {
				group = append(group, f)
			}
		}
		sortByName(group)
		out = append(out, group...)
	}
	e.files = out
}

func sortByName(files []exportFile) {
	slices.SortFunc(files, func(a, b exportFile) int { return strings.Compare(a.name, b.name) })
}

// Name is the world's name, for the file name.
func (e *WorldExport) Name() string { return e.name }

// Bytes is the unpacked size of the export, for the card.
func (e *WorldExport) Bytes() uint64 { return e.bytes }

// GamePasswordSet is whether the world had a game password, which the file
// does not hold.
func (e *WorldExport) GamePasswordSet() bool { return e.passwordSet }

// zipBuffer is the size of the copy buffer of one download.
const zipBuffer = 256 << 10

// WriteZip streams the staged files as a world file into w. foreman-world.json
// comes first. Saves and mod zips are stored, as they are already
// compressed; everything else is deflated. Nothing is held in memory but one
// buffer. On a failure it returns at once without writing the ZIP's
// directory, so what was sent is not a complete file (SPEC 5.13).
func (e *WorldExport) WriteZip(w io.Writer, foremanVersion string) error {
	e.mu.Lock()
	closed := e.closed
	e.mu.Unlock()
	if closed {
		return errors.New("export: the staged files are gone")
	}
	zw := zip.NewWriter(w)
	at := e.now().UTC().Truncate(time.Second)
	doc, err := json.Marshal(WorldFileDoc{
		// The lowest format that holds what is written (SPEC 5.13): there are
		// no snapshots in this file, so it is format 1 whatever this Foreman
		// could write.
		FormatVersion:   WorldFileFormat,
		ExportedAt:      at,
		ForemanVer:      foremanVersion,
		Saves:           e.saves,
		GamePasswordSet: e.passwordSet,
	})
	if err != nil {
		return err
	}
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: WorldFileFormatFile, Method: zip.Deflate, Modified: at})
	if err != nil {
		return err
	}
	if _, err := fw.Write(append(doc, '\n')); err != nil {
		return err
	}
	buf := make([]byte, zipBuffer)
	for _, f := range e.files {
		if err := e.writeEntry(zw, f.name, buf); err != nil {
			return err
		}
	}
	return zw.Close()
}

// writeEntry copies one staged file into the ZIP.
func (e *WorldExport) writeEntry(zw *zip.Writer, name string, buf []byte) error {
	in, info, err := openRegular(e.stage, name)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	defer func() { _ = in.Close() }()
	method := zip.Deflate
	if isZip(name) {
		method = zip.Store
	}
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: info.ModTime()})
	if err != nil {
		return err
	}
	_, err = io.CopyBuffer(fw, struct{ io.Reader }{in}, buf)
	return err
}

// Close removes the staging folder. A crash leaves it with the .staging-
// prefix, which Recover clears at startup.
func (e *WorldExport) Close() error {
	var err error
	e.once.Do(func() {
		e.mu.Lock()
		e.closed = true
		e.mu.Unlock()
		_ = e.stage.Close()
		err = e.worlds.RemoveAll(e.dirName)
		if cerr := e.worlds.Close(); err == nil {
			err = cerr
		}
	})
	return err
}
