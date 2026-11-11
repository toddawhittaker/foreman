package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Tests for the mod settings editor (SPEC 9.6, 9.13, 17.10, 22.3). The
// definitions come from a stand-in for the definitions run; the files are
// ones the real game wrote for the fmtest mod.

func val(t modsettings.ValueType) *modsettings.Value { return &modsettings.Value{Type: t} }

func intVal(n int64) *modsettings.Value {
	v := val(modsettings.TypeInt)
	v.Int = n
	return v
}

func dblVal(f float64) *modsettings.Value {
	v := val(modsettings.TypeDouble)
	v.Double = f
	return v
}

func strVal(s string) modsettings.Value {
	return modsettings.Value{Type: modsettings.TypeString, String: s}
}

// fmtestDefs are the fmtest mod's settings, as a definitions run reports them.
func fmtestDefs() modsettings.Definitions {
	var out modsettings.Definitions
	for _, st := range []struct {
		setting modsettings.SettingType
		prefix  string
	}{{modsettings.Startup, "fm-sta-"}, {modsettings.RuntimeGlobal, "fm-glo-"}, {modsettings.RuntimePerUser, "fm-usr-"}} {
		b := val(modsettings.TypeBool)
		b.Bool = true
		color := val(modsettings.TypeColor)
		color.Color = modsettings.Color{R: 1, G: 0.5, B: 0, A: 1}
		out = append(out,
			modsettings.Definition{Name: st.prefix + "bool", Mod: "fmtest", Setting: st.setting, Type: modsettings.TypeBool, Default: b, Order: "a", Editable: true},
			modsettings.Definition{Name: st.prefix + "int", Mod: "fmtest", Setting: st.setting, Type: modsettings.TypeInt, Default: intVal(5), Min: intVal(1), Max: intVal(10), Order: "b", Editable: true},
			modsettings.Definition{Name: st.prefix + "double", Mod: "fmtest", Setting: st.setting, Type: modsettings.TypeDouble, Default: dblVal(1.5), Min: dblVal(0.5), Max: dblVal(9.5), Order: "c", Editable: true},
			modsettings.Definition{Name: st.prefix + "string", Mod: "fmtest", Setting: st.setting, Type: modsettings.TypeString, Default: &modsettings.Value{Type: modsettings.TypeString, String: "red"},
				AllowedValues: []modsettings.Value{strVal("red"), strVal("green"), strVal("blue")}, Order: "d", Editable: true},
			modsettings.Definition{Name: st.prefix + "color", Mod: "fmtest", Setting: st.setting, Type: modsettings.TypeColor, Default: color, Order: "e", Editable: true},
		)
	}
	return append(out,
		modsettings.Definition{Name: "fm-sta-hidden", Mod: "fmtest", Setting: modsettings.Startup, Type: modsettings.TypeBool, Default: val(modsettings.TypeBool), Hidden: true, Editable: true},
		modsettings.Definition{Name: "fm-sta-forced", Mod: "fmtest", Setting: modsettings.Startup, Type: modsettings.TypeInt, Default: intVal(3), AllowedValues: []modsettings.Value{*intVal(3)}, Editable: true},
		modsettings.Definition{Name: "fm-sta-weird", Mod: "fmtest", Setting: modsettings.Startup, Type: "weird-setting"},
	)
}

func gameFile(t *testing.T, version string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "modsettings", "testdata", "game-"+version+".dat")) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeDefs puts a stand-in for the definitions run in place for the test.
func fakeDefs(t *testing.T, defs *game.ModSettingDefs, err error) {
	t.Helper()
	old := modSettingDefs
	modSettingDefs = func(*game.Controller, context.Context, string) (*game.ModSettingDefs, error) { return defs, err }
	t.Cleanup(func() { modSettingDefs = old })
}

func fmtestSettingDefs(t *testing.T) *game.ModSettingDefs {
	t.Helper()
	return &game.ModSettingDefs{
		Defs:     fmtestDefs(),
		Defaults: gameFile(t, "2.0.77"),
		Locale: modsettings.Locale{"en": {
			"fm-sta-int": {Name: "Startup integer", Description: "An integer from 1 to 10."},
		}},
	}
}

