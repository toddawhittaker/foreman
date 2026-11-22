// Package saveplayers holds the control-only mod that reads the players of a
// save in a one-off game container (SPEC 8.14, 8.16, 14.3, ADR 0030).
package saveplayers

import _ "embed"

// The mod's name and version, and so its folder under the mod directory. Its
// info.json is made at run time for the game's version line, since the game
// refuses a mod whose factorio_version is not its own line (2.1 refuses
// "2.0").
const (
	ModName    = "foreman-saveplayers"
	ModVersion = "1.0.0"
	ModDir     = ModName + "_" + ModVersion
)

// Control is the mod's control.lua. It writes saveplayers.json (format 1):
// the game version, the tick, the active mods, and each player's name and
// play time in ticks. It never reads the save's online flag or last-online
// date.
//
//go:embed control.lua
var Control []byte
