package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// MetaFile is the name of a world's metadata file (SPEC 5.1).
const MetaFile = "world.json"

// FormatVersion is the newest world.json format this Foreman knows.
// Fields are only ever added, so an older file reads as-is.
const FormatVersion = 1

// ErrNewerFormat is returned when changing a world whose world.json was
// written by a newer Foreman.
var ErrNewerFormat = errors.New("a newer version of Foreman wrote this world, so this version cannot change it")

// Meta is world.json. Fields this Foreman does not know are kept and
// written back unchanged, so a newer Foreman's fields survive (SPEC 5.1).
type Meta struct {
	FormatVersion int
	Name          string
	GameVersion   string
	SpaceAge      bool
	// LoadSave names the save to load; empty means the newest (SPEC 7.11).
	LoadSave  string
	CreatedAt time.Time
	// ImportedFrom says which inbox folder and save the world was imported
	// from; nil for a world not imported from a folder (SPEC 5.5).
	ImportedFrom *ImportedFrom

	unknown map[string]json.RawMessage
}

// metaKnown is the on-disk shape of the fields Foreman knows.
type metaKnown struct {
	FormatVersion int           `json:"format_version"`
	Name          string        `json:"name"`
	GameVersion   string        `json:"game_version"`
	SpaceAge      bool          `json:"space_age"`
	LoadSave      string        `json:"load_save,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	ImportedFrom  *ImportedFrom `json:"imported_from,omitempty"`
}

// ImportedFrom records the inbox folder a world was imported from and that
// folder's newest save at the time, so the Import page can tell a folder
// already imported from one with a newer save (SPEC 5.5).
type ImportedFrom struct {
	Folder    string    `json:"folder"`
	Save      string    `json:"save"`
	SaveSize  uint64    `json:"save_size"`
	SaveMtime time.Time `json:"save_mtime"`
	At        time.Time `json:"at"`
}

var knownKeys = []string{"format_version", "name", "game_version", "space_age", "load_save", "created_at", "imported_from"}

// Newer reports whether a newer Foreman wrote m, so this one must not change it.
func (m Meta) Newer() bool { return m.FormatVersion > FormatVersion }

// MarshalJSON writes the known fields over any unknown ones.
func (m Meta) MarshalJSON() ([]byte, error) {
	known, err := json.Marshal(metaKnown{
		FormatVersion: m.FormatVersion,
		Name:          m.Name,
		GameVersion:   m.GameVersion,
		SpaceAge:      m.SpaceAge,
		LoadSave:      m.LoadSave,
		CreatedAt:     m.CreatedAt.UTC(),
		ImportedFrom:  m.ImportedFrom,
	})
	if err != nil {
		return nil, err
	}
	all := map[string]json.RawMessage{}
	for k, v := range m.unknown {
		all[k] = v
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(known, &fields); err != nil {
		return nil, err
	}
	for k, v := range fields {
		all[k] = v
	}
	return json.Marshal(all)
}

// UnmarshalJSON reads the known fields and keeps the rest.
func (m *Meta) UnmarshalJSON(b []byte) error {
	var k metaKnown
	if err := json.Unmarshal(b, &k); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	for _, key := range knownKeys {
		delete(all, key)
	}
	if len(all) == 0 {
		all = nil
	}
	*m = Meta{
		FormatVersion: k.FormatVersion,
		Name:          k.Name,
		GameVersion:   k.GameVersion,
		SpaceAge:      k.SpaceAge,
		LoadSave:      k.LoadSave,
		CreatedAt:     k.CreatedAt,
		ImportedFrom:  k.ImportedFrom,
		unknown:       all,
	}
	return nil
}

// ReadMeta reads world.json from a world's root.
func ReadMeta(root *os.Root) (Meta, error) {
	b, err := ReadSmallFile(root, MetaFile, maxMetaFile)
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		// The decoder's message quotes file contents; say only what is wrong.
		return Meta{}, fmt.Errorf("read %s: it is not valid JSON of the expected shape", MetaFile)
	}
	if m.FormatVersion < 1 {
		return Meta{}, fmt.Errorf("read %s: format_version is missing", MetaFile)
	}
	return m, nil
}

// WriteMeta replaces world.json in a world's root atomically. It refuses a
// world written by a newer Foreman.
func WriteMeta(root *os.Root, m Meta) error {
	if m.Newer() {
		return ErrNewerFormat
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(root, MetaFile, append(b, '\n'))
}

// readChangeable reads world.json for a change, refusing a world a newer
// Foreman wrote (SPEC 5.1).
func readChangeable(root *os.Root) (Meta, error) {
	meta, err := ReadMeta(root)
	if err == nil && meta.Newer() {
		return Meta{}, ErrNewerFormat
	}
	return meta, err
}

// displayName is the world's name from world.json, or its id when
// world.json could not be read or has no name.
func displayName(id string, meta Meta, err error) string {
	if err != nil || meta.Name == "" {
		return id
	}
	return meta.Name
}
