package world

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"regexp"
)

// Refusals of Generate.
var (
	ErrBadPreset = errors.New("the preset name may hold only lowercase letters, digits, hyphens and underscores")
	// ErrBadMapJSON covers both map settings files.
	ErrBadMapJSON = errors.New("the map settings are not valid JSON")
)

// presetRE is the shape of the game's preset names, such as
// "rich-resources". The name reaches the game's start script as an
// environment variable, so nothing else is let through.
var presetRE = regexp.MustCompile(`^[a-z0-9_-]{0,40}$`)

// NewWorld is what Generate makes a world from (SPEC 5.6).
type NewWorld struct {
	Name        string
	GameVersion string
	SpaceAge    bool
	// Preset is the game's own preset name, or "" for none.
	Preset string
	// Seed is the map seed. Generate never invents one: nil lets the game
	// choose, and the web layer fills a blank seed before the preview so
	// the preview matches the world.
	Seed *uint32
	// MapGenSettings and MapSettings are already-validated JSON, written
	// to config/map-gen-settings.json and config/map-settings.json. Empty
	// means no file.
	MapGenSettings []byte
	MapSettings    []byte
}

// Generate makes a new world that has no save: its folders, any map
// settings files and a world.json with new_map set, so the world's first
// start makes the map (SPEC 5.6, 4.3). The first world made becomes active.
// It returns the new world's id.
func (m *Manager) Generate(ctx context.Context, nw NewWorld) (string, error) {
	if !presetRE.MatchString(nw.Preset) {
		return "", ErrBadPreset
	}
	files := []struct {
		name string
		data []byte
	}{{MapGenSettingsFile, nw.MapGenSettings}, {MapSettingsFile, nw.MapSettings}}
	for _, f := range files {
		if len(f.data) > maxConfigFile {
			return "", ErrConfigTooBig
		}
		if len(f.data) > 0 && !json.Valid(f.data) {
			return "", ErrBadMapJSON
		}
	}
	if err := m.checkNew(ctx, nw.Name, nw.GameVersion, nw.SpaceAge); err != nil {
		return "", err
	}
	opts := createOptions{newMap: &NewMap{Preset: nw.Preset, Seed: nw.Seed}}
	return m.create(ctx, nw.Name, nw.GameVersion, nw.SpaceAge, opts, func(worlds *os.Root, dir string) error {
		dst, err := worlds.OpenRoot(dir)
		if err != nil {
			return err
		}
		defer func() { _ = dst.Close() }()
		for _, f := range files {
			if len(f.data) == 0 {
				continue
			}
			if err := writeFileAtomicMode(dst, path.Join(ConfigDir, f.name), f.data); err != nil {
				return err
			}
		}
		return nil
	})
}

// ClearNewMap removes new_map from the world once its first start has made
// the map. It takes the world's lock, waiting as AddSave does, and does
// nothing when new_map is not set.
func (m *Manager) ClearNewMap(id string) error {
	unlock, err := m.lockWait(id, m.addSaveWait)
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
	if err != nil || meta.NewMap == nil {
		return err
	}
	meta.NewMap = nil
	return WriteMeta(root, meta)
}
