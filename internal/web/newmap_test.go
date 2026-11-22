package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// newMapWorld makes an Unfinished 2.0.77 world through Generate.
func newMapWorld(t *testing.T, hn *harness, c *http.Cookie) string {
	t.Helper()
	hn.withActive("2.0.77")
	return idFromLocation(t, hn.postLong(PathGenerate, generateForm(), c))
}

// getLong reads a long route (step 3's page waits for the planet list).
func getLong(hn *harness, p string, c *http.Cookie) *httptest.ResponseRecorder {
	hn.t.Helper()
	r := httptest.NewRequest(http.MethodGet, p, nil)
	r.AddCookie(c)
	return hn.serveLong(r)
}

// newMapPost is step 3's form for a 2.0.77 Space Age world, as the page sends it.
func newMapPost(extra ...string) url.Values {
	f := generateForm(extra...)
	f.Del("game_version_typed")
	f.Set("game_version", "2.0.77")
	return f
}

// SPEC 5.6, 17.19: without wait the page comes at once, with the form to
// load after it and a reload for a browser without script; with wait it
// holds the form, the step bar marking step 3.
func TestNewMapPageLoadsItsFormAfterThePage(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := newMapWorld(t, hn, c)

	rec := getLong(hn, newMapPath(id), c)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{`id="newmap-load"`, `hx-get="` + newMapPath(id) + `?wait=1"`, `<noscript><meta http-equiv="refresh"`} {
		if !strings.Contains(body, want) {
			t.Errorf("no %s in the first page", want)
		}
	}
	if strings.Contains(body, `id="generate-form"`) {
		t.Error("the first page already holds the form")
	}

	body = getLong(hn, newMapPath(id)+"?wait=1", c).Body.String()
	for _, want := range []string{`id="generate-form"`, `id="newmap-body"`, `href="` + newMapPath(id) + `" aria-current="step"`, `href="` + modsPath(id) + `"`, `id="generate-preset"`, `id="newmap-save"`} {
		if !strings.Contains(body, want) {
			t.Errorf("no %s on the waited page", want)
		}
	}
	if strings.Count(body, `aria-current="step"`) != 1 {
		t.Error("not exactly one current step")
	}
}

// SPEC 5.6: only an Unfinished world has step 3; any other world gets a
// page that says why, with no form.
func TestNewMapRefusesAWorldWhoseMapIsMade(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := hn.withActive("2.0.77") // made without new_map, as an imported world
	for _, p := range []string{newMapPath(id), newMapPath(id) + "?wait=1"} {
		rec := getLong(hn, p, c)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "map is already made") || strings.Contains(rec.Body.String(), `id="generate-form"`) {
			t.Errorf("%s: status %d", p, rec.Code)
		}
	}
	rec := hn.postLong(newMapPath(id), newMapPost(), c)
	if rec.Code != http.StatusConflict {
		t.Errorf("save status %d, want 409", rec.Code)
	}
	if hn.config(id, world.MapGenSettingsFile).Found {
		t.Error("a file was written for a world with a made map")
	}
	if got := getLong(hn, newMapPath("nope"), c).Code; got != http.StatusNotFound {
		t.Errorf("missing world status %d", got)
	}
}

// SPEC 5.6, 12.9: the form starts from the world's new_map; a save writes
// through SetNewMap, is audited naming the preset, and lands on a page
// whose next step is a link.
func TestNewMapSaveWritesTheSettingsAndAudits(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := newMapWorld(t, hn, c)

	rec := hn.postLong(newMapPath(id), newMapPost("preset", "rich-resources", "seed", "77"), c)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != newMapPath(id)+"?saved=1" {
		t.Fatalf("status %d location %q:\n%s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	m := hn.meta(id)
	if m.NewMap == nil || m.NewMap.Preset != "rich-resources" || m.NewMap.Seed == nil || *m.NewMap.Seed != 77 {
		t.Errorf("new_map %+v", m.NewMap)
	}
	if got := hn.audits(AuditNewMap); len(got) != 1 || !strings.Contains(got[0], "preset rich-resources, 2.0.77") {
		t.Errorf("audit %q", got)
	}
	body := getLong(hn, rec.Header().Get("Location"), c).Body.String()
	for _, want := range []string{`id="newmap-done"`, `id="newmap-next"`, `<option value="rich-resources" selected>`, `value="77"`} {
		if !strings.Contains(body, want) {
			t.Errorf("no %s after the save", want)
		}
	}
}

// SPEC 5.6: a changed setting writes both files complete, which the page
// reads back.
func TestNewMapSaveOfAChangedSettingWritesTheFiles(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := newMapWorld(t, hn, c)
	page := getLong(hn, newMapPath(id)+"?wait=1", c).Body.String()
	m := regexp.MustCompile(`(?s)<form method="post" action="` + newMapPath(id) + `"[^>]*>(.*?)</form>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no step 3 form:\n%s", page)
	}
	form := fieldValues(m[1])
	form.Set("mg.width", "100")
	rec := hn.postLong(newMapPath(id), form, c)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
	if !hn.config(id, world.MapGenSettingsFile).Found || !hn.config(id, world.MapSettingsFile).Found {
		t.Error("the files are not written")
	}
	if got := hn.audits(AuditNewMap); len(got) != 1 || !strings.Contains(got[0], "modified") {
		t.Errorf("audit %q", got)
	}
}

// SPEC 5.6: a map exchange string of another game line is refused, telling
// the person to change the world's version; one of the world's line fills
// the settings.
func TestNewMapFillFromExchangeString(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := newMapWorld(t, hn, c)
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "mapexchange", "unittestdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}

	rec := hn.postLong(newMapPath(id)+"/refresh", newMapPost("fill", "1", "exchange", read("2.1.24.txt")), c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Change the world&#39;s game version on its page") {
		t.Errorf("other line: status %d", rec.Code)
	}

	rec = hn.postLong(newMapPath(id)+"/refresh", newMapPost("fill", "1", "exchange", read("2.0.77-custom.txt")), c)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Contains(body, `id="exchange-string-error"`) || !strings.Contains(body, `data-unsaved="1"`) {
		t.Errorf("same line: status %d", rec.Code)
	}
	if hn.config(id, world.MapGenSettingsFile).Found {
		t.Error("a fill saved the settings")
	}
}

// SPEC 5.6: controls the saved settings name that the mods no longer add
// are listed, so the person knows the next save drops them.
func TestGoneControls(t *testing.T) {
	cat := mapgen.Builtin("2.0", true)
	got := goneControls([]byte(`{"autoplace_controls":{"iron-ore":{},"fake-ore":{},"aaa-gone":{}}}`), cat)
	if !slices.Equal(got, []string{"aaa-gone", "fake-ore"}) {
		t.Errorf("gone %v", got)
	}
	if got := goneControls([]byte(`not json`), cat); got != nil {
		t.Errorf("gone %v for a damaged file", got)
	}
}

// SPEC 17.10: the step bar is three links, the current one marked.
func TestFlowSteps(t *testing.T) {
	steps := flowSteps("w1", flowStepMods)
	if len(steps) != 3 || steps[0].Path != worldPath("w1") || steps[1].Path != modsPath("w1") || steps[2].Path != newMapPath("w1") {
		t.Fatalf("steps %+v", steps)
	}
	for i, s := range steps {
		if s.Current != (i == 1) {
			t.Errorf("step %d current %t", i+1, s.Current)
		}
	}
}
