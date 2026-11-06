// Package gameenv holds the names Foreman's game containers share with the
// fake game: the environment variables the start script reads (SPEC 4.3),
// and the label and mount of the one-off preview container (SPEC 5.6, 14.3).
// They are constants here so the real code and the fake cannot drift apart.
package gameenv

// Environment variables of a game container. The start script reads them,
// so no value is ever built into the script's text.
const (
	// NewMap is set when the world's first start makes the map.
	NewMap = "FOREMAN_NEW_MAP"
	// WorldID names the save the create step writes.
	WorldID = "FOREMAN_WORLD_ID"
	// Preset and Seed are set only when the world's new_map has them.
	Preset = "FOREMAN_PRESET"
	Seed   = "FOREMAN_SEED"
)

// Environment variables of a preview container.
const (
	// PreviewPlanets lists the planets to draw, separated by spaces.
	PreviewPlanets = "FOREMAN_PREVIEW_PLANETS"
	// PreviewPlanetFlag is set on 2.x, where --map-preview-planet exists.
	PreviewPlanetFlag = "FOREMAN_PREVIEW_PLANET_FLAG"
)

// LabelPreview marks a preview container. Its value is the host data
// directory, so one install never removes another's.
const LabelPreview = "io.github.toddawhittaker.foreman.preview"

// PreviewMount is where a preview container sees its work folder.
const PreviewMount = "/out"
