package web

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The SPEC 18.10b gate for the admin tools: every two-step tool's first
// call changes nothing and names the snapshot the UI would take; a code
// works only once, for its own token, tool and arguments, within 5 minutes;
// no code or secret reaches a result or the log; every admin tool carries
// its hint; and the operate and read scopes do not see the admin tools.

// gateCase is one two-step tool set up where it can act: the client, the
// arguments, the snapshot label the UI takes for the same action ("" for
// none), and the secrets the rig holds. g is the rig's fake game, nil on a
// rig without one; still fails the test when the row the tool would delete
// from the database is gone.
type gateCase struct {
	hn       *harness
	g        *gameRig
	cs       *mcp.ClientSession
	args     map[string]any
	snapshot string
	secrets  []string
	still    func(t *testing.T)
}

// liveGate is a running world with bob online and both game secrets set.
func liveGate(t *testing.T, args map[string]any) gateCase {
	g, cs, id := liveRig(t)
	rconPW, gamePW := g.secrets(id)
	args["world_id"] = id
	return gateCase{hn: g.harness, g: g, cs: cs, args: args, secrets: []string{rconPW, gamePW}}
}

var factorioSecrets = []string{modportaltest.Token, modportaltest.Username}

// newGamePassword is a made-up game password a test sets, to look for.
const newGamePassword = "new-pw-Kx52-unlikely" //nolint:gosec // not a real credential

// gateCases sets up each two-step tool; the gate fails for a two-step tool
// missing here, so a new one cannot skip the gate.
var gateCases = map[string]func(t *testing.T) gateCase{
	"force_stop":          func(t *testing.T) gateCase { return liveGate(t, map[string]any{}) },
	"kick_player":         func(t *testing.T) gateCase { return liveGate(t, map[string]any{"name": "bob", "reason": "AFK"}) },
	"ban_player":          func(t *testing.T) gateCase { return liveGate(t, map[string]any{"name": "bob", "reason": "grief"}) },
	"run_console_command": func(t *testing.T) gateCase { return liveGate(t, map[string]any{"command": "/time"}) },
	"restore_snapshot": func(t *testing.T) gateCase {
		g, cs, id, snap, _ := adminRig(t)
		return gateCase{hn: g.harness, g: g, cs: cs, args: map[string]any{"world_id": id, "snapshot": snap.Name}, snapshot: world.LabelBeforeRestore}
	},
	"delete_snapshots": func(t *testing.T) gateCase {
		g, cs, id, snap, _ := adminRig(t)
		return gateCase{hn: g.harness, g: g, cs: cs, args: map[string]any{"world_id": id, "snapshots": []string{snap.Name}}}
	},
	"change_mods": func(t *testing.T) gateCase {
		g, cs := modsMCP(t, true)
		g.portal.AddZip("alpha", "1.0.0")
		id := g.world20("Base")
		return gateCase{hn: g.harness, g: g.gameRig, cs: cs, args: map[string]any{"world_id": id, "kind": "install", "names": []string{"alpha"}},
			snapshot: world.LabelBeforeMods, secrets: factorioSecrets}
	},
	"change_mod_settings": func(t *testing.T) gateCase {
		g, cs := modsMCP(t, true)
		id := g.world20("Base", "fmtest_1.0.0")
		fakeDefs(t, fmtestSettingDefs(t), nil)
		g.writeSettingsFile(id, gameFile(t, "2.0.77"))
		return gateCase{hn: g.harness, g: g.gameRig, cs: cs, args: map[string]any{"world_id": id, "values": map[string]any{"fm-sta-int": "9"}},
			snapshot: world.LabelBeforeMods, secrets: factorioSecrets}
	},
	"change_game_settings": func(t *testing.T) gateCase {
		g, cs := mcpRig(t, auth.ScopeAdmin, image21)
		id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
		rconPW, gamePW := g.secrets(id)
		return gateCase{hn: g.harness, g: g, cs: cs, args: map[string]any{"world_id": id, "settings": map[string]any{"max_players": 20},
			"game_password": newGamePassword}, snapshot: world.LabelBeforeSettings, secrets: []string{rconPW, gamePW, newGamePassword}}
	},
	"update_game_version": func(t *testing.T) gateCase {
		g, cs := t5VersionRig(t)
		id := g.newWorld("Base", fixtureNoSA, false)
		g.addImage(newVersion)
		return gateCase{hn: g.harness, g: g, cs: cs, args: map[string]any{"world_id": id, "version": newVersion}, snapshot: world.LabelBeforeUpdate}
	},
	"rollback_game_version": func(t *testing.T) gateCase {
		g, cs := t5VersionRig(t)
		id := g.newWorld("Base", fixtureNoSA, false)
		g.addImage(newVersion)
		if _, res, _ := confirmCall(t, cs, "update_game_version", map[string]any{"world_id": id, "version": newVersion}); res.IsError {
			t.Fatalf("update: %s", resultText(res))
		}
		return gateCase{hn: g.harness, g: g, cs: cs, args: map[string]any{"world_id": id}, snapshot: world.LabelBeforeRestore}
	},
	"delete_world": func(t *testing.T) gateCase {
		r := newWorldsRig(t)
		_, id := r.seedTwo()
		return gateCase{hn: r.harness, cs: r.cs, args: map[string]any{"world_id": id}, snapshot: world.LabelBeforeDelete}
	},
	"delete_favorite": func(t *testing.T) gateCase {
		r := newWorldsRig(t)
		res, made := call(t, r.cs, "make_favorite", map[string]any{"world_id": favSource(t, r.harness), "name": "Club"})
		if res.IsError {
			t.Fatalf("make_favorite: %s", resultText(res))
		}
		favID, _ := made["id"].(string)
		return gateCase{hn: r.harness, cs: r.cs, args: map[string]any{"favorite_id": favID}, still: func(t *testing.T) {
			if _, _, err := r.favs.Get(favID); err != nil {
				t.Errorf("the favorite is gone after the preview: %v", err)
			}
		}}
	},
	"delete_webhook": func(t *testing.T) gateCase {
		r := newForemanRig(t, keyed)
		hook, err := r.n.Add(context.Background(), notify.Setup{Preset: notify.Discord, URL: secret.New(hookURL), Events: defaultEvents(), Lang: "en"}, "alice")
		if err != nil {
			t.Fatal(err)
		}
		return gateCase{hn: r.harness, cs: r.cs, args: map[string]any{"webhook_id": hook.ID}, secrets: []string{hookURL, hookToken}, still: func(t *testing.T) {
			if list, err := r.n.List(context.Background()); err != nil || len(list) != 1 || list[0].ID != hook.ID {
				t.Errorf("the webhook is gone after the preview: %+v %v", list, err)
			}
		}}
	},
	"set_foreman_settings": func(t *testing.T) gateCase {
		r := newForemanRig(t, keyed)
		return gateCase{hn: r.harness, cs: r.cs, args: map[string]any{"snapshot_retention": 4, "mod_upload_mb": 300}, still: func(t *testing.T) {
			if got := r.generalServer().snapshotRetention(context.Background()); got != 10 {
				t.Errorf("retention is %d after the preview", got)
			}
		}}
	},
}

