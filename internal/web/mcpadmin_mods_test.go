package web

import (
	"context"
	"html"
	"maps"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/modsettings"
)

// Tests for the mods and mod settings tools (SPEC 21.7, 21.8, 9). The portal
// is the Go fake the Mods tab's tests use.

// modsMCP is a mods rig with an admin token named Claude connected.
func modsMCP(t *testing.T, withAccount bool) (*modsRig, *mcp.ClientSession) {
	t.Helper()
	return modsMCPWrapped(t, withAccount, nil)
}

func modsMCPWrapped(t *testing.T, withAccount bool, wrap func(ModPortal) ModPortal) (*modsRig, *mcp.ClientSession) {
	t.Helper()
	g := newModsRigWrapped(t, withAccount, wrap)
	u, _, err := g.store.UserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	turnMCPOn(t, g.harness)
	secret, _ := apiToken(t, g.harness, u, "Claude", auth.ScopeAdmin)
	return g, mcpConnect(t, g.url, secret)
}

// uncachedPortal asks the fake portal afresh for every mod, so a release
// added between two calls shows in the second.
type uncachedPortal struct {
	ModPortal
	base string
}

func (p *uncachedPortal) Mod(ctx context.Context, name string) (modportal.Mod, error) {
	return modportal.NewForTest(p.base).Mod(ctx, name)
}

func (p *uncachedPortal) Mods(ctx context.Context, names []string) map[string]modportal.ModResult {
	return modportal.NewForTest(p.base).Mods(ctx, names)
}

// unchanged is the data folder and the audit log, to check a first call
// changed nothing.
type unchanged struct {
	tree  map[string]string
	audit int
}

func (g *modsRig) state() unchanged {
	g.t.Helper()
	return unchanged{tree: dataTree(g.t, g.worlds.Dirs().Worlds), audit: len(g.audit())}
}

func (g *modsRig) wantUnchanged(was unchanged) {
	g.t.Helper()
	if now := g.state(); now.audit != was.audit || !maps.Equal(now.tree, was.tree) {
		g.t.Errorf("a preview changed the data folder or the audit log (%d audit rows, were %d)", now.audit, was.audit)
	}
}

var reAnyTag = regexp.MustCompile(`<[^>]*>`)

// pageWords is a page's text with tags gone, as a reader sees it.
func pageWords(body string) string {
	return strings.Join(strings.Fields(html.UnescapeString(reAnyTag.ReplaceAllString(body, ""))), " ")
}

// wantWillOnPage checks every will sentence of a preview is on the UI's
// confirm page (SPEC 21.8).
func wantWillOnPage(t *testing.T, preview map[string]any, body string) {
	t.Helper()
	will, _ := preview["will"].([]any)
	if len(will) == 0 {
		t.Fatalf("no will sentences: %s", asJSON(preview))
	}
	words := pageWords(body)
	for _, w := range will {
		if !strings.Contains(words, w.(string)) {
			t.Errorf("the page does not say %q", w)
		}
	}
}

func wantNoToken(t *testing.T, g *modsRig, outs ...any) {
	t.Helper()
	for _, o := range outs {
		if strings.Contains(asJSON(o), modportaltest.Token) {
			t.Error("a result holds the factorio.com token")
		}
	}
	if strings.Contains(g.logs.String(), modportaltest.Token) {
		t.Error("the log holds the factorio.com token")
	}
}

// SPEC 21.8, 14.12: the two-step tools are marked destructive, the others
// not, and each description names the untrusted fields.
func TestMCPModToolsAnnotations(t *testing.T) {
	_, cs := modsMCP(t, true)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"search_mods": false, "change_mods": true, "list_mod_settings": false, "change_mod_settings": true}
	for _, tool := range res.Tools {
		two, ok := want[tool.Name]
		if !ok {
			continue
		}
		delete(want, tool.Name)
		if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint != two {
			t.Errorf("%s's destructive hint is not %v", tool.Name, two)
		}
		if !strings.Contains(tool.Description, "untrusted_") {
			t.Errorf("%s's description names no untrusted field", tool.Name)
		}
	}
	if len(want) > 0 {
		t.Errorf("missing tools: %v", want)
	}
}

// SPEC 9.1, 21.7: search_mods finds what the Mods tab's search finds, with
// portal text in untrusted fields, and is refused as the page refuses.
func TestMCPSearchMods(t *testing.T) {
	g, cs := modsMCP(t, false)
	id := g.world20("Base")
	res, out := call(t, cs, "search_mods", map[string]any{"world_id": id, "query": "jetpack"})
	if res.IsError || !strings.Contains(asJSON(out), `"untrusted_name":"jetpack"`) || out["no_account"] != true {
		t.Fatalf("search: %s %s", resultText(res), asJSON(out))
	}
	if res, _ := call(t, cs, "search_mods", map[string]any{"world_id": id, "query": " "}); !res.IsError || !strings.Contains(resultText(res), "Enter part of a mod") {
		t.Errorf("empty search: %s", resultText(res))
	}
	if res, _ := call(t, cs, "search_mods", map[string]any{"query": "jetpack"}); !res.IsError || !strings.Contains(resultText(res), "world_id") {
		t.Errorf("no world: %s", resultText(res))
	}
}

