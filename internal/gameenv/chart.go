package gameenv

// Names a real-game picture run shares with the fake game. It is a preview
// container with PreviewKind set to KindChart that creates a throwaway map,
// loads it with --test-save and leaves one planet's chart in the work folder
// (SPEC 5.6, 14.3).
const (
	// KindChart is PreviewKind's value for a real-game picture run.
	KindChart = "chart"
	// ChartRadius is the charted square's half width in chunks: the run
	// charts chunks -radius to radius-1 on each axis.
	ChartRadius = "FOREMAN_CHART_RADIUS"
	// ChartPlanet is the planet the run draws, one of the world's planets.
	ChartPlanet = "FOREMAN_CHART_PLANET"
	// ChartDir is the folder the run leaves in the work folder:
	// manifest.json and the planet's chart file, in the factory map's format.
	ChartDir = "chart"
)
