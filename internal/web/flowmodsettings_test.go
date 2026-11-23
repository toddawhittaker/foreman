package web

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// flowWorld is an Unfinished world with the test mod fmtest.
func (g *modsRig) flowWorld() string {
	g.t.Helper()
	id := g.world20("Fresh", "fmtest_1.0.0")
	g.makeNewMap(id)
	return id
}

// flowForm reads step 3's form part and the values a browser would post.
func (g *modsRig) flowForm(id string) (string, url.Values) {
	g.t.Helper()
	rep := g.do(http.MethodGet, flowModSettingsPath(id)+"/form", nil, http.Header{"Cookie": {g.cookie.String()}})
	if rep.StatusCode != http.StatusOK {
		g.t.Fatalf("form: %d\n%s", rep.StatusCode, rep.body)
	}
	return rep.body, formValues(g.t, rep.body)
}

// SPEC 5.6: step 3 is the startup settings only, grouped by mod, with step 3
// current and a Skip on to step 4; the form loads after the page.
func TestFlowModSettingsShowsTheStartupSettings(t *testing.T) {
	g := newModsRig(t, false)
	id := g.flowWorld()
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	page := g.get(flowModSettingsPath(id))
	wantIn(t, "page", page, `hx-get="`+flowModSettingsPath(id)+`/form"`, `href="`+flowModSettingsPath(id)+`" aria-current="step"`,
		`<a href="`+newMapPath(id)+`" id="flow-modsettings-skip">`)
	body, v := g.flowForm(id)
	wantIn(t, "form", body, `id="`+fieldOf("fm-sta-int", modsettings.Startup)+`"`, `action="`+flowModSettingsPath(id)+`"`, `<legend>`)
	if strings.Contains(body, fieldOf("fm-glo-int", modsettings.RuntimeGlobal)) || v["map"] != nil {
		t.Error("step 3 shows the map settings of the mods")
	}
	// Without script the page waits and comes whole.
	wantIn(t, "waited page", g.get(flowModSettingsPath(id)+"?wait=1"), `id="`+fieldOf("fm-sta-int", modsettings.Startup)+`"`)
}

// SPEC 5.6: saving writes the change and goes on to step 4; saving nothing
// goes on too.
func TestFlowModSettingsSaveGoesOnToTheMap(t *testing.T) {
	g := newModsRig(t, false)
	id := g.flowWorld()
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	_, v := g.flowForm(id)
	g.wantRedirectTo(newMapPath(id), g.post(flowModSettingsPath(id), v))
	if g.modSnapshots(id) != 0 {
		t.Error("saving nothing took a snapshot")
	}
	v.Set(fieldOf("fm-sta-int", modsettings.Startup), "9")
	g.wantRedirectTo(newMapPath(id), g.post(flowModSettingsPath(id), v))
	if got := g.settingValue(id, modsettings.Startup, "fm-sta-int"); got.Int != 9 {
		t.Errorf("fm-sta-int is %+v, want 9", got)
	}
	if g.modSnapshots(id) != 1 {
		t.Error("no snapshot before the change")
	}
}

// SPEC 5.6: with no startup setting the step passes on to step 4: a world
// with only built-in mods at once, otherwise once the settings are read.
func TestFlowModSettingsSkipsItselfWhenThereIsNothing(t *testing.T) {
	g := newModsRig(t, false)
	bare := g.world20("Bare")
	g.makeNewMap(bare)
	g.wantRedirectTo(skippedPath(bare), g.do(http.MethodGet, flowModSettingsPath(bare), nil, http.Header{"Cookie": {g.cookie.String()}}))

	id := g.flowWorld()
	defs := fmtestSettingDefs(t)
	var runtimeOnly modsettings.Definitions
	for _, d := range defs.Defs {
		if d.Setting != modsettings.Startup {
			runtimeOnly = append(runtimeOnly, d)
		}
	}
	fakeDefs(t, &game.ModSettingDefs{Defs: runtimeOnly, Defaults: defs.Defaults}, nil)
	rep := g.do(http.MethodGet, flowModSettingsPath(id)+"/form", nil, http.Header{"Cookie": {g.cookie.String()}})
	if rep.Header.Get("HX-Redirect") != skippedPath(id) {
		t.Errorf("form part: %d, HX-Redirect %q", rep.StatusCode, rep.Header.Get("HX-Redirect"))
	}
	g.wantRedirectTo(skippedPath(id), g.do(http.MethodGet, flowModSettingsPath(id)+"?wait=1", nil, http.Header{"Cookie": {g.cookie.String()}}))
}

