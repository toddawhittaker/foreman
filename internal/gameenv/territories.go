package gameenv

// Names a territory run shares with the fake game. It is a preview
// container, with LabelPreview and PreviewMount, that reads Vulcanus's
// demolisher territories instead of drawing pictures (SPEC 5.6, 14.3).
const (
	// PreviewKind says what a preview container makes; unset is pictures.
	PreviewKind = "FOREMAN_PREVIEW_KIND"
	// KindTerritories is PreviewKind's value for a territory run.
	KindTerritories = "territories"
	// TerritoryRadius is the picture's half width in chunks: the run reads
	// chunks -radius to radius-1 on each axis.
	TerritoryRadius = "FOREMAN_TERRITORY_RADIUS"
	// TerritoryFile is the JSON a territory run leaves in the work folder.
	TerritoryFile = "territories.json"
)
