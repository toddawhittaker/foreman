//go:build ignore

// Extract reads each game line's map-generation data from the real
// factoriotools/factorio image and writes internal/mapgen/data/<line>/.
// Run it from this directory with "go generate" (needs Docker and the images
// already pulled). Nothing here runs at startup (SPEC 4.2).
//
//	go run extract.go [version ...]   (default: the versions in defaultVersions)
//
// It also copies each tested image's map-settings.example.json to
// data/<line>/map-settings-<version>.json, one per map-settings layout.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

var defaultVersions = []string{"1.1.110", "2.0.77", "2.1.24"}

// mapSettingsOnly are tested images of an older map-settings layout of a
// line: only their map-settings example is read, the line's other data comes
// from defaultVersions. 2.1.9 has the layout before 2.1.13 (SPEC 5.6).
var mapSettingsOnly = []string{"2.1.9"}

type control struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Richness bool   `json:"richness"`
	// CanBeDisabled says the game's map generator offers an on/off box.
	CanBeDisabled bool `json:"can_be_disabled"`
}

type planet struct {
	Name     string    `json:"name"`
	SpaceAge bool      `json:"space_age"`
	Controls []control `json:"controls"`
}

type lineData struct {
	GameVersion string   `json:"game_version"`
	Presets     []string `json:"presets"`
	Planets     []planet `json:"planets"`
	// MapTypes are the elevation expressions Nauvis's map type offers, the
	// default first.
	MapTypes []string `json:"map_types"`
}

type rawControl struct {
	Category string `json:"category"`
	Richness *bool  `json:"richness"`
	Order    string `json:"order"`
	// CanBeDisabled is absent when true, the game's default.
	CanBeDisabled *bool `json:"can_be_disabled"`
}

type rawNoise struct {
	IntendedProperty string `json:"intended_property"`
	Order            string `json:"order"`
}

type rawPlanet struct {
	Order          string `json:"order"`
	MapGenSettings struct {
		AutoplaceControls map[string]json.RawMessage `json:"autoplace_controls"`
	} `json:"map_gen_settings"`
}

type rawDump struct {
	Presets  map[string]map[string]json.RawMessage `json:"map-gen-presets"`
	Controls map[string]rawControl                 `json:"autoplace-control"`
	Planets  map[string]rawPlanet                  `json:"planet"`
	Noise    map[string]rawNoise                   `json:"noise-expression"`
}

func main() {
	versions := os.Args[1:]
	if len(versions) == 0 {
		versions = defaultVersions
	}
	for _, v := range versions {
		if err := extract(v); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", v, err)
			os.Exit(1)
		}
	}
	if len(os.Args) > 1 {
		return
	}
	for _, v := range mapSettingsOnly {
		if err := extractMapSettings(v); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", v, err)
			os.Exit(1)
		}
	}
}

// extractMapSettings copies the image's own map-settings example. The game
// refuses a map-settings file missing any key its version knows (SPEC 5.6),
// so the file is kept per image and never written by hand.
func extractMapSettings(version string) error {
	line, ok := mapgen.Line(version)
	if !ok {
		return fmt.Errorf("not a patch release of a supported line")
	}
	b, err := dockerOut("factoriotools/factorio:"+version, "cat", "/opt/factorio/data/map-settings.example.json")
	if err != nil {
		return err
	}
	if !json.Valid(b) {
		return fmt.Errorf("map-settings.example.json is not valid JSON")
	}
	dir := filepath.Join("data", line)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "map-settings-"+version+".json"), b, 0o644)
}

func extract(version string) error {
	line, ok := mapgen.Line(version)
	if !ok {
		return fmt.Errorf("not a patch release of a supported line")
	}
	image := "factoriotools/factorio:" + version

	base, err := dump(image, line, false)
	if err != nil {
		return err
	}
	out := lineData{GameVersion: version}
	out.Presets, err = presetNames(base)
	if err != nil {
		return err
	}
	out.MapTypes, err = mapTypes(base)
	if err != nil {
		return err
	}
	if line == "1.1" {
		// 1.1 has no planet prototypes: Nauvis uses every control.
		var names []string
		for n := range base.Controls {
			names = append(names, n)
		}
		p, err := buildPlanet("nauvis", false, names, base)
		if err != nil {
			return err
		}
		out.Planets = []planet{p}
	} else {
		out.Planets, err = planets(base, false, nil)
		if err != nil {
			return err
		}
		withDLC, err := dump(image, line, true)
		if err != nil {
			return err
		}
		more, err := planets(withDLC, true, out.Planets)
		if err != nil {
			return err
		}
		out.Planets = append(out.Planets, more...)
	}

	dir := filepath.Join("data", line)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	b, err = dockerOut(image, "cat", "/opt/factorio/data/map-gen-settings.example.json")
	if err != nil {
		return err
	}
	if !json.Valid(b) {
		return fmt.Errorf("map-gen-settings.example.json is not valid JSON")
	}
	if err := os.WriteFile(filepath.Join(dir, "map-gen-settings.json"), b, 0o644); err != nil {
		return err
	}
	if err := extractMapSettings(version); err != nil {
		return err
	}
	fmt.Printf("%s: %d presets, %d planets\n", version, len(out.Presets), len(out.Planets))
	return nil
}

func presetNames(d rawDump) ([]string, error) {
	raw, ok := d.Presets["default"]
	if !ok {
		return nil, fmt.Errorf("no map-gen-presets prototype in the data dump")
	}
	type entry struct{ name, order string }
	var es []entry
	for k, v := range raw {
		if k == "type" || k == "name" {
			continue
		}
		var o struct {
			Order string `json:"order"`
		}
		if err := json.Unmarshal(v, &o); err != nil {
			return nil, fmt.Errorf("preset %s: %w", k, err)
		}
		es = append(es, entry{k, o.Order})
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].order != es[j].order {
			return es[i].order < es[j].order
		}
		return es[i].name < es[j].name
	})
	var names []string
	for _, e := range es {
		names = append(names, e.name)
	}
	return names, nil
}

