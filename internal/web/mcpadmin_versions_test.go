package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/releases"
	"github.com/toddawhittaker/foreman/internal/world"
)

// t5VersionRig is an update rig with an admin token's MCP session.
func t5VersionRig(t *testing.T) (*gameRig, *mcp.ClientSession) {
	t.Helper()
	g := newUpdateRig(t, &fakeReleases{versions: []string{"2.0.77", oldVersion, newVersion}, latest: releases.Latest{Stable: newVersion}})
	u, _, err := g.store.UserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	turnMCPOn(t, g.harness)
	secret, _ := apiToken(t, g.harness, u, "Claude", auth.ScopeAdmin)
	return g, mcpConnect(t, g.url, secret)
}

// SPEC 21.7, 10.7: list_game_versions lists the image versions, only newer
// ones for a world, and what a rollback would go back to.
func TestMCPListGameVersions(t *testing.T) {
	g, cs := t5VersionRig(t)
	id := g.newWorld("Base", fixtureNoSA, false)
	res, out := call(t, cs, "list_game_versions", map[string]any{})
	if res.IsError || asJSON(out["versions"]) != `["2.0.77","2.1.24","2.1.25"]` || out["stable"] != newVersion {
		t.Errorf("all: %s %s", resultText(res), asJSON(out))
	}
	res, out = call(t, cs, "list_game_versions", map[string]any{"world_id": id})
	if res.IsError || asJSON(out["versions"]) != `["2.1.25"]` || out["current_version"] != oldVersion || out["rollback_to"] != nil {
		t.Errorf("for a world: %s %s", resultText(res), asJSON(out))
	}
}

// SPEC 21.8, 10.3, 10.4, 12.9: an update and its rollback each change
// nothing on the first call, act on the second, take the pages' snapshots
// and are audited with the token's name.
func TestMCPUpdateAndRollback(t *testing.T) {
	g, cs := t5VersionRig(t)
	id := g.newWorld("Base", fixtureNoSA, false)
	g.addImage(newVersion)
	save := g.readSave(id)
	args := map[string]any{"world_id": id, "version": newVersion}

	tree, audits := t5Tree(t, g), len(g.audit())
	res, out := call(t, cs, "update_game_version", args)
	t5Unchanged(t, g, "the update preview", tree, audits)
	if res.IsError || out["done"] != false || out["snapshot"] != world.LabelBeforeUpdate || out["confirm_code"] == "" ||
		!strings.Contains(asJSON(out["will"]), "Set the world's game version to the new one.") {
		t.Fatalf("update preview: %s %s", resultText(res), asJSON(out))
	}
	g.wantUnchanged("the update preview", id, oldVersion, save, 0)

	_, res, out = confirmCall(t, cs, "update_game_version", args)
	if res.IsError || out["done"] != true || out["finished"] != true {
		t.Fatalf("update: %s %s", resultText(res), asJSON(out))
	}
	if g.version(id) != newVersion || len(g.auditByToken(game.AuditUpdate)) != 1 {
		t.Errorf("after update: %s %+v", g.version(id), g.audit())
	}
	if !strings.Contains(asJSON(out["snapshots_taken"]), world.LabelBeforeUpdate) {
		t.Errorf("update snapshots: %s", asJSON(out))
	}

	tree, audits = t5Tree(t, g), len(g.audit())
	res, out = call(t, cs, "rollback_game_version", map[string]any{"world_id": id})
	t5Unchanged(t, g, "the rollback preview", tree, audits)
	if res.IsError || out["snapshot"] != world.LabelBeforeRestore || out["confirm_code"] == "" {
		t.Fatalf("rollback preview: %s %s", resultText(res), asJSON(out))
	}
	_, res, out = confirmCall(t, cs, "rollback_game_version", map[string]any{"world_id": id})
	if res.IsError || out["done"] != true || out["finished"] != true {
		t.Fatalf("rollback: %s %s", resultText(res), asJSON(out))
	}
	if g.version(id) != oldVersion || len(g.auditByToken(game.AuditRollback)) != 1 {
		t.Errorf("after rollback: %s %+v", g.version(id), g.audit())
	}
}

// SPEC 10.3, 10.4: refused for the reasons the pages refuse.
func TestMCPVersionRefusals(t *testing.T) {
	g, cs := t5VersionRig(t)
	id := g.newWorld("Base", fixtureNoSA, false)
	tree, audits := t5Tree(t, g), len(g.audit())
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"update_game_version", map[string]any{"world_id": id, "version": oldVersion}, inEnglish(i18n.M("server.error.version_not_newer"))},
		{"update_game_version", map[string]any{"world_id": id, "version": "9.9"}, ""},
		{"update_game_version", map[string]any{"version": newVersion}, "world_id"},
		{"rollback_game_version", map[string]any{"world_id": id}, inEnglish(i18n.M("server.error.no_rollback"))},
	} {
		res, _ := call(t, cs, c.tool, c.args)
		if !res.IsError || !strings.Contains(resultText(res), c.want) {
			t.Errorf("%s %v: %s, want %q", c.tool, c.args, resultText(res), c.want)
		}
	}
	t5Unchanged(t, g, "the refusals", tree, audits)
}

// SPEC 21.8 ruling 2: an update confirmed after the game started is
// refused with a fresh preview and code.
func TestMCPUpdateMoved(t *testing.T) {
	g, cs := t5VersionRig(t)
	id := g.newWorld("Base", fixtureNoSA, false)
	g.addImage(newVersion)
	args := map[string]any{"world_id": id, "version": newVersion}
	code := askCode(t, cs, "update_game_version", args)
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	deadline := time.Now().Add(15 * time.Second)
	for !game.IsRunning(g.ctrl.Status(id).State) {
		if time.Now().After(deadline) {
			t.Fatal("the game did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	res, out := call(t, cs, "update_game_version", withCode(args, code))
	if !res.IsError || !strings.Contains(resultText(res), "changed since the preview") || out["confirm_code"] == "" || out["confirm_code"] == code {
		t.Fatalf("moved: %s %s", resultText(res), asJSON(out))
	}
	if g.version(id) != oldVersion || g.hasAudit(game.AuditUpdate) {
		t.Error("the refused confirm updated the world")
	}
}

// SPEC 21.8: the update and rollback previews' sentences appear on the
// UI's confirm pages built from the same page structs.
func TestMCPVersionPreviewParity(t *testing.T) {
	sum := world.Summary{ID: "w1", Name: "Base", GameVersion: oldVersion}
	for _, running := range []bool{false, true} {
		up := UpdatePage{World: sum, Target: newVersion, Running: running, CountdownForm: defaultCountdown()}
		t5WantWill(t, t5PageText(t, up), updateWill(up))
		rb := RollbackPage{World: sum, Running: running, Version: "2.0.77", CountdownForm: defaultCountdown(),
			Snapshot: world.Snapshot{Name: "s1", Label: world.LabelBeforeUpdate, Reason: world.ReasonGameUpdate, TakenAt: time.Date(2026, 11, 17, 9, 30, 0, 0, time.UTC)}}
		t5WantWill(t, t5PageText(t, rb), rollbackWill(rb))
	}
}
