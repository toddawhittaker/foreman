package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
)

// SPEC 9.2: an install plan lists each asked mod's releases for the
// world's line, the newest that fits chosen; choosing an older one draws
// the plan again with that release and its own dependencies.
func TestPlanVersionList(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("ver-lib", "1.0.0")
	g.portal.AddZip("ver-lib", "2.0.0")
	g.portal.AddZip("ver-a", "1.0.0", "ver-lib >= 1.0.0")
	g.portal.AddZip("ver-a", "2.0.0", "ver-lib >= 2.0.0")
	g.portal.AddZipFor("1.1", "ver-a", "0.9.0")
	id := g.world20("Base")
	plan := "/worlds/" + id + "/mods/plan"

	code, body := g.getStatus(plan + "?kind=install&name=ver-a")
	if code != http.StatusOK {
		t.Fatalf("status %d\n%s", code, body)
	}
	wantIn(t, "default", plain(body), "ver-a 2.0.0", `<label for="mod-ver-a-version">Version of ver-a</label>`,
		`<select id="mod-ver-a-version" name="version-ver-a" hx-get="`+plan+`" aria-describedby="plan-versions-hint">`,
		`<option value="" selected>Newest that fits (2.0.0)</option>`, `<option value="2.0.0">2.0.0</option>`, `<option value="1.0.0">1.0.0</option>`,
		"Only releases for Factorio 2.0")
	if strings.Contains(body, "0.9.0") {
		t.Error("a release for another game line is listed")
	}

	// Choosing 1.0.0 redraws the plan with it; the library is still the newest that fits.
	rec := g.harness.get(plan+"?kind=install&name=ver-a&version-ver-a=1.0.0", withCookie(g.cookie), withHeader("HX-Request", "true"))
	body = rec.Body.String()
	wantIn(t, "chosen", plain(body), "ver-a 1.0.0", "ver-lib 2.0.0", `<option value="1.0.0" selected>1.0.0</option>`,
		`<option value="">Newest that fits</option>`, "Install 2 mods")

	// Applying it installs exactly the chosen release.
	_, full := g.getStatus(plan + "?kind=install&name=ver-a&version-ver-a=1.0.0")
	v := planValues(t, full)
	v.Set("version-ver-a", "1.0.0")
	g.wantRedirectHome(g.post(plan, v))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if !g.hasFile(id, "ver-a_1.0.0.zip") || g.hasFile(id, "ver-a_2.0.0.zip") {
		t.Error("the chosen release was not the one installed")
	}
}

// SPEC 9.3: Change version on the installed table holds the mod at its
// version, so nothing changes until another is chosen; an older one goes
// back after a bad update, with a snapshot first (SPEC 9.4).
func TestChangeVersionGoesBack(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("ver-b", "1.0.0")
	g.portal.AddZip("ver-b", "1.1.0")
	id := g.world20("Base", "ver-b_1.1.0")
	plan := "/worlds/" + id + "/mods/plan"

	_, tab := g.getStatus(modsPath(id))
	link := plan + "?kind=update&amp;name=ver-b&amp;version-ver-b=1.1.0"
	wantIn(t, "tab", tab, `<a href="`+link+`" id="`+slotID("version", "ver-b")+`" class="button button--quiet mod-version">Change version<span class="visually-hidden"> <span translate="no">ver-b</span></span></a>`)

	code, body := g.getStatus(plan + "?kind=update&name=ver-b&version-ver-b=1.1.0")
	if code != http.StatusOK {
		t.Fatalf("status %d\n%s", code, body)
	}
	wantIn(t, "held", plain(body), "Change the version of ver-b in Base?", `<option value="1.1.0" selected>1.1.0 (installed)</option>`,
		`<option value="1.0.0">1.0.0</option>`, `id="plan-same"`, "Nothing changes at this version.", ">Cancel, change nothing</a>")
	for _, gone := range []string{`id="act-apply"`, `id="plan-steps"`, "stay as they are"} {
		if strings.Contains(body, gone) {
			t.Errorf("a plan that changes nothing shows %s", gone)
		}
	}

	code, body = g.getStatus(plan + "?kind=update&name=ver-b&version-ver-b=1.0.0")
	if code != http.StatusOK {
		t.Fatalf("status %d\n%s", code, body)
	}
	wantIn(t, "back", plain(body), "<dt>Downgrade</dt>", "ver-b 1.1.0 to 1.0.0", `id="act-apply">Change 1 mod</button>`, "Take a snapshot")
	g.wantRedirectHome(g.post(plan, withVersions(planValues(t, body), body)))
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if !g.hasFile(id, "ver-b_1.0.0.zip") || g.hasFile(id, "ver-b_1.1.0.zip") {
		t.Error("the mod did not go back to 1.0.0")
	}
	if n := g.modSnapshots(id); n != 1 {
		t.Errorf("%d snapshots, want 1", n)
	}
}

var reVersionChosen = regexp.MustCompile(`(?s)<select [^>]*name="(version-[^"]+)"[^>]*>.*?<option value="([^"]*)" selected>`)

// withVersions adds the version lists' chosen values, which are selects,
// not hidden fields.
func withVersions(v url.Values, body string) url.Values {
	for _, m := range reVersionChosen.FindAllStringSubmatch(body, -1) {
		v.Set(m[1], m[2])
	}
	return v
}

