package mapgen

// Which planets the new-map form can switch cliffs off on (SPEC 5.6). The keys
// were proved by counting cliffs over RCON on 1.1.110, 2.0.77 and 2.1.24:
//
//   - 1.1 has one world: cliff_settings.richness = 0 removes every cliff.
//   - 2.x gives each planet its own control: autoplace_controls.<planet>_cliff.size = 0
//     removes the cliffs of that planet only.
//
// cliff_settings.richness must never be written on 2.x: there it reaches every
// planet, including Vulcanus, which has no cliff control of its own. Cliffs
// off disables no achievement on any line.

// CliffToggle reports whether cliffs can be switched off on a planet of a line.
// It is true for Nauvis on every line, and for Gleba and Fulgora on 2.x; the
// caller only asks about planets the world has. Unknown lines and planets,
// Vulcanus and Aquilo give false.
func CliffToggle(line, planet string) bool {
	switch planet {
	case "nauvis":
		_, ok := parsed[line]
		return ok
	case "gleba", "fulgora":
		return line == "2.0" || line == "2.1"
	}
	return false
}
