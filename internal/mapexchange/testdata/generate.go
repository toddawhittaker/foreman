//go:build ignore

// Generate makes the golden fixtures: for each game version it creates maps
// headlessly in the real game image, and writes the map exchange string the
// game gives for each map (<version>/<name>.txt) and the game's own parse of
// that string (<version>/<name>.json). See README.md. Run it from this
// directory:
//
//	go run generate.go [version ...]
//
// It needs Docker and the factoriotools/factorio images already pulled, and
// starts only throwaway containers of its own.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

var defaultVersions = []string{"1.1.110", "2.0.66", "2.0.77", "2.1.9", "2.1.24"}

// seedBase makes the preset maps reproducible; each preset adds its position.
const seedBase = 1000

// customs are the two custom cases, as changes to the line's example files.
// Objects merge into the example; any other value replaces it.
func customs(line string, spaceAge bool) []custom {
	moisture := map[string]any{
		"control:moisture:frequency": "2", "control:moisture:bias": "0.25",
		"control:aux:frequency": "0.5", "control:aux:bias": "-0.25",
	}
	elevation := map[string]any{"elevation": "elevation_island", "trees_forest_path_cutout": 1}
	if line == "1.1" {
		moisture = map[string]any{
			"control-setting:moisture:frequency:multiplier": "2", "control-setting:moisture:bias": "0.25",
			"control-setting:aux:frequency:multiplier": "0.5", "control-setting:aux:bias": "-0.25",
		}
		elevation = map[string]any{"elevation": "0_17-island"}
	}
	one := map[string]any{
		"width": 3000, "height": 2000, "starting_area": 2.5, "peaceful_mode": true, "seed": 123456789,
		"autoplace_controls": map[string]any{
			"iron-ore":   map[string]any{"frequency": 3, "size": 0.5, "richness": 2},
			"copper-ore": map[string]any{"frequency": 0.25, "size": 4, "richness": 0.5},
			"coal":       map[string]any{"frequency": 6, "size": 6, "richness": 6},
			"enemy-base": map[string]any{"frequency": 0.17, "size": 0.17},
			"trees":      map[string]any{"frequency": 2, "size": 0.5},
		},
		"cliff_settings":            map[string]any{"name": "cliff", "cliff_elevation_0": 5, "cliff_elevation_interval": 20, "richness": 3},
		"property_expression_names": moisture,
	}
	two := map[string]any{
		"width": 0, "height": 0, "starting_area": 0.33,
		// Above the largest signed 32-bit number: seeds are unsigned.
		"seed": 4000000000,
		"autoplace_controls": map[string]any{
			"stone":       map[string]any{"frequency": 0.17, "size": 0.17, "richness": 0.17},
			"uranium-ore": map[string]any{"frequency": 6, "size": 6, "richness": 6},
			"crude-oil":   map[string]any{"frequency": 0.5, "size": 2, "richness": 4},
		},
		"property_expression_names": elevation,
	}
	settings := map[string]any{
		"pollution":           map[string]any{"diffusion_ratio": 0.05, "ageing": 2},
		"enemy_evolution":     map[string]any{"time_factor": 0.00005, "pollution_factor": 0.000002},
		"enemy_expansion":     map[string]any{"enabled": false},
		"difficulty_settings": map[string]any{"technology_price_multiplier": 3},
	}
	if line == "1.1" {
		one["water"], one["terrain_segmentation"] = 0.5, 2
		two["water"], two["terrain_segmentation"] = 4, 0.25
		settings["difficulty_settings"] = map[string]any{"technology_price_multiplier": 3, "recipe_difficulty": 1, "technology_difficulty": 1, "research_queue_setting": "always"}
	} else {
		two["autoplace_controls"].(map[string]any)["water"] = map[string]any{"frequency": 4, "size": 0.25}
		two["autoplace_controls"].(map[string]any)["rocks"] = map[string]any{"frequency": 4, "size": 2}
		settings["difficulty_settings"].(map[string]any)["spoil_time_modifier"] = 0.5
	}
	if spaceAge {
		extra := func(m map[string]any, name string, v map[string]any) {
			m["autoplace_controls"].(map[string]any)[name] = v
		}
		extra(one, "tungsten_ore", map[string]any{"frequency": 2, "size": 2, "richness": 3})
		extra(one, "aquilo_crude_oil", map[string]any{"frequency": 0.5, "size": 0.5, "richness": 0.5})
		extra(two, "scrap", map[string]any{"frequency": 6, "size": 6, "richness": 6})
		extra(two, "gleba_stone", map[string]any{"frequency": 0.17, "size": 0.17, "richness": 0.17})
		extra(two, "fulgora_islands", map[string]any{"frequency": 3, "size": 3})
	}
	return []custom{
		{name: "custom-1", mapGen: one},
		{name: "custom-2", mapGen: two, mapSettings: settings},
	}
}

type custom struct {
	name        string
	mapGen      map[string]any
	mapSettings map[string]any
}

func main() {
	versions := os.Args[1:]
	if len(versions) == 0 {
		versions = defaultVersions
	}
	for _, v := range versions {
		if err := run(v); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", v, err)
			os.Exit(1)
		}
	}
}

