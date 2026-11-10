package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"html/template"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"text/template/parse"
	"unicode"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// The checks of SPEC 22.5 that read the templates, scripts, stylesheet and
// Go code; the catalogue's own checks are in internal/i18n.

// allowedWords may appear outside the catalogue: names that are never
// translated.
var allowedWords = []string{"Foreman", "Factorio", "Space Age", "RCON"}

var reEntity = regexp.MustCompile(`&(#[0-9]+|#x[0-9a-fA-F]+|[a-zA-Z]+);`)

// hasWords reports whether s holds letters other than the allowed words.
func hasWords(s string) bool {
	s = reEntity.ReplaceAllString(s, " ")
	for _, w := range allowedWords {
		s = strings.ReplaceAll(s, w, " ")
	}
	return strings.IndexFunc(s, unicode.IsLetter) >= 0
}

// action stands for anything a template computes, in flattened markup.
const action = "\x00"

// flatten writes a template's markup with each action replaced by a
// marker, both branches of an if or range in turn.
func flatten(n parse.Node, b *strings.Builder) {
	switch n := n.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			flatten(c, b)
		}
	case *parse.TextNode:
		b.Write(n.Text)
	case *parse.IfNode:
		flattenBranch(&n.BranchNode, b)
	case *parse.RangeNode:
		flattenBranch(&n.BranchNode, b)
	case *parse.WithNode:
		flattenBranch(&n.BranchNode, b)
	case *parse.CommentNode:
	default:
		b.WriteString(action)
	}
}

func flattenBranch(n *parse.BranchNode, b *strings.Builder) {
	b.WriteString(action)
	flatten(n.List, b)
	if n.ElseList != nil {
		flatten(n.ElseList, b)
	}
}

// checkedAttrs are the attributes whose values a person reads or hears.
var checkedAttrs = map[string]bool{"aria-label": true, "title": true, "placeholder": true, "alt": true}

// hardCodedText returns the text in markup s that a person would see or
// hear: text between tags, and the values of checkedAttrs. Script and style
// bodies, comments and code (file names, commands) are skipped.
func hardCodedText(s string) []string {
	var out []string
	var text strings.Builder
	flush := func() {
		if t := strings.TrimSpace(text.String()); t != "" && hasWords(t) {
			out = append(out, t)
		}
		text.Reset()
	}
	for i := 0; i < len(s); {
		if s[i] != '<' {
			text.WriteByte(s[i])
			i++
			continue
		}
		flush()
		switch {
		case strings.HasPrefix(s[i:], "<!--"):
			end := strings.Index(s[i:], "-->")
			if end < 0 {
				return out
			}
			i += end + 3
			continue
		case strings.HasPrefix(s[i:], "<!"):
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				return out
			}
			i += end + 1
			continue
		}
		var name string
		i, name = scanTag(s, i+1, &out)
		if name == "script" || name == "style" || name == "code" {
			end := strings.Index(s[i:], "</"+name)
			if end < 0 {
				return out
			}
			i += end
		}
	}
	flush()
	return out
}

// scanTag reads one tag from s[i:], just after its "<", adding the checked
// attribute values that hold words to out. It returns the index after the
// tag and the tag's name.
func scanTag(s string, i int, out *[]string) (int, string) {
	isName := func(c byte) bool {
		return c != ' ' && c != '\t' && c != '\n' && c != '>' && c != '=' && c != '/' && c != '"' && c != '\''
	}
	start := i
	for i < len(s) && isName(s[i]) {
		i++
	}
	tag := strings.ToLower(s[start:i])
	for i < len(s) && s[i] != '>' {
		if !isName(s[i]) {
			i++
			continue
		}
		start := i
		for i < len(s) && isName(s[i]) {
			i++
		}
		attr := strings.ToLower(strings.Trim(s[start:i], action))
		if i >= len(s) || s[i] != '=' {
			continue
		}
		i++
		var val string
		if i < len(s) && (s[i] == '"' || s[i] == '\'') {
			q := s[i]
			end := strings.IndexByte(s[i+1:], q)
			if end < 0 {
				return len(s), tag
			}
			val = s[i+1 : i+1+end]
			i += end + 2
		} else {
			start := i
			for i < len(s) && isName(s[i]) {
				i++
			}
			val = s[start:i]
		}
		if checkedAttrs[attr] && hasWords(val) {
			*out = append(*out, attr+"="+val)
		}
	}
	return i + 1, tag
}

// parseTemplateFile parses one template file on its own, for its trees.
func parseTemplateFile(t *testing.T, name string) map[string]*parse.Tree {
	t.Helper()
	src, err := templateFS.ReadFile("templates/" + name)
	if err != nil {
		t.Fatal(err)
	}
	r := &Renderer{static: map[string]staticFile{}}
	funcs := template.FuncMap{}
	for k, f := range r.sharedFuncs() {
		funcs[k] = f
	}
	for k, f := range localeFuncs(i18n.English) {
		funcs[k] = f
	}
	tmpl, err := template.New(name).Funcs(funcs).Parse(string(src))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	trees := map[string]*parse.Tree{}
	for _, d := range tmpl.Templates() {
		if d.Tree != nil {
			trees[d.Name()] = d.Tree
		}
	}
	return trees
}