// gameState is what the fake game was asked to do and how its containers
// stand: Docker calls that change something, each container's status and
// players, and the RCON commands it got other than Foreman's polling.
func gameState(g *gameRig) string {
	if g == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range g.fake.State().Containers {
		cmds := slices.DeleteFunc(slices.Clone(c.Commands), func(s string) bool { return s == "/players online" })
		fmt.Fprintf(&b, "%s %s %q %q\n", c.Name, c.Status, c.Players, cmds)
	}
	fmt.Fprintf(&b, "%q", g.mutations())
	return b.String()
}

// adminTools lists the admin scope's tools with their annotations.
func adminTools(t *testing.T, cs *mcp.ClientSession) []*mcp.Tool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var out []*mcp.Tool
	for _, tool := range res.Tools {
		if !slices.Contains(readTools, tool.Name) && !slices.Contains(actionTools, tool.Name) {
			out = append(out, tool)
		}
	}
	return out
}

func isTwoStep(tool *mcp.Tool) bool {
	return tool.Annotations != nil && tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint
}

// takesCode reports whether a tool's input has confirm_code, the mark of a
// two-step tool whatever its hint says.
func takesCode(t *testing.T, tool *mcp.Tool) bool {
	t.Helper()
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal([]byte(asJSON(tool.InputSchema)), &schema); err != nil {
		t.Fatal(err)
	}
	_, ok := schema.Properties[confirmCodeKey]
	return ok
}

