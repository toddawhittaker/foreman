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
	wantIn(t, "plan", plain(body), "recommended by fp-k2</label>", "Mods marked as recommended start ticked.",
		"fp-gone, recommended by fp-k2, is left out")
	wantIn(t, "plan", body, `<input type="hidden" name="offered" value="fp-lane">`,
		`value="fp-lane" id="mod-fp-lane-optional" hx-post="/worlds/`+id+`/mods/plan/redraw" checked aria-describedby="mod-fp-lane-optional-detail">`)

	// Unticked: the box sends nothing, but "offered" says it was shown.
	off := planPath(id, url.Values{"kind": {"install"}, "name": {"fp-k2"}, "offered": {"fp-lane"}})
	code, body = g.getStatus(off)
	if code != http.StatusOK || strings.Contains(body, `hx-post="/worlds/`+id+`/mods/plan/redraw" checked`) {
		t.Fatalf("unticked plan %d:\n%s", code, body)
	}
	if strings.Contains(planRows(body), "fp-lane 1.3.1") {
		t.Error("an unticked recommended mod is still installed")
	}
	wantIn(t, "unticked", body, `<input type="hidden" name="offered" value="fp-lane">`)
}

// Two mods installed together, as Moshine and Muluna are: every mod
// they recommend has a ticked box in its own section and none sits in the
// install list, so each can be unticked (SPEC 9.2).
func TestRecommendedOfTwoModsHaveOwnSection(t *testing.T) {
	g := newModsRig(t, true)
	for _, n := range []string{"fp-sun", "fp-solar", "fp-tup", "fp-rigor", "fp-assets"} {
		g.portal.AddZip(n, "1.3.11")
	}
	g.portal.AddZip("fp-moshine", "1.2.14", "base >= 2.0", "fp-assets >= 1.0.2", "+ fp-sun >= 1.3.0", "+ fp-solar")
	g.portal.AddZip("fp-muluna", "2.9.16", "base >= 2.0", "+ fp-tup >= 1.0.8", "+ fp-rigor", "? fp-moshine >= 1.1.1")
	id := g.world20("Base")
	path := planPath(id, url.Values{"kind": {"install"}, "name": {"fp-moshine", "fp-muluna"}})
	code, body := g.getStatus(path)
	if code != http.StatusOK || !strings.Contains(body, `id="act-apply"`) {
		t.Fatalf("plan %d, not applicable:\n%s", code, body)
	}
	wantIn(t, "plan", body, `<fieldset id="plan-recommended">`)
	rows := planRows(body)
	for _, n := range []string{"fp-sun", "fp-solar", "fp-tup", "fp-rigor"} {
		wantIn(t, n, body, `value="`+n+`" id="mod-`+n+`-optional" hx-post="/worlds/`+id+`/mods/plan/redraw" checked`)
		if strings.Contains(rows, n) {
			t.Errorf("%s is in the install list:\n%s", n, rows)
		}
	}
	if !strings.Contains(rows, "fp-assets") {
		t.Errorf("required fp-assets missing from the install list:\n%s", rows)
	}
	wantIn(t, "button", plain(body), "Install 3 mods and 4 recommended")

	// Unticking one leaves it out and keeps its box.
	off := planPath(id, url.Values{"kind": {"install"}, "name": {"fp-moshine", "fp-muluna"},
		"optional": {"fp-solar", "fp-tup", "fp-rigor"}, "offered": {"fp-sun", "fp-solar", "fp-tup", "fp-rigor"}})
	_, body = g.getStatus(off)
	wantIn(t, "unticked", body, `value="fp-sun" id="mod-fp-sun-optional" hx-post="/worlds/`+id+`/mods/plan/redraw" aria-describedby`)
	wantIn(t, "unticked button", plain(body), "Install 3 mods and 3 recommended")
}

// An installed mod that a "+" dependency must upgrade has no box, so it
// stays in the plan's list rather than vanishing from the page (SPEC 9.2).
func TestRecommendedUpgradeOfInstalledModStaysListed(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("fp-sun", "1.3.11")
	g.portal.AddZip("fp-moshine", "1.2.14", "base >= 2.0", "+ fp-sun >= 1.3.11")
	id := g.world20("Base", "fp-sun_1.3.0")
	path := planPath(id, url.Values{"kind": {"install"}, "name": {"fp-moshine"}})
	code, body := g.getStatus(path)
	if code != http.StatusOK {
		t.Fatalf("plan %d:\n%s", code, body)
	}
	if rows := planRows(body); !strings.Contains(rows, "fp-sun") {
		t.Errorf("the upgrade of installed fp-sun is missing from the plan:\n%s", rows)
	}
}

// A redraw keeps the offers in the order the page showed them, and an offer
// the redraw brings goes after them even when its name sorts first, so no
// box moves under the pointer (SPEC 9.14).
func TestOfferOrderKeptAcrossRedraw(t *testing.T) {
	g := newModsRig(t, true)
	for _, n := range []string{"fp-b", "fp-c", "fp-0"} {
		g.portal.AddZip(n, "1.0.0")
	}
	g.portal.AddZip("fp-a", "1.0.0", "base >= 2.0", "? fp-0")
	g.portal.AddZip("fp-main", "1.0.0", "base >= 2.0", "? fp-a", "? fp-b", "? fp-c")
	id := g.world20("Base")
	order := func(body string) []string {
		var out []string
		for _, part := range strings.Split(body, `<input type="hidden" name="shown" value="`)[1:] {
			out = append(out, part[:strings.Index(part, `"`)])
		}
		return out
	}
	_, first := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"fp-main"}}))
	if got := order(first); strings.Join(got, " ") != "fp-a fp-b fp-c" {
		t.Fatalf("first order %v", got)
	}
	// Ticking fp-a offers fp-0, which sorts first by name but goes last.
	_, ticked := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"fp-main"}, "optional": {"fp-a"}, "shown": order(first)}))
	if got := order(ticked); strings.Join(got, " ") != "fp-a fp-b fp-c fp-0" {
		t.Errorf("after the tick %v, want fp-a fp-b fp-c fp-0", got)
	}
	// A page that showed another order keeps it.
	_, kept := g.getStatus(planPath(id, url.Values{"kind": {"install"}, "name": {"fp-main"}, "shown": {"fp-c", "fp-a", "fp-b"}}))
	if got := order(kept); strings.Join(got, " ") != "fp-c fp-a fp-b" {
		t.Errorf("shown order not kept: %v", got)
	}
}
