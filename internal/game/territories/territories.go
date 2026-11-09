// Package territories holds the control-only mod that reads Vulcanus's
// demolisher territories in a one-off game container (SPEC 5.6, 14.3).
package territories

import (
	_ "embed"
	"encoding/json"
)

// The mod's name and version, and so its folder under the mod directory.
const (
	ModName    = "foreman-territories"
	ModVersion = "1.0.0"
	ModDir     = ModName + "_" + ModVersion
)

// Files the mod folder holds besides info.json and control.lua.
const (
	// RadiusFile is written for each run; it returns the picture's half
	// width in chunks, since a mod can read neither files nor the
	// environment.
	RadiusFile = "radius.lua"
)

// Control is the mod's control.lua.
//
//go:embed control.lua
var Control []byte

// InfoJSON is the mod's info.json for a game line such as "2.1". The game
// loads a mod only when its factorio_version is the game's own line.
func InfoJSON(line string) []byte {
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
		Title:           "Foreman territories",
		Author:          "Foreman",
		FactorioVersion: line,
		Description:     "Writes Vulcanus's demolisher territories for Foreman's map preview. Adds no prototypes.",
		Dependencies:    []string{"base", "space-age"},
	}, "", "  ")
	return append(b, '\n')
}