// gateClient is an admin client on a rig that registers every admin tool:
// it has a game, a mod planner and favorites.
func gateClient(t *testing.T) *mcp.ClientSession {
	t.Helper()
	_, cs := modsMCP(t, false)
	return cs
}

// auditDump is every audit row as text, so a test sees any change to it.
func auditDump(t *testing.T, hn *harness) string {
	t.Helper()
	list, err := hn.store.ListAudit(context.Background(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	return asJSON(list)
}

// wantNoSecretIn fails when any secret is in any text.
func wantNoSecretIn(t *testing.T, where string, secrets []string, texts ...string) {
	t.Helper()
	for _, s := range secrets {
		for _, text := range texts {
			if s != "" && strings.Contains(text, s) {
				t.Errorf("%s holds a secret (%.6s...)", where, s)
			}
		}
	}
}

// SPEC 21.8, 18.10b: for every two-step tool (marked destructive, or
// taking confirm_code), the first call changes nothing: not the data folder
// (paths, sizes, times and contents), the audit log, the fake game's
// containers and the commands sent to it, or the row the tool deletes. The
// preview names the snapshot label the UI would take; the confirm acts; no
// code or secret reaches a result or the log.
func TestMCPAdminGateTwoStepToolsPreviewChangesNothing(t *testing.T) {
	var twoStep []string
	for _, tool := range adminTools(t, gateClient(t)) {
		marked, code := isTwoStep(tool), takesCode(t, tool)
		if code && !marked {
			t.Errorf("%s takes confirm_code but its DestructiveHint is not true", tool.Name)
		}
		if marked || code {
			twoStep = append(twoStep, tool.Name)
		}
	}
	if len(twoStep) == 0 {
		t.Fatal("no admin tool is two-step")
	}
	for _, name := range twoStep {
		t.Run(name, func(t *testing.T) {
			setup, ok := gateCases[name]
			if !ok {
				t.Fatalf("%s is two-step but has no gate case", name)
			}
			c := setup(t)
			dirs := c.hn.worlds.Dirs()
			tree, digest, audits, played := dataStat(t, dirs), dataDigest(t, dirs), auditDump(t, c.hn), gameState(c.g)

			res, preview := call(t, c.cs, name, c.args)
			code, _ := preview[confirmCodeKey].(string)
			if res.IsError || preview["done"] != false || code == "" {
				t.Fatalf("preview: %s %s", resultText(res), asJSON(preview))
			}
			if got := dataStat(t, dirs); !maps.Equal(got, tree) {
				t.Errorf("the preview changed the data folder: %s", diffStat(tree, got))
			}
			if dataDigest(t, dirs) != digest {
				t.Error("the preview changed a file's contents in the data folder")
			}
			if auditDump(t, c.hn) != audits {
				t.Error("the preview changed the audit log")
			}
			if got := gameState(c.g); got != played {
				t.Errorf("the preview acted on the game:\nbefore %s\nafter  %s", played, got)
			}
			if c.still != nil {
				c.still(t)
			}
			if got, _ := preview["snapshot"].(string); got != c.snapshot {
				t.Errorf("preview snapshot %q, the UI takes %q", got, c.snapshot)
			}

			res2, done := call(t, c.cs, name, withCode(c.args, code))
			if res2.IsError || done["done"] != true {
				t.Fatalf("confirm: %s %s", resultText(res2), asJSON(done))
			}
			texts := []string{asJSON(preview), resultText(res), asJSON(done), resultText(res2)}
			// The code is in the preview by design; nowhere after it.
			wantNoSecretIn(t, "the confirm result or the log", []string{code}, asJSON(done), resultText(res2), c.hn.logs.String(), auditDump(t, c.hn))
			wantNoSecretIn(t, "a result, the log or the audit log", c.secrets, append(texts, c.hn.logs.String(), auditDump(t, c.hn))...)
		})
	}
}

// SPEC 14.19: a code is refused for another token, another tool, other
// arguments, after 5 minutes, on a second use, and after a refused confirm;
// each refusal acts on nothing.
func TestMCPAdminGateCodeRefusals(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	const settings = `{"name": "Base", "max_players": 8}`
	g.writeConfig(id, world.ServerSettingsFile, settings)
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	otherSecret, _ := apiToken(t, g.harness, u, "Other", auth.ScopeAdmin)
	other := mcpConnect(t, g.url, otherSecret)
	args := map[string]any{"world_id": id, "settings": map[string]any{"max_players": 20}}
	unchanged := func(t *testing.T) {
		t.Helper()
		var got struct {
			MaxPlayers *int `json:"max_players"`
		}
		text := readSettingsFile(t, g, id, world.ServerSettingsFile)
		if err := json.Unmarshal([]byte(text), &got); err != nil || got.MaxPlayers == nil || *got.MaxPlayers != 8 {
			t.Fatalf("a refused code acted: max_players is not 8: %s", text)
		}
	}
	refused := func(t *testing.T, res *mcp.CallToolResult) {
		t.Helper()
		if !res.IsError || resultText(res) != errConfirmRefused.Error() {
			t.Errorf("not refused as a bad code: %s", resultText(res))
		}
		unchanged(t)
	}

	t.Run("another token", func(t *testing.T) {
		code := askCode(t, cs, "change_game_settings", args)
		res, _ := call(t, other, "change_game_settings", withCode(args, code))
		refused(t, res)
	})
	t.Run("another tool", func(t *testing.T) {
		code := askCode(t, cs, "change_game_settings", args)
		// The other tool spends the code before its own checks, so its
		// refusal is the bad-code one, not a reason of its own.
		res, _ := call(t, cs, "update_game_version", map[string]any{"world_id": id, "version": "2.1.24", confirmCodeKey: code})
		refused(t, res)
		res, _ = call(t, cs, "change_game_settings", withCode(args, code))
		refused(t, res)
	})
	t.Run("other arguments", func(t *testing.T) {
		code := askCode(t, cs, "change_game_settings", args)
		res, _ := call(t, cs, "change_game_settings", withCode(map[string]any{"world_id": id, "settings": map[string]any{"max_players": 21}}, code))
		refused(t, res)
	})
	t.Run("after 5 minutes", func(t *testing.T) {
		code := askCode(t, cs, "change_game_settings", args)
		g.clock.Advance(confirmTTL)
		res, _ := call(t, cs, "change_game_settings", withCode(args, code))
		refused(t, res)
	})
	t.Run("after a refused confirm", func(t *testing.T) {
		code := askCode(t, cs, "change_game_settings", args)
		res, _ := call(t, cs, "change_game_settings", withCode(map[string]any{"world_id": id, "settings": map[string]any{"max_players": 22}}, code))
		refused(t, res)
		res, _ = call(t, cs, "change_game_settings", withCode(args, code))
		refused(t, res)
	})
	t.Run("second use", func(t *testing.T) {
		code := askCode(t, cs, "change_game_settings", args)
		if res, out := call(t, cs, "change_game_settings", withCode(args, code)); res.IsError || out["done"] != true {
			t.Fatalf("first use: %s %s", resultText(res), asJSON(out))
		}
		g.writeConfig(id, world.ServerSettingsFile, settings)
		res, _ := call(t, cs, "change_game_settings", withCode(args, code))
		refused(t, res)
	})
}

// sweepCall is one call in the secrets sweep: a tool and valid arguments.
// A two-step tool is called without a code, so it only previews.
type sweepCall struct {
	tool string
	args func() map[string]any
}

// sweep runs the calls in order, fails on any error, and returns every
// result's text.
func sweep(t *testing.T, cs *mcp.ClientSession, calls []sweepCall, ran map[string]bool) []string {
	t.Helper()
	var texts []string
	for _, c := range calls {
		args := c.args()
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: c.tool, Arguments: args})
		if err != nil {
			t.Errorf("%s %s: %v", c.tool, asJSON(args), err)
			continue
		}
		text := resultText(res) + asJSON(res.StructuredContent)
		if res.IsError {
			t.Errorf("%s %s failed: %s", c.tool, asJSON(args), text)
		}
		ran[c.tool] = true
		texts = append(texts, text)
	}
	return texts
}