func (g *modsRig) settingsFile(id string) []byte {
	g.t.Helper()
	b, err := os.ReadFile(filepath.Join(g.modsDir(id), world.ModSettingsFile))
	if err != nil {
		g.t.Fatal(err)
	}
	return b
}

// settingValue reads one value from the world's mod-settings.dat.
func (g *modsRig) settingValue(id string, setting modsettings.SettingType, name string) modsettings.Value {
	g.t.Helper()
	f, err := modsettings.Decode(g.settingsFile(id))
	if err != nil {
		g.t.Fatal(err)
	}
	v, ok, err := f.Get(setting, name)
	if err != nil || !ok {
		g.t.Fatalf("%s: %v %v", name, ok, err)
	}
	return v
}

func (g *modsRig) writeSettingsFile(id string, b []byte) {
	g.t.Helper()
	if err := os.WriteFile(filepath.Join(g.modsDir(id), world.ModSettingsFile), b, 0o600); err != nil {
		g.t.Fatal(err)
	}
}

var (
	reMSInput    = regexp.MustCompile(`<input [^>]*>`)
	reMSSelect   = regexp.MustCompile(`(?s)<select id="[^"]*" name="([^"]*)".*?</select>`)
	reMSSelected = regexp.MustCompile(`<option value="([^"]*)" selected>`)
	reMSAttr     = regexp.MustCompile(`\b(name|value|type)="([^"]*)"`)
)

// settingsForm reads the form part and the values a browser would post
// with nothing changed: every field, and a box only when it is ticked.
func (g *modsRig) settingsForm(id string) (string, url.Values) {
	g.t.Helper()
	rep := g.do(http.MethodGet, modSettingsEditPath(id)+"/form", nil, http.Header{"Cookie": {g.cookie.String()}})
	code, body := rep.StatusCode, rep.body
	if code != http.StatusOK {
		g.t.Fatalf("form: %d\n%s", code, body)
	}
	v := url.Values{}
	for _, tag := range reMSInput.FindAllString(body, -1) {
		attrs := map[string]string{}
		for _, m := range reMSAttr.FindAllStringSubmatch(tag, -1) {
			attrs[m[1]] = html.UnescapeString(m[2])
		}
		if attrs["name"] == "" || attrs["name"] == "countdown" || (attrs["type"] == "checkbox" && !strings.Contains(tag, " checked")) {
			continue
		}
		v.Set(attrs["name"], attrs["value"])
	}
	for _, m := range reMSSelect.FindAllStringSubmatch(body, -1) {
		if sel := reMSSelected.FindStringSubmatch(m[0]); sel != nil {
			v.Set(m[1], html.UnescapeString(sel[1]))
		}
	}
	return body, v
}

func fieldOf(name string, setting modsettings.SettingType) string {
	return settingFieldID(modsettings.Definition{Name: name, Setting: setting})
}

// SPEC 9.6: the page links from the Mods tab and loads its form after
// the page, because a cache miss runs the game.
func TestModSettingsPageLoadsTheFormAfter(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, nil, errors.New("must not run with the page"))
	tab := g.get(modsPath(id))
	panel := between(tab, `id="settings-heading"`, `</section>`)
	if i, j := strings.Index(panel, `id="act-settings-edit"`), strings.Index(panel, "mod-settings-replace"); i < 0 || i > j {
		t.Errorf("Edit mod settings is not the panel's first action:\n%s", panel)
	}
	wantIn(t, "tab", tab, `href="/worlds/`+id+`/mods/settings"`)
	code, page := g.getStatus(modSettingsEditPath(id))
	if code != http.StatusOK {
		t.Fatalf("page: %d", code)
	}
	wantIn(t, "page", page, `hx-get="/worlds/`+id+`/mods/settings/form" hx-trigger="load"`, `role="status"`, "Reading the mods&#39; settings.")
	if strings.Contains(page, "<form method=\"post\" action=\"/worlds/"+id+"/mods/settings\"") {
		t.Error("the page drew the form before the definitions were read")
	}
}

