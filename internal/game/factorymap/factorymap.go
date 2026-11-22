// Package factorymap holds the control-only mod that reads a save's charted
// chunks and buildings in a one-off game container (SPEC 5.16, 14.3).
package factorymap

import _ "embed"

// The mod's name and version, and so its folder under the mod directory.
const (
	ModName    = "foreman-factorymap"
	ModVersion = "1.0.0"
	ModDir     = ModName + "_" + ModVersion
)

// Control is the mod's control.lua. It writes the factorymap folder:
// manifest.json and one file per surface (format 1).
//
//go:embed control.lua
var Control []byte
