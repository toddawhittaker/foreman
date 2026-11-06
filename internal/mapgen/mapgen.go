// Package mapgen holds the game's own map-generation data for each supported
// line (SPEC 5.6, 10.8): preset names, planets, autoplace controls and the
// default map-gen-settings file. It was read from the real game images and is
// embedded, so nothing is looked up or downloaded at run time (SPEC 4.2).
// "go generate" re-reads it; see extract.go.
package mapgen

//go:generate go run extract.go

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
)

//go:embed data
var files embed.FS

// Control categories the form treats apart from the others, as the game
// names them. The game's other categories are resource and enemy.
const (
	CategoryTerrain = "terrain"
	CategoryCliff   = "cliff"
)

// Control is one autoplace control: a setting with its own frequency and
// size, and richness when the game offers it for the control.
type Control struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Richness bool   `json:"richness"`
}

type planet struct {
	Name     string    `json:"name"`
	SpaceAge bool      `json:"space_age"`
	Controls []Control `json:"controls"`
}

type lineData struct {
	GameVersion string   `json:"game_version"`
	Presets     []string `json:"presets"`
	Planets     []planet `json:"planets"`
}

// Lines lists the supported game lines, oldest first.
var Lines = []string{"1.1", "2.0", "2.1"}

var versionRE = regexp.MustCompile(`^(1\.1|2\.0|2\.1)\.(?:0|[1-9][0-9]*)$`)

// Line returns the game line ("2.0") of an exact patch release ("2.0.77").
// It reports false for a line Foreman does not support and for anything that
// is not a full patch release (SPEC 10.7, 10.8).
func Line(version string) (string, bool) {
	m := versionRE.FindStringSubmatch(version)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// parsed is each line's data. The embedded data is part of the
// binary, so a failure here is a build mistake, not a run-time condition.
var parsed = func() map[string]lineData {
	m := map[string]lineData{}
	for _, l := range Lines {
		b, err := files.ReadFile("data/" + l + "/data.json")
		if err != nil {
			panic(fmt.Sprintf("mapgen: %v", err))
		}
		var d lineData
		if err := json.Unmarshal(b, &d); err != nil {
			panic(fmt.Sprintf("mapgen: data/%s/data.json: %v", l, err))
		}
		m[l] = d
	}
	return m
}()

// Presets returns the names the game accepts for --preset on a line, in the
// order its map generator lists them. It returns nil for an unknown line.
func Presets(line string) []string {
	return append([]string(nil), parsed[line].Presets...)
}

// Planets returns the planets a new world on a line has, Nauvis first: only
// Nauvis without Space Age, and always on 1.1, which has no Space Age.
func Planets(line string, spaceAge bool) []string {
	var out []string
	for _, p := range parsed[line].Planets {
		if spaceAge || !p.SpaceAge {
			out = append(out, p.Name)
		}
	}
	return out
}

// Controls returns a planet's autoplace controls in the game's order. It
// returns nil when the line has no such planet. A planet from Space Age is
// returned even if the caller has not asked for Space Age: use Planets to
// know which planets a world has.
func Controls(line, planet string) []Control {
	for _, p := range parsed[line].Planets {
		if p.Name == planet {
			return append([]Control(nil), p.Controls...)
		}
	}
	return nil
}

// DefaultMapGen returns the game's example map-gen-settings.json for a line,
// as shipped: keys that start with "_" are comments the game ignores. It
// returns nil for an unknown line.
func DefaultMapGen(line string) []byte { return read(line, "map-gen-settings.json") }

func read(line, name string) []byte {
	if _, ok := parsed[line]; !ok {
		return nil
	}
	b, err := files.ReadFile("data/" + line + "/" + name)
	if err != nil {
		return nil
	}
	return b
}