// SPEC 9.6, plan R1, R9, R11: one control per startup setting, hidden ones
// left out, a one-value setting locked, an unknown type not editable; map
// settings read-only on a world with a save; per-player settings one line.
func TestModSettingsFormShowsTheSettings(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	body, v := g.settingsForm(id)
	if v.Get("fingerprint") == "" || len(v.Get("seen")) != 64 || v.Get("map") != "" {
		t.Errorf("hidden fields: %v", v)
	}
	intID := fieldOf("fm-sta-int", modsettings.Startup)
	wantIn(t, "form", body,
		`<label for="`+intID+`" class="required">Startup integer</label>`, `required aria-describedby="`+intID+`-hint"`,
		`<div hx-swap-oob="innerHTML:#announcer">Mod settings loaded.</div>`, `inputmode="numeric"`,
		`<input type="text" id="`+intID+`" name="`+intID+`" value="`,
		`<p class="hint" id="`+intID+`-hint">An integer from 1 to 10.</p>`,
		`type="checkbox" id="`+fieldOf("fm-sta-bool", modsettings.Startup)+`"`,
		`id="`+fieldOf("fm-sta-double", modsettings.Startup)+`" name="`+fieldOf("fm-sta-double", modsettings.Startup)+`" value="1.5" inputmode="decimal"`,
		`<select id="`+fieldOf("fm-sta-string", modsettings.Startup)+`"`,
		`type="color" id="`+fieldOf("fm-sta-color", modsettings.Startup)+`"`,
		"The mod allows only this value.", "Foreman cannot edit this kind of setting.",
		"These are the values for a new map.", "Per-player settings are set by each player",
		"Save settings")
	for _, absent := range []string{"fm-sta-hidden", fieldOf("fm-sta-hidden", modsettings.Startup), fieldOf("fm-glo-int", modsettings.RuntimeGlobal), "fm-usr-int"} {
		if strings.Contains(body, absent) {
			t.Errorf("the form shows %s", absent)
		}
	}
	// SPEC 22.3: text the mod has only in English is marked so on a German page.
	de := g.do(http.MethodGet, modSettingsEditPath(id)+"/form", nil, http.Header{"Cookie": {g.cookie.String() + "; " + LangCookie + "=de"}})
	wantIn(t, "German form", de.body, `<label for="`+intID+`" lang="en" class="required">Startup integer</label>`, `id="`+intID+`-hint" lang="en"`)
}

// makeNewMap turns a world into one whose next start makes its map: no
// save and new_map set (SPEC 5.6).
func (g *modsRig) makeNewMap(id string) {
	t := g.t
	t.Helper()
	if err := os.Remove(filepath.Join(g.modsDir(id), "..", world.SavesDir, "base.zip")); err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(g.modsDir(id), "..", "world.json")
	raw, err := os.ReadFile(metaPath) //nolint:gosec // a test temp folder
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	meta["new_map"] = map[string]any{"preset": "default"}
	raw, _ = json.Marshal(meta)
	if err := os.WriteFile(metaPath, raw, 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
}

// Plan R2: a world with no save yet edits its map settings in the same
// form, and a save writes them.
func TestModSettingsNewMapEditsMapSettings(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Fresh", "fmtest_1.0.0")
	g.makeNewMap(id)
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	body, v := g.settingsForm(id)
	if v.Get("map") != "1" {
		t.Error("no map flag on a new map's form")
	}
	wantIn(t, "form", body, `id="`+fieldOf("fm-glo-int", modsettings.RuntimeGlobal)+`"`, "these values go into its new map")
	if strings.Contains(body, "These are the values for a new map.") {
		t.Error("a new map's settings are shown read-only")
	}
	v.Set(fieldOf("fm-glo-int", modsettings.RuntimeGlobal), "8")
	if rep := g.post(modSettingsEditPath(id), v); rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %d\n%s", rep.StatusCode, rep.body)
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if got := g.settingValue(id, modsettings.RuntimeGlobal, "fm-glo-int"); got.Int != 8 {
		t.Errorf("fm-glo-int is %+v, want 8", got)
	}
}

// Plan R1: on a world with a save, map fields in a post are not written,
// and a post that claims they were controls is refused as stale.
func TestModSettingsMapFieldsOfASavedWorld(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	before := gameFile(t, "2.0.77")
	g.writeSettingsFile(id, before)
	_, v := g.settingsForm(id)
	v.Set(fieldOf("fm-glo-int", modsettings.RuntimeGlobal), "8")
	if rep := g.post(modSettingsEditPath(id), v); rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, "You changed no setting") {
		t.Errorf("a map field on a saved world: %d", rep.StatusCode)
	}
	v.Set("map", "1")
	if rep := g.post(modSettingsEditPath(id), v); rep.StatusCode != http.StatusConflict {
		t.Errorf("a map flag on a saved world: %d", rep.StatusCode)
	}
	if string(g.settingsFile(id)) != string(before) || g.modSnapshots(id) != 0 {
		t.Error("a map field changed a saved world")
	}
}

