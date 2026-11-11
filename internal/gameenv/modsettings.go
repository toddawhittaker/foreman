// Contract stub: task T1 of the mod settings epic owns this file.

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
	// ModSettingsDefaultsFile is the mod-settings.dat holding every default
	// that the game writes during the run.
	ModSettingsDefaultsFile = "mod-settings.dat"
)
