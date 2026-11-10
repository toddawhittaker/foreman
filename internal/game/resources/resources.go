// Package resources holds the control-only mod that counts the resource
// patches of one planet's picture in a one-off game container (SPEC 5.6,
// 14.3).
package resources

import _ "embed"

// The mod's name and version, and so its folder under the mod directory.
const (
	ModName    = "foreman-resources"
	ModVersion = "1.0.0"
	ModDir     = ModName + "_" + ModVersion
)

// Files the mod folder holds besides info.json and control.lua.
const (
	// ParamsFile is written for each run; it returns {radius = N, planet =
	// "<name>"}, the picture's half width in chunks and the planet to
	// count, since a mod can read neither files nor the environment.
	ParamsFile = "params.lua"
)

// Control is the mod's control.lua.
//
//go:embed control.lua
var Control []byte