// SPEC 9.6, 9.13, plan R8: a save writes only the changed value, after a
// snapshot, and the audit names the setting, never its value.
func TestModSettingsSaveWritesTheChange(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	before := gameFile(t, "2.0.77")
	g.writeSettingsFile(id, before)
	_, v := g.settingsForm(id)
	v.Set(fieldOf("fm-sta-int", modsettings.Startup), "9")
	rep := g.post(modSettingsEditPath(id), v)
	if rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %d\n%s", rep.StatusCode, rep.body)
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	f, err := modsettings.Decode(g.settingsFile(id))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok, err := f.Get(modsettings.Startup, "fm-sta-int"); err != nil || !ok || got.Int != 9 {
		t.Errorf("fm-sta-int is %+v %v %v, want 9", got, ok, err)
	}
	old, _ := modsettings.Decode(before)
	for _, name := range []string{"fm-sta-bool", "fm-sta-double", "fm-sta-string", "fm-sta-color"} {
		a, _, _ := old.Get(modsettings.Startup, name)
		b, _, _ := f.Get(modsettings.Startup, name)
		if a != b {
			t.Errorf("%s changed: %+v to %+v", name, a, b)
		}
	}
	if g.modSnapshots(id) != 1 {
		t.Error("no snapshot before the change")
	}
	found := false
	for _, a := range g.audit() {
		if a.Action == game.AuditMods {
			found = true
			if !strings.Contains(a.Detail, "fm-sta-int") || strings.Contains(a.Detail, "9") {
				t.Errorf("audit details %q", a.Detail)
			}
		}
	}
	if !found {
		t.Error("no audit entry")
	}
}

// Plan R9, DESIGN 7.2: a refused value comes back in its field with its
// error, the summary links to it, and nothing is written.
func TestModSettingsRefusesABadValue(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	before := gameFile(t, "2.0.77")
	g.writeSettingsFile(id, before)
	_, v := g.settingsForm(id)
	intID := fieldOf("fm-sta-int", modsettings.Startup)
	v.Set(intID, "42")
	rep := g.post(modSettingsEditPath(id), v)
	if rep.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", rep.StatusCode)
	}
	wantIn(t, "refusal", rep.body, `id="error-summary"`, `href="#`+intID+`"`, `value="42"`,
		`aria-describedby="`+intID+`-hint `+intID+`-error" aria-invalid="true"`,
		"Enter a value from 1 to 10 for Startup integer.")
	if string(g.settingsFile(id)) != string(before) || g.modSnapshots(id) != 0 {
		t.Error("a refused value changed the world")
	}
}

