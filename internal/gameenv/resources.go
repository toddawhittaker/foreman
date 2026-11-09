package gameenv

// Names a resource count run shares with the fake game. It is a preview
// container, with LabelPreview and PreviewMount and PreviewKind set to
// KindResources, that counts the resource patches in one planet's picture
// instead of drawing pictures (SPEC 5.6, 14.3). The file's format is in
// SPEC 14.6 and ADR 0015.
const (
	// KindResources is PreviewKind's value for a resource count run.
	KindResources = "resources"
	// ResourceRadius is the picture's half width in chunks: the run counts
	// chunks -radius to radius-1 on each axis.
	ResourceRadius = "FOREMAN_RESOURCE_RADIUS"
	// ResourcePlanet is the planet the run counts, one of mapgen.Planets.
	ResourcePlanet = "FOREMAN_RESOURCE_PLANET"
	// ResourceFile is the JSON a resource count run leaves in the work
	// folder.
	ResourceFile = "resources.json"
)
