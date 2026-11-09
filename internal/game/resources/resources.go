// Package resources holds the control-only mod that counts the resource
// patches of one planet's picture in a one-off game container (SPEC 5.6,
// 14.3).
package resources

import (
	_ "embed"
	"encoding/json"
)

// The mod's name and version, and so its folder under the mod directory.
const (
	ModName    = "foreman-resources"
	ModVersion = "1.0.0"
	ModDir     = ModName + "_" + ModVersion
)

// Files the mod folder holds besides info.json.
const (
	ControlFile = "control.lua"
	// ParamsFile is written for each run; it returns {radius = N, planet =
	// "<name>"}, the picture's half width in chunks and the planet to
	// count, since a mod can read neither files nor the environment.
	ParamsFile = "params.lua"
)

// Control is the mod's control.lua.
//
//go:embed control.lua
var Control []byte

// InfoJSON is the mod's info.json for a game line such as "2.1". The game
// loads a mod only when its factorio_version is the game's own line. It
// depends on Space Age only when the map has it.
func InfoJSON(line string, spaceAge bool) []byte {
	deps := []string{"base"}
	if spaceAge {
		deps = append(deps, "space-age")
	}
	// Marshalling a plain struct of strings cannot fail.
	b, _ := json.MarshalIndent(struct {
		Name            string   `json:"name"`
		Version         string   `json:"version"`
		Title           string   `json:"title"`
		Author          string   `json:"author"`
		FactorioVersion string   `json:"factorio_version"`
		Description     string   `json:"description"`
		Dependencies    []string `json:"dependencies"`
	}{
		Name:            ModName,
		Version:         ModVersion,
		Title:           "Foreman resources",
		Author:          "Foreman",
		FactorioVersion: line,
		Description:     "Counts one planet's resource patches for Foreman's map preview. Adds no prototypes.",
		Dependencies:    deps,
	}, "", "  ")
	return append(b, '\n')
}
