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

// CrashedMods is the names of the mods whose files the game's error names,
// in the order they appear, for a run that failed in the game (SPEC 5.6,
// 8.7). It is empty when the error is not a game failure or names no mod.
// A name is what the log says: the caller checks it is one of the world's
// mods before it shows it.
func CrashedMods(err error) []string {
	var pe *PreviewError
	if !errors.As(err, &pe) || pe.OOM {
		return nil
	}
	var out []string
	for _, m := range modPathRE.FindAllStringSubmatch(pe.Log, -1) {
		if name := m[1]; !slices.Contains(notWorldMods, name) && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}
