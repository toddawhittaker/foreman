// Package territories holds the control-only mod that reads Vulcanus's
// demolisher territories in a one-off game container (SPEC 5.6, 14.3).
package territories

import _ "embed"

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
