// Package saveplayers holds the control-only mod that reads the players of a
// save in a one-off game container (SPEC 8.16, 14.3, ADR 0030).
package saveplayers

import _ "embed"

// The mod's name and version, and so its folder under the mod directory.
const (
	ModName    = "foreman-saveplayers"
	ModVersion = "1.0.0"
	ModDir     = ModName + "_" + ModVersion
)

// Control is the mod's control.lua. It writes saveplayers.json (format 1).
//
//go:embed control.lua
var Control []byte
