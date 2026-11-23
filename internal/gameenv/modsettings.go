package gameenv

// Names a mod settings definitions run shares with the fake game. It is a
// preview container with PreviewKind set to KindModSettings that writes the
// world's mod setting definitions and a defaults mod-settings.dat instead of
// drawing pictures (SPEC 9.6, 14.3).
const (
	// KindModSettings is PreviewKind's value for a definitions run.
	KindModSettings = "modsettings"
	// ModSettingsDefsFile is the JSON of definitions the run leaves in the
	// work folder.
	ModSettingsDefsFile = "mod-settings-defs.json"
	// PlanetsFile is the JSON of planets, autoplace controls and resources
	// the same run leaves in the work folder (SPEC 5.6). It may be missing.
	PlanetsFile = "planets.json"
	// IconsFile is the JSON of icon paths the same run leaves in the work
	// folder (SPEC 14.6). It may be missing.
	IconsFile = "icons.json"
	// StarMapFile is the JSON of space locations and routes the same run
	// leaves in the work folder (SPEC 14.6). It may be missing.
	StarMapFile = "starmap.json"
)
