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

// cssConflicts lists each selector that two rules, in one @media, @container
// and @supports context, give two values for one property. One component
// has one rule (DESIGN 11), so a second rule that restyles it is a mistake.
// Two values in one rule are a fallback and allowed.
func cssConflicts(rules []cssRule) []string {
	type set struct {
		value string
		at    string
	}
	seen := map[string]set{}
	var out []string
	for _, r := range rules {
		at := fmt.Sprintf("%s:%d", r.file, r.line)
		for _, sel := range r.selectors {
			for _, d := range r.decls {
				key := r.context + " | " + sel + " | " + d.Name
				prev, ok := seen[key]
				if ok && prev.value != d.Value && prev.at != at {
					where := sel
					if r.context != "" {
						where += " inside " + r.context
					}
					out = append(out, fmt.Sprintf("%s: %s sets %s to %q, and %s sets it to %q", where, prev.at, d.Name, prev.value, at, d.Value))
				}
				seen[key] = set{d.Value, at}
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
	for i, p := range stylesheetParts {
		listed = append(listed, p.File)
		body, err := staticFS.ReadFile("static/css/" + p.File)
		if err != nil {
			t.Fatal(err)
		}
		// Each part is one block in its layer; the first also names the order.
		want := "@layer " + p.Layer + " {"
		if i == 0 {
			want = "@layer tokens, base, components, pages;\n\n" + want
		}
		if css := strings.TrimSpace(blankComments(string(body))); !strings.HasPrefix(css, want) || !strings.HasSuffix(css, "}") || cssBlockEnd(css, strings.Index(css, want)+len(want)-1) != len(css)-1 {
			t.Errorf("%s is not one block in @layer %s", p.File, p.Layer)
		}
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

func TestStylesheetHasNoConflictingRules(t *testing.T) {
	for _, c := range cssConflicts(stylesheetRules(t)) {
		t.Error(c)
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
	if len(got) != 1 || !strings.Contains(got[0], "planted.css:2 sets min-block-size") || !strings.Contains(got[0], "planted.css:6") {
		t.Errorf("want one conflict, .button's min-block-size at lines 2 and 6; got %q", got)
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