func arg(m map[string]any) func() map[string]any { return func() map[string]any { return m } }

// SPEC 14.8, 14.19: every admin tool, called with valid arguments (the
// two-step ones for their preview), succeeds, and returns no RCON password,
// game password, factorio.com username or token, webhook address or secret
// key in its result; neither does the log or the audit log.
func TestMCPAdminGateNoSecretFromAnyTool(t *testing.T) {
	ran := map[string]bool{}

	// The worlds' tools, on a rig with a game, mods, favorites and an
	// account: a running world with bob online, a stopped one with a
	// snapshot and a mod that has settings.
	g, cs := modsMCP(t, true)
	g.portal.AddZip("alpha", "1.0.0")
	live := g.startRunning()
	g.join(live, "bob")
	rconPW, gamePW := g.secrets(live)
	stopped := g.world20("Stopped", "fmtest_1.0.0")
	fakeDefs(t, fmtestSettingDefs(t), nil)
	g.writeSettingsFile(stopped, gameFile(t, "2.0.77"))
	snap, err := g.worlds.TakeSnapshot(stopped, "Keep", "alice")
	if err != nil {
		t.Fatal(err)
	}
	var favID string
	favorite := func() map[string]any { return map[string]any{"favorite_id": favID} }
	worldCalls := []sweepCall{
		{"list_deleted_worlds", arg(map[string]any{})},
		{"list_favorites", arg(map[string]any{})},
		{"list_saves", arg(map[string]any{"world_id": stopped})},
		{"get_game_settings", arg(map[string]any{"world_id": live})},
		{"list_mod_settings", arg(map[string]any{"world_id": stopped})},
		{"search_mods", arg(map[string]any{"world_id": stopped, "query": "alpha"})},
		{"force_stop", arg(map[string]any{"world_id": live})},
		{"kick_player", arg(map[string]any{"world_id": live, "name": "bob", "reason": "AFK"})},
		{"ban_player", arg(map[string]any{"world_id": live, "name": "bob", "reason": "grief"})},
		{"run_console_command", arg(map[string]any{"world_id": live, "command": "/time"})},
		{"player_action", arg(map[string]any{"world_id": live, "action": "promote", "name": "bob"})},
		{"download_game_image", arg(map[string]any{"world_id": live})},
		{"change_game_settings", arg(map[string]any{"world_id": stopped, "settings": map[string]any{"max_players": 20}, "game_password": newGamePassword})},
		{"set_whitelist_switch", arg(map[string]any{"world_id": stopped, "on": true})},
		{"change_mods", arg(map[string]any{"world_id": stopped, "kind": "install", "names": []string{"alpha"}})},
		{"change_mod_settings", arg(map[string]any{"world_id": stopped, "values": map[string]any{"fm-sta-int": "9"}})},
		{"restore_snapshot", arg(map[string]any{"world_id": stopped, "snapshot": snap.Name})},
		{"delete_snapshots", arg(map[string]any{"world_id": stopped, "snapshots": []string{snap.Name}})},
		{"choose_save", arg(map[string]any{"world_id": stopped, "save": "base.zip"})},
		{"make_favorite", arg(map[string]any{"world_id": stopped, "name": "Club"})},
		{"rename_favorite", func() map[string]any { return map[string]any{"favorite_id": favID, "name": "Club two"} }},
		{"delete_favorite", favorite},
		{"clone_world", arg(map[string]any{"world_id": stopped, "name": "Copy", "map": "keep"})},
		{"generate_world", arg(map[string]any{"name": "Fresh", "game_version": v20})},
		{"rename_world", arg(map[string]any{"world_id": stopped, "name": "Stopped two"})},
		{"archive_world", arg(map[string]any{"world_id": stopped})},
		{"unarchive_world", arg(map[string]any{"world_id": stopped})},
		{"delete_world", arg(map[string]any{"world_id": stopped})},
	}
	// make_favorite's id is needed by the calls after it.
	var texts []string
	for i, c := range worldCalls {
		texts = append(texts, sweep(t, cs, worldCalls[i:i+1], ran)...)
		if c.tool == "make_favorite" {
			_, list := call(t, cs, "list_favorites", nil)
			if favs, _ := list["favorites"].([]any); len(favs) == 1 {
				favID, _ = favs[0].(map[string]any)["id"].(string)
			}
			if favID == "" {
				t.Fatalf("no favorite id: %s", asJSON(list))
			}
		}
	}
	wantNoSecretIn(t, "a world tool", append([]string{rconPW, gamePW, newGamePassword}, factorioSecrets...),
		append(texts, g.logs.String(), auditDump(t, g.harness))...)

	// The version tools, on a rig with a release list.
	vg, vcs := t5VersionRig(t)
	vid := vg.newWorld("Base", fixtureNoSA, false)
	vg.addImage(newVersion)
	texts = sweep(t, vcs, []sweepCall{
		{"list_game_versions", arg(map[string]any{"world_id": vid})},
		{"update_game_version", arg(map[string]any{"world_id": vid, "version": newVersion})},
	}, ran)
	if _, res, _ := confirmCall(t, vcs, "update_game_version", map[string]any{"world_id": vid, "version": newVersion}); res.IsError {
		t.Fatalf("update: %s", resultText(res))
	}
	texts = append(texts, sweep(t, vcs, []sweepCall{{"rollback_game_version", arg(map[string]any{"world_id": vid})}}, ran)...)
	vrcon, vgame := vg.secrets(vid)
	wantNoSecretIn(t, "a version tool", []string{vrcon, vgame}, append(texts, vg.logs.String(), auditDump(t, vg.harness))...)

	// Foreman-wide tools, on a rig with webhooks, a schedule and an account.
	fr := newForemanRig(t, keyed)
	hook, err := fr.n.Add(context.Background(), notify.Setup{Preset: notify.Discord, URL: secret.New(hookURL), Events: defaultEvents(), Lang: "en"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	texts = sweep(t, fr.cs, []sweepCall{
		{"foreman_settings", arg(map[string]any{})},
		{"add_webhook", arg(map[string]any{"preset": "discord", "url": hookURL, "lang": "en"})},
		{"change_webhook", arg(map[string]any{"webhook_id": hook.ID, "lang": "en"})},
		{"delete_webhook", arg(map[string]any{"webhook_id": hook.ID})},
		{"set_factorio_account", arg(map[string]any{"username": modportaltest.Username, "token": modportaltest.Token})},
		{"foreman_settings", arg(map[string]any{})},
		{"remove_factorio_account", arg(map[string]any{})},
		{"set_public_mods", arg(map[string]any{"on": true})},
		{"set_foreman_settings", arg(map[string]any{"snapshot_retention": 5})},
		{"set_restart_schedule", arg(map[string]any{"enabled": false})},
	}, ran)
	wantNoSecretIn(t, "a Foreman tool", append([]string{hookURL, hookToken, acctKey}, factorioSecrets...),
		append(texts, fr.logs.String(), auditDump(t, fr.harness))...)

	for _, name := range adminToolNames {
		if !ran[name] {
			t.Errorf("the sweep did not call %s", name)
		}
	}
}

// SPEC 21.8: every admin tool sets its hint explicitly: read-only for the
// reads, a destructive hint (true or false) for the rest, since MCP reads a
// missing one as destructive.
func TestMCPAdminGateEveryToolHasAHint(t *testing.T) {
	tools := adminTools(t, gateClient(t))
	if len(tools) != len(adminToolNames) {
		t.Errorf("%d admin tools, want %d", len(tools), len(adminToolNames))
	}
	for _, tool := range tools {
		a := tool.Annotations
		if a == nil || (!a.ReadOnlyHint && a.DestructiveHint == nil) {
			t.Errorf("%s sets no hint", tool.Name)
		}
		if a != nil && a.ReadOnlyHint && a.DestructiveHint != nil && *a.DestructiveHint {
			t.Errorf("%s is both read-only and destructive", tool.Name)
		}
	}
}

// SPEC 21.3, 21.7: an operate or read token calling any admin tool is told
// the tool is unknown, and nothing happens.
func TestMCPAdminGateLowerScopesRefused(t *testing.T) {
	g := newGameRig(t, image21)
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	turnMCPOn(t, g.harness)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	for _, scope := range []string{auth.ScopeOperate, auth.ScopeRead} {
		secretValue, _ := apiToken(t, g.harness, u, "T-"+scope, scope)
		cs := mcpConnect(t, g.url, secretValue)
		tree, audits := dataStat(t, g.worlds.Dirs()), auditDump(t, g.harness)
		for _, name := range adminToolNames {
			_, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{"world_id": id}})
			if err == nil || !strings.Contains(err.Error(), "unknown tool") {
				t.Errorf("%s token's %s: %v", scope, name, err)
			}
		}
		if !maps.Equal(dataStat(t, g.worlds.Dirs()), tree) || auditDump(t, g.harness) != audits {
			t.Errorf("a %s token's admin calls changed something", scope)
		}
	}
}
