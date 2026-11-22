// Package chart holds the control-only mod that draws one planet from a real
// game, for the real-game picture of Generate's step 3 (SPEC 5.6, 14.3).
package chart

import _ "embed"

// The mod's name and version, and so its folder under the mod directory.
const (
	ModName    = "foreman-chart"
	ModVersion = "1.0.0"
	ModDir     = ModName + "_" + ModVersion
)

// ParamsFile is written for each run beside control.lua; it returns {radius =
// N, planet = "<name>"}, since a mod can read neither files nor the
// environment.
const ParamsFile = "params.lua"

// The picture's fixed parts and its output limits (SPEC 14.3, 14.6).
const (
	// Radius is the half width of the charted square in chunks: 1024 by 1024
	// tiles.
	Radius = 16
	// MaxChunks is the most chunks a chart may hold: the whole square.
	MaxChunks = (2 * Radius) * (2 * Radius)
	// MaxFileBytes is the most bytes the chart file may hold; one planet
	// measured about 275 KB.
	MaxFileBytes = 4 << 20
)

// Control is the mod's control.lua.
//
//go:embed control.lua
var Control []byte
