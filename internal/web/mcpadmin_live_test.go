package web

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

// hunter is a password to look for.
const hunter = "hunter2-unlikely"

// consoleCommands lists what the game was sent, leaving out Foreman's own
// polling of who is online.
func (g *gameRig) consoleCommands(id string) []string {
	var out []string
	for _, c := range g.commands(id) {
		// Foreman sends these itself: the online list, and /time for the
		// play time (SPEC 8.15). The tests' own commands are never these.
		if c != "/players online" && c != "/time" {
			out = append(out, c)
		}
	}
	return out
}

// wantNoSecrets fails when a game or RCON password is in any text.
func wantNoSecrets(t *testing.T, rconPW, gamePW string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		for _, secret := range []string{rconPW, gamePW} {
			if strings.Contains(text, secret) {
				t.Errorf("a secret reached a result: %q in %s", secret, text)
			}
		}
	}
}

// liveRig is an admin client over a running world with bob online.
func liveRig(t *testing.T) (g *gameRig, cs *mcp.ClientSession, id string) {
	t.Helper()
	g, cs = mcpRig(t, auth.ScopeAdmin, image21)
	id = g.startRunning()
	g.join(id, "bob")
	return g, cs, id
}

// SPEC 21.7, 21.8, 14.12: the live tools are admin tools; the two-step ones
// are marked destructive, and each tool with game text names its untrusted
// field.
func TestMCPLiveToolsAreMarkedAndNameUntrustedFields(t *testing.T) {
	_, cs := mcpRig(t, auth.ScopeAdmin, image21)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		destructive bool
		untrusted   string
	}{
		"force_stop": {true, "untrusted_online"}, "kick_player": {true, "untrusted_reply"}, "ban_player": {true, "untrusted_reply"},
		"run_console_command": {true, "untrusted_reply"}, "player_action": {false, "untrusted_reply"}, "download_game_image": {false, "untrusted_failure"},
	}
	found := 0
	for _, tool := range res.Tools {
		w, ok := want[tool.Name]
		if !ok {
			continue
		}
		found++
		if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint != w.destructive {
			t.Errorf("%s: destructive hint is %+v, want %v", tool.Name, tool.Annotations, w.destructive)
		}
		if !strings.Contains(tool.Description, w.untrusted) {
			t.Errorf("%s does not name %s: %s", tool.Name, w.untrusted, tool.Description)
		}
	}
	if found != len(want) {
		t.Errorf("found %d of %d tools", found, len(want))
	}
	_, operate := mcpRig(t, auth.ScopeOperate, image21)
	res, err = operate.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if _, ok := want[tool.Name]; ok {
			t.Errorf("the operate scope lists %s", tool.Name)
		}
	}
}

// SPEC 7.7, 21.8: the first call stops nothing; the second stops the game
// at once, audited with the token's name.
func TestMCPForceStop(t *testing.T) {
	g, cs, id := liveRig(t)
	rconPW, gamePW := g.secrets(id)
	args := map[string]any{"world_id": id}
	digest, audits, calls := dataDigest(t, g.worlds.Dirs()), len(g.audit()), len(g.mutations())

	res, out := call(t, cs, "force_stop", args)
	if res.IsError || out["done"] != false || out[confirmCodeKey] == "" || !strings.Contains(asJSON(out["untrusted_online"]), "bob") ||
		!strings.Contains(asJSON(out["will"]), "Stop the game at once") {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(out))
	}
	if dataDigest(t, g.worlds.Dirs()) != digest || len(g.audit()) != audits || len(g.mutations()) != calls || g.containerStatus(id) != "running" {
		t.Fatal("the preview changed something")
	}

	_, res, out = confirmCall(t, cs, "force_stop", args)
	if res.IsError || out["done"] != true || out["finished"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	wantNoSecrets(t, rconPW, gamePW, asJSON(out), resultText(res))
	if g.ctrl.Status(id).State != game.StateStopped {
		t.Errorf("state after the forced stop: %s", g.ctrl.Status(id).State)
	}
	if got := g.auditByToken(game.AuditForceStop); len(got) != 1 || got[0].Username != "alice" {
		t.Errorf("force stop audit by token: %+v", got)
	}
}

// SPEC 7.7: a stopped game has nothing to force stop, as the Server page
// offers no button; a missing world is refused.
func TestMCPForceStopRefusals(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	for name, args := range map[string]map[string]any{"stopped": {"world_id": id}, "no world": {"world_id": "nosuchworld"}, "no id": {}} {
		if res, _ := call(t, cs, "force_stop", args); !res.IsError {
			t.Errorf("%s was accepted: %s", name, resultText(res))
		}
	}
}

