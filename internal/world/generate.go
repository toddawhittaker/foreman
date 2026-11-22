package world

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
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
	// ServerSettings and ModSettings come from a favorite (SPEC 5.12) and
	// are written to config/server-settings.json and mods/mod-settings.dat.
	// Empty means no file. Secrets are removed from ServerSettings, so the
	// person sets them on the new world.
	ServerSettings []byte
	ModSettings    []byte
}

// checkMapFiles is the check Generate and SetNewMap share on a preset name
// and the two map settings files: size, then JSON.
func checkMapFiles(preset string, mapGen, mapSettings []byte) error {
	if !presetRE.MatchString(preset) {
		return ErrBadPreset
	}
	for _, data := range [][]byte{mapGen, mapSettings} {
		if len(data) > maxConfigFile {
			return ErrConfigTooBig
		}
		if len(data) > 0 && !json.Valid(data) {
			return ErrBadMapJSON
		}
	}
	return nil
}

// writeMapFiles is the one writer of config/map-gen-settings.json and
// config/map-settings.json (ADR 0010). A file with data is replaced
// atomically; an empty one is removed, so an unchanged preset leaves no
// file behind. root is a world's folder.
func writeMapFiles(root *os.Root, mapGen, mapSettings []byte) error {
	for _, f := range []struct {
		name string
		data []byte
	}{{MapGenSettingsFile, mapGen}, {MapSettingsFile, mapSettings}} {
		p := path.Join(ConfigDir, f.name)
		if len(f.data) > 0 {
			if err := writeFileAtomicMode(root, p, f.data); err != nil {
				return err
			}
		} else if err := root.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Generate makes a new world that has no save: its folders, any map
// settings files and a world.json with new_map set, so the world's first
// start makes the map (SPEC 5.6, 4.3). The first world made becomes active.
// It returns the new world's id.
func (m *Manager) Generate(ctx context.Context, nw NewWorld) (string, error) {
	if err := checkMapFiles(nw.Preset, nw.MapGenSettings, nw.MapSettings); err != nil {
		return "", err
	}
	var server []byte
	if len(nw.ServerSettings) > 0 {
		if len(nw.ServerSettings) > maxConfigFile {
			return "", ErrConfigTooBig
		}
		if server, _ = gamesettings.StripSecrets(nw.ServerSettings); server == nil {
			return "", ErrBadServerSettings
		}
	}
	// A file Foreman creates keeps 20 autosaves, not the game's 5 (SPEC 11.1).
	if server == nil {
		server = gamesettings.NewServerSettings(strings.TrimSpace(nw.Name))
	} else {
		server = gamesettings.WithAutosaveSlots(server)
	}
	if len(server) > maxConfigFile {
		return "", ErrConfigTooBig
	}
	if len(nw.ModSettings) > MaxModSettings {
		return "", ErrModSettingsTooBig
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
		if err := writeMapFiles(dst, nw.MapGenSettings, nw.MapSettings); err != nil {
			return err
		}
		if err := writeFileAtomicMode(dst, path.Join(ConfigDir, ServerSettingsFile), server); err != nil {
			return err
		}
		if len(nw.ModSettings) > 0 {
			return WriteFileAtomic(dst, path.Join(ModsDir, ModSettingsFile), nw.ModSettings)
		}
		return nil
	})
}

// ClearNewMapHeld removes new_map from the world once its first start has
// made the map. The caller holds Lock(id). It does nothing when new_map is
// not set.
func (m *Manager) ClearNewMapHeld(id string) error {
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