// SPEC 9.2, 21.8, 12.9: change_mods' first call changes nothing and shows
// the plan page's steps; the second installs as the page does, audited with
// the token's name, with the snapshot first.
func TestMCPChangeModsInstall(t *testing.T) {
	g, cs := modsMCP(t, true)
	g.portal.AddZip("alpha", "1.0.0", "beta >= 1.0.0")
	g.portal.AddZip("beta", "1.0.0")
	id := g.world20("Base")
	args := map[string]any{"world_id": id, "kind": "install", "names": []string{"alpha"}}

	was := g.state()
	res, preview := call(t, cs, "change_mods", args)
	if res.IsError || preview["done"] != false || preview["snapshot"] != "Before mod change" || preview["confirm_code"] == nil {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(preview))
	}
	wantIn(t, "preview", asJSON(preview), `"untrusted_name":"alpha"`, `"untrusted_name":"beta"`, "Download 2 mods")
	g.wantUnchanged(was)
	_, page := g.getStatus(kindPlanPath(id, mods.KindInstall, "alpha"))
	wantWillOnPage(t, preview, page)

	res, out := call(t, cs, "change_mods", withCode(args, preview["confirm_code"].(string)))
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if !g.hasFile(id, "alpha_1.0.0.zip") || !g.hasFile(id, "beta_1.0.0.zip") || g.modSnapshots(id) != 1 {
		t.Errorf("not installed with its snapshot: %v", dirNames(t, g.modsDir(id)))
	}
	if len(g.auditByToken(game.AuditMods)) != 1 {
		t.Errorf("audit: %+v", g.audit())
	}
	wantNoToken(t, g, preview, out)
}

// SPEC 21.8, 17.8: a plan that moved between the calls, as with a new
// release on the portal, is refused with a fresh plan and code.
func TestMCPChangeModsMovedPlan(t *testing.T) {
	fresh := &uncachedPortal{}
	g, cs := modsMCPWrapped(t, true, func(p ModPortal) ModPortal { fresh.ModPortal = p; return fresh })
	fresh.base = g.portal.URL
	g.portal.AddZip("alpha", "1.0.0")
	id := g.world20("Base", "alpha_0.9.0")
	args := map[string]any{"world_id": id, "kind": "update", "names": []string{"alpha"}}
	code := askCode(t, cs, "change_mods", args)
	g.portal.AddZip("alpha", "1.1.0")
	res, out := call(t, cs, "change_mods", withCode(args, code))
	again, _ := out[confirmCodeKey].(string)
	if !res.IsError || out["done"] != false || again == "" || again == code || !strings.Contains(resultText(res), "changed since the preview") {
		t.Fatalf("moved: %s %s", resultText(res), asJSON(out))
	}
	if !strings.Contains(asJSON(out), `"to":"1.1.0"`) || g.modSnapshots(id) != 0 || len(g.auditByToken(game.AuditMods)) != 0 {
		t.Fatalf("the fresh plan or the world: %s", asJSON(out))
	}
	// The mods folder moving is refused the same way.
	g.writeMod(id, "gamma", "1.0.0")
	if res, out := call(t, cs, "change_mods", withCode(args, again)); !res.IsError || out[confirmCodeKey] == nil {
		t.Fatalf("a moved folder: %s %s", resultText(res), asJSON(out))
	} else {
		again = out[confirmCodeKey].(string)
	}
	if res, out := call(t, cs, "change_mods", withCode(args, again)); res.IsError || out["done"] != true {
		t.Fatalf("the fresh code: %s %s", resultText(res), asJSON(out))
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if !g.hasFile(id, "alpha_1.1.0.zip") {
		t.Errorf("not updated: %v", dirNames(t, g.modsDir(id)))
	}
}

// SPEC 9.2, 9.11, 17.8: change_mods is refused for the reasons the plan
// page shows no button, with no code.
func TestMCPChangeModsRefusals(t *testing.T) {
	g, cs := modsMCP(t, false)
	g.portal.AddZip("alpha", "1.0.0", "! beta")
	g.portal.AddZip("gamma", "1.0.0")
	id := g.world20("Base", "beta_1.0.0")
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"conflict", map[string]any{"world_id": id, "kind": "install", "names": []string{"alpha"}}, "conflicts"},
		{"no account", map[string]any{"world_id": id, "kind": "install", "names": []string{"gamma"}}, "factorio.com account"},
		{"upload", map[string]any{"world_id": id, "kind": "upload"}, "web UI"},
		{"bad kind", map[string]any{"world_id": id, "kind": "explode"}, "kind must be"},
		{"bad choice", map[string]any{"world_id": id, "kind": "sync", "choices": map[string]any{"x": "upload"}}, "nearest or without"},
		{"no world", map[string]any{"kind": "disable", "names": []string{"beta"}}, "world_id"},
		{"bad countdown", map[string]any{"world_id": id, "kind": "disable", "names": []string{"beta"}, "countdown_seconds": 9999}, "countdown_seconds"},
	}
	was := g.state()
	for _, c := range cases {
		res, out := call(t, cs, "change_mods", c.args)
		if !res.IsError || out[confirmCodeKey] != nil || !strings.Contains(resultText(res), c.want) {
			t.Errorf("%s: %s %s", c.name, resultText(res), asJSON(out))
		}
	}
	g.wantUnchanged(was)
	if res, out := call(t, cs, "change_mods", map[string]any{"world_id": id, "kind": "install", "names": []string{"alpha"}}); !strings.Contains(asJSON(out), "cannot run together with beta") {
		t.Errorf("the conflict is not listed: %s %s", resultText(res), asJSON(out))
	}
}

