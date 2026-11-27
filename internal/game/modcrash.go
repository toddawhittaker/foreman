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

// eventLineRE is the line of a control-stage error that names the mod
// whose script failed, "Error while running event <mod>::<event>".
var eventLineRE = regexp.MustCompile(`^Error while running event ([A-Za-z0-9][A-Za-z0-9 ._-]*?)::`)

// nonRecoverableRE is a control-stage error line, which names the mod by its
// title and version only: "The mod <title> (<version>) caused a
// non-recoverable error."
var nonRecoverableRE = regexp.MustCompile(`The mod (.+) \(([^()\s]+)\) caused a non-recoverable error`)

// notWorldMods are the paths in a log that are never the world's mod at
// fault: the game's own data and Foreman's helper mod.
var notWorldMods = []string{"core", "base", modsettingsmod.ModName}

// CrashedMod is the name of the mod the game's error names, for a run that
// failed in the game (SPEC 5.6, 8.7): in a control-stage error, the mod of
// the event line next to the error line; otherwise the first path into a
// mod in the game's first error line. A traceback lists the frames that led
// there, library mods among them, so it never names the mod. The name is
// empty when the error is not a game failure or names no mod of the world. A
// name is what the log says: the caller checks it is one of the world's mods
// before it shows it.
func CrashedMod(err error) string {
	pe := gameFailure(err)
	if pe == nil {
		return ""
	}
	if m := eventLineRE.FindStringSubmatch(pe.EventLine); m != nil && !slices.Contains(notWorldMods, m[1]) {
		return m[1]
	}
	for _, m := range modPathRE.FindAllStringSubmatch(pe.ErrorLine, -1) {
		if !slices.Contains(notWorldMods, m[1]) {
			return m[1]
		}
	}
	return ""
}

// CrashedModTitle is the title and version a control-stage error line names
// its mod by, "The mod <title> (<version>) caused a non-recoverable error",
// for the caller to match to one of the world's mods when CrashedMod names
// none (SPEC 5.6, 8.7). Both are empty when the error is not a game failure
// or its line has another form.
func CrashedModTitle(err error) (title, version string) {
	pe := gameFailure(err)
	if pe == nil {
		return "", ""
	}
	if m := nonRecoverableRE.FindStringSubmatch(pe.ErrorLine); m != nil {
		return m[1], m[2]
	}
	return "", ""
}

// gameFailure is err's failed run, or nil when err is not a run the game
// failed: no run's error, or out of memory.
func gameFailure(err error) *PreviewError {
	var pe *PreviewError
	if !errors.As(err, &pe) || pe.OOM {
		return nil
	}
	return pe
}
