package world

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
)

// Limits of a world file's small entries (SPEC 14.6).
const (
	maxWorldFileFormat = 1 << 10
	maxWorldFileMeta   = 64 << 10
	// worldFileMaxRatio is the most a world file may unpack to, as a multiple
	// of its own size. Saves and mods are stored, so a real file is near 1.
	worldFileMaxRatio = 50
	// maxStagedWorldFiles is how many staged uploads may wait at once.
	maxStagedWorldFiles = 2
)

// CreateWorldFileTemp opens a staged file for an uploaded world file, in the
// worlds folder with the PrefixWorldFile prefix, and returns it with its name
// (SPEC 5.14). It removes staged files older than an hour and keeps at most
// two.
func (m *Manager) CreateWorldFileTemp() (*os.File, string, error) {
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = worlds.Close() }()
	m.pruneStagedWorldFiles(worlds)
	return createTempNamed(worlds, PrefixWorldFile+randomSuffix(), FileMode)
}

// pruneStagedWorldFiles removes staged world files older than an hour, then
// the oldest until there is room for one more.
func (m *Manager) pruneStagedWorldFiles(worlds *os.Root) {
	entries, err := fs.ReadDir(worlds.FS(), ".")
	if err != nil {
		return
	}
	type staged struct {
		name string
		info fs.FileInfo
	}
	var kept []staged
	now := m.now()
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), PrefixWorldFile) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > stagedModMaxAge {
			_ = worlds.Remove(e.Name())
			continue
		}
		kept = append(kept, staged{e.Name(), info})
	}
	slices.SortFunc(kept, func(a, b staged) int { return a.info.ModTime().Compare(b.info.ModTime()) })
	for len(kept) >= maxStagedWorldFiles {
		_ = worlds.Remove(kept[0].name)
		kept = kept[1:]
	}
}

// ReadWorldFile reads a staged world file's directory and small entries,
// under the limits of 14.5 and 14.6, for the preview. It changes nothing.
func (m *Manager) ReadWorldFile(temp string, lim WorldFileLimits) (WorldFileInfo, error) {
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return WorldFileInfo{}, err
	}
	defer func() { _ = worlds.Close() }()
	wf, err := openWorldFile(worlds, temp, lim)
	if err != nil {
		return WorldFileInfo{}, err
	}
	defer func() { _ = wf.Close() }()
	return wf.info(), nil
}

// ImportWorldFile makes a new world from a staged world file under the name
// given, and returns its id. It removes temp on success and on a bad file,
// and keeps it on a refusal the form can fix: a name in use, or too little
// disk space (SPEC 5.14).
func (m *Manager) ImportWorldFile(ctx context.Context, temp, name string, lim WorldFileLimits) (string, error) {
	worlds, err := openFolder(m.dirs.Worlds)
	if err != nil {
		return "", err
	}
	defer func() { _ = worlds.Close() }()
	id, err := m.importWorldFile(ctx, worlds, temp, name, lim)
	if err == nil || errors.Is(err, ErrWorldFileBad) || errors.Is(err, ErrWorldFileNewer) {
		if validWorldFileTemp(temp) {
			_ = worlds.Remove(temp)
		}
	}
	return id, err
}

func (m *Manager) importWorldFile(ctx context.Context, worlds *os.Root, temp, name string, lim WorldFileLimits) (string, error) {
	wf, err := openWorldFile(worlds, temp, lim)
	if err != nil {
		return "", err
	}
	defer func() { _ = wf.Close() }()
	meta := wf.meta
	if err := m.checkNew(ctx, name, meta.GameVersion, meta.SpaceAge); err != nil {
		return "", err
	}
	release, err := Reserve(m.dirs.Worlds, wf.bytes)
	if err != nil {
		return "", err
	}
	defer release()
	opts := createOptions{
		newMap:       meta.NewMap,
		loadSave:     meta.LoadSave,
		useWhitelist: meta.UseWhitelist,
		factoryMap:   meta.FactoryMap,
		unknown:      meta.unknown,
	}
	return m.create(ctx, name, meta.GameVersion, meta.SpaceAge, opts, func(worlds *os.Root, dir string) error {
		dst, err := worlds.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer func() { _ = dst.Close() }()
		return wf.extract(ctx, dst, name)
	})
}