// SPEC 21.8: a game whose state changed since the preview is refused with a
// fresh preview and code.
func TestMCPForceStopRefusesAMovedState(t *testing.T) {
	g, cs, id := liveRig(t)
	args := map[string]any{"world_id": id}
	old := askCode(t, cs, "force_stop", args)
	if err := g.fake.Crash(game.ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for g.ctrl.Status(id).State != game.StateCrashed {
		if time.Now().After(deadline) {
			t.Skipf("the game never showed as crashed, but %s", g.ctrl.Status(id).State)
		}
		time.Sleep(20 * time.Millisecond)
	}
	res, out := call(t, cs, "force_stop", withCode(args, old))
	if fresh, _ := out[confirmCodeKey].(string); !res.IsError || fresh == "" || fresh == old {
		t.Fatalf("a moved state: %s %s", resultText(res), asJSON(out))
	}
	if g.hasAudit(game.AuditForceStop) {
		t.Error("the refused call stopped the game")
	}
}

// SPEC 7.7, 21.8: a forced stop preview and its confirm page say the same.
func TestForceStopWillMatchesThePage(t *testing.T) {
	for name, p := range map[string]ForceStopPage{
		"never played": {World: world.Summary{ID: "base", Name: "Base"}},
		"played":       {World: world.Summary{ID: "base", Name: "Base", LastPlayed: time.Date(2026, 11, 2, 11, 30, 0, 0, time.UTC)}, Online: Online{Asked: true, Names: []string{"bob"}}},
	} {
		t.Run(name, func(t *testing.T) { wantWill(t, forceStopWill(p), pageText(t, p)) })
	}
}

// SPEC 3.7: the image download runs as the Server page's button does and is
// audited with the token's name.
func TestMCPDownloadGameImage(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	res, out := call(t, cs, "download_game_image", map[string]any{"world_id": id})
	if res.IsError || out["finished"] != true || !strings.Contains(asJSON(out), `"ok":true`) {
		t.Fatalf("download: %s %s", resultText(res), asJSON(out))
	}
	if got := g.auditByToken(game.AuditImageDownload); len(got) != 1 {
		t.Errorf("download audit by token: %+v", got)
	}
	if res, _ := call(t, cs, "download_game_image", map[string]any{"world_id": "nosuchworld"}); !res.IsError {
		t.Error("a missing world was accepted")
	}
}

// SPEC 8.2, 21.8: a kick does nothing on the first call and sends the kick,
// audited with the token's name, on the second.
func TestMCPKickPlayer(t *testing.T) {
	g, cs, id := liveRig(t)
	rconPW, gamePW := g.secrets(id)
	args := map[string]any{"world_id": id, "name": "bob", "reason": "AFK"}
	audits := g.playerAudits()

	res, out := call(t, cs, "kick_player", args)
	if res.IsError || out["done"] != false || out[confirmCodeKey] == "" || !strings.Contains(asJSON(out["will"]), "Disconnect bob now.") {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(out))
	}
	if len(g.playerCommands(id)) != 0 || g.playerAudits() != audits {
		t.Fatal("the preview kicked or audited")
	}

	_, res, out = confirmCall(t, cs, "kick_player", args)
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	wantNoSecrets(t, rconPW, gamePW, asJSON(out), resultText(res))
	if cmds := g.playerCommands(id); len(cmds) != 1 || !strings.HasPrefix(cmds[0], "/kick bob") {
		t.Errorf("commands: %q", cmds)
	}
	if got := g.auditByToken(game.AuditPlayer); len(got) != 1 || got[0].Username != "alice" {
		t.Errorf("player audit by token: %+v", got)
	}
}

