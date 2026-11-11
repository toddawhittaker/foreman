package web

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// previewKey says which settings a preview is of (SPEC 5.6): all of them,
// and each planet's share, which is every setting except the other planets'
// own autoplace controls. Another planet's controls never change a planet's
// map, so its picture and count are kept when only they change.
type previewKey struct {
	all     string
	planets map[string]string
}

// planet is the key of one planet's picture and count; a planet the key
// does not list falls back to all the settings.
func (k previewKey) planet(p string) string {
	if s, ok := k.planets[p]; ok {
		return s
	}
	return k.all
}

// settingsKey hashes everything that changes the map, so the same settings
// find the pictures and counts already made. The size is part of it, as it
// changes every picture; the planet asked for and the world name are not.
func settingsKey(req game.PreviewRequest) previewKey {
	shared := func(mapGen, mapSettings []byte) string {
		return hashJSON(struct {
			Version, Preset     string
			SpaceAge            bool
			Seed                uint32
			Size                int
			MapGen, MapSettings []byte
		}{req.GameVersion, req.Preset, req.SpaceAge, req.Seed, cmp.Or(req.Size, game.PreviewSizeDefault), mapGen, mapSettings})
	}
	k := previewKey{all: shared(req.MapGenSettings, req.MapSettings)}
	line, _ := mapgen.Line(req.GameVersion)
	mapGen, mapSettings := req.MapGenSettings, req.MapSettings
	if mapGen == nil && mapSettings == nil && req.Preset == "" {
		// The default preset passes no files until a setting changes; the files
		// the form then writes for the planets left alone make the same map
		// (checked on the real 2.1.24 game), so they share a key.
		mapGen, mapSettings = defaultFiles(line, req.GameVersion, req.SpaceAge)
	}
	rest, own, ok := splitControls(line, mapGen)
	if !ok {
		return k
	}
	common := shared(rest, mapSettings)
	k.planets = map[string]string{}
	for _, p := range mapgen.Planets(line, req.SpaceAge) {
		k.planets[p] = hashJSON(struct {
			Shared string
			Own    map[string]json.RawMessage
		}{common, own[p]})
	}
	return k
}

// splitControls takes each planet's own autoplace controls out of the
// map-gen settings. Anything that is not a known planet's control stays in
// rest, so it is in every planet's key. ok is false when the settings are
// not a JSON object.
func splitControls(line string, mapGen []byte) (rest []byte, own map[string]map[string]json.RawMessage, ok bool) {
	var top map[string]json.RawMessage
	if len(mapGen) > 0 && json.Unmarshal(mapGen, &top) != nil {
		return nil, nil, false
	}
	var controls map[string]json.RawMessage
	if raw, has := top["autoplace_controls"]; has && json.Unmarshal(raw, &controls) != nil {
		return nil, nil, false
	}
	own = map[string]map[string]json.RawMessage{}
	for _, p := range mapgen.Planets(line, true) {
		for _, c := range mapgen.Controls(line, p) {
			v, has := controls[c.Name]
			if !has {
				continue
			}
			if own[p] == nil {
				own[p] = map[string]json.RawMessage{}
			}
			own[p][c.Name] = v
			delete(controls, c.Name)
		}
	}
	// The run passes the request's seed with --map-gen-seed, and that seed is
	// in the key already; the file's own is null until a setting changes.
	delete(top, "seed")
	// No controls left, absent, null and {} all leave every control at its default.
	delete(top, "autoplace_controls")
	if len(controls) > 0 {
		b, err := json.Marshal(controls)
		if err != nil {
			return nil, nil, false
		}
		top["autoplace_controls"] = b
	}
	rest, err := json.Marshal(top)
	if err != nil {
		return nil, nil, false
	}
	return rest, own, true
}

// defaultFiles are the files Generate writes for the default preset once a
// setting is changed (generate.go, settingsWorld): the form's map-gen file
// and, where the layout is tested, the preset's map settings.
func defaultFiles(line, version string, spaceAge bool) (mapGen, mapSettings []byte) {
	_, mapGen, _ = parseMapGenValues(url.Values{}, line, spaceAge)
	mapSettings, err := mapgen.MapSettings(version, defaultPreset, nil)
	if err != nil {
		mapSettings = nil
	}
	return mapGen, mapSettings
}

func hashJSON(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
