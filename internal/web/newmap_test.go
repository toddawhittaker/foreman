package web

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
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
		b, err := os.ReadFile(filepath.Join("..", "mapexchange", "unittestdata", name)) //nolint:gosec // a fixed test data file
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

// SPEC 5.6, 17.10: the step bar is four links, the current one marked.
func TestFlowSteps(t *testing.T) {
	steps := flowSteps("w1", flowStepMods, false)
	if len(steps) != 4 || steps[0].Path != worldPath("w1") || steps[1].Path != modsPath("w1") ||
		steps[2].Path != flowModSettingsPath("w1") || steps[3].Path != newMapPath("w1") {
		t.Fatalf("steps %+v", steps)
	}
	for i, s := range steps {
		if s.Current != (i == 1) {
			t.Errorf("step %d current %t", i+1, s.Current)
		}
	}
}

// SPEC 5.6: with no startup setting, step 3 is plain text marked (none).
func TestFlowStepsWithoutModSettings(t *testing.T) {
	steps := flowSteps("w1", flowStepMap, true)
	if steps[2].Path != "" || steps[2].Label.Key != "newmap.steps.mod_settings_none" || steps[3].Path != newMapPath("w1") || !steps[3].Current {
		t.Fatalf("steps %+v", steps)
	}
}

// SPEC 5.6: on step 1 there is no world yet, so the step bar marks step 1
// and the later steps are not links.
func TestFlowStepsBeforeTheWorldExists(t *testing.T) {
	steps := GeneratePage{}.Steps()
	if len(steps) != 4 || steps[0].Path != PathGenerate || !steps[0].Current {
		t.Fatalf("steps %+v", steps)
	}
	for _, s := range steps[1:] {
		if s.Path != "" || s.Current {
			t.Fatalf("steps %+v", steps)
		}
	}
	hn := newGenerateHarness(t, nil)
	page := hn.get(PathGenerate, withCookie(hn.signIn())).Body.String()
	bar := regexp.MustCompile(`(?s)<nav class="subnav"[^>]*id="flow-steps">.*?</nav>`).FindString(page)
	if bar == "" {
		t.Fatalf("no step bar on step 1:\n%s", page)
	}
	if n := strings.Count(bar, "<a "); n != 1 || !strings.Contains(bar, `<a href="`+PathGenerate+`" aria-current="step">`) {
		t.Errorf("step 1's bar has %d links:\n%s", n, bar)
	}
}

// newMapPageValues are the values step 3's form on the waited page sends.
func newMapPageValues(t *testing.T, hn *harness, c *http.Cookie, id string) url.Values {
	t.Helper()
	page := getLong(hn, newMapPath(id)+"?wait=1", c).Body.String()
	m := regexp.MustCompile(`(?s)<form method="post" action="` + newMapPath(id) + `"[^>]*>(.*?)</form>`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no step 3 form:\n%s", page)
	}
	return fieldValues(m[1])
}