// SPEC 8.2: kick and ban are refused for the page's reasons: a name or a
// reason the console would misread, a game not running, and, for a kick, a
// player who is not in the game.
func TestMCPKickAndBanRefusals(t *testing.T) {
	g, cs, id := liveRig(t)
	for name, c := range map[string]struct {
		tool string
		args map[string]any
	}{
		"bad name":     {"kick_player", map[string]any{"world_id": id, "name": "bob smith"}},
		"empty name":   {"ban_player", map[string]any{"world_id": id, "name": ""}},
		"long reason":  {"ban_player", map[string]any{"world_id": id, "name": "bob", "reason": strings.Repeat("x", 500)}},
		"not in game":  {"kick_player", map[string]any{"world_id": id, "name": "carol"}},
		"no world":     {"kick_player", map[string]any{"world_id": "nosuchworld", "name": "bob"}},
		"no world id":  {"ban_player", map[string]any{"name": "bob"}},
		"bad reason":   {"ban_player", map[string]any{"world_id": id, "name": "bob", "reason": "two\nlines"}},
		"name as flag": {"kick_player", map[string]any{"world_id": id, "name": "bob\x00"}},
	} {
		if res, out := call(t, cs, c.tool, c.args); !res.IsError || out[confirmCodeKey] != nil {
			t.Errorf("%s was accepted: %s %s", name, resultText(res), asJSON(out))
		}
	}
	other := g.newWorld("Second", "2.1.24-no-space-age.zip", false)
	if res, _ := call(t, cs, "ban_player", map[string]any{"world_id": other, "name": "bob"}); !res.IsError {
		t.Error("a ban on a game that is not running was accepted")
	}
	if len(g.playerCommands(id)) != 0 {
		t.Errorf("a refused call sent %q", g.playerCommands(id))
	}
}

// SPEC 8.2, 21.8: a ban's preview changes if the player leaves; the confirm
// is then refused with a fresh preview and code, and the fresh code bans.
func TestMCPBanRefusesAMovedState(t *testing.T) {
	g, cs, id := liveRig(t)
	args := map[string]any{"world_id": id, "name": "bob"}
	old := askCode(t, cs, "ban_player", args)
	if err := g.fake.Leave(game.ContainerName(id), "bob"); err != nil {
		t.Fatal(err)
	}
	res, out := call(t, cs, "ban_player", withCode(args, old))
	fresh, _ := out[confirmCodeKey].(string)
	if !res.IsError || fresh == "" || fresh == old || strings.Contains(asJSON(out["will"]), "Disconnect bob now") {
		t.Fatalf("a moved state: %s %s", resultText(res), asJSON(out))
	}
	if len(g.playerCommands(id)) != 0 {
		t.Fatalf("the refused call sent %q", g.playerCommands(id))
	}
	res, out = call(t, cs, "ban_player", withCode(args, fresh))
	if res.IsError || out["done"] != true || len(g.playerCommands(id)) != 1 {
		t.Fatalf("the fresh code: %s %s, sent %q", resultText(res), asJSON(out), g.playerCommands(id))
	}
}

// SPEC 8.2, 21.8: a kick or ban preview and its confirm page say the same.
func TestPlayerWillMatchesThePage(t *testing.T) {
	for name, p := range map[string]PlayerActionPage{
		"kick":           {World: world.Summary{ID: "base", Name: "Base"}, Action: game.PlayerKick, Name: "bob", Online: true},
		"ban":            {World: world.Summary{ID: "base", Name: "Base"}, Action: game.PlayerBan, Name: "bob", Online: true},
		"ban, not on":    {World: world.Summary{ID: "base", Name: "Base"}, Action: game.PlayerBan, Name: "bob", Gone: true},
		"ban, not known": {World: world.Summary{ID: "base", Name: "Base"}, Action: game.PlayerBan, Name: "bob"},
	} {
		t.Run(name, func(t *testing.T) { wantWill(t, playerWill(p), pageText(t, p)) })
	}
}

// SPEC 8.2, 8.3: the one-step player actions send the game's command at
// once, audited with the token's name, and refuse what the page refuses.
func TestMCPPlayerAction(t *testing.T) {
	g, cs, id := liveRig(t)
	// Promote first: the game refuses to demote a player who is not an admin.
	for _, action := range []string{"promote", "demote"} {
		command := "/" + action + " bob"
		res, out := call(t, cs, "player_action", map[string]any{"world_id": id, "action": action, "name": "bob"})
		if res.IsError || out["done"] != true {
			t.Fatalf("%s: %s %s", action, resultText(res), asJSON(out))
		}
		if cmds := g.playerCommands(id); len(cmds) == 0 || cmds[len(cmds)-1] != command {
			t.Errorf("%s sent %q", action, cmds)
		}
	}
	// The world was started with the whitelist switch off, so the game would
	// not keep a whitelist change, and the page refuses it.
	if res, _ := call(t, cs, "player_action", map[string]any{"world_id": id, "action": "whitelist_add", "name": "bob"}); !res.IsError ||
		!strings.Contains(resultText(res), "whitelist switch off") {
		t.Errorf("a whitelist change with the switch off: %s", resultText(res))
	}
	if got := g.auditByToken(game.AuditPlayer); len(got) != 2 {
		t.Errorf("player audits by token: %+v", got)
	}
	sent := len(g.playerCommands(id))
	for name, args := range map[string]map[string]any{
		"kick is two-step": {"world_id": id, "action": "kick", "name": "bob"},
		"unknown action":   {"world_id": id, "action": "op", "name": "bob"},
		"hyphen spelling":  {"world_id": id, "action": "whitelist-add", "name": "bob"},
		"bad name":         {"world_id": id, "action": "promote", "name": "bob smith"},
		"no name":          {"world_id": id, "action": "promote", "name": ""},
		"no world":         {"world_id": "nosuchworld", "action": "promote", "name": "bob"},
	} {
		if res, _ := call(t, cs, "player_action", args); !res.IsError {
			t.Errorf("%s was accepted: %s", name, resultText(res))
		}
	}
	if len(g.playerCommands(id)) != sent {
		t.Errorf("a refused call sent %q", g.playerCommands(id)[sent:])
	}
}