// A version is checked on the server against the portal's releases: one
// the portal has not got, or one for another line, is a conflict and is
// never downloaded; one that is not a version at all is refused.
func TestPlanVersionIsChecked(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("ver-c", "1.0.0")
	g.portal.AddZipFor("1.1", "ver-c", "0.5.0")
	id := g.world20("Base")
	plan := "/worlds/" + id + "/mods/plan"

	for _, v := range []string{"9.9.9", "0.5.0"} {
		code, body := g.getStatus(plan + "?kind=install&name=ver-c&version-ver-c=" + v)
		if code != http.StatusConflict {
			t.Fatalf("%s: status %d", v, code)
		}
		wantIn(t, v, plain(body), "The mod portal has no release "+v+" of ver-c for this game version. Choose another version.",
			`<option value="`+v+`" selected>`+v+`</option>`)
		// Posting it anyway applies nothing.
		vals := url.Values{"kind": {"install"}, "name": {"ver-c"}, "version-ver-c": {v}}
		if rep := g.post(plan, vals); rep.StatusCode != http.StatusConflict {
			t.Errorf("%s: post status %d", v, rep.StatusCode)
		}
	}
	for _, bad := range []string{"../../etc", "1.0", "1.0.0.0", "1.0.99999", "x"} {
		if code, _ := g.getStatus(plan + "?kind=install&name=ver-c&version-ver-c=" + url.QueryEscape(bad)); code != http.StatusBadRequest {
			t.Errorf("%q: status %d, want 400", bad, code)
		}
	}
	if n := g.portal.Downloads("ver-c"); n != 0 {
		t.Errorf("%d downloads", n)
	}
}

// The form keeps a chosen version only for an asked mod of an install or
// update, and every link back to the plan carries it.
func TestPlanFormVersions(t *testing.T) {
	f, ok := readPlanForm(url.Values{"kind": {"install"}, "name": {"a", "b"}, "version-a": {"1.2.3"}, "version-z": {"1.0.0"}, "version-b": {""}})
	if !ok {
		t.Fatal("refused")
	}
	if len(f.Versions) != 1 || f.Versions["a"] != "1.2.3" {
		t.Errorf("versions = %v", f.Versions)
	}
	if got := f.query().Encode(); got != "kind=install&name=a&name=b&version-a=1.2.3" {
		t.Errorf("query = %s", got)
	}
	// Taking a out of the plan takes its version too.
	if got := f.withNames([]string{"b"}).query().Encode(); got != "kind=install&name=b" {
		t.Errorf("without a = %s", got)
	}
	f, ok = readPlanForm(url.Values{"kind": {"disable"}, "name": {"a"}, "version-a": {"nonsense"}})
	if !ok || len(f.Versions) != 0 {
		t.Errorf("disable: ok %v, versions %v", ok, f.Versions)
	}
	p := ModPlanPage{Form: planForm{Kind: mods.KindUpdate, Names: []string{"a"}, Versions: map[string]string{"a": "1.0.0"}}}
	if p.Versions() != nil {
		t.Error("a list without the portal's releases")
	}
}

// Two asked mods share one fieldset and its hint; one mod's list is a
// plain field with the hint under its label (DESIGN 7.2).
func TestPlanVersionListShape(t *testing.T) {
	g := newModsRig(t, true)
	g.portal.AddZip("ver-d", "1.0.0")
	g.portal.AddZip("ver-e", "1.0.0")
	id := g.world20("Base")
	plan := "/worlds/" + id + "/mods/plan"
	_, one := g.getStatus(plan + "?kind=install&name=ver-d")
	if strings.Contains(one, `class="plan-versions"`) {
		t.Error("one mod's list is in a fieldset")
	}
	wantIn(t, "one", plain(one), `<label for="mod-ver-d-version">Version of ver-d</label>
        <p class="hint" id="plan-versions-hint">`)
	_, two := g.getStatus(plan + "?kind=install&name=ver-d&name=ver-e")
	wantIn(t, "two", two, `<fieldset class="plan-versions">
      <legend>Versions</legend>`, `id="mod-ver-d-version"`, `id="mod-ver-e-version"`)
}

// The portal status takes Change version away from a mod the portal does
// not have, as it does the name's link.
func TestStatusHidesChangeVersion(t *testing.T) {
	g := newModsRig(t, true)
	id := g.world20("Base", "zz-not-on-portal_1.0.0")
	_, body := g.getStatus(modsPath(id) + "/status")
	wantIn(t, "status", body, `<span id="`+slotID("version", "zz-not-on-portal")+`" hidden hx-swap-oob="true"></span>`)
}

// An asked mod kept at its version is left out of Kept only when its
// version list shows it; without the portal's releases it stays listed.
func TestKeptHidesAnAskedModOnlyWithAVersionList(t *testing.T) {
	keep := mods.Change{Name: "a", Action: mods.ActKeep, Why: mods.Reason{Code: mods.WhyAsked}}
	p := ModPlanPage{Form: planForm{Kind: mods.KindUpdate, Names: []string{"a"}}, Plan: mods.Plan{Changes: []mods.Change{keep}}}
	if got := p.Kept(); len(got) != 1 || got[0].Name != "a" {
		t.Errorf("without releases: kept %v", got)
	}
	p.Plan.Releases = map[string][]string{"a": {"1.0.0"}}
	if got := p.Kept(); len(got) != 0 {
		t.Errorf("with releases: kept %v", got)
	}
}
