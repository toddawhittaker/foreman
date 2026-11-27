package web

import (
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// cssRule is one style rule of a stylesheet part: where it is, the @media,
// @container and @supports it sits in, its selectors and its declarations.
type cssRule struct {
	file, context string
	line          int
	selectors     []string
	decls         []cssProperty
}

// blankComments replaces each comment with spaces, keeping its line breaks,
// so positions still give the right line.
func blankComments(css string) string {
	return cssComment.ReplaceAllStringFunc(css, func(c string) string {
		return strings.Map(func(r rune) rune {
			if r == '\n' {
				return r
			}
			return ' '
		}, c)
	})
}

// cssEnd returns the index of the first of stops at depth 0 from i, skipping
// strings and anything inside brackets, or len(s).
func cssEnd(s string, i int, stops string) int {
	depth := 0
	for ; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"' || c == '\'':
			for i++; i < len(s) && s[i] != c; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			depth--
		case depth == 0 && strings.IndexByte(stops, c) >= 0:
			return i
		}
	}
	return len(s)
}

// cssBlockEnd returns the index of the brace that closes the one at open.
func cssBlockEnd(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			for i++; i < len(s) && s[i] != c; i++ {
				if s[i] == '\\' {
					i++
				}
			}
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

// cssSquash collapses runs of white space outside strings.
func cssSquash(s string) string {
	var b strings.Builder
	quote, space := byte(0), false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			b.WriteByte(c)
			if c == quote {
				quote = 0
			}
			continue
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			space = true
			continue
		case c == '"' || c == '\'':
			quote = c
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteByte(c)
	}
	return b.String()
}

// parseCSS reads the style rules of one stylesheet part. A cascade layer
// adds nothing to a rule's context; @font-face and @keyframes are skipped.
func parseCSS(file, css string) []cssRule {
	css = blankComments(css)
	var out []cssRule
	var walk func(from, to int, context string)
	walk = func(from, to int, context string) {
		for i := from; i < to; {
			end := cssEnd(css[:to], i, "{;")
			prelude := cssSquash(css[i:end])
			if end >= to || css[end] == ';' {
				i = end + 1
				continue
			}
			close := cssBlockEnd(css, end)
			switch {
			case prelude == "":
			case strings.HasPrefix(prelude, "@layer"):
				walk(end+1, close, context)
			case strings.HasPrefix(prelude, "@media"), strings.HasPrefix(prelude, "@container"), strings.HasPrefix(prelude, "@supports"):
				walk(end+1, close, strings.TrimSpace(context+" "+prelude))
			case strings.HasPrefix(prelude, "@"):
			default:
				start := end - len(strings.TrimLeft(css[i:end], " \t\r\n"))
				r := cssRule{file: file, context: context, line: 1 + strings.Count(css[:start], "\n"), selectors: splitTopLevel(prelude, ',')}
				body := css[end+1 : close]
				for j := 0; j < len(body); {
					k := cssEnd(body, j, ";")
					if name, value, ok := strings.Cut(body[j:k], ":"); ok {
						r.decls = append(r.decls, cssProperty{Name: strings.ToLower(strings.TrimSpace(name)), Value: cssSquash(value)})
					}
					j = k + 1
				}
				out = append(out, r)
			}
			i = close + 1
		}
	}
	walk(0, len(css), "")
	return out
}

// stylesheetRules reads every part of the stylesheet.
func stylesheetRules(t *testing.T) []cssRule {
	t.Helper()
	var out []cssRule
	for _, p := range stylesheetParts {
		body, err := staticFS.ReadFile("static/css/" + p.File)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, parseCSS(p.File, string(body))...)
	}
	if len(out) < 1000 {
		t.Fatalf("read only %d rules; the parser no longer fits the stylesheet", len(out))
	}
	return out
}

// cssBox are the four sides a one-to-four value box shorthand sets, in the
// order its values name them, as logical sides in a left-to-right,
// top-to-bottom page.
var cssBox = []string{"block-start", "inline-end", "block-end", "inline-start"}

