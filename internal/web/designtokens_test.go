package web

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

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
	inCSS := map[string]themedValue{}
	for _, tok := range designTokens(string(css)) {
		inCSS[tok.Name] = tok.themedValue
	}
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
			t.Errorf("%s is in foreman.css (light %q, dark %q) but not in a DESIGN.md token table", name, c.Light, c.Dark)
		case !inC:
			t.Errorf("%s is in DESIGN.md but not defined on :root in foreman.css", name)
		case c != d:
			t.Errorf("%s differs: foreman.css has light %q, dark %q; DESIGN.md has light %q, dark %q", name, c.Light, c.Dark, d.Light, d.Dark)
		}
	}
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
		for _, p := range declarations(block[1]) {
			tokens[p.Name] = themedValue{Light: p.Value, Dark: p.Value}
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
		if !strings.HasPrefix(cells[0], "`--") || len(cells) < 2 {
			continue
		}
		name := strings.Trim(cells[0], "`")
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
			tokens[name] = themedValue{Light: docValue(cells[light]), Dark: docValue(cells[dark])}
		} else {
			v := docValue(cells[1])
			tokens[name] = themedValue{Light: v, Dark: v}
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
	css := `:root { --a: light-dark(#ABCDEF, #123456); --b: 0.25rem; --i-x: url("a;b"); }
@media (min-width: 1px) { :root { --b: 1rem; } }
.card { --icon: var(--i-x); }
:root[data-theme="dark"] { --c: 1px; }
:root { --s: 0 1px 2px light-dark(rgb(1 2 3 / 4%), transparent), inset 0 1px 0 light-dark(transparent, rgb(5 6 7)); }`
	want := []designToken{
		{"--a", themedValue{Light: "#abcdef", Dark: "#123456"}},
		{"--b", themedValue{Light: "0.25rem", Dark: "0.25rem"}},
		{"--s", themedValue{Light: "0 1px 2px rgb(1 2 3 / 4%)", Dark: "inset 0 1px 0 rgb(5 6 7)"}},
	}
	got := designTokens(css)
	if len(got) != len(want) {
		t.Fatalf("designTokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("designTokens[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	if icons := iconNames(css); len(icons) != 1 || icons[0] != "x" {
		t.Errorf("iconNames = %v, want [x]", icons)
	}

	doc := docTokens("| Token | Dark | Light | Use |\n|---|---|---|---|\n| `--a` | `#123456` | `#ABCDEF` | x |\n\n" +
		"| Token | Value | Use |\n|---|---|---|\n| `--b` | 0.25rem (4 px) | y |\n| `--p` | `min(1rem,  4vw)`, more | z |\n")
	if doc["--a"] != want[0].themedValue || doc["--b"] != want[1].themedValue || doc["--p"].Light != "min(1rem,4vw)" {
		t.Errorf("docTokens = %+v", doc)
	}
}
