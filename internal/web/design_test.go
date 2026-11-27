package web

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	// Modifiers built from a name are compared by their prefix. The one hook
	// only this page needs is status-card--<state>: the Server page tints its
	// plate from its heading's badge (pages/server.css), a specimen from this.
	pages := ""
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
		pages += string(b)
	}
	code, _ := filepath.Glob("*.go")
	for _, f := range code {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f) //nolint:gosec // this package's own source
		if err != nil {
			t.Fatal(err)
		}
		pages += string(b)
	}
	seen := map[string]bool{}
	for _, m := range classAttr.FindAllStringSubmatch(body, -1) {
		for _, c := range strings.Fields(m[1]) {
			if seen[c] || strings.HasPrefix(c, "design-") {
				continue
			}
			seen[c] = true
			if strings.Contains(pages, c) {
				continue
			}
			if i := strings.Index(c, "--"); i > 0 && (strings.Contains(pages, c[:i+2]+"{{") || strings.Contains(pages, `"`+c[:i+2]+`"`)) {
				continue
			}
			if strings.HasPrefix(c, "status-card--") {
				continue
			}
			t.Errorf("class %q is on the component page but on no real page", c)
		}
	}
}

var classAttr = regexp.MustCompile(`class="([^"]*)"`)