func templateFiles(t *testing.T) []string {
	t.Helper()
	files, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range files {
		files[i] = path.Base(f)
	}
	return files
}

// Templates hold no text of their own outside the catalogue (SPEC 22.5).
func TestTemplatesHaveNoHardCodedText(t *testing.T) {
	for _, f := range templateFiles(t) {
		for name, tree := range parseTemplateFile(t, f) {
			var b strings.Builder
			flatten(tree.Root, &b)
			for _, s := range hardCodedText(b.String()) {
				t.Errorf("%s (%s): text not from the catalogue: %q", f, name, strings.ReplaceAll(s, action, "{{…}}"))
			}
		}
	}
}

// templateKeys collects the keys passed to t, tHTML and link in every template.
func templateKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	var visit func(n parse.Node)
	visit = func(n parse.Node) {
		switch n := n.(type) {
		case *parse.ListNode:
			if n == nil {
				return
			}
			for _, c := range n.Nodes {
				visit(c)
			}
		case *parse.ActionNode:
			visit(n.Pipe)
		case *parse.IfNode:
			visit(n.Pipe)
			visit(n.List)
			visit(n.ElseList)
		case *parse.RangeNode:
			visit(n.Pipe)
			visit(n.List)
			visit(n.ElseList)
		case *parse.WithNode:
			visit(n.Pipe)
			visit(n.List)
			visit(n.ElseList)
		case *parse.TemplateNode:
			visit(n.Pipe)
		case *parse.PipeNode:
			if n == nil {
				return
			}
			for _, c := range n.Cmds {
				visit(c)
			}
		case *parse.CommandNode:
			if len(n.Args) > 1 {
				if id, ok := n.Args[0].(*parse.IdentifierNode); ok {
					switch id.Ident {
					case "t", "tHTML":
						addKey(t, keys, n.Args[1])
					case "link", "extLink":
						if len(n.Args) > 2 {
							addKey(t, keys, n.Args[2])
						}
					}
				}
			}
			for _, a := range n.Args {
				visit(a)
			}
		}
	}
	for _, f := range templateFiles(t) {
		for _, tree := range parseTemplateFile(t, f) {
			visit(tree.Root)
		}
	}
	return keys
}

func addKey(t *testing.T, keys map[string]bool, n parse.Node) {
	t.Helper()
	s, ok := n.(*parse.StringNode)
	if !ok {
		t.Errorf("%s: a catalogue key must be a string literal", n)
		return
	}
	keys[s.Text] = true
}

// goKeys collects the literal keys of M, T, HTML and Link calls in the
// non-test Go files of internal/web, internal/i18n and internal/territory.
func goKeys(t *testing.T) map[string]bool {
	t.Helper()
	keys := map[string]bool{}
	for _, dir := range []string{".", "../i18n", "../territory"} {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				var name string
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					name = fn.Sel.Name
				}
				var at []int
				switch name {
				case "M", "T", "HTML":
					at = []int{0}
				case "Link", "ExternalLink":
					at = []int{1}
				}
				for _, i := range at {
					if i >= len(call.Args) {
						continue
					}
					lit, ok := call.Args[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					if k, err := strconv.Unquote(lit.Value); err == nil {
						keys[k] = true
					}
				}
				return true
			})
		}
	}
	return keys
}

// Every key the code uses is in the catalogue, and every key in the
// catalogue is used (SPEC 22.5). The scripts' keys come from jsKeys.
func TestCatalogueKeysUsed(t *testing.T) {
	used := templateKeys(t)
	for k := range goKeys(t) {
		used[k] = true
	}
	for _, k := range jsKeys {
		used[k] = true
	}
	en := map[string]bool{}
	for _, k := range i18n.EnglishKeys() {
		en[k] = true
	}
	for k := range used {
		if !en[k] {
			t.Errorf("key %s is used but not in the English catalogue", k)
		}
	}
	for k := range en {
		if !used[k] {
			t.Errorf("key %s is in the catalogue but nothing uses it", k)
		}
	}
}

// jsWordsAllowed are script strings that look like words but are not
// shown: the unit names Intl.RelativeTimeFormat takes, and the strict-mode
// directive.
var jsWordsAllowed = map[string]bool{
	"second": true, "minute": true, "hour": true, "day": true, "week": true, "month": true, "year": true,
	"use strict": true,
	// Key names for KeyboardEvent.key, not text.
	"Escape": true, "Enter": true,
}

var (
	reCapitalWord = regexp.MustCompile(`(^|[^A-Za-z])[A-Z][a-z]`)
	rePlainWord   = regexp.MustCompile(`^[A-Za-z]+[,.:;!?]?$`)
	reTemplateSub = regexp.MustCompile(`\$\{[^}]*\}`)
)