// validWorldFileTemp reports whether temp names a staged world file in the
// worlds folder itself, so a crafted name reaches nothing else.
func validWorldFileTemp(temp string) bool {
	return strings.HasPrefix(temp, PrefixWorldFile) && path.Base(temp) == temp && !strings.ContainsAny(temp, `/\`)
}

// worldFile is a staged world file that passed every check of 14.5 and 14.6
// short of writing.
type worldFile struct {
	f      *os.File
	doc    WorldFileDoc
	meta   Meta
	server []byte // server-settings.json, stripped; nil when absent
	// files are the entries to write, each with its own limit.
	files []worldFileEntryLimit
	saves int
	mods  int
	bytes uint64
}

type worldFileEntryLimit struct {
	e     *zip.File
	limit int64
}

func (wf *worldFile) Close() error { return wf.f.Close() }

func (wf *worldFile) info() WorldFileInfo {
	return WorldFileInfo{
		Name:            wf.meta.Name,
		GameVersion:     wf.meta.GameVersion,
		SpaceAge:        wf.meta.SpaceAge,
		Saves:           wf.saves,
		ModFiles:        wf.mods,
		Bytes:           int64(wf.bytes), //nolint:gosec // capped far below MaxInt64 by the ratio check
		LoadSave:        wf.meta.LoadSave,
		ForemanVersion:  wf.doc.ForemanVer,
		GamePasswordSet: wf.doc.GamePasswordSet,
	}
}

// openWorldFile opens the staged file temp and runs the checks of 14.5 and
// 14.6 in order: size, entry count, entry names and modes, per-entry limits,
// the unpack ratio, then the content of the small entries. Every refusal is
// ErrWorldFileBad or ErrWorldFileNewer, which never repeat what the file
// holds.
func openWorldFile(worlds *os.Root, temp string, lim WorldFileLimits) (*worldFile, error) {
	if !validWorldFileTemp(temp) {
		return nil, fmt.Errorf("not a staged world file: %q", temp)
	}
	f, err := worlds.Open(temp)
	if err != nil {
		return nil, err
	}
	wf := &worldFile{f: f}
	if err := wf.check(lim); err != nil {
		_ = f.Close()
		return nil, err
	}
	return wf, nil
}

func (wf *worldFile) check(lim WorldFileLimits) error {
	info, err := wf.f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	if !info.Mode().IsRegular() || size <= 0 || size > MaxWorldFile {
		return ErrWorldFileBad
	}
	if zipEntriesOver(wf.f, size, maxWorldFileEntries) {
		return ErrWorldFileBad
	}
	zr, err := zip.NewReader(wf.f, size)
	if err != nil || len(zr.File) > maxWorldFileEntries {
		return ErrWorldFileBad
	}

	// seen maps each entry name, without a trailing slash, to whether it is
	// a folder.
	seen := map[string]bool{}
	var formatEntry, metaEntry, serverEntry *zip.File
	for _, e := range zr.File {
		kind, ok := worldFileEntry(e.Name)
		key := strings.TrimSuffix(e.Name, "/")
		if _, dup := seen[key]; !ok || dup {
			return ErrWorldFileBad
		}
		seen[key] = kind == entryDir
		mode := e.Mode()
		if kind == entryDir {
			if !mode.IsDir() {
				return ErrWorldFileBad
			}
			continue
		}
		if !mode.IsRegular() {
			return ErrWorldFileBad
		}
		limit := entryLimit(kind, e.Name, lim)
		// A stored entry's two sizes are the same; differing sizes mean a
		// header that lies, caught here before any data is read.
		storedLie := e.Method == zip.Store && e.CompressedSize64 != e.UncompressedSize64
		if storedLie || limit <= 0 || e.UncompressedSize64 > uint64(limit) {
			return ErrWorldFileBad
		}
		wf.bytes += e.UncompressedSize64
		switch kind {
		case entryFormat:
			formatEntry = e
		case entryMeta:
			metaEntry = e
		case entrySave:
			wf.saves++
			wf.files = append(wf.files, worldFileEntryLimit{e, limit})
		case entryMod:
			wf.mods++
			wf.files = append(wf.files, worldFileEntryLimit{e, limit})
		case entryConfig:
			if e.Name == ConfigDir+"/"+ServerSettingsFile {
				serverEntry = e
				continue
			}
			wf.files = append(wf.files, worldFileEntryLimit{e, limit})
		}
	}
	// A file may not sit where another entry needs a folder.
	for name := range seen {
		for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
			if isDir, found := seen[dir]; found && !isDir {
				return ErrWorldFileBad
			}
		}
	}
	if wf.bytes > uint64(size)*worldFileMaxRatio { //nolint:gosec // size is positive
		return ErrWorldFileBad
	}
	return wf.checkContent(formatEntry, metaEntry, serverEntry)
}

// entryLimit is the most bytes one entry may unpack to (SPEC 14.6).
func entryLimit(kind entryKind, name string, lim WorldFileLimits) int64 {
	switch kind {
	case entryFormat:
		return maxWorldFileFormat
	case entryMeta:
		return maxWorldFileMeta
	case entryConfig:
		return maxConfigFile
	case entrySave:
		return lim.Save
	case entryMod:
		if name == ModsDir+"/"+ModSettingsFile {
			return MaxModSettings
		}
		return lim.Mod
	}
	return 0
}

// checkContent reads and checks the format file, world.json and
// server-settings.json (SPEC 5.14, 14.6).
func (wf *worldFile) checkContent(formatEntry, metaEntry, serverEntry *zip.File) error {
	if formatEntry == nil || metaEntry == nil {
		return ErrWorldFileBad
	}
	b, err := readZipEntry(formatEntry, maxWorldFileFormat)
	if err != nil {
		return ErrWorldFileBad
	}
	// The version is read first, so a newer file's new fields do not make
	// it look damaged.
	var version struct {
		FormatVersion int `json:"format_version"`
	}
	if jsonObject(b, &version) != nil {
		return ErrWorldFileBad
	}
	if version.FormatVersion > WorldFileFormat {
		return ErrWorldFileNewer
	}
	if version.FormatVersion != WorldFileFormat || strictJSON(b, &wf.doc) != nil {
		return ErrWorldFileBad
	}
	if wf.doc.Saves != ExportAllSaves && wf.doc.Saves != ExportLoadedSave {
		return ErrWorldFileBad
	}

	if b, err = readZipEntry(metaEntry, maxWorldFileMeta); err != nil {
		return ErrWorldFileBad
	}
	if jsonObject(b, &wf.meta) != nil {
		return ErrWorldFileBad
	}
	if wf.meta.Newer() {
		return ErrWorldFileNewer
	}
	meta := &wf.meta
	if meta.FormatVersion < 1 || !SupportedGameVersion(meta.GameVersion) ||
		(meta.SpaceAge && strings.HasPrefix(meta.GameVersion, "1.")) {
		return ErrWorldFileBad
	}
	if fm := meta.FactoryMap; fm != nil && fm.Mode != FactoryMapManual && fm.Mode != FactoryMapAutomatic && fm.Mode != FactoryMapOff {
		return ErrWorldFileBad
	}
	// These name a folder on the old host, or hide the world here.
	meta.ImportedFrom = nil
	meta.Archived = false
	if meta.LoadSave != "" && !slices.ContainsFunc(wf.files, func(f worldFileEntryLimit) bool {
		return f.e.Name == SavesDir+"/"+meta.LoadSave
	}) {
		meta.LoadSave = ""
	}
	if wf.saves == 0 && meta.NewMap == nil {
		return ErrWorldFileBad
	}

	if serverEntry != nil {
		b, err := readZipEntry(serverEntry, maxConfigFile)
		if err != nil {
			return ErrWorldFileBad
		}
		stripped, _ := gamesettings.StripSecrets(b)
		if stripped == nil {
			return ErrWorldFileBad
		}
		wf.server = stripped
	}
	return nil
}

// jsonObject decodes exactly one JSON value into v, keeping unknown fields.
func jsonObject(b []byte, v any) error {
	var raw map[string]any
	if err := strictJSON(b, &raw); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// extract writes every entry into dst, a new world's staging folder, at the
// mode copyMode gives, with each entry's time. Go checks each entry's CRC-32
// as it reaches the end. name is the new world's, for a server-settings.json
// the file lacks.
func (wf *worldFile) extract(ctx context.Context, dst *os.Root, name string) error {
	for _, f := range wf.files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := extractEntry(dst, f.e, f.limit); err != nil {
			return err
		}
	}
	if wf.server == nil {
		return writeNewServerSettings(dst, name)
	}
	return writeFileAtomicMode(dst, path.Join(ConfigDir, ServerSettingsFile), wf.server)
}

// extractEntry writes one entry through a temp file, refusing it once it
// passes limit whatever its header said.
func extractEntry(dst *os.Root, e *zip.File, limit int64) error {
	if err := mkdirAll(dst, path.Dir(e.Name)); err != nil {
		return err
	}
	rc, err := e.Open()
	if err != nil {
		return ErrWorldFileBad
	}
	defer func() { _ = rc.Close() }()
	out, temp, err := createTempMode(dst, path.Dir(e.Name), copyMode(e.Name))
	if err != nil {
		return err
	}
	src := &entryReader{r: io.LimitReader(rc, limit+1)}
	n, err := io.Copy(out, src)
	if src.failed || (err == nil && n > limit) {
		// A read failure is a damaged file: a bad CRC, a short entry, or
		// more bytes than the header said. A write failure stays itself.
		err = ErrWorldFileBad
	}
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && !e.Modified.IsZero() {
		err = dst.Chtimes(temp, e.Modified, e.Modified)
	}
	if err == nil {
		err = Commit(dst, temp, e.Name)
	}
	if err != nil {
		_ = dst.Remove(temp)
	}
	return err
}

// entryReader notes whether reading an entry failed, so extractEntry can
// tell a damaged file from a failed write.
type entryReader struct {
	r      io.Reader
	failed bool
}

func (r *entryReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err != nil && err != io.EOF {
		r.failed = true
	}
	return n, err
}