// SPEC 9.13: a form drawn from another file saves nothing and shows the
// current values.
func TestModSettingsStaleFormSavesNothing(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	_, v := g.settingsForm(id)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	v.Set(fieldOf("fm-sta-int", modsettings.Startup), "9")
	rep := g.post(modSettingsEditPath(id), v)
	if rep.StatusCode != http.StatusConflict {
		t.Fatalf("status %d", rep.StatusCode)
	}
	wantIn(t, "stale", rep.body, "The settings changed since you opened this page")
	if g.modSnapshots(id) != 0 {
		t.Error("a stale form changed the world")
	}
}

// A form posted with nothing changed says so and changes nothing.
func TestModSettingsNoChange(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	_, v := g.settingsForm(id)
	rep := g.post(modSettingsEditPath(id), v)
	if rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, "You changed no setting") || g.modSnapshots(id) != 0 {
		t.Errorf("no change: %d", rep.StatusCode)
	}
}

// SPEC 9.6: each failure of the definitions run says what happened and
// offers the file download and upload, which still work.
func TestModSettingsRunFailures(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	g.writeSettingsFile(id, []byte("x"))
	for _, c := range []struct {
		err  error
		want string
	}{
		{game.ErrModSettingsImageMissing, "Foreman needs it to read the mods&#39; settings."},
		{game.ErrModSettingsOutOfMemory, "ran out of memory, so Foreman stopped it."},
		{game.ErrModSettingsTimeout, "took too long, so Foreman stopped it."},
		{fmt.Errorf("run: %w: /host/path mod text", game.ErrModSettingsBadOutput), "The game did not report the mods&#39; settings"},
		{fmt.Errorf("lock: %w", world.ErrBusy), "Another operation is running on this world."},
		{game.ErrPreviewBusy, "Another map preview or check is using the game."},
		{fmt.Errorf("link: %w", world.ErrModsTooBig), "too many or too large for Foreman"},
		{context.Canceled, "Foreman could not read the mods&#39; settings."},
		{errors.New("boom"), "Foreman could not read the mods&#39; settings."},
	} {
		fakeDefs(t, nil, c.err)
		body, _ := g.settingsForm(id)
		wantIn(t, c.want, body, c.want, `id="act-settings-download"`, `href="/worlds/`+id+`/mods#mod-settings-replace"`, `id="act-retry"`,
			`<div hx-swap-oob="innerHTML:#announcer">`)
		for _, leak := range []string{"boom", "/host/path", "mod text", "context canceled"} {
			if strings.Contains(body, leak) {
				t.Errorf("%q reached the page", leak)
			}
		}
	}
}

// Every mod settings route needs a signed-in admin.
func TestModSettingsRoutesNeedSignIn(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base")
	for _, p := range []string{modSettingsEditPath(id), modSettingsEditPath(id) + "/form"} {
		if rep := g.do(http.MethodGet, p, nil, nil); rep.StatusCode != http.StatusSeeOther && rep.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s answered %d to a stranger", p, rep.StatusCode)
		}
	}
	rep := g.do(http.MethodPost, modSettingsEditPath(id), url.Values{}, nil)
	if rep.StatusCode != http.StatusSeeOther && rep.StatusCode != http.StatusUnauthorized && rep.StatusCode != http.StatusForbidden {
		t.Errorf("post answered %d to a stranger", rep.StatusCode)
	}
}

func TestColorRaw(t *testing.T) {
	if got, ok := colorRaw("#ff8000", "0.5"); !ok || got != "1,0.5019607843137255,0,0.5" {
		t.Errorf("colorRaw = %q %v", got, ok)
	}
	for _, bad := range []string{"ff8000", "#ff80", "#gg0000", ""} {
		if _, ok := colorRaw(bad, "1"); ok {
			t.Errorf("colorRaw(%q) accepted", bad)
		}
	}
	if got := colorHex(modsettings.Color{R: 255, G: 128, B: 0, A: 255}); got != "#ff8000" {
		t.Errorf("a 0 to 255 color reads as %s", got)
	}
}