// SPEC 8.2: a world whose game is not running refuses a player action in
// the page's words.
func TestMCPPlayerActionNeedsARunningGame(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	res, _ := call(t, cs, "player_action", map[string]any{"world_id": id, "action": "promote", "name": "bob"})
	if !res.IsError || !strings.Contains(resultText(res), "not running") {
		t.Errorf("a stopped game: %s", resultText(res))
	}
}

// SPEC 8.4, 12.9, 14.8, 21.8: a console command is sent only on the second
// call, unchanged; the preview and the audit log hold the masked command; no
// password reaches a result.
func TestMCPConsoleCommand(t *testing.T) {
	g, cs, id := liveRig(t)
	rconPW, gamePW := g.secrets(id)
	command := "/config set password " + hunter
	args := map[string]any{"world_id": id, "command": command}
	audits := len(g.audit())

	res, out := call(t, cs, "run_console_command", args)
	if res.IsError || out["done"] != false || out[confirmCodeKey] == "" || out["command"] != "/config set password "+game.Mask {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(out))
	}
	if !strings.Contains(asJSON(out["will"]), "exactly as given") || !strings.Contains(asJSON(out["will"]), "audit log") {
		t.Errorf("will: %s", asJSON(out["will"]))
	}
	if len(g.audit()) != audits || len(g.consoleCommands(id)) != 0 {
		t.Fatalf("the preview sent or audited: %q", g.consoleCommands(id))
	}

	_, res, out = confirmCall(t, cs, "run_console_command", args)
	if res.IsError || out["done"] != true || out["command"] != "/config set password "+game.Mask {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if cmds := g.consoleCommands(id); len(cmds) != 1 || cmds[0] != command {
		t.Errorf("the game was sent %q, not the command unchanged", cmds)
	}
	got := g.auditByToken(game.AuditConsole)
	if len(got) != 1 || got[0].Username != "alice" || !strings.Contains(got[0].Detail, game.Mask) {
		t.Fatalf("console audit by token: %+v", got)
	}
	for _, e := range g.audit() {
		if strings.Contains(e.Detail, hunter) {
			t.Errorf("the audit log holds the password: %+v", e)
		}
	}
	for _, text := range []string{asJSON(out), resultText(res), g.get(PathConsole)} {
		for _, secret := range []string{hunter, rconPW, gamePW} {
			if strings.Contains(text, secret) {
				t.Errorf("a secret reached a result or page: %q", secret)
			}
		}
	}
	if !strings.Contains(g.get(PathConsole), "/config set password "+game.Mask) {
		t.Error("the Console page does not show the command the tool sent")
	}
}

// SPEC 8.4, 14.8: a command whose reply would print a password gets a masked
// reply.
func TestMCPConsoleHidesASecretReply(t *testing.T) {
	g, cs, id := liveRig(t)
	_, res, out := confirmCall(t, cs, "run_console_command", map[string]any{"world_id": id, "command": "/config get password"})
	if res.IsError || out["untrusted_reply"] != game.Mask {
		t.Errorf("reply: %s %s", resultText(res), asJSON(out))
	}
	_ = g
}

