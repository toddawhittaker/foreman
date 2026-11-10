package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 9.2, 17.9: a conflict offers its fix as a link to the
// same plan with the other mod removed too, and a plan that cannot be
// applied does not list the steps of applying it.
func TestPlanConflictOffersItsFix(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("fp-clash", "1.0.0", "! fp-other")
	id := g.world20("Base", "fp-other_1.0.0")
	plan := "/worlds/" + id + "/mods/plan"

	code, body := g.getStatus(plan + "?kind=install&name=fp-clash&q=fp-c")
	if code != http.StatusConflict {
		t.Fatalf("status %d", code)
	}
	fixed := plan + "?also-remove=fp-other&amp;kind=install&amp;name=fp-clash&amp;q=fp-c"
	wantIn(t, "conflict", plain(body), "fp-clash cannot run together with fp-other.",
		`<a href="`+fixed+`" class="button" id="mod-fp-other-fix" hx-get="`+plan+`?also-remove=fp-other&amp;fixed=fp-other&amp;kind=install&amp;name=fp-clash&amp;q=fp-c"`,
		">Remove fp-other as part of this plan</a>", `<a href="/worlds/`+id+`/mods/search?q=fp-c">Back to search</a>`)
	for _, gone := range []string{`id="plan-steps"`, `id="act-apply"`, "Take a snapshot"} {
		if strings.Contains(body, gone) {
			t.Errorf("a plan with a conflict still shows %s", gone)
		}
	}

	// The fix's plan installs one, removes the other, and says so in its heading and button.
	code, body = g.getStatus(plan + "?also-remove=fp-other&kind=install&name=fp-clash&q=fp-c")
	if code != http.StatusOK {
		t.Fatalf("fixed plan %d\n%s", code, body)
	}
	wantIn(t, "fixed", plain(body), "Change the mods of Base?", "<dt>Install</dt>", "fp-clash 1.0.0", "<dt>Remove</dt>", "fp-other 1.0.0",
		`<input type="hidden" name="also-remove" value="fp-other">`, `class="button button--danger" id="act-apply">Change 2 mods</button>`, `<section class="confirm">`)

	// A redraw from the fix announces it and puts focus on the changes.
	rec := g.harness.get(plan+"?also-remove=fp-other&fixed=fp-other&kind=install&name=fp-clash&q=fp-c", withCookie(g.cookie), withHeader("HX-Request", "true"))
	wantIn(t, "redraw", rec.Body.String(), "Plan updated. It changes 2 mods.", `<p class="plan-label" id="plan-changes" tabindex="-1" autofocus>Changes:</p>`)

	// Applying it removes the other mod in the same change, with one snapshot.
	g.wantRedirectHome(g.post(plan, planValues(t, body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if !g.hasFile(id, "fp-clash_1.0.0.zip") || g.hasFile(id, "fp-other_1.0.0.zip") {
		t.Error("the fix's plan did not install one and remove the other")
	}
	if n := g.modSnapshots(id); n != 1 {
		t.Errorf("%d snapshots, want 1", n)
	}
}

// A mod another enabled mod needs: the fix disables that one too. A fix
// is never offered for a mod the admin asked for, a built-in mod or one
// already off; a folder mod is disabled, never removed.
func TestConflictFixes(t *testing.T) {
	have := []world.InstalledMod{
		{Name: "needer", Enabled: true}, {Name: "lib", Enabled: true}, {Name: "folder", Enabled: true, Local: true},
		{Name: "off"}, {Name: "base", Enabled: true, BuiltIn: true},
	}
	page := func(kind mods.Kind, names ...string) ModPlanPage {
		return ModPlanPage{World: world.Summary{ID: "w"}, Form: planForm{Kind: kind, Names: names}, Have: have}
	}
	cases := []struct {
		name   string
		page   ModPlanPage
		c      mods.Conflict
		mod    string
		remove bool
	}{
		{"needed", page(mods.KindDisable, "lib"), mods.Conflict{Code: mods.ConflictRequiredBy, Mod: "lib", Other: "needer"}, "needer", false},
		{"incompatible", page(mods.KindInstall, "new"), mods.Conflict{Code: mods.ConflictIncompatible, Mod: "new", Other: "lib"}, "lib", true},
		{"incompatible, the installed one refuses", page(mods.KindInstall, "new"), mods.Conflict{Code: mods.ConflictIncompatible, Mod: "lib", Other: "new"}, "lib", true},
		{"folder mod", page(mods.KindInstall, "new"), mods.Conflict{Code: mods.ConflictIncompatible, Mod: "new", Other: "folder"}, "folder", false},
		{"asked for", page(mods.KindEnable, "needer"), mods.Conflict{Code: mods.ConflictIncompatible, Mod: "needer", Other: "new"}, "", false},
		{"built in", page(mods.KindInstall, "new"), mods.Conflict{Code: mods.ConflictIncompatible, Mod: "new", Other: "base"}, "", false},
		{"already off", page(mods.KindInstall, "new"), mods.Conflict{Code: mods.ConflictIncompatible, Mod: "new", Other: "off"}, "", false},
		{"no fix for this kind", page(mods.KindInstall, "new"), mods.Conflict{Code: mods.ConflictNoRelease, Mod: "new"}, "", false},
	}
	for _, c := range cases {
		if mod, remove := c.page.conflictFix(c.c); mod != c.mod || remove != c.remove {
			t.Errorf("%s: fix %q remove %v, want %q %v", c.name, mod, remove, c.mod, c.remove)
		}
	}
	// A fix already taken is not offered again.
	p := page(mods.KindDisable, "lib")
	p.Form.AlsoDisable = []string{"needer"}
	if mod, _ := p.conflictFix(mods.Conflict{Code: mods.ConflictRequiredBy, Mod: "lib", Other: "needer"}); mod != "" {
		t.Errorf("fix offered again: %q", mod)
	}
}

// Issue 374: the mods a plan needs and leaves as they are fold into one
// line, so the changes stay in view; asked-for mods come first in their group.
func TestPlanKeptModsFold(t *testing.T) {
	r := newTestRenderer(t)
	p := ModPlanPage{World: world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.77"}, Form: planForm{Kind: mods.KindInstall, Names: []string{"zeta", "alpha"}}, Hash: "h",
		Plan: mods.Plan{Changes: []mods.Change{
			{Name: "alpha", To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyAsked}},
			{Name: "beta", To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyRequiredBy, Of: "alpha"}},
			{Name: "lib", From: "2.0.0", To: "2.0.0", Action: mods.ActKeep, Why: mods.Reason{Code: mods.WhyRequiredBy, Of: "beta"}},
			{Name: "zeta", To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyAsked}},
		}}}
	_, body := renderIn(t, r, pageCase{"plan", "todd", 200, p}, "en")
	body = plain(body)
	wantIn(t, "kept", body, "<summary>1 other mod stays as it is</summary>", "lib 2.0.0 (beta needs it)")
	z, a, b := strings.Index(body, "zeta 1.0.0"), strings.Index(body, "alpha 1.0.0"), strings.Index(body, "beta 1.0.0")
	if z < 0 || z > a || a > b {
		t.Errorf("install order: zeta %d, alpha %d, beta %d; want the asked mods first, in the order asked", z, a, b)
	}
	if strings.Contains(body, "Leave alone") {
		t.Error("the kept mods still have their own group")
	}
}