// cssShorthands are the shorthands the conflict guard expands, each to its
// longhands. A positional shorthand gives each longhand one of its values:
// a box takes one to four (top, right, bottom, left), a pair one or two. The
// rest give every longhand the whole declaration.
var cssShorthands = map[string]struct {
	longhands []string
	box, pair bool
}{
	"padding":        {longhands: cssSides("padding-", ""), box: true},
	"margin":         {longhands: cssSides("margin-", ""), box: true},
	"inset":          {longhands: cssSides("inset-", ""), box: true},
	"border-width":   {longhands: cssSides("border-", "-width"), box: true},
	"border-style":   {longhands: cssSides("border-", "-style"), box: true},
	"border-color":   {longhands: cssSides("border-", "-color"), box: true},
	"padding-block":  {longhands: []string{"padding-block-start", "padding-block-end"}, pair: true},
	"padding-inline": {longhands: []string{"padding-inline-start", "padding-inline-end"}, pair: true},
	"margin-block":   {longhands: []string{"margin-block-start", "margin-block-end"}, pair: true},
	"margin-inline":  {longhands: []string{"margin-inline-start", "margin-inline-end"}, pair: true},
	"inset-block":    {longhands: []string{"inset-block-start", "inset-block-end"}, pair: true},
	"inset-inline":   {longhands: []string{"inset-inline-start", "inset-inline-end"}, pair: true},
	"gap":            {longhands: []string{"row-gap", "column-gap"}, pair: true},
	"overflow":       {longhands: []string{"overflow-x", "overflow-y"}, pair: true},
	"place-items":    {longhands: []string{"align-items", "justify-items"}, pair: true},
	"border":         {longhands: cssBorderLonghands(cssBox...)},
	"border-block":   {longhands: cssBorderLonghands("block-start", "block-end")},
	"border-inline":  {longhands: cssBorderLonghands("inline-start", "inline-end")},
	"background": {longhands: []string{"background-color", "background-image", "background-position", "background-size",
		"background-repeat", "background-attachment", "background-origin", "background-clip"}},
}

func cssSides(prefix, suffix string) []string {
	var out []string
	for _, s := range cssBox {
		out = append(out, prefix+s+suffix)
	}
	return out
}

func cssBorderLonghands(sides ...string) []string {
	var out []string
	for _, s := range sides {
		for _, p := range []string{"-width", "-style", "-color"} {
			out = append(out, "border-"+s+p)
		}
	}
	return out
}

// cssLonghands gives the longhands a declaration sets, each with the value
// it gets. A positional shorthand whose values do not fit its form, and a
// whole-value shorthand, give every longhand the declaration itself, so two
// of them agree only when they are the same.
func cssLonghands(d cssProperty) []cssProperty {
	sh, ok := cssShorthands[d.Name]
	if !ok {
		return []cssProperty{d}
	}
	whole := d.Name + ": " + d.Value
	values := splitTopLevel(d.Value, ' ')
	pick := func(int) string { return whole }
	switch n := len(values); {
	case sh.box && n >= 1 && n <= 4:
		// top, right, bottom, left: a missing side copies its opposite.
		idx := [][]int{{0, 0, 0, 0}, {0, 1, 0, 1}, {0, 1, 2, 1}, {0, 1, 2, 3}}[n-1]
		pick = func(i int) string { return values[idx[i]] }
	case sh.pair && (n == 1 || n == 2):
		pick = func(i int) string { return values[min(i, n-1)] }
	}
	out := make([]cssProperty, len(sh.longhands))
	for i, l := range sh.longhands {
		out[i] = cssProperty{l, pick(i)}
	}
	return out
}

