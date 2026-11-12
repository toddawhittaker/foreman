// Package modsettings holds the control-only mod that reads a world's mod
// setting definitions in a one-off game container (SPEC 9.6, 14.3).
package modsettings

import _ "embed"

// The mod's name and version, and so its folder under the mod directory.
const (
	ModName    = "foreman-modsettings"
	ModVersion = "1.0.0"
	ModDir     = ModName + "_" + ModVersion
)

// Control is the mod's control.lua.
//
//go:embed control.lua
var Control []byte
