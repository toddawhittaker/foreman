package mapgen

// The map-settings.json a new world is created with (SPEC 5.6). The game
// refuses a file missing any key its exact version knows, so the file is
// always written complete: the example file from a tested image of the
// world's map-settings layout, then the preset's values, then the form's
// changes.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"
)

// ErrUntestedLayout means no tested game image has the version's
// map-settings layout (today 2.1.13 to 2.1.17), so Foreman cannot know the
// keys that version requires.
var ErrUntestedLayout = errors.New("no tested game image has this version's map-settings layout")

// settingsLayout is one map-settings layout's data, read from the image
// named by version.
type settingsLayout struct {
	version string
	example []byte
	// presets holds, per preset, every value the game gives a new map made
	// with only --preset that differs from the example file. For "default"
	// that is where the example file and the game's own defaults disagree.
	presets map[string]json.RawMessage
}

// settingsLayouts is keyed by MapSettingsLayout. The files are
// data/<line>/map-settings-<version>.json (the image's example, copied by
// extract.go) and preset-map-settings-<version>.json (the game's own parse of
// each preset, pinned by mapsettings_test.go).
var settingsLayouts = func() map[string]settingsLayout {
	m := map[string]settingsLayout{}
	for _, l := range Lines {
		names, err := fs.Glob(files, "data/"+l+"/map-settings-*.json")
		if err != nil || len(names) == 0 {
			panic(fmt.Sprintf("mapgen: no map-settings file for line %s", l))
		}
		for _, n := range names {
			v := strings.TrimSuffix(strings.TrimPrefix(n, "data/"+l+"/map-settings-"), ".json")
			layout, ok := MapSettingsLayout(v)
			if !ok {
				panic("mapgen: bad version in " + n)
			}
			if _, dup := m[layout]; dup {
				panic("mapgen: two map-settings files for layout " + layout)
			}
			ex, err := files.ReadFile(n)
			if err != nil {
				panic("mapgen: " + err.Error())
			}
			pb, err := files.ReadFile("data/" + l + "/preset-map-settings-" + v + ".json")
			if err != nil {
				panic("mapgen: " + err.Error())
			}
			var p map[string]json.RawMessage
			if err := json.Unmarshal(pb, &p); err != nil {
				panic("mapgen: preset-map-settings-" + v + ".json: " + err.Error())
			}
			m[layout] = settingsLayout{version: v, example: ex, presets: p}
		}
	}
	return m
}()

func layoutFor(version string) (settingsLayout, error) {
	if _, ok := Line(version); !ok {
		return settingsLayout{}, fmt.Errorf("mapgen: %q is not a supported game version", version)
	}
	name, ok := MapSettingsLayout(version)
	if !ok {
		return settingsLayout{}, fmt.Errorf("mapgen: %q is not a supported game version", version)
	}
	sl, ok := settingsLayouts[name]
	if !ok {
		return settingsLayout{}, fmt.Errorf("mapgen: %s: %w", version, ErrUntestedLayout)
	}
	return sl, nil
}

// MapSettings returns the complete map-settings.json for a new world on an
// exact game version: the version's example file, with the preset's values
// (as the game applies them) and then the overrides on top. Overrides are
// keyed by the dotted path of a value in the file, such as
// "enemy_evolution.time_factor" or "max_failed_behavior_count", and must keep
// the value's kind (number, true or false, text). It refuses an unknown
// version or preset, an override the file has no value for, and a version
// whose layout has no tested image (ErrUntestedLayout).
func MapSettings(version, preset string, overrides map[string]any) ([]byte, error) {
	sl, err := layoutFor(version)
	if err != nil {
		return nil, err
	}
	delta, ok := sl.presets[preset]
	if !ok {
		return nil, fmt.Errorf("mapgen: no preset %q on %s", preset, version)
	}
	file, err := decodeObject(sl.example)
	if err != nil {
		return nil, fmt.Errorf("mapgen: map-settings %s: %w", sl.version, err)
	}
	d, err := decodeObject(delta)
	if err != nil {
		return nil, fmt.Errorf("mapgen: preset %s: %w", preset, err)
	}
	for path, v := range leaves(d, "") {
		if err := set(file, path, v); err != nil {
			return nil, fmt.Errorf("mapgen: preset %s: %w", preset, err)
		}
	}
	for path, v := range overrides {
		if err := set(file, path, v); err != nil {
			return nil, err
		}
	}
	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("mapgen: %w", err)
	}
	return append(out, '\n'), nil
}

func decodeObject(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

// leaves flattens an object to its dotted paths.
func leaves(m map[string]any, prefix string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			for p, w := range leaves(sub, prefix+k+".") {
				out[p] = w
			}
			continue
		}
		out[prefix+k] = v
	}
	return out
}

// set replaces the value at a dotted path that the file already has, keeping
// its kind. Keys starting with "_" are the file's comments, not settings.
func set(file map[string]any, path string, v any) error {
	parts := strings.Split(path, ".")
	obj := file
	for _, p := range parts[:len(parts)-1] {
		next, ok := obj[p].(map[string]any)
		if !ok {
			return fmt.Errorf("mapgen: map settings have no %q", path)
		}
		obj = next
	}
	last := parts[len(parts)-1]
	old, ok := obj[last]
	if !ok || strings.HasPrefix(last, "_") {
		return fmt.Errorf("mapgen: map settings have no %q", path)
	}
	nv, ok := sameKind(old, v)
	if !ok {
		return fmt.Errorf("mapgen: map setting %q must stay a %T, got %v", path, old, v)
	}
	obj[last] = nv
	return nil
}

// sameKind returns v as the kind old has, or false when it is another kind.
func sameKind(old, v any) (any, bool) {
	switch old.(type) {
	case json.Number:
		switch n := v.(type) {
		case json.Number:
			if _, err := n.Float64(); err != nil {
				return nil, false
			}
			return n, true
		case float64:
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, false
			}
			return n, true
		case int:
			return n, true
		}
	case bool:
		b, ok := v.(bool)
		return b, ok
	case string:
		s, ok := v.(string)
		return s, ok
	}
	return nil, false
}
