package modsettings

import (
	"regexp"
	"strings"
)

// MaxLabelChars is the longest label, in characters, Foreman shows for a name
// a mod supplies, such as a planet or autoplace control (SPEC 14.6, Epic 12
// ruling R11).
const MaxLabelChars = 100

// richTextRE matches one game rich-text tag: [entity=iron-ore], [img=a/b],
// [color=red], [/color], [font=default-bold] and the like.
var richTextRE = regexp.MustCompile(`\[/?[a-zA-Z][a-zA-Z0-9_-]*(?:=[^\[\]]*)?\]`)

// StripRichText removes the game's rich-text tags from a resolved localised
// string, keeps the text between them, collapses runs of white space and cuts
// the result to MaxLabelChars characters. Apply it after ResolveLocalised.
func StripRichText(s string) string {
	s = strings.Join(strings.Fields(richTextRE.ReplaceAllString(s, " ")), " ")
	if r := []rune(s); len(r) > MaxLabelChars {
		s = strings.TrimSpace(string(r[:MaxLabelChars]))
	}
	return s
}
