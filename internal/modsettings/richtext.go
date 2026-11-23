package modsettings

import (
	"html"
	"html/template"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
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

// RichTextIcons is the screen-reader text for the icons rich text may show,
// in the page's language.
type RichTextIcons struct {
	Warning, Info string
}

// richTagRE matches one rich-text tag at the start of the input.
var richTagRE = regexp.MustCompile(`^\[(/?)([a-zA-Z][a-zA-Z0-9_-]*)(?:=([^\[\]]*))?\]`)

// macroRE matches a locale macro: __CONTROL__toggle-driving__, __1__.
var macroRE = regexp.MustCompile(`^__(?:([A-Z][A-Z_]*?)__([a-zA-Z0-9_-]+)|[0-9]+)__`)

// objectTags are the tags that name a game object by its prototype name.
var objectTags = map[string]bool{
	"item": true, "fluid": true, "entity": true, "technology": true, "recipe": true,
	"planet": true, "space-location": true, "virtual-signal": true, "tile": true,
}

// richColors maps the game's named colours to Foreman's classes, each tied
// to a theme token that passes contrast in both themes. Other colours,
// including hex and r,g,b values, render as plain text.
var richColors = map[string]string{
	"red": "rt-danger", "orange": "rt-warn", "yellow": "rt-warn", "green": "rt-ok",
	"blue": "rt-info", "cyan": "rt-info", "purple": "rt-info", "pink": "rt-info",
	"gray": "rt-muted", "grey": "rt-muted",
}

// Readable makes a prototype or action name readable:
// space-science-pack becomes "Space science pack".
func Readable(name string) string {
	s := strings.Join(strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' }), " ")
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// objectHTML is how an object tag ([item=space-science-pack]) shows: its
// name made readable, escaped. Every object tag goes through here.
func objectHTML(kind, name string) string {
	return html.EscapeString(Readable(name))
}

// iconHTML is how an [img=type/name] tag shows: Foreman's own icon for the
// game's warning and info sprites, the object's name for type/name, else
// nothing.
func iconHTML(path string, icons RichTextIcons) string {
	kind, name, ok := strings.Cut(path, "/")
	if !ok || name == "" || strings.Contains(name, "/") {
		return ""
	}
	if kind == "utility" {
		switch {
		case strings.Contains(name, "warning") || strings.Contains(name, "alert") || strings.Contains(name, "danger"):
			return `<span class="rt-icon rt-icon--warning" role="img" aria-label="` + html.EscapeString(icons.Warning) + `"></span>`
		case strings.Contains(name, "info"):
			return `<span class="rt-icon rt-icon--info" role="img" aria-label="` + html.EscapeString(icons.Info) + `"></span>`
		}
		return ""
	}
	return objectHTML(kind, name)
}

// RichTextHTML renders a resolved localised string from a mod as HTML,
// keeping a safe subset of the game's rich text (SPEC 14.6): bold fonts,
// named colours, object names, warning icons, key-binding actions and, when
// multiline, line breaks. The HTML is built from an allowed list; no text,
// attribute or class from the mod reaches it unescaped. Unknown tags,
// closing tags that match nothing and a lone [ stay as literal text.
func RichTextHTML(s string, multiline bool, icons RichTextIcons) template.HTML {
	s = strings.Map(dropInvisible, s)
	var b strings.Builder
	var open []string // closing HTML for each open [color] or [font], "" when text only
	var tags []string // the tag name for each entry in open
	for len(s) > 0 {
		switch {
		case s[0] == '[':
			if m := richTagRE.FindStringSubmatch(s); m != nil && richTag(&b, m, &open, &tags, icons) {
				s = s[len(m[0]):]
				continue
			}
		case strings.HasPrefix(s, `\n`) || s[0] == '\n':
			if multiline {
				b.WriteString("<br>")
			} else {
				b.WriteByte(' ')
			}
			if s[0] == '\\' {
				s = s[2:]
			} else {
				s = s[1:]
			}
			continue
		case strings.HasPrefix(s, "__"):
			if m := macroRE.FindStringSubmatch(s); m != nil {
				if m[1] == "CONTROL" {
					b.WriteString(`<kbd class="rt-key">` + html.EscapeString(Readable(m[2])) + "</kbd>")
				}
				s = s[len(m[0]):]
				continue
			}
		}
		_, n := utf8.DecodeRuneInString(s)
		b.WriteString(html.EscapeString(s[:n]))
		s = s[n:]
	}
	for i := len(open) - 1; i >= 0; i-- {
		b.WriteString(open[i])
	}
	return template.HTML(b.String()) //nolint:gosec // built from escaped text and fixed markup only
}

// richTag writes one matched tag and reports whether it was on the allowed
// list; a tag that is not stays as literal text.
func richTag(b *strings.Builder, m []string, open, tags *[]string, icons RichTextIcons) bool {
	closing, name, value := m[1] == "/", strings.ToLower(m[2]), m[3]
	hasValue := strings.Contains(m[0], "=")
	if closing {
		if hasValue || len(*tags) == 0 || (*tags)[len(*tags)-1] != name {
			return false
		}
		b.WriteString((*open)[len(*open)-1])
		*open, *tags = (*open)[:len(*open)-1], (*tags)[:len(*tags)-1]
		return true
	}
	switch {
	case name == "color" && hasValue:
		end := ""
		if class, ok := richColors[strings.ToLower(strings.TrimSpace(value))]; ok {
			b.WriteString(`<span class="` + class + `">`)
			end = "</span>"
		}
		*open, *tags = append(*open, end), append(*tags, name)
	case name == "font" && hasValue:
		end := ""
		if f := strings.ToLower(value); f == "default-bold" || strings.HasPrefix(f, "heading-") || strings.HasSuffix(f, "-bold") {
			b.WriteString("<strong>")
			end = "</strong>"
		}
		*open, *tags = append(*open, end), append(*tags, name)
	case objectTags[name] && hasValue && value != "":
		b.WriteString(objectHTML(name, value))
	case name == "img" && hasValue:
		b.WriteString(iconHTML(value, icons))
	case name == "space-age" && !hasValue:
		b.WriteString("Space Age")
	default:
		return false
	}
	return true
}