// SPEC 9.4, 21.8: on a running world the preview says the game stops and
// starts again and lists the drops; the code stands for the drops' box.
func TestMCPChangeModsDropsOnARunningWorld(t *testing.T) {
	g, cs := modsMCP(t, true)
	id := g.world20("Base", "alpha_1.0.0")
	g.startGame(id)
	args := map[string]any{"world_id": id, "kind": "disable", "names": []string{"alpha"}, "countdown_seconds": 0}
	was := g.state()
	res, preview := call(t, cs, "change_mods", args)
	if res.IsError || preview["running"] != true {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(preview))
	}
	wantIn(t, "preview", asJSON(preview), "save and stop Base", "then start Base again")
	g.wantUnchanged(was)
	_, page := g.getStatus(kindPlanPath(id, mods.KindDisable, "alpha"))
	wantWillOnPage(t, preview, page)
	res, out := call(t, cs, "change_mods", withCode(args, preview[confirmCodeKey].(string)))
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	g.waitPage(running)
	if len(g.auditByToken(game.AuditMods)) != 1 {
		t.Errorf("audit: %+v", g.audit())
	}
}

// SPEC 9.4: a mod the loading save uses is listed in untrusted_drops, and
// confirming accepts the drop.
func TestMCPChangeModsDrops(t *testing.T) {
	g, cs := modsMCP(t, true)
	id, err := g.worlds.Create(context.Background(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	g.copySave(id, "2.1.24-space-age-testmod.zip")
	g.writeMod(id, "foreman-test", "0.1.0")
	args := map[string]any{"world_id": id, "kind": "disable", "names": []string{"foreman-test"}}
	preview, res, out := confirmCall(t, cs, "change_mods", args)
	if !strings.Contains(asJSON(preview["untrusted_drops"]), "foreman-test") {
		t.Errorf("drops: %s", asJSON(preview))
	}
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
}

// SPEC 9.6: list_mod_settings shows the editor's settings and values.
func TestMCPListModSettings(t *testing.T) {
	g, cs := modsMCP(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	res, out := call(t, cs, "list_mod_settings", map[string]any{"world_id": id})
	if res.IsError || out["new_map"] != false {
		t.Fatalf("list: %s %s", resultText(res), asJSON(out))
	}
	wantIn(t, "list", asJSON(out), `"untrusted_name":"fm-sta-int"`, `"untrusted_label":"Startup integer"`, `"untrusted_name":"fm-glo-int"`)
	if strings.Contains(asJSON(out), "fm-usr-") || strings.Contains(asJSON(out), "fm-sta-hidden") {
		t.Errorf("per-player or hidden settings listed: %s", asJSON(out))
	}
}

// SPEC 9.6, 21.5: the definitions run is waited for only so long; it goes
// on, and the call says to come back.
func TestMCPListModSettingsBoundedWait(t *testing.T) {
	g, cs := modsMCP(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	was := mcpWait
	mcpWait = 50 * time.Millisecond
	t.Cleanup(func() { mcpWait = was })
	old := modSettingDefs
	modSettingDefs = func(_ *game.Controller, ctx context.Context, _ string) (*game.ModSettingDefs, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	t.Cleanup(func() { modSettingDefs = old })
	if res, _ := call(t, cs, "list_mod_settings", map[string]any{"world_id": id}); !res.IsError || !strings.Contains(resultText(res), "call again") {
		t.Errorf("slow run: %s", resultText(res))
	}
}

// SPEC 9.6, 21.8, 12.9: change_mod_settings' first call changes nothing and
// says what the editor says saving does; the second writes the value after
// a snapshot, audited with the token's name and without the value.
func TestMCPChangeModSettings(t *testing.T) {
	g, cs := modsMCP(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	args := map[string]any{"world_id": id, "values": map[string]any{"fm-sta-int": "9", "fm-sta-bool": "true"}}

	was := g.state()
	res, preview := call(t, cs, "change_mod_settings", args)
	if res.IsError || preview["done"] != false || preview["snapshot"] != "Before mod change" || preview[confirmCodeKey] == nil {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(preview))
	}
	// The box is already ticked, so only the int changes.
	wantIn(t, "preview", asJSON(preview), `"untrusted_name":"fm-sta-int"`, `"untrusted_to":"9"`)
	if strings.Contains(asJSON(preview), `"untrusted_name":"fm-sta-bool"`) {
		t.Errorf("an unchanged value is listed: %s", asJSON(preview))
	}
	g.wantUnchanged(was)
	page, _ := g.settingsForm(id)
	wantWillOnPage(t, preview, page)

	res, out := call(t, cs, "change_mod_settings", withCode(args, preview[confirmCodeKey].(string)))
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if err := g.waitResult(id, game.AuditMods); err != nil {
		t.Fatal(err)
	}
	if got := g.settingValue(id, modsettings.Startup, "fm-sta-int"); got.Int != 9 || g.modSnapshots(id) != 1 {
		t.Errorf("fm-sta-int is %+v with %d snapshots", got, g.modSnapshots(id))
	}
	rows := g.auditByToken(game.AuditMods)
	if len(rows) != 1 || !strings.Contains(rows[0].Detail, "fm-sta-int") || strings.Contains(rows[0].Detail, "9") {
		t.Errorf("audit: %+v", rows)
	}
}

// SPEC 21.8, 17.8: a settings file that changed, or a game that started,
// between the calls is refused with a fresh preview and code.
func TestMCPChangeModSettingsMoved(t *testing.T) {
	g, cs := modsMCP(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	args := map[string]any{"world_id": id, "values": map[string]any{"fm-sta-int": "9"}}
	code := askCode(t, cs, "change_mod_settings", args)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	res, out := call(t, cs, "change_mod_settings", withCode(args, code))
	fresh, _ := out[confirmCodeKey].(string)
	if !res.IsError || fresh == "" || fresh == code || !strings.Contains(resultText(res), "changed since the preview") {
		t.Fatalf("moved file: %s %s", resultText(res), asJSON(out))
	}
	g.startGame(id)
	res, out = call(t, cs, "change_mod_settings", withCode(args, fresh))
	if !res.IsError || out[confirmCodeKey] == nil || g.modSnapshots(id) != 0 {
		t.Fatalf("a game that started: %s %s", resultText(res), asJSON(out))
	}
	wantIn(t, "running preview", asJSON(out), "restarts the server")
}

// SPEC 9.6: values are refused as the editor refuses them, and names it
// offers no control for are refused.
func TestMCPChangeModSettingsRefusals(t *testing.T) {
	g, cs := modsMCP(t, false)
	id := g.world20("Base", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(id, gameFile(t, "2.0.77"))
	was := g.state()
	cases := []struct {
		values map[string]any
		want   string
	}{
		{map[string]any{"fm-sta-int": "42"}, "untrusted_refused"},
		{map[string]any{"fm-glo-int": "2"}, "map setting"},
		{map[string]any{"fm-sta-forced": "3"}, "cannot be changed"},
		{map[string]any{"nope": "1"}, "list_mod_settings"},
		{map[string]any{}, "values is required"},
	}
	for _, c := range cases {
		res, out := call(t, cs, "change_mod_settings", map[string]any{"world_id": id, "values": c.values})
		if !res.IsError || out[confirmCodeKey] != nil || !strings.Contains(resultText(res), c.want) {
			t.Errorf("%v: %s %s", c.values, resultText(res), asJSON(out))
		}
	}
	_, out := call(t, cs, "change_mod_settings", map[string]any{"world_id": id, "values": map[string]any{"fm-sta-int": "42"}})
	wantIn(t, "refused", asJSON(out), "Enter a value from 1 to 10 for Startup integer.")
	if res, out := call(t, cs, "change_mod_settings", map[string]any{"world_id": id, "values": map[string]any{"fm-sta-int": "7"}}); res.IsError || out[confirmCodeKey] != nil {
		t.Errorf("no change: %s %s", resultText(res), asJSON(out))
	}
	g.wantUnchanged(was)
}