// modSettingsPageCases render the editor in every language (TestEveryPageRenders).
func modSettingsPageCases() []pageCase {
	w := world.Summary{ID: "base", Name: longName, GameVersion: "2.0.77", Active: true}
	defs := &game.ModSettingDefs{Defs: fmtestDefs(), Locale: modsettings.Locale{"en": {"fm-sta-int": {Name: "Startup integer", Description: longName}}}}
	startup, mapGroups, perUser := settingGroups(defs, nil, nil, "en")
	for i, f := range startup[0].Fields {
		if f.Name == "fm-sta-int" {
			startup[0].Fields[i].Error = i18n.M("mods.settings.edit.err.range", "min", "1", "max", "10", "setting", "Startup integer")
		}
	}
	base := ModSettingsPage{World: w, Loaded: true, Startup: startup, Map: mapGroups, PerUser: perUser, HasFile: true, Fingerprint: "f", Seen: "s", CountdownForm: defaultCountdown()}
	running := base
	running.Running, running.NewMap = true, true
	return []pageCase{
		{"mod settings loading", "todd", 200, ModSettingsPage{World: w}},
		{"mod settings", "todd", 200, base},
		{"mod settings new map running", "todd", 200, running},
		{"mod settings run error", "todd", 200, ModSettingsPage{World: w, Loaded: true, HasFile: true, RunError: i18n.M("mods.settings.edit.run.no_image", "version", "2.0.77"), ImageLink: PathServer}},
		{"mod settings empty", "todd", 200, ModSettingsPage{World: w, Loaded: true, Changed: true}},
		{"mod settings no change", "todd", 200, ModSettingsPage{World: w, Loaded: true, NoChange: true}},
		{"mod settings busy", "todd", 200, ModSettingsPage{World: w, Loaded: true, Busy: i18n.M("mods.settings.edit.run.busy")}},
	}
}

// SPEC 9.6, plan R8: a world with no mod-settings.dat gets one built from
// the defaults file, with the change in it.
func TestModSettingsSaveWithoutAFile(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	_, v := g.settingsForm(id)
	v.Set(fieldOf("fm-sta-int", modsettings.Startup), "9")
	if rep := g.post(modSettingsEditPath(id), v); rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %d\n%s", rep.StatusCode, rep.body)
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if got := g.settingValue(id, modsettings.Startup, "fm-sta-int"); got.Int != 9 {
		t.Errorf("fm-sta-int is %+v, want 9", got)
	}
}

// Factorio 1.1 stores an int setting as a double: the form shows it whole,
// an untouched form changes nothing, and a change is written as a double.
func TestModSettingsFactorio11File(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	defs := fmtestSettingDefs(t)
	defs.Defaults = gameFile(t, "1.1.110")
	fakeDefs(t, defs, nil)
	g.writeSettingsFile(id, gameFile(t, "1.1.110"))
	intID := fieldOf("fm-sta-int", modsettings.Startup)
	_, v := g.settingsForm(id)
	if got := v.Get(intID); got == "" || strings.ContainsAny(got, ".e") {
		t.Errorf("the int shows as %q", got)
	}
	if rep := g.post(modSettingsEditPath(id), v); rep.StatusCode != http.StatusOK || !strings.Contains(rep.body, "You changed no setting") {
		t.Fatalf("untouched 1.1 form: %d", rep.StatusCode)
	}
	v.Set(intID, "9")
	if rep := g.post(modSettingsEditPath(id), v); rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %d\n%s", rep.StatusCode, rep.body)
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if got := g.settingValue(id, modsettings.Startup, "fm-sta-int"); got.Type != modsettings.TypeDouble || got.Double != 9 {
		t.Errorf("fm-sta-int is %+v, want the double 9", got)
	}
}