// SPEC 5.6, 17.9: when the world's own planets cannot be read, the save is
// refused with nothing written, and the message links to try again and to
// the Mods step.
func TestNewMapSaveRefusedOnTheBuiltinFallback(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := newMapWorld(t, hn, c)
	withWorldCatalogue(t, mapgen.Builtin("2.0", true), game.ErrPlanetsMissing)
	form := newMapPageValues(t, hn, c, id)
	form.Set("mg.width", "100")
	rec := hn.postLong(newMapPath(id), form, c)
	body := rec.Body.String()
	if rec.Code != http.StatusConflict || !strings.Contains(body, "Nothing was saved") {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{`href="` + newMapPath(id) + `" id="newmap-retry"`, `href="` + modsPath(id) + `" id="newmap-to-mods"`} {
		if !strings.Contains(body, want) {
			t.Errorf("no %s", want)
		}
	}
	if hn.config(id, world.MapGenSettingsFile).Found || hn.meta(id).NewMap.Preset != "" || len(hn.audits(AuditNewMap)) != 0 {
		t.Error("something was saved")
	}
}

// SPEC 5.6, ADR 0033: a world mod named like Foreman's helper mod says so on
// step 3, with the way to the Mods step, not that the planets could not be
// read, and a save is refused as with any fallback.
func TestNewMapNamesTheHelperModClash(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := newMapWorld(t, hn, c)
	withWorldCatalogue(t, mapgen.Builtin("2.0", true), fmt.Errorf("%w: %w", game.ErrModSettingsBadOutput, game.ErrControlModTaken))
	body := getLong(hn, newMapPath(id)+"?wait=1", c).Body.String()
	if !strings.Contains(body, html.EscapeString(i18n.English.T("mapgen.preview.control_mod_taken"))) {
		t.Errorf("no helper-mod message in:\n%s", body)
	}
	if strings.Contains(body, html.EscapeString(i18n.English.T("newmap.builtin.failed"))) {
		t.Error("the generic message is shown too")
	}
	if !strings.Contains(body, `id="newmap-builtin-mods"`) {
		t.Error("no link to the Mods step")
	}
	form := newMapPageValues(t, hn, c, id)
	if rec := hn.postLong(newMapPath(id), form, c); rec.Code != http.StatusConflict || hn.config(id, world.MapGenSettingsFile).Found {
		t.Errorf("status %d, or the settings were saved", rec.Code)
	}
}

// SPEC 5.6, 17.9: a save while the world's server is up is refused under
// the world's lock, with a link to the Server page.
func TestNewMapSaveRefusedWhileTheServerIsUp(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := newMapWorld(t, hn, c)
	old := newMapSaveIdle
	newMapSaveIdle = func(*server, string) error { return errNewMapServerUp }
	t.Cleanup(func() { newMapSaveIdle = old })
	rec := hn.postLong(newMapPath(id), newMapPost("preset", "rich-resources"), c)
	body := rec.Body.String()
	if rec.Code != http.StatusConflict || !strings.Contains(body, "Stop the server before changing the map settings") || !strings.Contains(body, `href="`+PathServer+`" id="newmap-to-server"`) {
		t.Fatalf("status %d", rec.Code)
	}
	if hn.meta(id).NewMap.Preset == "rich-resources" {
		t.Error("the preset was saved")
	}
}

// SPEC 5.6: a control no longer in the mods is listed, and the next save
// leaves it out of the file.
func TestNewMapSaveDropsAGoneControl(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := newMapWorld(t, hn, c)
	file := filepath.Join(hn.worlds.Dirs().Worlds, id, "config", world.MapGenSettingsFile)
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(`{"autoplace_controls":{"fake-ore":{"frequency":1,"size":1,"richness":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	page := getLong(hn, newMapPath(id)+"?wait=1", c).Body.String()
	if !strings.Contains(page, `id="newmap-gone"`) || !strings.Contains(page, ">fake-ore<") {
		t.Fatal("the gone control is not listed")
	}
	form := newMapPageValues(t, hn, c, id)
	form.Set("mg.width", "100")
	if rec := hn.postLong(newMapPath(id), form, c); rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d:\n%s", rec.Code, rec.Body.String())
	}
	saved := hn.config(id, world.MapGenSettingsFile)
	if !saved.Found || strings.Contains(string(saved.Data), "fake-ore") {
		t.Errorf("saved file %s", saved.Data)
	}
}

// SPEC 5.6, ADR 0033: step 3 lays out the world's own catalogue, and its
// form's preview draws from it, the world named.
func TestNewMapPreviewDrawsTheWorldsCatalogue(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.1.24")
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm("game_version_typed", "2.1.24", "map_key", "2.1|1"), c))
	withWorldCatalogue(t, cerysCatalogue(), nil)
	form := newMapPageValues(t, hn, c, id)
	if form.Get("world") != id {
		t.Fatalf("world field %q", form.Get("world"))
	}
	if !strings.Contains(getLong(hn, newMapPath(id)+"?wait=1", c).Body.String(), `data-planet="cerys"`) {
		t.Error("no Cerys row on step 3")
	}
	form.Set("planet", "cerys")
	rec := hn.postLong(PathGeneratePreview, form, c)
	if rec.Code != http.StatusOK || len(prev.got) != 1 {
		t.Fatalf("status %d, runs %d", rec.Code, len(prev.got))
	}
	if got := prev.got[0]; got.World != id || !slices.Equal(got.Planets, []string{"cerys"}) || !slices.Contains(got.KnownPlanets, "cerys") {
		t.Errorf("request %+v", got)
	}
}
