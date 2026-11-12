package gamesettings

import "unicode"

// EndsCommand reports whether r may not appear in text sent inside a console
// command: a control character or a line or paragraph separator could end
// the command or start another (SPEC 14.9).
func EndsCommand(r rune) bool {
	return unicode.IsControl(r) || r == ' ' || r == ' '
}
