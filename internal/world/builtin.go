package world

import (
	"regexp"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// ModBase is the game's own mod, enabled in every world.
const ModBase = "base"

var gameMinorRE = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.\d+)*$`)

// gameMinor returns "2.0" for "2.0.72" and for "2.0", and "" for anything
// that is not dotted numbers.
func gameMinor(gameVersion string) string {
	m := gameMinorRE.FindStringSubmatch(gameVersion)
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2]
}

// SupportedGameVersion reports whether Foreman runs this game version:
// one of the lines in mapgen.Lines.
func SupportedGameVersion(gameVersion string) bool {
	_, ok := mapgen.LineOf(gameVersion)
	return ok && ValidGameVersion(gameVersion)
}

// BuiltinMods lists the mods that ship with this game version: base, any
// mod that is always on, and the Space Age mods. It is nil for an
// unsupported version.
func BuiltinMods(gameVersion string) []string {
	if !SupportedGameVersion(gameVersion) {
		return nil
	}
	return append(append([]string{ModBase}, alwaysOnMods(gameVersion)...), SpaceAgeMods(gameVersion)...)
}

// alwaysOnMods lists the built-in mods of this game version that are not
// part of Space Age and stay enabled with Space Age off.
func alwaysOnMods(gameVersion string) []string {
	line, _ := mapgen.LineOf(gameVersion)
	return line.AlwaysOnMods
}

// SpaceAgeMods lists the Space Age DLC mods of this game version, which
// are enabled only when the world's Space Age flag is on (SPEC 9.7). Space
// Age does not exist before 2.0.
func SpaceAgeMods(gameVersion string) []string {
	if !SupportedGameVersion(gameVersion) {
		return nil
	}
	line, _ := mapgen.LineOf(gameVersion)
	return line.SpaceAgeMods
}