// SPEC 5.6: a world whose map is made has no step 3.
func TestFlowModSettingsRefusesAMadeWorld(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Made", "fmtest_1.0.0")
	if rep := g.do(http.MethodGet, flowModSettingsPath(id), nil, http.Header{"Cookie": {g.cookie.String()}}); rep.StatusCode != http.StatusConflict {
		t.Errorf("status %d, want 409", rep.StatusCode)
	}
}

// flowModSettingsPageCases are step 3 while it loads, with its form, and
// with a run error.
func flowModSettingsPageCases() []pageCase {
	w := world.Summary{ID: "fresh", Name: longName, GameVersion: "2.0.77", Unfinished: true}
	defs := &game.ModSettingDefs{Defs: fmtestDefs()}
	startup, _, _ := settingGroups(defs, nil, nil, "en")
	return []pageCase{
		{"flow mod settings loading", "todd", 200, FlowModSettingsPage{ModSettingsPage{World: w}}},
		{"flow mod settings", "todd", 200, FlowModSettingsPage{ModSettingsPage{World: w, Loaded: true, Startup: startup, Fingerprint: "f", Seen: "s"}}},
		{"flow mod settings run error", "todd", 200, FlowModSettingsPage{ModSettingsPage{World: w, Loaded: true, RunError: i18n.M("mods.settings.edit.run.failed")}}},
	}
}

// SPEC 5.6, 9.6: step 3 shows only startup groups, so an origin names a
// parent that has only map settings as plain text, not a link to a group
// that is not on the page, and the per-user hint is not shown.
func TestFlowModSettingsLinksOnlyGroupsOnThePage(t *testing.T) {
	g := newModsRig(t, false)
	id := g.flowWorld()
	g.writeFmtestInfo(id, "aaa-lib >= 1.0")
	g.writeMod(id, "aaa-lib", "1.0.0")
	defs := fmtestSettingDefs(t)
	// fmtest keeps its map and per-user settings; aaa-lib has a startup one.
	defs.Defs = slices.DeleteFunc(defs.Defs, func(d modsettings.Definition) bool { return d.Setting == modsettings.Startup })
	defs.Defs = append(defs.Defs, modsettings.Definition{Name: "aaa-sta-flag", Mod: "aaa-lib", Setting: modsettings.Startup, Type: modsettings.TypeBool, Default: val(modsettings.TypeBool), Order: "a", Editable: true})
	fakeDefs(t, defs, nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	body, _ := g.flowForm(id)
	wantIn(t, "origin", body, `class="hint mod-origin">Required by`)
	if strings.Contains(body, `mod-origin">Required by <a`) {
		t.Errorf("the origin links to a group not on the page:\n%s", body)
	}
	if strings.Contains(body, `id="mod-settings-per-user"`) {
		t.Errorf("step 3 shows the per-user hint:\n%s", body)
	}
}

// SPEC 5.6, 17.10: an automatic skip of step 3 lands on step 4 with a note
// saying why, and the step bar marks step 3 as skipped, apart from a step
// that cannot be opened yet. Step 4 opened any other way has no note.
func TestFlowModSettingsSkipSaysWhy(t *testing.T) {
	g := newModsRig(t, false)
	bare := g.world20("Bare")
	g.makeNewMap(bare)
	skipped := newMapPath(bare) + "?" + newMapSkippedQuery + "=1"
	g.wantRedirectTo(skipped, g.do(http.MethodGet, flowModSettingsPath(bare), nil, http.Header{"Cookie": {g.cookie.String()}}))
	get := func(path string) string {
		return g.do(http.MethodGet, path, nil, http.Header{"Cookie": {g.cookie.String()}}).body
	}
	wantIn(t, "skipped step 4", get(skipped), `id="newmap-skipped"`, `class="subnav-later subnav-skipped"`)
	wantIn(t, "skipped step 4 without script", get(skipped+"&"+newMapWaitQuery+"=1"), `id="newmap-skipped"`)
	if strings.Contains(get(newMapPath(bare)), `id="newmap-skipped"`) {
		t.Error("step 4 opened directly shows the skip note")
	}
}

// SPEC 5.6, 9.6: without script step 3 loads as the mod settings page does:
// the loading words show only while a request runs, and a link has the
// server read the form first, with no self-refresh.
func TestFlowModSettingsLoadsWithoutScriptAsTheSettingsPage(t *testing.T) {
	g := newModsRig(t, false)
	id := g.flowWorld()
	fakeDefs(t, fmtestSettingDefs(t), nil)
	body := g.get(flowModSettingsPath(id))
	wantIn(t, "step 3", body, `<div class="wait mod-settings-loading" role="status">`, `<a href="`+flowModSettingsPath(id)+`?wait=1" id="mod-settings-wait">`)
	if strings.Contains(body, `http-equiv="refresh"`) {
		t.Error("step 3 refreshes itself")
	}
}
