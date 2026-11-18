package web

import (
	"context"
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
// none), and the secrets the rig holds.
type gateCase struct {
	hn       *harness
	cs       *mcp.ClientSession
	args     map[string]any
	snapshot string
	secrets  []string
}

// liveGate is a running world with bob online and both game secrets set.
func liveGate(t *testing.T, args map[string]any) gateCase {
	g, cs, id := liveRig(t)
	rconPW, gamePW := g.secrets(id)
	args["world_id"] = id
	return gateCase{hn: g.harness, cs: cs, args: args, secrets: []string{rconPW, gamePW}}
}

var factorioSecrets = []string{modportaltest.Token, modportaltest.Username}

// gateCases sets up each two-step tool; the gate fails for a two-step tool
// missing here, so a new one cannot skip the gate.
var gateCases = map[string]func(t *testing.T) gateCase{
	"force_stop":          func(t *testing.T) gateCase { return liveGate(t, map[string]any{}) },
	"kick_player":         func(t *testing.T) gateCase { return liveGate(t, map[string]any{"name": "bob", "reason": "AFK"}) },
	"ban_player":          func(t *testing.T) gateCase { return liveGate(t, map[string]any{"name": "bob", "reason": "grief"}) },
	"run_console_command": func(t *testing.T) gateCase { return liveGate(t, map[string]any{"command": "/time"}) },
	"restore_snapshot": func(t *testing.T) gateCase {
		g, cs, id, snap, _ := adminRig(t)
		return gateCase{hn: g.harness, cs: cs, args: map[string]any{"world_id": id, "snapshot": snap.Name}, snapshot: world.LabelBeforeRestore}
	},
	"delete_snapshots": func(t *testing.T) gateCase {
		g, cs, id, snap, _ := adminRig(t)
		return gateCase{hn: g.harness, cs: cs, args: map[string]any{"world_id": id, "snapshots": []string{snap.Name}}}
	},
	"change_mods": func(t *testing.T) gateCase {
		g, cs := modsMCP(t, true)
		g.portal.AddZip("alpha", "1.0.0")
		id := g.world20("Base")
		return gateCase{hn: g.harness, cs: cs, args: map[string]any{"world_id": id, "kind": "install", "names": []string{"alpha"}},
			snapshot: world.LabelBeforeMods, secrets: factorioSecrets}
	},
	"change_mod_settings": func(t *testing.T) gateCase {
		g, cs := modsMCP(t, true)
		id := g.world20("Base", "fmtest_1.0.0")
		fakeDefs(t, fmtestSettingDefs(t), nil)
		g.writeSettingsFile(id, gameFile(t, "2.0.77"))
		return gateCase{hn: g.harness, cs: cs, args: map[string]any{"world_id": id, "values": map[string]any{"fm-sta-int": "9"}},
			snapshot: world.LabelBeforeMods, secrets: factorioSecrets}
	},
	"change_game_settings": func(t *testing.T) gateCase {
		g, cs := mcpRig(t, auth.ScopeAdmin, image21)
		id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
		rconPW, gamePW := g.secrets(id)
		return gateCase{hn: g.harness, cs: cs, args: map[string]any{"world_id": id, "settings": map[string]any{"max_players": 20}, //nolint:gosec // a made-up password to look for
			"game_password": "new-pw-Kx52-unlikely"}, snapshot: world.LabelBeforeSettings,
			secrets: []string{rconPW, gamePW, "new-pw-Kx52-unlikely"}}
	},
	"update_game_version": func(t *testing.T) gateCase {
		g, cs := t5VersionRig(t)
		id := g.newWorld("Base", fixtureNoSA, false)
		g.addImage(newVersion)
		return gateCase{hn: g.harness, cs: cs, args: map[string]any{"world_id": id, "version": newVersion}, snapshot: world.LabelBeforeUpdate}
	},
	"rollback_game_version": func(t *testing.T) gateCase {
		g, cs := t5VersionRig(t)
		id := g.newWorld("Base", fixtureNoSA, false)
		g.addImage(newVersion)
		if _, res, _ := confirmCall(t, cs, "update_game_version", map[string]any{"world_id": id, "version": newVersion}); res.IsError {
			t.Fatalf("update: %s", resultText(res))
		}
		return gateCase{hn: g.harness, cs: cs, args: map[string]any{"world_id": id}, snapshot: world.LabelBeforeRestore}
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
		return gateCase{hn: r.harness, cs: r.cs, args: map[string]any{"favorite_id": made["id"]}}
	},
	"delete_webhook": func(t *testing.T) gateCase {
		r := newForemanRig(t, keyed)
		hook, err := r.n.Add(context.Background(), notify.Setup{Preset: notify.Discord, URL: secret.New(hookURL), Events: defaultEvents(), Lang: "en"}, "alice")
		if err != nil {
			t.Fatal(err)
		}
		return gateCase{hn: r.harness, cs: r.cs, args: map[string]any{"webhook_id": hook.ID}, secrets: []string{hookURL, hookToken}}
	},
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

// SPEC 21.8, 18.10b: for every tool marked destructive, the first call
// changes nothing in the data folder (paths, sizes, times) or the audit log
// and names the snapshot label the UI would take; the confirm acts; no
// code or secret reaches a result or the log.
func TestMCPAdminGateTwoStepToolsPreviewChangesNothing(t *testing.T) {
	var twoStep []string
	for _, tool := range adminTools(t, gateClient(t)) {
		if isTwoStep(tool) {
			twoStep = append(twoStep, tool.Name)
		}
	}
	if len(twoStep) == 0 {
		t.Fatal("no admin tool is marked destructive")
	}
	for _, name := range twoStep {
		t.Run(name, func(t *testing.T) {
			setup, ok := gateCases[name]
			if !ok {
				t.Fatalf("%s is two-step but has no gate case", name)
			}
			c := setup(t)
			root := c.hn.worlds.Dirs()
			tree, audits := dataStat(t, root), auditDump(t, c.hn)

			res, preview := call(t, c.cs, name, c.args)
			code, _ := preview[confirmCodeKey].(string)
			if res.IsError || preview["done"] != false || code == "" {
				t.Fatalf("preview: %s %s", resultText(res), asJSON(preview))
			}
			if got := dataStat(t, root); !maps.Equal(got, tree) {
				t.Errorf("the preview changed the data folder: %s", diffStat(tree, got))
			}
			if auditDump(t, c.hn) != audits {
				t.Error("the preview changed the audit log")
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
	g.writeConfig(id, world.ServerSettingsFile, `{"name": "Base", "max_players": 8}`)
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	otherSecret, _ := apiToken(t, g.harness, u, "Other", auth.ScopeAdmin)
	other := mcpConnect(t, g.url, otherSecret)
	args := map[string]any{"world_id": id, "settings": map[string]any{"max_players": 20}}
	unchanged := func(t *testing.T) {
		t.Helper()
		if s := readSettingsFile(t, g, id, world.ServerSettingsFile); strings.Contains(s, "20") {
			t.Fatalf("a refused code acted: %s", s)
		}
	}
	refused := func(t *testing.T, res *mcp.CallToolResult) {
		t.Helper()
		if !res.IsError || !strings.Contains(resultText(res), errConfirmRefused.Error()) {
			t.Errorf("not refused: %s", resultText(res))
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
		res, _ := call(t, cs, "update_game_version", map[string]any{"world_id": id, "version": "2.1.24", confirmCodeKey: code})
		if !res.IsError {
			t.Errorf("accepted: %s", resultText(res))
		}
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
		g.writeConfig(id, world.ServerSettingsFile, `{"name": "Base", "max_players": 8}`)
		res, _ := call(t, cs, "change_game_settings", withCode(args, code))
		refused(t, res)
	})
}

// SPEC 14.8, 14.19: a call to every admin tool, with and without a world,
// returns no RCON password, game password, factorio.com username or token,
// or webhook address, in its result, its error or the log.
func TestMCPAdminGateNoSecretFromAnyTool(t *testing.T) {
	type rig struct {
		hn      *harness
		cs      *mcp.ClientSession
		id      string
		secrets []string
	}
	live := liveGate(t, map[string]any{})
	mods, modsCS := modsMCP(t, true)
	modsID := mods.world20("Base")
	fr := newForemanRig(t, keyed)
	if _, err := fr.n.Add(context.Background(), notify.Setup{Preset: notify.Discord, URL: secret.New(hookURL), Events: defaultEvents(), Lang: "en"}, "alice"); err != nil {
		t.Fatal(err)
	}
	rigs := []rig{
		{live.hn, live.cs, live.args["world_id"].(string), live.secrets},
		{mods.harness, modsCS, modsID, factorioSecrets},
		{fr.harness, fr.cs, "", []string{hookURL, hookToken, acctKey}},
	}
	for _, r := range rigs {
		for _, tool := range adminTools(t, r.cs) {
			for _, args := range []map[string]any{{}, {"world_id": r.id, "webhook_id": 1, "name": "bob", "query": "a"}} {
				res, err := r.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool.Name, Arguments: args})
				text := ""
				if err != nil {
					text = err.Error()
				} else {
					text = resultText(res) + asJSON(res.StructuredContent)
				}
				wantNoSecretIn(t, tool.Name, r.secrets, text)
			}
		}
		wantNoSecretIn(t, "the log", r.secrets, r.hn.logs.String(), auditDump(t, r.hn))
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
