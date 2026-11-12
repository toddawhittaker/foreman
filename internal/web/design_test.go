package web

import (
	"net/http"
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
		t.Fatalf("read %d icons and %d colours from foreman.css; the parser no longer fits it", len(p.Icons), len(p.TokensWith("--color-")))
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