// cssNormal drops the white space that does not change what a selector or
// an at-rule's condition means, so `.a>.b` and `.a > .b`, or
// `(min-width:40rem)` and `(min-width: 40rem)`, compare equal. A space
// before a colon stays: `.a :hover` is not `.a:hover`.
func cssNormal(s string) string {
	s = cssSquash(s)
	var b strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == ' ':
			prev, next := byte(0), byte(0)
			if b.Len() > 0 {
				prev = b.String()[b.Len()-1]
			}
			if i+1 < len(s) {
				next = s[i+1]
			}
			if strings.IndexByte(">+~,(:", prev) >= 0 || strings.IndexByte(">+~,()", next) >= 0 {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// cssConflict is one conflict the guard finds. key names it by context,
// selector, and each side's file and property, without lines or values, so
// an allowed one stays allowed while the lines around it move.
type cssConflict struct{ key, msg string }

// cssConflicts lists each selector that two rules, in one @media, @container
// and @supports context, give two values for one property. One component
// has one rule (DESIGN 11), so a second rule that restyles it is a mistake.
// Two values in one rule are a fallback and allowed. Shorthands count as
// the longhands they set, so `padding` and a later `padding-inline` meet.
func cssConflicts(rules []cssRule) []cssConflict {
	type set struct {
		value, name, decl, file, at string
	}
	seen := map[string]set{}
	var out []cssConflict
	for _, r := range rules {
		at := fmt.Sprintf("%s:%d", r.file, r.line)
		context := cssNormal(r.context)
		for _, sel := range r.selectors {
			nsel := cssNormal(sel)
			for _, d := range r.decls {
				decl := d.Name + ": " + d.Value
				reported := map[string]bool{}
				for _, l := range cssLonghands(d) {
					key := context + " | " + nsel + " | " + l.Name
					prev, ok := seen[key]
					seen[key] = set{l.Value, d.Name, decl, r.file, at}
					if !ok || prev.value == l.Value || prev.at == at || reported[prev.decl] {
						continue
					}
					reported[prev.decl] = true
					where := sel
					if r.context != "" {
						where += " inside " + r.context
					}
					out = append(out, cssConflict{
						key: strings.TrimSpace(context+" "+nsel) + " | " + prev.file + " " + prev.name + " | " + r.file + " " + d.Name,
						msg: fmt.Sprintf("%s: %s sets %q, and %s sets %q", where, prev.at, prev.decl, at, decl),
					})
				}
			}
		}
	}
	return out
}

// tokenFamilies are the properties each group of size tokens is for.
var tokenFamilies = []struct {
	prefix string
	props  *regexp.Regexp
}{
	{"--space-", regexp.MustCompile(`^(?:margin|padding)(?:-|$)|^(?:row-|column-)?gap$`)},
	{"--text-", regexp.MustCompile(`^font-size$`)},
	{"--radius", regexp.MustCompile(`radius$`)},
}

var cssLength = regexp.MustCompile(`(?:^|[\s(,*/+-])(\d*\.?\d+)(rem|px)\b`)

// cssPixels gives a length in CSS pixels at the default 16 px text size.
func cssPixels(number, unit string) float64 {
	n, _ := strconv.ParseFloat(number, 64)
	if unit == "rem" {
		return n * 16
	}
	return n
}

// rawTokenValues lists each raw spacing, font size or radius whose length
// a token already names, in the same unit or as its pixel equivalent, unless
// allow lists it by "file selector property".
func rawTokenValues(rules []cssRule, tokens []cssProperty, allow map[string]string) []string {
	named := map[string]map[float64]string{}
	for _, f := range tokenFamilies {
		named[f.prefix] = map[float64]string{}
		for _, tok := range tokens {
			if m := cssLength.FindStringSubmatch(" " + tok.Value); strings.HasPrefix(tok.Name, f.prefix) && m != nil && strings.TrimSpace(m[0]) == tok.Value {
				named[f.prefix][cssPixels(m[1], m[2])] = tok.Name
			}
		}
	}
	var out []string
	for _, r := range rules {
		for _, d := range r.decls {
			for _, f := range tokenFamilies {
				if strings.HasPrefix(d.Name, "--") || !f.props.MatchString(d.Name) {
					continue
				}
				for _, m := range cssLength.FindAllStringSubmatch(d.Value, -1) {
					tok, ok := named[f.prefix][cssPixels(m[1], m[2])]
					if !ok {
						continue
					}
					for _, sel := range r.selectors {
						if _, ok := allow[r.file+" "+sel+" "+d.Name]; !ok {
							out = append(out, fmt.Sprintf("%s:%d: %s { %s: %s } has %s%s where %s exists", r.file, r.line, sel, d.Name, d.Value, m[1], m[2], tok))
						}
					}
				}
			}
		}
	}
	return out
}

// rawValueExceptions are the deliberate raw values, by part, selector and
// property, each with its reason.
var rawValueExceptions = map[string]string{
	"plates.css main > .panel.danger-zone padding-block-start": "room for the hazard stripe, which is 8 px at any text size",
}

// layerFaults lists each part that is not one block in its layer, and the
// first part if it does not open by declaring the layer order: the distinct
// layers of parts, in the order they first appear. A part whose layer the
// first does not declare fails, since the first's line no longer matches.
func layerFaults(parts []stylesheetPart, read func(file string) (string, error)) []string {
	var layers []string
	for _, p := range parts {
		if !slices.Contains(layers, p.Layer) {
			layers = append(layers, p.Layer)
		}
	}
	order := "@layer " + strings.Join(layers, ", ") + ";"
	var out []string
	for i, p := range parts {
		body, err := read(p.File)
		if err != nil {
			out = append(out, err.Error())
			continue
		}
		// Each part is one block in its layer; the first also names the order.
		want := "@layer " + p.Layer + " {"
		if i == 0 {
			want = order + "\n\n" + want
		}
		if css := strings.TrimSpace(blankComments(body)); !strings.HasPrefix(css, want) || !strings.HasSuffix(css, "}") || cssBlockEnd(css, strings.Index(css, want)+len(want)-1) != len(css)-1 {
			if i == 0 {
				out = append(out, fmt.Sprintf("%s does not open with %q and then one block in @layer %s", p.File, order, p.Layer))
			} else {
				out = append(out, fmt.Sprintf("%s is not one block in @layer %s", p.File, p.Layer))
			}
		}
	}
	return out
}

func TestStylesheetPartsAreListed(t *testing.T) {
	var onDisk []string
	err := fs.WalkDir(staticFS, "static/css", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			onDisk = append(onDisk, strings.TrimPrefix(p, "static/css/"))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, p := range stylesheetParts {
		listed = append(listed, p.File)
	}
	for _, f := range layerFaults(stylesheetParts, func(file string) (string, error) {
		body, err := staticFS.ReadFile("static/css/" + file)
		return string(body), err
	}) {
		t.Error(f)
	}
	slices.Sort(onDisk)
	slices.Sort(listed)
	if !slices.Equal(onDisk, listed) {
		t.Errorf("static/css holds %v, but stylesheetParts lists %v", onDisk, listed)
	}
}

// Pages load one foreman.css joined from the parts, and no part on its own.
func TestStylesheetServedJoined(t *testing.T) {
	r := newTestRenderer(t)
	css, err := stylesheet()
	if err != nil {
		t.Fatal(err)
	}
	if got := r.static[stylesheetName]; string(got.body) != string(css) || got.contentType != staticTypes[".css"] {
		t.Errorf("%s is not the joined stylesheet (%d bytes, want %d)", stylesheetName, len(got.body), len(css))
	}
	if !strings.HasPrefix(string(css), "/* css/tokens.css */") {
		t.Errorf("the joined stylesheet does not open with the tokens: %.60q", css)
	}
	for name := range r.static {
		if strings.HasPrefix(name, "css/") {
			t.Errorf("%s is served on its own", name)
		}
	}
}

// shorthandConflictsToFixInWave1 is TEMPORARY. These are the conflicts the
// guard found in the stylesheet when it began expanding shorthands (#983):
// a shorthand in one rule and a longhand of it in a later rule for the same
// selector. They are to be fixed in wave 1 of fixes batch 20, which owns the
// CSS files, by folding each later rule into the first; remove each entry
// with its fix, and this list once it is empty. Nothing may be added to it.
var shorthandConflictsToFixInWave1 = map[string]bool{
	".field select | forms.css padding-inline | forms.css padding-inline-end":                                         true,
	".field select | forms.css background | forms.css background-image":                                               true,
	".field select | forms.css background | forms.css background-repeat":                                              true,
	".field select | forms.css background | forms.css background-position":                                            true,
	".field select | forms.css background | forms.css background-size":                                                true,
	"form.general-grid.setting-rows>.required-note | pages/settings.css margin | pages/settings.css margin-block-end": true,
	"form.general-grid.setting-rows>p:first-child | pages/settings.css margin | pages/settings.css margin-block-end":  true,
	".plan-tree__deps | pages/mods.css margin | pages/mods.css margin-block-start":                                    true,
	".plan-tree__deps | pages/mods.css margin | pages/mods.css margin-inline-start":                                   true,
	".plan-tree__deps | pages/mods.css padding | pages/mods.css padding-inline-start":                                 true,
}

func TestStylesheetHasNoConflictingRules(t *testing.T) {
	found := map[string]bool{}
	for _, c := range cssConflicts(stylesheetRules(t)) {
		found[c.key] = true
		if !shorthandConflictsToFixInWave1[c.key] {
			t.Error(c.msg)
		}
	}
	for key := range shorthandConflictsToFixInWave1 {
		if !found[key] {
			t.Errorf("%q is fixed; remove it from shorthandConflictsToFixInWave1", key)
		}
	}
}

func TestStylesheetUsesSizeTokens(t *testing.T) {
	css, err := stylesheet()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rawTokenValues(stylesheetRules(t), rootProperties(string(css)), rawValueExceptions) {
		t.Error(c)
	}
}

// The guard catches what it is for: a second rule that restyles a
// component, and a raw value a token names.
func TestStylesheetGuardCatchesPlantedMistakes(t *testing.T) {
	planted := `@layer components {
.button { min-block-size: 2.75rem; color: red; }
@media (min-width: 40rem) { .button { min-block-size: 3rem; } }
.field, .button { color: red; }
.button:hover { color: blue; }
.button {
  min-block-size: 3rem;
}
.fallback { block-size: 100vh; block-size: 100dvh; }
.hint { margin-block: 0.5rem 2px; font-size: 13px; border-radius: 0.2em; }
}`
	rules := parseCSS("planted.css", planted)
	got := cssConflicts(rules)
	if len(got) != 1 || !strings.Contains(got[0].msg, `planted.css:2 sets "min-block-size: 2.75rem"`) || !strings.Contains(got[0].msg, "planted.css:6") {
		t.Errorf("want one conflict, .button's min-block-size at lines 2 and 6; got %v", got)
	}
	tokens := []cssProperty{{"--space-2", "0.5rem"}, {"--text-xs", "0.8125rem"}, {"--radius", "3px"}}
	raw := rawTokenValues(rules, tokens, nil)
	if len(raw) != 2 || !strings.Contains(raw[0], "0.5rem where --space-2") || !strings.Contains(raw[1], "13px where --text-xs") {
		t.Errorf("want the 0.5rem margin and the 13 px font size; got %q", raw)
	}
	if raw := rawTokenValues(rules, tokens, map[string]string{"planted.css .hint margin-block": "a reason", "planted.css .hint font-size": "a reason"}); len(raw) != 0 {
		t.Errorf("allowed values are still reported: %q", raw)
	}
}

// A shorthand meets its longhands: padding and a later padding-inline, or
// background and a later background-color, conflict, unless the values a
// longhand gets agree. Spacing in a selector or condition does not hide a
// second rule.
func TestStylesheetGuardCatchesShorthandAndSpacing(t *testing.T) {
	planted := `@layer components {
.x { padding: 0; background: red; gap: 1rem; }
.x { padding-inline: 1rem; }
.x { background-color: blue; }
.x { row-gap: 1rem; }
.y { padding: 0 1rem; margin: 0; margin-block-end: 1rem; }
.y { padding-inline: 1rem; }
.a > .b { color: red; }
.a>.b { color: blue; }
@media (min-width:40rem) { .c { color: red; } }
@media (min-width: 40rem) { .c { color: blue; } }
.d :hover { color: red; }
.d:hover { color: blue; }
}`
	var got []string
	for _, c := range cssConflicts(parseCSS("planted.css", planted)) {
		got = append(got, c.msg)
	}
	want := []string{
		`.x: planted.css:2 sets "padding: 0", and planted.css:3 sets "padding-inline: 1rem"`,
		`.x: planted.css:2 sets "background: red", and planted.css:4 sets "background-color: blue"`,
		`.a>.b: planted.css:8 sets "color: red", and planted.css:9 sets "color: blue"`,
		`.c inside @media (min-width: 40rem): planted.css:10 sets "color: red", and planted.css:11 sets "color: blue"`,
	}
	if !slices.Equal(got, want) {
		t.Errorf("got conflicts\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for d, want := range map[cssProperty][]cssProperty{
		{"margin", "1px 2px 3px"}: {{"margin-block-start", "1px"}, {"margin-inline-end", "2px"}, {"margin-block-end", "3px"}, {"margin-inline-start", "2px"}},
		{"gap", "1rem"}:           {{"row-gap", "1rem"}, {"column-gap", "1rem"}},
		{"overflow", "auto clip"}: {{"overflow-x", "auto"}, {"overflow-y", "clip"}},
		{"color", "red"}:          {{"color", "red"}},
	} {
		if got := cssLonghands(d); !slices.Equal(got, want) {
			t.Errorf("%s: %s gives %v, want %v", d.Name, d.Value, got, want)
		}
	}
}

// The layer order is the distinct layers of the parts, so a part in a layer
// the first part does not declare fails.
func TestStylesheetGuardCatchesUndeclaredLayer(t *testing.T) {
	files := map[string]string{
		"a.css": "@layer tokens, base;\n\n@layer tokens {\n:root { --x: 1; }\n}\n",
		"b.css": "/* b */\n@layer base { p { margin: 0; } }\n",
		"c.css": "@layer extra { p { color: red; } }\n",
	}
	read := func(f string) (string, error) { return files[f], nil }
	if got := layerFaults([]stylesheetPart{{"a.css", "tokens"}, {"b.css", "base"}}, read); len(got) != 0 {
		t.Errorf("declared layers fail: %q", got)
	}
	got := layerFaults([]stylesheetPart{{"a.css", "tokens"}, {"b.css", "base"}, {"c.css", "extra"}}, read)
	if len(got) != 1 || !strings.Contains(got[0], `a.css does not open with "@layer tokens, base, extra;"`) {
		t.Errorf("want a.css to fail for not declaring extra; got %q", got)
	}
	if got := layerFaults([]stylesheetPart{{"a.css", "tokens"}, {"c.css", "base"}}, read); len(got) != 1 || got[0] != "c.css is not one block in @layer base" {
		t.Errorf("want c.css in the wrong layer to fail; got %q", got)
	}
}