// Each control posts its change: a box unticked, a choice from a list, and
// a color's opacity alone.
func TestModSettingsEachControlChanges(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	_, v := g.settingsForm(id)
	boolID, strID, colorID := fieldOf("fm-sta-bool", modsettings.Startup), fieldOf("fm-sta-string", modsettings.Startup), fieldOf("fm-sta-color", modsettings.Startup)
	if v.Get(boolID) != "true" {
		t.Fatalf("the box is not ticked: %v", v)
	}
	v.Del(boolID)
	v.Set(strID, "blue")
	v.Set(colorID+"-alpha", "0.25")
	if rep := g.post(modSettingsEditPath(id), v); rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %d\n%s", rep.StatusCode, rep.body)
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if got := g.settingValue(id, modsettings.Startup, "fm-sta-bool"); got.Bool {
		t.Error("the box is still on")
	}
	if got := g.settingValue(id, modsettings.Startup, "fm-sta-string"); got.String != "blue" {
		t.Errorf("the choice is %q", got.String)
	}
	if got := g.settingValue(id, modsettings.Startup, "fm-sta-color"); got.Color.A != 0.25 || got.Color.R != 1 {
		t.Errorf("the color is %+v", got.Color)
	}
}

// SPEC 9.13, 17.8: on a running world the form offers the countdown and a
// save restarts the game; a form drawn while it was stopped is refused.
func TestModSettingsRunningWorld(t *testing.T) {
	g := newModsRig(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	_, stopped := g.settingsForm(id)
	g.startGame(id)

	stopped.Set(fieldOf("fm-sta-int", modsettings.Startup), "9")
	if rep := g.post(modSettingsEditPath(id), stopped); rep.StatusCode != http.StatusConflict || !strings.Contains(rep.body, "Save and restart") {
		t.Errorf("a form drawn while stopped: %d", rep.StatusCode)
	}
	body, v := g.settingsForm(id)
	wantIn(t, "running form", body, `name="running" value="1"`, `id="countdown"`, "Save and restart")
	v.Set(fieldOf("fm-sta-int", modsettings.Startup), "9")
	v.Set("countdown", "0")
	if rep := g.post(modSettingsEditPath(id), v); rep.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: %d\n%s", rep.StatusCode, rep.body)
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	g.waitPage(running)
	if got := g.settingValue(id, modsettings.Startup, "fm-sta-int"); got.Int != 9 {
		t.Errorf("fm-sta-int is %+v, want 9", got)
	}
}

// A text value that holds a line break posts without it from an untouched
// field, which is not a change.
func TestReadFieldIgnoresDroppedLineBreaks(t *testing.T) {
	f := SettingField{ID: "x", Kind: kindText, Value: "a\r\nb"}
	r, _ := http.NewRequest(http.MethodPost, "/", strings.NewReader("x=ab"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, changed := readField(r, &f); changed || !f.Error.IsZero() {
		t.Error("an untouched text field with a line break counted as a change")
	}
}

func TestDecimalPoint(t *testing.T) {
	for _, c := range []struct{ kind, in, want string }{
		{kindDouble, "2,5", "2.5"}, {kindDouble, "2.5", "2.5"}, {kindDouble, "1,000.5", "1,000.5"},
		{kindDouble, "1,2,3", "1,2,3"}, {kindInt, "2,5", "2,5"}, {kindText, "a,b", "a,b"},
	} {
		if got := decimalPoint(c.kind, c.in); got != c.want {
			t.Errorf("decimalPoint(%s, %q) = %q, want %q", c.kind, c.in, got, c.want)
		}
	}
}

// A range refusal with no limit at all never shows an empty placeholder.
func TestFieldErrorRangeWithoutLimits(t *testing.T) {
	f := &SettingField{Label: "Big"}
	if got := fieldError(f, &modsettings.ValidationError{MessageID: "mods.settings.edit.err.range"}); got.Key != "mods.settings.edit.err.number" {
		t.Errorf("key %s", got.Key)
	}
	if got := fieldError(f, &modsettings.ValidationError{MessageID: "mods.settings.edit.err.range", Params: map[string]string{"max": "3"}}); got.Key != "mods.settings.edit.err.range_max" {
		t.Errorf("key %s", got.Key)
	}
}
