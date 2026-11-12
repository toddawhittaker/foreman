package web

import (
	"regexp"
	"strings"
)

// The design tokens as foreman.css defines them, read from the stylesheet
// itself so the token check and the component page have one source
// (DESIGN 3, 4).

// themedValue is a token's value in each theme; a token that does not use
// light-dark() has the same value in both.
type themedValue struct {
	Light, Dark string
}

// designToken is one custom property declared on :root, in file order.
type designToken struct {
	Name string
	themedValue
}

// Bare is the name without its leading "--", for a template to write as
// var(--{{.Bare}}): html/template refuses a style value that starts "--".
func (t designToken) Bare() string { return strings.TrimPrefix(t.Name, "--") }

// cssProperty is one custom property declaration, its value normalised.
type cssProperty struct {
	Name, Value string
}

var cssComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

// rootProperties reads the custom properties declared in the top-level
// :root rules, the ones outside any @media, in file order. Per-component
// locals (--icon, --zoom) are set on other selectors, and the rules inside
// @media only adjust a token for the window, so neither is read.
func rootProperties(css string) []cssProperty {
	css = cssComment.ReplaceAllString(css, "")
	var out []cssProperty
	depth, start := 0, 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			if depth == 0 && strings.TrimSpace(css[start:i]) == ":root" {
				out = append(out, declarations(css[i+1:matchingBrace(css, i)])...)
			}
			depth++
		case '}':
			depth--
			if depth == 0 {
				start = i + 1
			}
		case ';':
			if depth == 0 {
				start = i + 1
			}
		}
	}
	return out
}

// iconPrefix starts the name of each line icon's custom property; the icons
// are a set described in DESIGN 7.9, not values a token table lists.
const iconPrefix = "--i-"

// designTokens is every :root custom property except the line icons.
func designTokens(css string) []designToken {
	var out []designToken
	for _, p := range rootProperties(css) {
		if !strings.HasPrefix(p.Name, iconPrefix) {
			out = append(out, designToken{Name: p.Name, themedValue: resolveThemes(p.Value)})
		}
	}
	return out
}

// iconNames is the line icon set, each name without its prefix, in file order.
func iconNames(css string) []string {
	var out []string
	for _, p := range rootProperties(css) {
		if name, ok := strings.CutPrefix(p.Name, iconPrefix); ok {
			out = append(out, name)
		}
	}
	return out
}

// matchingBrace returns the index of the brace that closes the one at open.
func matchingBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(s)
}

// declarations splits a rule body into its custom properties. A semicolon
// inside quotes or parentheses (a data URI) does not end a declaration.
func declarations(body string) []cssProperty {
	var parts []string
	depth, quote, start := 0, byte(0), 0
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ';' && depth == 0:
			parts = append(parts, body[start:i])
			start = i + 1
		}
	}
	parts = append(parts, body[start:])
	var out []cssProperty
	for _, p := range parts {
		name, value, ok := strings.Cut(strings.TrimSpace(p), ":")
		if ok && strings.HasPrefix(name, "--") {
			out = append(out, cssProperty{Name: strings.TrimSpace(name), Value: normaliseValue(value)})
		}
	}
	return out
}

// lightDark matches light-dark(a, b) where each side holds at most one
// level of brackets, such as rgb(0 0 0 / 45%).
var lightDark = regexp.MustCompile(`light-dark\(([^(),]*(?:\([^()]*\))?[^(),]*),([^(),]*(?:\([^()]*\))?[^(),]*)\)`)

// resolveThemes gives a value as each theme sees it. In a shadow, the layers
// whose colour is transparent in a theme are dropped, because light-dark()
// takes only colours and so each theme's layers are hidden in the other.
func resolveThemes(value string) themedValue {
	if !strings.Contains(value, "light-dark(") {
		return themedValue{Light: value, Dark: value}
	}
	pick := func(side string) string {
		var kept []string
		for _, layer := range splitTopLevel(lightDark.ReplaceAllString(value, side), ',') {
			if !strings.HasSuffix(layer, " transparent") {
				kept = append(kept, layer)
			}
		}
		return normaliseValue(strings.Join(kept, ","))
	}
	return themedValue{Light: pick("$1"), Dark: pick("$2")}
}

// splitTopLevel splits s at sep where it is outside brackets.
func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(parts, strings.TrimSpace(s[start:]))
}

var (
	spaceRun    = regexp.MustCompile(`\s+`)
	spaceAround = regexp.MustCompile(`\s*([(),])\s*`)
	hexColour   = regexp.MustCompile(`#[0-9A-Fa-f]{3,8}\b`)
)

// normaliseValue removes differences that do not change a value: runs of
// white space, space beside brackets and commas, and the case of hex colours.
func normaliseValue(v string) string {
	v = spaceRun.ReplaceAllString(strings.TrimSpace(v), " ")
	v = spaceAround.ReplaceAllString(v, "$1")
	return hexColour.ReplaceAllStringFunc(v, strings.ToLower)
}
