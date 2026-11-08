package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// A mod whose "+" dependency the portal lacks still gets an applicable
// plan; the one it has starts ticked and can be unticked (SPEC 9.2).
func TestRecommendedDependencyPlan(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("fp-lane", "1.3.1")
	g.portal.AddZip("fp-k2", "2.1.4", "base >= 2.0", "+ fp-lane >= 1.3.0", "+ fp-gone")
	id := g.world20("Base")
	path := planPath(id, url.Values{"kind": {"install"}, "name": {"fp-k2"}})
	code, body := g.getStatus(path)
	if code != http.StatusOK || !strings.Contains(body, `id="act-apply"`) {
		t.Fatalf("plan %d, not applicable:\n%s", code, body)
	}
	wantIn(t, "plan", plain(body), "fp-lane, recommended by fp-k2", "Mods marked as recommended start ticked.",
		"fp-gone, recommended by fp-k2, is left out")
	wantIn(t, "plan", body, `<input type="hidden" name="offered" value="fp-lane">`,
		`value="fp-lane" id="mod-fp-lane-optional" hx-get="/worlds/`+id+`/mods/plan" checked>`)

	// Unticked: the box sends nothing, but "offered" says it was shown.
	off := planPath(id, url.Values{"kind": {"install"}, "name": {"fp-k2"}, "offered": {"fp-lane"}})
	code, body = g.getStatus(off)
	if code != http.StatusOK || strings.Contains(body, `hx-get="/worlds/`+id+`/mods/plan" checked>`) {
		t.Fatalf("unticked plan %d:\n%s", code, body)
	}
	if strings.Contains(plain(body), "fp-lane 1.3.1") {
		t.Error("an unticked recommended mod is still installed")
	}
	wantIn(t, "unticked", body, `<input type="hidden" name="declined" value="fp-lane">`)
}