// SPEC 8.4: the tool is refused for the page's reasons.
func TestMCPConsoleRefusals(t *testing.T) {
	g, cs, id := liveRig(t)
	for name, args := range map[string]map[string]any{
		"empty":       {"world_id": id, "command": "  "},
		"two lines":   {"world_id": id, "command": "/evolution\n/players"},
		"nul":         {"world_id": id, "command": "/evolution\x00"},
		"too long":    {"world_id": id, "command": "/c " + strings.Repeat("x", 1001)},
		"no world":    {"world_id": "nosuchworld", "command": "/evolution"},
		"no world id": {"command": "/evolution"},
	} {
		if res, out := call(t, cs, "run_console_command", args); !res.IsError || out[confirmCodeKey] != nil {
			t.Errorf("%s was accepted: %s", name, resultText(res))
		}
	}
	other := g.newWorld("Second", "2.1.24-no-space-age.zip", false)
	if res, _ := call(t, cs, "run_console_command", map[string]any{"world_id": other, "command": "/evolution"}); !res.IsError || !strings.Contains(resultText(res), "not running") {
		t.Errorf("a stopped game: %s", resultText(res))
	}
	if len(g.consoleCommands(id)) != 0 {
		t.Errorf("a refused call sent %q", g.consoleCommands(id))
	}
}

// SPEC 21.8: a code made for one command does not send another.
func TestMCPConsoleCodeIsForTheCommandAsked(t *testing.T) {
	g, cs, id := liveRig(t)
	code := askCode(t, cs, "run_console_command", map[string]any{"world_id": id, "command": "/evolution"})
	res, _ := call(t, cs, "run_console_command", map[string]any{"world_id": id, "command": "/players", confirmCodeKey: code})
	if !res.IsError || len(g.consoleCommands(id)) != 0 {
		t.Errorf("a code for another command: %s, sent %q", resultText(res), g.consoleCommands(id))
	}
}

// SPEC 14.19, 21.8: a confirm whose checks refuse still spends its code, so
// the same code is refused as a code, not as the check, the second time.
func TestMCPTwoStepToolsSpendTheCodeBeforeTheirChecks(t *testing.T) {
	spent := func(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, code string) {
		t.Helper()
		res, _ := call(t, cs, tool, withCode(args, code))
		if !res.IsError || strings.Contains(resultText(res), "confirm code") {
			t.Errorf("the confirm after the state changed should be refused by its check: %s", resultText(res))
		}
		res, _ = call(t, cs, tool, withCode(args, code))
		if !res.IsError || !strings.Contains(resultText(res), "confirm code was refused") {
			t.Errorf("the code was not spent by the refused confirm: %s", resultText(res))
		}
	}
	t.Run("run_console_command", func(t *testing.T) {
		g, cs, id := liveRig(t)
		// Not /time: Foreman asks that itself at the stop's save (SPEC 8.15).
		args := map[string]any{"world_id": id, "command": "/evolution"}
		code := askCode(t, cs, "run_console_command", args)
		call(t, cs, "stop_server", map[string]any{"world_id": id, "countdown_seconds": 0})
		spent(t, cs, "run_console_command", args, code)
		if slices.Contains(g.consoleCommands(id), "/evolution") {
			t.Errorf("sent %q", g.consoleCommands(id))
		}
	})
	t.Run("force_stop", func(t *testing.T) {
		g, cs, id := liveRig(t)
		args := map[string]any{"world_id": id}
		code := askCode(t, cs, "force_stop", args)
		call(t, cs, "stop_server", map[string]any{"world_id": id, "countdown_seconds": 0})
		spent(t, cs, "force_stop", args, code)
		if g.hasAudit(game.AuditForceStop) {
			t.Error("force stop audited")
		}
	})
	t.Run("kick_player", func(t *testing.T) {
		g, cs, id := liveRig(t)
		args := map[string]any{"world_id": id, "name": "bob"}
		code := askCode(t, cs, "kick_player", args)
		if err := g.fake.Leave(game.ContainerName(id), "bob"); err != nil {
			t.Fatal(err)
		}
		spent(t, cs, "kick_player", args, code)
	})
	t.Run("restore_snapshot", func(t *testing.T) {
		g, cs, id, snap, _ := adminRig(t)
		args := map[string]any{"world_id": id, "snapshot": snap.Name}
		code := askCode(t, cs, "restore_snapshot", args)
		if err := g.worlds.DeleteSnapshot(id, snap.Name, func(world.Snapshot) error { return nil }); err != nil {
			t.Fatal(err)
		}
		spent(t, cs, "restore_snapshot", args, code)
	})
	t.Run("delete_snapshots", func(t *testing.T) {
		g, cs, id, snap, _ := adminRig(t)
		args := map[string]any{"world_id": id, "snapshots": []string{snap.Name}}
		code := askCode(t, cs, "delete_snapshots", args)
		if err := g.worlds.DeleteSnapshot(id, snap.Name, func(world.Snapshot) error { return nil }); err != nil {
			t.Fatal(err)
		}
		spent(t, cs, "delete_snapshots", args, code)
	})
}