func run(version string) error {
	line, ok := mapgen.Line(version)
	if !ok {
		return fmt.Errorf("not a patch release of a supported line")
	}
	modes := []bool{false}
	if line != "1.1" {
		modes = append(modes, true)
	}
	work, err := os.MkdirTemp("", "mapexchange-golden-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := os.Chmod(work, 0o777); err != nil {
		return err
	}
	if err := setupMod(work, line); err != nil {
		return err
	}
	out := version
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, sa := range modes {
		suffix := ""
		if sa {
			suffix = "_space-age"
		}
		for i, p := range mapgen.Presets(line) {
			args := []string{"--preset", p, "--map-gen-seed", fmt.Sprint(seedBase + i)}
			if err := one(work, version, line, sa, p+suffix, args); err != nil {
				return err
			}
		}
		for _, c := range customs(line, sa) {
			args, err := customArgs(work, version, c)
			if err != nil {
				return err
			}
			if err := one(work, version, line, sa, c.name+suffix, args); err != nil {
				return err
			}
		}
	}
	return nil
}

// setupMod writes the game's config, the helper mod and an empty save folder.
func setupMod(work, line string) error {
	if err := os.WriteFile(filepath.Join(work, "config.ini"), []byte("[path]\nread-data=/opt/factorio/data\nwrite-data=/work\n"), 0o644); err != nil {
		return err
	}
	dir := filepath.Join(work, "mods", "mxdump")
	for _, d := range []string{dir, filepath.Join(work, "saves"), filepath.Join(work, "script-output")} {
		if err := os.MkdirAll(d, 0o777); err != nil {
			return err
		}
	}
	for _, d := range []string{filepath.Join(work, "mods"), filepath.Join(work, "saves"), filepath.Join(work, "script-output"), dir} {
		if err := os.Chmod(d, 0o777); err != nil {
			return err
		}
	}
	lua, err := os.ReadFile("mxdump/control.lua")
	if err != nil {
		return err
	}
	info, err := os.ReadFile("mxdump/info.json")
	if err != nil {
		return err
	}
	info = bytes.ReplaceAll(info, []byte("@LINE@"), []byte(line))
	if err := os.WriteFile(filepath.Join(dir, "control.lua"), lua, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "info.json"), info, 0o644)
}

func writeModList(work, line string, spaceAge bool) error {
	mods := []string{"base"}
	enabled := map[string]bool{"mxdump": true}
	if line != "1.1" {
		for _, m := range []string{"elevated-rails", "quality", "space-age"} {
			mods = append(mods, m)
			enabled[m] = spaceAge
		}
	}
	if line == "2.1" {
		// recycler is part of the base game from 2.1, with or without Space Age.
		mods = append(mods, "recycler")
		enabled["recycler"] = true
	}
	mods = append(mods, "mxdump")
	enabled["base"] = true
	var list []map[string]any
	for _, m := range mods {
		list = append(list, map[string]any{"name": m, "enabled": enabled[m]})
	}
	b, _ := json.Marshal(map[string]any{"mods": list})
	return os.WriteFile(filepath.Join(work, "mods", "mod-list.json"), b, 0o644)
}

// customArgs writes the custom case's settings files, the example file with
// the case's changes merged in, and returns the game options that use them.
// The example comes from the same image: map-settings.json needs every key
// the game version knows, and these differ between patches of one line.
func customArgs(work, version string, c custom) ([]string, error) {
	args := []string{}
	for _, f := range []struct {
		opt, file string
		change    map[string]any
	}{
		{"--map-gen-settings", "map-gen-settings.json", c.mapGen},
		{"--map-settings", "map-settings.json", c.mapSettings},
	} {
		if f.change == nil {
			continue
		}
		base, err := exec.Command("docker", "run", "--rm", "--entrypoint", "cat", "factoriotools/factorio:"+version,
			"/opt/factorio/data/"+strings.TrimSuffix(f.file, ".json")+".example.json").Output()
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(base, &m); err != nil {
			return nil, err
		}
		merge(m, f.change)
		b, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(work, f.file), b, 0o644); err != nil {
			return nil, err
		}
		args = append(args, f.opt, "/work/"+f.file)
	}
	return args, nil
}

func merge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

func one(work, version, line string, spaceAge bool, name string, args []string) error {
	if err := writeModList(work, line, spaceAge); err != nil {
		return err
	}
	for _, f := range []string{"exchange.txt", "parsed.json"} {
		os.Remove(filepath.Join(work, "script-output", f))
	}
	os.Remove(filepath.Join(work, "saves", "new.zip"))
	argv := append([]string{"run", "--rm", "-u", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-v", work + ":/work", "--entrypoint", "/opt/factorio/bin/x64/factorio",
		"factoriotools/factorio:" + version,
		"--config", "/work/config.ini", "--mod-directory", "/work/mods", "--create", "/work/saves/new.zip"}, args...)
	if out, err := exec.Command("docker", argv...).CombinedOutput(); err != nil {
		return fmt.Errorf("%s: creating the map: %v\n%s", name, err, out)
	}
	ex, err := os.ReadFile(filepath.Join(work, "script-output", "exchange.txt"))
	if err != nil {
		return fmt.Errorf("%s: the helper mod wrote no string: %w", name, err)
	}
	parsed, err := os.ReadFile(filepath.Join(work, "script-output", "parsed.json"))
	if err != nil {
		return err
	}
	// Decode and encode again so keys come out sorted and a rerun gives the same bytes.
	var v any
	dec := json.NewDecoder(bytes.NewReader(parsed))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	norm, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(version, name+".txt"), []byte(strings.TrimSpace(string(ex))+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(version, name+".json"), append(norm, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s %s\n", version, name)
	return nil
}
