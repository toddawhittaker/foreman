package game

import (
	"errors"
	"regexp"
	"slices"

	modsettingsmod "github.com/toddawhittaker/foreman/internal/game/modsettings"
)

// modPathRE is a path into a mod in the game's log, "__<mod>__/", as an
// error in a mod's script names it (SPEC 8.7).
var modPathRE = regexp.MustCompile(`__([A-Za-z0-9][A-Za-z0-9 ._-]*?)__/`)

// notWorldMods are the paths in a log that are never the world's mod at
// fault: the game's own data and Foreman's helper mod.
var notWorldMods = []string{"core", "base", modsettingsmod.ModName}

// CrashedMod is the name of the mod the game's error line names, for a run
// that failed in the game (SPEC 5.6, 8.7): the first path into a mod in the
// game's first error line. A traceback lists the frames that led there,
// library mods among them, so it never names the mod. The name is empty when
// the error is not a game failure or its line names no mod of the world. A
// name is what the log says: the caller checks it is one of the world's mods
// before it shows it.
func CrashedMod(err error) string {
	var pe *PreviewError
	if !errors.As(err, &pe) || pe.OOM {
		return ""
	}
	for _, m := range modPathRE.FindAllStringSubmatch(pe.ErrorLine, -1) {
		if !slices.Contains(notWorldMods, m[1]) {
			return m[1]
		}
	}
	return ""
}
