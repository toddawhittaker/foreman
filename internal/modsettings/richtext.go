package modsettings

import (
	"regexp"
	"strings"
	"unicode"
)

// MaxLabelChars is the longest label, in characters, Foreman shows for a name
// a mod supplies, such as a planet or autoplace control (SPEC 14.6).
const MaxLabelChars = 100

// richTextRE matches one game rich-text tag: [entity=iron-ore], [img=a/b],
// [color=red], [/color], [font=default-bold] and the like.
var richTextRE = regexp.MustCompile(`\[/?[a-zA-Z][a-zA-Z0-9_-]*(?:=[^\[\]]*)?\]`)

// dropInvisible removes Unicode format characters (direction overrides,
// zero-width characters, the byte order mark) and control characters, which
// can reorder or hide text. White space is kept for Fields to collapse.
func dropInvisible(r rune) rune {
	if !unicode.IsSpace(r) && (unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Cc, r)) {
		return -1
	}
	return r
}

// StripRichText removes the game's rich-text tags from a resolved localised
// string, keeps the text between them, removes invisible and control
// characters (SPEC 14.6), collapses runs of white space and cuts the result
// to MaxLabelChars characters. Apply it after ResolveLocalised.
func StripRichText(s string) string {
	s = strings.Map(dropInvisible, s)
	s = strings.Join(strings.Fields(richTextRE.ReplaceAllString(s, " ")), " ")
	if r := []rune(s); len(r) > MaxLabelChars {
		s = strings.TrimSpace(string(r[:MaxLabelChars]))
	}
	return s
}
