package world

import "regexp"

// ModBase is the game's own mod, enabled in every world.
const ModBase = "base"

// spaceAgeMods are the Space Age DLC mods for each supported game minor
// version (SPEC 9.7). Space Age does not exist before 2.0.
var spaceAgeMods = map[string][]string{
	"1.1": nil,
	"2.0": {"elevated-rails", "quality", "space-age"},
	"2.1": {"elevated-rails", "quality", "space-age"},
}

// alwaysOnMods are built-in mods that are not part of Space Age and stay
// enabled with Space Age off: recycler is its own mod from 2.1.
var alwaysOnMods = map[string][]string{
	"2.1": {"recycler"},
}

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
// 1.1, 2.0 or 2.1.
func SupportedGameVersion(gameVersion string) bool {
	_, ok := spaceAgeMods[gameMinor(gameVersion)]
	return ok && ValidGameVersion(gameVersion)
}

// BuiltinMods lists the mods that ship with this game version: base, any
// mod that is always on, and the Space Age mods. It is nil for an
// unsupported version.
func BuiltinMods(gameVersion string) []string {
	if !SupportedGameVersion(gameVersion) {
		return nil
	}
	minor := gameMinor(gameVersion)
	out := []string{ModBase}
	out = append(out, alwaysOnMods[minor]...)
	return append(out, spaceAgeMods[minor]...)
}

// SpaceAgeMods lists the Space Age DLC mods of this game version, which
// are enabled only when the world's Space Age flag is on (SPEC 9.7).
func SpaceAgeMods(gameVersion string) []string {
	if !SupportedGameVersion(gameVersion) {
		return nil
	}
	return append([]string(nil), spaceAgeMods[gameMinor(gameVersion)]...)
}