// mapTypes lists the noise expressions meant for elevation: the game's map
// type choices. The one named "elevation" is the default and comes first.
func mapTypes(d rawDump) ([]string, error) {
	if d.Noise["elevation"].IntendedProperty != "elevation" {
		return nil, fmt.Errorf("no default elevation expression in the data dump")
	}
	var names []string
	for n, e := range d.Noise {
		if e.IntendedProperty == "elevation" && n != "elevation" {
			names = append(names, n)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := d.Noise[names[i]].Order, d.Noise[names[j]].Order
		if a != b {
			return a < b
		}
		return names[i] < names[j]
	})
	return append([]string{"elevation"}, names...), nil
}

// planets lists the planets in the dump. A planet that the base game already
// has (known) must come out the same with the DLC on; one that differs means
// the extract cannot be trusted.
func planets(d rawDump, spaceAge bool, known []planet) ([]planet, error) {
	type entry struct{ name, order string }
	var es []entry
	for n, p := range d.Planets {
		es = append(es, entry{n, p.Order})
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].order != es[j].order {
			return es[i].order < es[j].order
		}
		return es[i].name < es[j].name
	})
	var out []planet
	for _, e := range es {
		var names []string
		for c := range d.Planets[e.name].MapGenSettings.AutoplaceControls {
			names = append(names, c)
		}
		p, err := buildPlanet(e.name, spaceAge, names, d)
		if err != nil {
			return nil, err
		}
		var prev *planet
		for i := range known {
			if known[i].Name == e.name {
				prev = &known[i]
			}
		}
		if prev != nil {
			p.SpaceAge = prev.SpaceAge
			// Space Age makes some controls impossible to switch off; the
			// stricter answer is kept for both.
			if len(p.Controls) == len(prev.Controls) {
				for i := range p.Controls {
					if !p.Controls[i].CanBeDisabled {
						prev.Controls[i].CanBeDisabled = false
					}
				}
				for i := range p.Controls {
					p.Controls[i].CanBeDisabled = prev.Controls[i].CanBeDisabled
				}
			}
			if !reflect.DeepEqual(*prev, p) {
				return nil, fmt.Errorf("planet %s differs with Space Age on", e.name)
			}
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func buildPlanet(name string, spaceAge bool, controls []string, d rawDump) (planet, error) {
	sort.Slice(controls, func(i, j int) bool {
		a, b := d.Controls[controls[i]], d.Controls[controls[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return controls[i] < controls[j]
	})
	p := planet{Name: name, SpaceAge: spaceAge}
	for _, c := range controls {
		rc, ok := d.Controls[c]
		if !ok {
			return p, fmt.Errorf("planet %s uses unknown autoplace control %s", name, c)
		}
		switch rc.Category {
		case "resource", "terrain", "cliff", "enemy":
		default:
			return p, fmt.Errorf("control %s has category %q", c, rc.Category)
		}
		p.Controls = append(p.Controls, control{Name: c, Category: rc.Category, Richness: rc.Richness != nil && *rc.Richness,
			CanBeDisabled: rc.CanBeDisabled == nil || *rc.CanBeDisabled})
	}
	return p, nil
}

// dump runs the game's --dump-data in a throwaway container and reads the result.
func dump(image, line string, spaceAge bool) (rawDump, error) {
	var d rawDump
	work, err := os.MkdirTemp("", "mapgen-extract-")
	if err != nil {
		return d, err
	}
	defer os.RemoveAll(work)
	if err := os.Chmod(work, 0o777); err != nil {
		return d, err
	}
	gl, _ := mapgen.LineOf(line)
	mods := `{"name":"base","enabled":true}`
	for _, m := range gl.SpaceAgeMods {
		mods += fmt.Sprintf(`,{"name":%q,"enabled":%t}`, m, spaceAge)
	}
	for _, m := range gl.AlwaysOnMods {
		mods += fmt.Sprintf(`,{"name":%q,"enabled":true}`, m)
	}
	files := map[string]string{
		"config.ini":         "[path]\nread-data=/opt/factorio/data\nwrite-data=/work\n",
		"mods/mod-list.json": `{"mods":[` + mods + `]}`,
	}
	if err := os.MkdirAll(filepath.Join(work, "mods"), 0o777); err != nil {
		return d, err
	}
	for n, c := range files {
		if err := os.WriteFile(filepath.Join(work, n), []byte(c), 0o644); err != nil {
			return d, err
		}
	}
	if err := os.Chmod(filepath.Join(work, "mods"), 0o777); err != nil {
		return d, err
	}
	cmd := exec.Command("docker", "run", "--rm", "-u", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-v", work+":/work", "--entrypoint", "/opt/factorio/bin/x64/factorio", image,
		"--config", "/work/config.ini", "--mod-directory", "/work/mods", "--dump-data")
	if out, err := cmd.CombinedOutput(); err != nil {
		return d, fmt.Errorf("dump-data: %v\n%s", err, out)
	}
	f, err := os.Open(filepath.Join(work, "script-output", "data-raw-dump.json"))
	if err != nil {
		return d, err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&d); err != nil {
		return d, err
	}
	return d, nil
}

func dockerOut(image string, argv ...string) ([]byte, error) {
	args := append([]string{"run", "--rm", "--entrypoint", argv[0], image}, argv[1:]...)
	out, err := exec.Command("docker", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}
