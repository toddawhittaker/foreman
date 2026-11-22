package world

import (
	"errors"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
)

// The world file format (SPEC 5.13, 5.14, ADR 0031). A world file is one ZIP:
// a world folder in the 5.1 layout plus the format file. Files made by
// Foreman stay on people's disks for years, so the layout is fixed here, and
// the writer uses the lowest format version that can hold what it writes.
const (
	// WorldFileFormatFile is the first entry of a world file.
	WorldFileFormatFile = "foreman-world.json"
	// WorldFileFormat is the newest world file format this Foreman reads and
	// writes.
	WorldFileFormat = 1
	// MaxWorldFile is the most bytes of a world file an import takes (SPEC
	// 14.6): a fixed cap beside the disk check of 6.11.
	MaxWorldFile int64 = 64 << 30
	// maxWorldFileEntries is the most entries a world file may hold, counted
	// before the ZIP's directory is built (SPEC 14.6).
	maxWorldFileEntries = 100_000
	// PrefixWorldFile begins the name of a staged upload of a world file;
	// the .partial- prefix means Recover clears it at startup.
	PrefixWorldFile = PrefixPartial + "worldfile-"
)

// ExportSaves says which saves an export holds (SPEC 5.13).
type ExportSaves string

// The choices of which saves an export holds.
const (
	// ExportAllSaves is every save: the default.
	ExportAllSaves ExportSaves = "all"
	// ExportLoadedSave is only the save that loads next (LoadTarget).
	ExportLoadedSave ExportSaves = "loaded"
)

// Refusals of a world file. The text never repeats what the file holds.
var (
	// ErrWorldFileBad is a world file that is not one, is damaged, or breaks
	// a rule of 14.5 or 14.6.
	ErrWorldFileBad = errors.New("this is not a world file Foreman can read")
	// ErrWorldFileNewer is a world file a newer Foreman made.
	ErrWorldFileNewer = errors.New("a newer version of Foreman made this world file, so this version cannot read it")
)

// WorldFileDoc is foreman-world.json, the format file.
type WorldFileDoc struct {
	FormatVersion int         `json:"format_version"`
	ExportedAt    time.Time   `json:"exported_at"`
	ForemanVer    string      `json:"foreman_version"`
	Saves         ExportSaves `json:"saves"`
	// GamePasswordSet says only that a game password existed, never its value.
	GamePasswordSet bool `json:"game_password_set"`
}

// WorldFileLimits are the per-entry limits an import enforces: the save upload
// setting and the mod upload setting (SPEC 14.6).
type WorldFileLimits struct {
	Save, Mod int64
}

// WorldFileInfo is what the import preview shows about a staged world file.
type WorldFileInfo struct {
	// Name, GameVersion and SpaceAge come from the file's world.json.
	Name        string
	GameVersion string
	SpaceAge    bool
	// Saves and ModFiles count the entries; Bytes is their unpacked size.
	Saves, ModFiles int
	Bytes           int64
	// LoadSave is the save that loads, "" when the world makes its map at the
	// first start.
	LoadSave string
	// ForemanVersion is the Foreman that made the file.
	ForemanVersion string
	// GamePasswordSet is true when the exported world had a game password,
	// which is not in the file.
	GamePasswordSet bool
}

// entryKind says what a world file entry is.
type entryKind int

const (
	entryFormat entryKind = iota + 1 // foreman-world.json
	entryMeta                        // world.json
	entrySave                        // saves/<save>.zip
	entryConfig                      // config/<one of ConfigFiles>
	entryMod                         // mods/<path>
	entryDir                         // a folder entry under saves/, config/ or mods/
)

// worldFileEntry classifies a ZIP entry name by the rules of 14.5. It is the
// one place that decides which names may be read from a world file; anything
// it refuses refuses the whole file. A name must be relative, use forward
// slashes only, hold no empty, "." or ".." segment and no control character.
// Folder entries (a trailing slash) are allowed only under saves/, config/
// and mods/.
func worldFileEntry(name string) (entryKind, bool) {
	if name == "" || strings.ContainsAny(name, `\`) || strings.HasPrefix(name, "/") {
		return 0, false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return 0, false
		}
	}
	dir := strings.HasSuffix(name, "/")
	clean := strings.TrimSuffix(name, "/")
	if clean == "" {
		return 0, false
	}
	segments := strings.Split(clean, "/")
	for _, s := range segments {
		if s == "" || s == "." || s == ".." {
			return 0, false
		}
	}
	if len(segments) == 1 {
		switch {
		case dir && (clean == SavesDir || clean == ConfigDir || clean == ModsDir):
			return entryDir, true
		case dir:
			return 0, false
		case clean == WorldFileFormatFile:
			return entryFormat, true
		case clean == MetaFile:
			return entryMeta, true
		}
		return 0, false
	}
	rest := strings.Join(segments[1:], "/")
	switch segments[0] {
	case SavesDir:
		// A save is a plain file directly in saves/.
		if len(segments) != 2 || dir || !ValidSaveName(segments[1]) {
			return 0, false
		}
		return entrySave, true
	case ConfigDir:
		if len(segments) != 2 || dir || !slices.Contains(ConfigFiles, segments[1]) {
			return 0, false
		}
		return entryConfig, true
	case ModsDir:
		if !filepath.IsLocal(rest) || path.Clean(rest) != rest {
			return 0, false
		}
		if dir {
			return entryDir, true
		}
		return entryMod, true
	}
	return 0, false
}