// jsStringHasWords reports whether a script string reads as text for a
// person: it has a capitalised word, or two plain words with a space
// between. Class lists, selectors and event names do not.
func jsStringHasWords(s string) bool {
	s = reTemplateSub.ReplaceAllString(s, " ")
	if jsWordsAllowed[s] {
		return false
	}
	for _, w := range allowedWords {
		s = strings.ReplaceAll(s, w, " ")
	}
	words := 0
	for _, f := range strings.Fields(s) {
		if reCapitalWord.MatchString(f) && rePlainWord.MatchString(f) {
			return true
		}
		if rePlainWord.MatchString(f) {
			words++
		}
	}
	if words >= 2 {
		return true
	}
	// A non-Latin letter is always text.
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsLetter(r) && r > unicode.MaxLatin1 }) >= 0
}

// jsStrings returns the string literals in script src, skipping comments.
// It does not understand regular expression literals; Foreman's scripts
// have no quotes inside those.
func jsStrings(src string) []string {
	var out []string
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case strings.HasPrefix(src[i:], "//"):
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				return out
			}
			i += end
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i:], "*/")
			if end < 0 {
				return out
			}
			i += end + 1
		case c == '"' || c == '\'' || c == '`':
			var b strings.Builder
			j := i + 1
			for ; j < len(src) && src[j] != c; j++ {
				if src[j] == '\\' && j+1 < len(src) {
					j++
				}
				b.WriteByte(src[j])
			}
			out = append(out, b.String())
			i = j
		}
	}
	return out
}

// Scripts get their text from the #i18n block, never from literals.
func TestScriptsHaveNoHardCodedText(t *testing.T) {
	files, err := fs.Glob(staticFS, "static/*.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		src, _ := staticFS.ReadFile(f)
		for _, s := range jsStrings(string(src)) {
			if jsStringHasWords(s) {
				t.Errorf("%s: text not from the catalogue: %q", f, s)
			}
		}
	}
}

var (
	reCSSContent = regexp.MustCompile(`(?m)\bcontent:\s*([^;}]*)`)
	reCSSString  = regexp.MustCompile(`"([^"]*)"|'([^']*)'`)
	reCSSAttr    = regexp.MustCompile(`attr\([^)]*\)`)
)

// A stylesheet's content values hold no words: "", symbols or attr(...),
// so a label comes from the page in its own language.
func TestStylesheetHasNoHardCodedText(t *testing.T) {
	files, err := fs.Glob(staticFS, "static/*.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		src, _ := staticFS.ReadFile(f)
		for _, m := range reCSSContent.FindAllStringSubmatch(string(src), -1) {
			if cssContentHasWords(m[1]) {
				t.Errorf("%s: content with words: %q", f, m[1])
			}
		}
	}
}

func cssContentHasWords(v string) bool {
	v = reCSSAttr.ReplaceAllString(v, "")
	for _, s := range reCSSString.FindAllStringSubmatch(v, -1) {
		if strings.IndexFunc(s[1]+s[2], unicode.IsLetter) >= 0 {
			return true
		}
	}
	return false
}

// The checks themselves catch what they are for.
func TestTextChecksCatchText(t *testing.T) {
	got := hardCodedText(`<p>Hello</p><p class="x">` + action + `</p><span aria-label="Close">` + action + `</span>` +
		`<td title="` + action + `">&nbsp;Foreman &ndash; Factorio <code>worlds/a.zip</code></td><script>var a = "Word here";</script><!-- a comment -->`)
	if !slices.Equal(got, []string{"Hello", "aria-label=Close"}) {
		t.Errorf("template check found %q", got)
	}
	var b strings.Builder
	flatten(parseTemplateFile(t, "signin.html")["title"].Root, &b)
	if b.String() != action {
		t.Errorf("flattened title %q", b.String())
	}
	words := []string{"Live updates stopped.", "ending now", "Note:", " seconds left", "Same as the save (", "Готово"}
	none := []string{"", "message message--note", "main h1", "htmx:sseMessage", "DOMContentLoaded", "second", "[data-until]", "text/html", "1.", " (", "aria-describedby", "${n}"}
	for _, s := range words {
		if !jsStringHasWords(s) {
			t.Errorf("script check missed %q", s)
		}
	}
	for _, s := range none {
		if jsStringHasWords(s) {
			t.Errorf("script check flagged %q", s)
		}
	}
	if got := jsStrings("a('x', \"y\\\"z\", `w ${v}`) // 'no'\n/* \"no\" */ b('k')"); !slices.Equal(got, []string{"x", `y"z`, "w ${v}", "k"}) {
		t.Errorf("jsStrings %q", got)
	}
	for v, want := range map[string]bool{`""`: false, `"> " / ""`: false, `attr(data-label) ": "`: false, `"Taken: "`: true, `"→"`: false} {
		if got := cssContentHasWords(v); got != want {
			t.Errorf("css %s: %v, want %v", v, got, want)
		}
	}
}
