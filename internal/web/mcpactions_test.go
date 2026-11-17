package web

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
)

// auditByToken returns the audit entries of one action made through the
// token named Claude.
func (g *gameRig) auditByToken(action string) []store.AuditEntry {
	var out []store.AuditEntry
	for _, e := range g.audit() {
		if e.Action == action && e.TokenName == "Claude" {
			out = append(out, e)
		}
	}
	return out
}

// SPEC 21.5, 12.9: start and stop run the Server page's code, wait for the
// action, say how it ended, and are audited under the token's owner and
// name. The Server page shows the result too.
func TestMCPStartAndStop(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeOperate, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)

	res, out := call(t, cs, "start_server", map[string]any{"world_id": id})
	if res.IsError || out["finished"] != true || !strings.Contains(asJSON(out), `"ok":true`) {
		t.Fatalf("start: %s %s", resultText(res), asJSON(out))
	}
	if g.ctrl.Status(id).State != game.StateRunning {
		t.Errorf("state after start: %s", g.ctrl.Status(id).State)
	}
	if got := g.auditByToken(game.AuditStart); len(got) != 1 || got[0].Username != "alice" {
		t.Errorf("start audit: %+v", got)
	}

	res, _ = call(t, cs, "stop_server", map[string]any{"world_id": id, "countdown_seconds": 601})
	if !res.IsError || !strings.Contains(resultText(res), "0 to 600") {
		t.Errorf("a countdown over 10 minutes: %s", resultText(res))
	}
	res, out = call(t, cs, "stop_server", map[string]any{"world_id": id, "countdown_seconds": 0})
	if res.IsError || out["finished"] != true {
		t.Fatalf("stop: %s %s", resultText(res), asJSON(out))
	}
	if g.ctrl.Status(id).State != game.StateStopped || len(g.auditByToken(game.AuditStop)) != 1 {
		t.Errorf("after stop: %s, audits %+v", g.ctrl.Status(id).State, g.audit())
	}
}

// SPEC 7.3: start and restart act only on the active world.
func TestMCPStartNeedsActiveWorld(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeOperate, image21, image20)
	g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	old := g.oldWorld("Old")
	for _, tool := range []string{"start_server", "restart_server"} {
		res, _ := call(t, cs, tool, map[string]any{"world_id": old})
		if !res.IsError || !strings.Contains(resultText(res), "not the active world") {
			t.Errorf("%s on a world not active: %s", tool, resultText(res))
		}
	}
	res, _ := call(t, cs, "start_server", map[string]any{})
	if !res.IsError {
		t.Error("start without world_id was accepted")
	}
}

// SPEC 21.5: a call waits only so long; an action still running then is
// said to be running, carries on, and server_status reports its result.
func TestMCPActionStillRunning(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeOperate, image21)
	id := g.startRunning()
	g.join(id, "bob") // the countdown warns only when someone is on
	was := mcpWait
	mcpWait = 50 * time.Millisecond
	t.Cleanup(func() { mcpWait = was })

	res, out := call(t, cs, "restart_server", map[string]any{"world_id": id, "countdown_seconds": 2})
	if res.IsError || out["finished"] != false || !strings.Contains(resultText(res), "still running") {
		t.Fatalf("restart: %s %s", resultText(res), asJSON(out))
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, st := call(t, cs, "server_status", map[string]any{"world_id": id})
		if last, ok := st["last_result"].(map[string]any); ok && last["action"] == game.AuditRestart {
			if last["ok"] != true {
				t.Errorf("restart result: %s", asJSON(st))
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server_status never reported the restart: %s", asJSON(st))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !slices.ContainsFunc(g.commands(id), func(c string) bool { return strings.Contains(c, "2 seconds") }) {
		t.Errorf("players were not warned: %q", g.commands(id))
	}
}

// SPEC 21.5, 6.3: a snapshot call returns the snapshot it took, audited
// with the token's name.
func TestMCPTakeSnapshot(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeOperate, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	res, out := call(t, cs, "take_snapshot", map[string]any{"world_id": id, "label": "Before the boss"})
	taken, _ := out["snapshots_taken"].([]any)
	if res.IsError || out["finished"] != true || len(taken) != 1 || !strings.Contains(asJSON(taken), `"label":"Before the boss"`) {
		t.Fatalf("snapshot: %s %s", resultText(res), asJSON(out))
	}
	list, err := g.worlds.Snapshots(id)
	if err != nil || len(list) != 1 || list[0].TakenBy != "alice" {
		t.Errorf("snapshots: %+v %v", list, err)
	}
}

// SPEC 21.5, 7.9: switch_world makes another world active, says which
// snapshot it took, and leaves a stopped game stopped unless asked.
func TestMCPSwitchWorld(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeOperate, image21, image20)
	g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	old := g.oldWorld("Old")
	res, out := call(t, cs, "switch_world", map[string]any{"world_id": old})
	if res.IsError || out["finished"] != true || !strings.Contains(asJSON(out), `"ok":true`) {
		t.Fatalf("switch: %s %s", resultText(res), asJSON(out))
	}
	sum, err := g.worlds.Get(context.Background(), old)
	if err != nil || !sum.Active {
		t.Errorf("Old is not active: %+v %v", sum, err)
	}
	if g.ctrl.Status(old).State != game.StateStopped {
		t.Errorf("the switch started the game: %s", g.ctrl.Status(old).State)
	}
	if len(g.auditByToken(game.AuditSwitch)) != 1 {
		t.Errorf("switch audit: %+v", g.audit())
	}
	if taken, _ := out["snapshots_taken"].([]any); len(taken) == 0 || !strings.Contains(asJSON(taken), `"reason":"switch"`) {
		t.Errorf("the switch's snapshot was not reported: %s", asJSON(out))
	}
}

// SPEC 21.6, 8.5: chat goes out with the token's name in brackets in front,
// is audited, and follows the Chat page's rules with the prefix counted.
func TestMCPChat(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeOperate, image21)
	id := g.startRunning()

	res, out := call(t, cs, "send_chat", map[string]any{"world_id": id, "message": "Restarting in 5 minutes"})
	if res.IsError || out["sent"] != "[Claude] Restarting in 5 minutes" {
		t.Fatalf("chat: %s %s", resultText(res), asJSON(out))
	}
	if !slices.Contains(g.commands(id), "[Claude] Restarting in 5 minutes") {
		t.Errorf("the game was sent %q", g.commands(id))
	}
	if got := g.auditByToken(game.AuditChat); len(got) != 1 || got[0].Detail != "[Claude] Restarting in 5 minutes" {
		t.Errorf("chat audit: %+v", got)
	}

	for name, msg := range map[string]string{
		"command":        "/c game.print(1)",
		"empty":          "   ",
		"too long whole": strings.Repeat("x", game.MaxChatRunes-len("[Claude] ")+1),
	} {
		res, _ := call(t, cs, "send_chat", map[string]any{"world_id": id, "message": msg})
		if !res.IsError {
			t.Errorf("%s was sent", name)
		}
	}
	if res, _ := call(t, cs, "send_chat", map[string]any{"world_id": id, "message": strings.Repeat("x", game.MaxChatRunes-len("[Claude] "))}); res.IsError {
		t.Errorf("a line at the limit was refused: %s", resultText(res))
	}
}
