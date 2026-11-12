package web

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// themedValue is a token's value in each theme; a token that does not use
// light-dark() has the same value in both.
type themedValue struct {
	light, dark string
}

// The design tokens in foreman.css must be the ones docs/DESIGN.md lists,
// with the same values (DESIGN 2, 3 and 4). The CSS is what ships, so when
// the two differ the doc is the one to fix, unless the change is meant.
func TestDesignTokensMatchDoc(t *testing.T) {
	css, err := os.ReadFile("static/foreman.css")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile("../../docs/DESIGN.md")
	if err != nil {
		t.Fatal(err)
	}
	inCSS := cssTokens(string(css))
	inDoc := docTokens(string(doc))
	if len(inCSS) == 0 || len(inDoc) == 0 {
		t.Fatalf("found %d tokens in the CSS and %d in the doc; the parsers no longer fit the files", len(inCSS), len(inDoc))
	}

	var names []string
	for name := range inCSS {
		names = append(names, name)
	}
	for name := range inDoc {
		if _, ok := inCSS[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		c, inC := inCSS[name]
		d, inD := inDoc[name]
		switch {
		case !inD:
			t.Errorf("%s is in foreman.css (light %q, dark %q) but not in a DESIGN.md token table", name, c.light, c.dark)
		case !inC:
			t.Errorf("%s is in DESIGN.md but not defined on :root in foreman.css", name)
		case c != d:
			t.Errorf("%s differs: foreman.css has light %q, dark %q; DESIGN.md has light %q, dark %q", name, c.light, c.dark, d.light, d.dark)
		}
	}
}

// cssTokens reads the custom properties declared in the top-level :root
// rules, the ones outside any @media. Per-component locals (--icon, --zoom)
// are set on other selectors and so are not tokens. The icon images (--i-*)
// are a set described in DESIGN 7.9, not values a table can list.
func cssTokens(css string) map[string]themedValue {
	css = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(css, "")
	tokens := map[string]themedValue{}
	depth := 0
	start := 0
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '{':
			if depth == 0 {
				selector := strings.TrimSpace(css[start:i])
				if selector == ":root" {
					end := matchingBrace(css, i)
					for name, value := range declarations(css[i+1 : end]) {
						if !strings.HasPrefix(name, "--i-") {
							tokens[name] = resolveThemes(value)
						}
					}
				}
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
	return tokens
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
func declarations(body string) map[string]string {
	out := map[string]string{}
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
	for _, p := range parts {
		name, value, ok := strings.Cut(strings.TrimSpace(p), ":")
		if ok && strings.HasPrefix(name, "--") {
			out[strings.TrimSpace(name)] = normaliseValue(value)
		}
	}
	return out
}

var lightDark = regexp.MustCompile(`light-dark\(([^(),]*(?:\([^()]*\))?[^(),]*),([^(),]*(?:\([^()]*\))?[^(),]*)\)`)

// resolveThemes gives a value as each theme sees it. In a shadow, the layers
// whose colour is transparent in a theme are dropped, because light-dark()
// takes only colours and so each theme's layers are hidden in the other.
func resolveThemes(value string) themedValue {
	pick := func(group string) string {
		v := lightDark.ReplaceAllString(value, group)
		var kept []string
		for _, layer := range splitTopLevel(v, ',') {
			if !strings.HasSuffix(layer, " transparent") {
				kept = append(kept, layer)
			}
		}
		return normaliseValue(strings.Join(kept, ","))
	}
	if !strings.Contains(value, "light-dark(") {
		return themedValue{light: value, dark: value}
	}
	return themedValue{light: pick("$1"), dark: pick("$2")}
}

// splitTopLevel splits s at sep where it is outside parentheses.
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

var (
	codeSpan    = regexp.MustCompile("`([^`]*)`")
	parenthesis = regexp.MustCompile(`\s*\([^)]*\)`)
	cssBlock    = regexp.MustCompile("(?s)```css\n(.*?)```")
)

// docTokens reads the tokens DESIGN.md lists: each table row whose first
// cell is a `--name`, and each declaration in a css code block. A table with
// Dark and Light columns gives both themes; any other gives one value from
// its second column.
func docTokens(md string) map[string]themedValue {
	tokens := map[string]themedValue{}
	for _, block := range cssBlock.FindAllStringSubmatch(md, -1) {
		for name, value := range declarations(block[1]) {
			tokens[name] = themedValue{light: value, dark: value}
		}
	}
	var header []string
	for _, line := range strings.Split(md, "\n") {
		if !strings.HasPrefix(line, "|") {
			header = nil
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if header == nil {
			header = cells
			continue
		}
		name := strings.Trim(cells[0], "`")
		if !strings.HasPrefix(cells[0], "`--") || len(cells) < 2 {
			continue
		}
		dark, light := -1, -1
		for i, h := range header {
			switch h {
			case "Dark":
				dark = i
			case "Light":
				light = i
			}
		}
		if dark > 0 && light > 0 {
			tokens[name] = themedValue{light: docValue(cells[light]), dark: docValue(cells[dark])}
		} else {
			v := docValue(cells[1])
			tokens[name] = themedValue{light: v, dark: v}
		}
	}
	return tokens
}

// docValue is a cell's first code span, or else its text without a
// parenthetical note such as "(4 px)".
func docValue(cell string) string {
	if m := codeSpan.FindStringSubmatch(cell); m != nil {
		return normaliseValue(m[1])
	}
	return normaliseValue(parenthesis.ReplaceAllString(cell, ""))
}

// The check's own parsing: a doc's annotations and spacing do not count as
// drift, and light-dark() gives each theme its own value.
func TestDesignTokenParsing(t *testing.T) {
	css := cssTokens(`:root { --a: light-dark(#ABCDEF, #123456); --b: 0.25rem; --i-x: url("a;b"); }
@media (min-width: 1px) { :root { --b: 1rem; } }
.card { --icon: var(--i-x); }
:root[data-theme="dark"] { --c: 1px; }
:root { --s: 0 1px 2px light-dark(rgb(1 2 3 / 4%), transparent), inset 0 1px 0 light-dark(transparent, rgb(5 6 7)); }`)
	want := map[string]themedValue{
		"--a": {light: "#abcdef", dark: "#123456"},
		"--b": {light: "0.25rem", dark: "0.25rem"},
		"--s": {light: "0 1px 2px rgb(1 2 3 / 4%)", dark: "inset 0 1px 0 rgb(5 6 7)"},
	}
	if len(css) != len(want) {
		t.Errorf("cssTokens found %v, want %v", css, want)
	}
	for name, v := range want {
		if css[name] != v {
			t.Errorf("cssTokens %s = %+v, want %+v", name, css[name], v)
		}
	}

	doc := docTokens("| Token | Dark | Light | Use |\n|---|---|---|---|\n| `--a` | `#123456` | `#ABCDEF` | x |\n\n" +
		"| Token | Value | Use |\n|---|---|---|\n| `--b` | 0.25rem (4 px) | y |\n| `--p` | `min(1rem,  4vw)`, more | z |\n")
	if doc["--a"] != want["--a"] || doc["--b"] != want["--b"] || doc["--p"].light != "min(1rem,4vw)" {
		t.Errorf("docTokens = %+v", doc)
	}
}
