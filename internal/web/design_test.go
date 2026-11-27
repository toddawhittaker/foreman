package web

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The component page needs a signed-in user, like every other page.
func TestDesignPageNeedsSignIn(t *testing.T) {
	hn := newHarness(t, nil)
	hn.addUser("alice", testPassword)
	rec := hn.get(PathDesign)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), PathSignIn) {
		t.Fatalf("signed out: got %d to %q, want a redirect to sign in", rec.Code, rec.Header().Get("Location"))
	}
}

// Every group is drawn in both themes, and every token and icon the
// stylesheet defines is on the page (DESIGN 3, 4, 7.9).
func TestDesignPageShowsEveryGroupTokenAndIcon(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	rec := hn.get(PathDesign, withCookie(c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, g := range designGroups {
		if !strings.Contains(body, `id="design-`+g.Key+`"`) {
			t.Errorf("no section for %s", g.Key)
		}
	}
	if n := strings.Count(body, `class="design-pane" data-theme="light"`); n != len(designGroups) {
		t.Errorf("%d light panes, want %d", n, len(designGroups))
	}
	if n := strings.Count(body, `class="design-pane" data-theme="dark"`); n != len(designGroups) {
		t.Errorf("%d dark panes, want %d", n, len(designGroups))
	}
	p, err := newDesignPage()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Icons) < 30 || len(p.TokensWith("--color-")) < 30 {
		t.Fatalf("read %d icons and %d colours from the stylesheet; the parser no longer fits it", len(p.Icons), len(p.TokensWith("--color-")))
	}
	for _, name := range p.Icons {
		if !strings.Contains(body, "var(--i-"+name+")") {
			t.Errorf("icon %s is not shown", name)
		}
	}
	for _, tok := range p.Tokens {
		if !strings.Contains(body, "<code>"+tok.Name+"</code>") {
			t.Errorf("token %s is not shown", tok.Name)
		}
	}
	for _, prefix := range []string{"--color-", "--text-", "--space-", "--radius", "--shadow-"} {
		for _, tok := range p.TokensWith(prefix) {
			if !strings.Contains(body, "var("+tok.Name+")") {
				t.Errorf("token %s is not drawn", tok.Name)
			}
		}
	}
	for _, st := range designStates {
		if !strings.Contains(body, "status-card--"+string(st)) || !strings.Contains(body, "state--"+string(st)) {
			t.Errorf("state %s has no badge or status card", st)
		}
	}
}

// testDesignPage is the component page, for the render cases of every page.
func testDesignPage() DesignPage {
	p, err := newDesignPage()
	if err != nil {
		panic(err)
	}
	return p
}

// The components are drawn as the app's pages draw them (docs/EPIC-fixes-20.md,
// ruling 2): the shared partials render, the Server page's structure is
// there so the page's rules for it apply, and no class is a copy that no real
// page uses.
func TestDesignPageDrawsTheComponentsAsThePagesDo(t *testing.T) {
	hn := newHarness(t, nil)
	body := hn.get(PathDesign, withCookie(hn.signIn())).Body.String()
	for _, want := range []string{
		`<header class="site">`, `class="menu__user"`,
		`class="subnav subnav--worlds"`, `class="subnav subnav--settings"`, `aria-label="Server pages"`, `aria-label="World pages"`,
		`class="flow-steps"`, `class="segmented"`, `class="tablist"`, `class="pager result-pager"`,
		`class="server-grid"`, `class="links card shortcuts"`, `class="panel card recent-snaps"`, `class="section-head"`,
		`sse-swap="control"`, `sse-swap="facts"`, `class="plate__band"`, `class="mod-status"`,
	} {
		// Once in each theme's pane.
		if n := strings.Count(body, want); n < 2 {
			t.Errorf("%s appears %d times, want it in both themes", want, n)
		}
	}
	// A class no real template or handler uses is a stale copy of a component.
	// The hooks only this page needs are status-card--<state>: the Server
	// page tints its plate from its heading's badge (pages/server.css), a
	// specimen from this.
	known := realClasses(t)
	for _, c := range known.missing(body) {
		// working--idle is the state a page's script gives the working bar to
		// hide it and keep its room; no page adopts the bar yet.
		if strings.HasPrefix(c, "status-card--") || c == "working--idle" {
			continue
		}
		t.Errorf("class %q is on the component page but on no real page", c)
	}
}

// A class counts only as a whole name: "tab" is not made real by "table", and
// a made-up class is reported. Only a name built with an action, such as
// state--{{.State}}, is matched by its prefix.
func TestDesignClassCheckComparesWholeNames(t *testing.T) {
	known := realClasses(t)
	got := known.missing(`<p class="tabl zzz-made-up button button--primary state--running">`)
	want := []string{"tabl", "zzz-made-up"}
	if !slices.Equal(got, want) {
		t.Errorf("reported %q, want %q", got, want)
	}
	for _, name := range []string{"tab", "mod-list", "button"} {
		if _, ok := known.exact[name]; !ok {
			t.Errorf("%q should be a real class", name)
		}
	}
	// A stale name that only starts like a real one is still stale.
	if got := known.missing(`<p class="tab-old button--primaryx">`); len(got) != 2 {
		t.Errorf("reported %q, want both names", got)
	}
}

// classSet is the class names real templates and handlers write: whole names,
// and the prefixes of names built with an action (state--{{.State}}) or by
// joining strings in Go.
type classSet struct {
	exact    map[string]struct{}
	prefixes []string
}

var (
	// An attribute's value may hold actions, which hold quotes of their own.
	classAttr = regexp.MustCompile(`class="((?:[^"{]|\{\{.*?\}\})*)"`)
	// Actions that only choose text, so what they wrap is whole names.
	controlAction = regexp.MustCompile(`\{\{-?\s*(?:/\*.*?\*/|if\b.*?|else\b.*?|end|range\b.*?|with\b.*?)\s*-?\}\}`)
	// A choosing action with text right after it.
	controlGlued = regexp.MustCompile(`\{\{-?\s*(?:if\b.*?|else\b.*?)\s*-?\}\}([^\s{])`)
	// Any other action prints something, so a name it touches is built.
	printAction = regexp.MustCompile(`\{\{.*?\}\}`)
	goString    = regexp.MustCompile("`[^`]*`|\"(?:[^\"\\\\\n]|\\\\.)*\"")
)

// add records the names in one class attribute's value.
func (c *classSet) add(value string) {
	// An action that chooses text and has more text right after it builds a
	// name (toast--{{if .Error}}error{{end}}); one followed by a space only
	// switches a whole name on.
	value = controlGlued.ReplaceAllString(value, "\x00${1}")
	value = controlAction.ReplaceAllString(value, " ")
	value = printAction.ReplaceAllString(value, "\x00")
	for _, f := range strings.Fields(value) {
		if before, _, built := strings.Cut(f, "\x00"); built {
			if before != "" {
				c.prefixes = append(c.prefixes, before)
			}
			continue
		}
		c.exact[f] = struct{}{}
	}
}

// realClasses reads every class the other templates and the handlers write.
func realClasses(t *testing.T) classSet {
	t.Helper()
	c := classSet{exact: map[string]struct{}{}}
	files, err := templateFS.ReadDir("templates")
	if err != nil || len(files) == 0 {
		t.Fatalf("read the templates: %v (%d files)", err, len(files))
	}
	for _, f := range files {
		if f.Name() == "design.html" {
			continue
		}
		b, err := templateFS.ReadFile("templates/" + f.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range classAttr.FindAllStringSubmatch(string(b), -1) {
			c.add(m[1])
		}
	}
	// A handler can name a class too, whole or as a prefix it joins a name to.
	code, _ := filepath.Glob("*.go")
	for _, f := range code {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f) //nolint:gosec // this package's own source
		if err != nil {
			t.Fatal(err)
		}
		for _, lit := range goString.FindAllString(string(b), -1) {
			lit = strings.ReplaceAll(strings.Trim(lit, "`\""), `\"`, `"`)
			for _, m := range classAttr.FindAllStringSubmatch(lit, -1) {
				c.add(m[1])
			}
			for _, w := range strings.Fields(lit) {
				c.exact[w] = struct{}{}
				if strings.HasSuffix(w, "-") {
					c.prefixes = append(c.prefixes, w)
				}
			}
		}
	}
	return c
}

// missing lists the classes in body that no real page writes, each once, in
// the order they first appear. A name of the component page's own (design-) is
// not a component.
func (c classSet) missing(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range classAttr.FindAllStringSubmatch(body, -1) {
		for _, name := range strings.Fields(m[1]) {
			if seen[name] || strings.HasPrefix(name, "design-") {
				continue
			}
			seen[name] = true
			if _, ok := c.exact[name]; ok {
				continue
			}
			if slices.ContainsFunc(c.prefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
				continue
			}
			out = append(out, name)
		}
	}
	return out
}
