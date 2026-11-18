package web

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

func snapshotNamesOf(t *testing.T, g *gameRig, id string) []string {
	t.Helper()
	list, err := g.worlds.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range list {
		out = append(out, s.Name)
	}
	return out
}

// adminRig is an admin-scope MCP client over a world with one manual
// snapshot, taken before the save was played on.
func adminRig(t *testing.T) (g *gameRig, cs *mcp.ClientSession, id string, snap world.Snapshot, saved []byte) {
	t.Helper()
	g, cs, id, snap, saved = adminRigUnplayed(t)
	g.playOn(id)
	return g, cs, id, snap, saved
}

// adminRigUnplayed is adminRig with the save left as the snapshot has it,
// so the game can still start.
func adminRigUnplayed(t *testing.T) (g *gameRig, cs *mcp.ClientSession, id string, snap world.Snapshot, saved []byte) {
	t.Helper()
	g, cs = mcpRig(t, auth.ScopeAdmin, image21)
	id = g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	snap, err := g.worlds.TakeSnapshot(id, "Keep me", "alice")
	if err != nil {
		t.Fatal(err)
	}
	return g, cs, id, snap, g.readSave(id)
}

// SPEC 21.7, 21.8: the admin scope lists the snapshot tools, marks both
// two-step, and the operate scope does not list them.
func TestMCPSnapshotToolsAreAdminAndTwoStep(t *testing.T) {
	_, cs := mcpRig(t, auth.ScopeAdmin, image21)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, tool := range res.Tools {
		switch tool.Name {
		case "restore_snapshot", "delete_snapshots":
			found[tool.Name] = true
			if tool.Annotations == nil || tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
				t.Errorf("%s is not marked destructive", tool.Name)
			}
			if !strings.Contains(tool.Description, "untrusted_label") {
				t.Errorf("%s does not name its untrusted field: %s", tool.Name, tool.Description)
			}
		}
	}
	if len(found) != 2 {
		t.Errorf("admin tools found: %v", found)
	}
	_, operate := mcpRig(t, auth.ScopeOperate, image21)
	res, err = operate.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name == "restore_snapshot" || tool.Name == "delete_snapshots" {
			t.Errorf("the operate scope lists %s", tool.Name)
		}
	}
}

// SPEC 6.5, 21.8: the first call changes nothing and names the snapshot the
// restore takes; the second restores, takes "Before restore" and audits the
// token's name.
func TestMCPRestoreSnapshot(t *testing.T) {
	g, cs, id, snap, saved := adminRig(t)
	args := map[string]any{"world_id": id, "snapshot": snap.Name}
	digest, audits := dataDigest(t, g.worlds.Dirs()), len(g.audit())

	res, out := call(t, cs, "restore_snapshot", args)
	if res.IsError || out["done"] != false || out["snapshot"] != "Before restore" || out[confirmCodeKey] == "" {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(out))
	}
	if !strings.Contains(asJSON(out["will"]), `First, take a snapshot of Base, labeled \"Before restore\"`) {
		t.Errorf("will: %s", asJSON(out["will"]))
	}
	if dataDigest(t, g.worlds.Dirs()) != digest || len(g.audit()) != audits {
		t.Fatal("the preview changed the data folder or the audit log")
	}

	_, res, out = confirmCall(t, cs, "restore_snapshot", args)
	if res.IsError || out["done"] != true || out["finished"] != true || !strings.Contains(asJSON(out["snapshots_taken"]), `"untrusted_label":"Before restore"`) {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if got := g.readSave(id); string(got) != string(saved) {
		t.Errorf("the save was not restored: %q", got)
	}
	if got := snapshotNamesOf(t, g, id); len(got) != 2 {
		t.Errorf("snapshots after the restore: %v", got)
	}
	got := g.auditByToken(game.AuditRestore)
	if len(got) != 1 || got[0].Username != "alice" || got[0].WorldID != id {
		t.Errorf("restore audit by token: %+v", got)
	}
}

// SPEC 6.5, 21.8: a running game is warned, stopped, restored and started
// again; the preview names who is online as untrusted.
func TestMCPRestoreRunningWorld(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.startRunning()
	g.join(id, "bob")
	snap, err := g.worlds.TakeSnapshot(id, "Keep me", "alice")
	if err != nil {
		t.Fatal(err)
	}
	saved := g.readSave(id)
	g.playOn(id)
	args := map[string]any{"world_id": id, "snapshot": snap.Name, "countdown_seconds": 0}

	preview, res, out := confirmCall(t, cs, "restore_snapshot", args)
	if !strings.Contains(asJSON(preview["will"]), "First, stop the game") || !strings.Contains(asJSON(preview["untrusted_online"]), "bob") {
		t.Errorf("preview: %s", asJSON(preview))
	}
	if res.IsError || out["finished"] != true || string(g.readSave(id)) != string(saved) || g.ctrl.Status(id).State != game.StateRunning {
		t.Fatalf("confirm: %s %s, state %s", resultText(res), asJSON(out), g.ctrl.Status(id).State)
	}
}

// SPEC 6.6: a deleted world's snapshot can be restored through its id, which
// makes the world again and takes no snapshot first.
func TestMCPRestoreDeletedWorld(t *testing.T) {
	g, cs, _, _, _ := adminRig(t)
	other := g.newWorld("Other", "2.1.24-no-space-age.zip", false)
	snap, err := g.worlds.TakeSnapshot(other, "Old times", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.worlds.Delete(context.Background(), other, "Other", false, "alice"); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"world_id": other, "snapshot": snap.Name}
	preview, res, out := confirmCall(t, cs, "restore_snapshot", args)
	if preview["snapshot"] != nil || preview["world_deleted"] != true || !strings.Contains(asJSON(preview["will"]), "recreate this world") {
		t.Errorf("preview: %s", asJSON(preview))
	}
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if _, err := g.worlds.Get(context.Background(), other); err != nil {
		t.Errorf("the world was not recreated: %v", err)
	}
}

// SPEC 6.5, 21.8: the mod steps a "Before mod change" snapshot undoes are
// listed in untrusted_mod_steps.
func TestMCPRestoreListsModSteps(t *testing.T) {
	g, cs, id, _, _ := adminRig(t)
	mods := filepath.Join(g.worlds.Dirs().Worlds, id, world.ModsDir)
	if err := os.MkdirAll(mods, 0o750); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(mods, "zzz_1.0.0.zip")
	for f, text := range map[string]string{zip: "x", filepath.Join(mods, "mod-list.json"): `{"mods":[{"name":"base","enabled":true},{"name":"zzz","enabled":true}]}`} {
		if err := os.WriteFile(f, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := g.worlds.AutoSnapshot(id, world.ReasonMods, world.LabelBeforeMods, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(zip); err != nil {
		t.Fatal(err)
	}
	_, out := call(t, cs, "restore_snapshot", map[string]any{"world_id": id, "snapshot": snap.Name})
	if !strings.Contains(asJSON(out["untrusted_mod_steps"]), "zzz") {
		t.Errorf("mod steps: %s", asJSON(out))
	}
}

// SPEC 21.8: a moved state refuses the confirm and returns a fresh preview
// and code; the old code is spent.
func TestMCPRestoreRefusesAMovedState(t *testing.T) {
	g, cs, id, snap, _ := adminRigUnplayed(t)
	args := map[string]any{"world_id": id, "snapshot": snap.Name, "countdown_seconds": 0}
	old := askCode(t, cs, "restore_snapshot", args)
	// The game starts, so the restore now stops it first: a different preview.
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage(running)
	res, out := call(t, cs, "restore_snapshot", withCode(args, old))
	fresh, _ := out[confirmCodeKey].(string)
	if !res.IsError || out["done"] != false || fresh == "" || fresh == old || !strings.Contains(asJSON(out["will"]), "First, stop the game") {
		t.Fatalf("a moved state: %s %s", resultText(res), asJSON(out))
	}
	if got := snapshotNamesOf(t, g, id); len(got) != 1 || g.hasAudit(game.AuditRestore) {
		t.Fatalf("the refused call restored: snapshots %v", got)
	}
	if res, _ := call(t, cs, "restore_snapshot", withCode(args, old)); !res.IsError {
		t.Error("the spent code worked again")
	}
	res, out = call(t, cs, "restore_snapshot", withCode(args, fresh))
	if res.IsError || out["done"] != true || len(g.auditByToken(game.AuditRestore)) != 1 {
		t.Fatalf("the fresh code: %s %s", resultText(res), asJSON(out))
	}
}

// SPEC 6.5: the tool is refused for the reasons the page is: a snapshot or
// world that does not exist, a countdown out of range, a missing argument.
func TestMCPRestoreRefusals(t *testing.T) {
	_, cs, id, snap, _ := adminRig(t)
	for name, args := range map[string]map[string]any{
		"no snapshot":     {"world_id": id, "snapshot": "20200101T000000Z"},
		"not a name":      {"world_id": id, "snapshot": "../etc"},
		"no world":        {"world_id": "nosuchworld", "snapshot": snap.Name},
		"bad countdown":   {"world_id": id, "snapshot": snap.Name, "countdown_seconds": 601},
		"no world id":     {"snapshot": snap.Name},
		"no snapshot arg": {"world_id": id},
	} {
		res, _ := call(t, cs, "restore_snapshot", args)
		if !res.IsError {
			t.Errorf("%s was accepted: %s", name, resultText(res))
		}
	}
}

// SPEC 6.5, 21.8: a restore preview and confirm page say the same things.
func TestRestoreWillMatchesThePage(t *testing.T) {
	snap := world.Snapshot{Name: "20261102T120000Z", Manifest: world.Manifest{Label: "Keep me", Reason: world.ReasonManual,
		TakenAt: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC), Files: []world.ManifestEntry{{Path: "saves/base.zip", Size: 2048}, {Path: "map.json", Size: 10}}}}
	for name, p := range map[string]RestorePage{
		"stopped": {WorldID: "base", WorldName: "Base", Snapshot: snap, CountdownForm: defaultCountdown()},
		"running": {WorldID: "base", WorldName: "Base", Snapshot: snap, Running: true, CountdownForm: defaultCountdown()},
		"deleted": {WorldID: "base", WorldName: "Base", Snapshot: snap, Deleted: true, CountdownForm: defaultCountdown()},
	} {
		t.Run(name, func(t *testing.T) { wantWill(t, restoreWill(p), pageText(t, p)) })
	}
}

func threeSnapshots(t *testing.T, g *gameRig, id string) []world.Snapshot {
	t.Helper()
	var out []world.Snapshot
	for _, label := range []string{"One", "Two", "Three"} {
		g.clock.Advance(time.Minute)
		snap, err := g.worlds.TakeSnapshot(id, label, "alice")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, snap)
	}
	return out
}

// SPEC 6.12, 21.8, 12.9: the first call deletes nothing; the second deletes
// each named snapshot, auditing the token's name before each delete.
func TestMCPDeleteSnapshots(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	snaps := threeSnapshots(t, g, id)
	args := map[string]any{"world_id": id, "snapshots": []string{snaps[0].Name, snaps[1].Name}}
	digest, audits := dataDigest(t, g.worlds.Dirs()), len(g.audit())

	res, out := call(t, cs, "delete_snapshots", args)
	if res.IsError || out["done"] != false || out[confirmCodeKey] == "" || !strings.Contains(asJSON(out["deleting"]), `"untrusted_label":"One"`) {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(out))
	}
	if dataDigest(t, g.worlds.Dirs()) != digest || len(g.audit()) != audits {
		t.Fatal("the preview changed the data folder or the audit log")
	}

	_, res, out = confirmCall(t, cs, "delete_snapshots", args)
	if res.IsError || out["done"] != true || out["finished"] != true || out["deleted"] != float64(2) {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if got := snapshotNamesOf(t, g, id); len(got) != 1 || got[0] != snaps[2].Name {
		t.Errorf("snapshots left: %v", got)
	}
	if got := g.auditByToken(AuditSnapshotDelete); len(got) != 2 || got[0].Username != "alice" {
		t.Errorf("delete audit by token: %+v", got)
	}
}

// SPEC 6.12: a snapshot already gone is listed, not an error, and a call
// with nothing to delete is refused without a code.
func TestMCPDeleteSnapshotsNothingToDelete(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	res, out := call(t, cs, "delete_snapshots", map[string]any{"world_id": id, "snapshots": []string{"20200101T000000Z"}})
	if !res.IsError || out[confirmCodeKey] != nil || !strings.Contains(asJSON(out["already_gone"]), "20200101T000000Z") {
		t.Errorf("a snapshot already gone: %s %s", resultText(res), asJSON(out))
	}
	for name, args := range map[string]map[string]any{
		"no names":  {"world_id": id, "snapshots": []string{}},
		"bad name":  {"world_id": id, "snapshots": []string{"../x"}},
		"bad world": {"world_id": "../x", "snapshots": []string{"20200101T000000Z"}},
	} {
		if res, _ := call(t, cs, "delete_snapshots", args); !res.IsError {
			t.Errorf("%s was accepted: %s", name, resultText(res))
		}
	}
}

// SPEC 21.8: a snapshot deleted after the preview refuses the confirm with a
// fresh preview; nothing was deleted.
func TestMCPDeleteSnapshotsRefusesAMovedState(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	snaps := threeSnapshots(t, g, id)
	args := map[string]any{"world_id": id, "snapshots": []string{snaps[0].Name, snaps[1].Name}}
	old := askCode(t, cs, "delete_snapshots", args)
	if err := g.worlds.DeleteSnapshot(id, snaps[0].Name, func(world.Snapshot) error { return nil }); err != nil {
		t.Fatal(err)
	}
	res, out := call(t, cs, "delete_snapshots", withCode(args, old))
	fresh, _ := out[confirmCodeKey].(string)
	if !res.IsError || fresh == "" || fresh == old || !strings.Contains(asJSON(out["already_gone"]), snaps[0].Name) {
		t.Fatalf("a moved state: %s %s", resultText(res), asJSON(out))
	}
	if got := snapshotNamesOf(t, g, id); len(got) != 2 {
		t.Fatalf("the refused call deleted: %v", got)
	}
	if g.hasAudit(AuditSnapshotDelete) {
		t.Error("the refused call was audited")
	}
	res, out = call(t, cs, "delete_snapshots", withCode(args, fresh))
	if res.IsError || out["deleted"] != float64(1) {
		t.Fatalf("the fresh code: %s %s", resultText(res), asJSON(out))
	}
}

// SPEC 6.12: a snapshot of a deleted world is deleted through its id; and
// one an operation may need stays, as on the page.
func TestMCPDeleteSnapshotsOfADeletedWorld(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	other := g.newWorld("Other", "2.1.24-no-space-age.zip", false)
	snap, err := g.worlds.TakeSnapshot(other, "Old times", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := g.worlds.Delete(context.Background(), other, "Other", false, "alice"); err != nil {
		t.Fatal(err)
	}
	_, res, out := confirmCall(t, cs, "delete_snapshots", map[string]any{"world_id": other, "snapshots": []string{"20200101T000000Z", snap.Name}})
	if res.IsError || out["deleted"] != float64(1) {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if got := snapshotNamesOf(t, g, other); len(got) != 0 {
		t.Errorf("snapshots left: %v", got)
	}
	if !strings.Contains(asJSON(out["world"]), `"name":"Other"`) {
		t.Errorf("the deleted world's name: %s", asJSON(out["world"]))
	}
}

// SPEC 6.12: a snapshot already gone is not a snapshot deleted, so a call
// whose only other snapshot stays is an error that deleted nothing.
func TestMCPDeleteSnapshotsErrorWhenNothingWasRemoved(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	snaps := threeSnapshots(t, g, id)
	args := map[string]any{"world_id": id, "snapshots": []string{"20200101T000000Z", snaps[0].Name}}
	code := askCode(t, cs, "delete_snapshots", args)
	unlock, err := g.worlds.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	res, out := call(t, cs, "delete_snapshots", withCode(args, code))
	if !res.IsError || out["deleted"] != nil || !strings.Contains(asJSON(out["staying"]), snaps[0].Name) {
		t.Fatalf("one gone, one stays: %s %s", resultText(res), asJSON(out))
	}
	if got := snapshotNamesOf(t, g, id); len(got) != 3 {
		t.Errorf("snapshots left: %v", got)
	}
}

// SPEC 6.12: while an operation runs on the world its snapshots stay.
func TestMCPDeleteSnapshotsStayWhileAnOperationRuns(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.startRunning()
	g.join(id, "bob")
	snap, err := g.worlds.TakeSnapshot(id, "Keep me", "alice")
	if err != nil {
		t.Fatal(err)
	}
	was := mcpWait
	mcpWait = 50 * time.Millisecond
	t.Cleanup(func() { mcpWait = was })
	if res, out := call(t, cs, "restart_server", map[string]any{"world_id": id, "countdown_seconds": 5}); res.IsError || out["finished"] != false {
		t.Fatalf("restart: %s %s", resultText(res), asJSON(out))
	}
	// restart_server answers after mcpWait whether or not the restart has
	// begun, so wait for its countdown before the delete (it was a flake
	// on a loaded CI runner).
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if op := g.ctrl.Status(id).Operation; op != nil {
			if _, ok := op.CountdownEnds(); ok {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the restart's countdown never began")
		}
	}
	res, out := call(t, cs, "delete_snapshots", map[string]any{"world_id": id, "snapshots": []string{snap.Name}})
	if !res.IsError || !strings.Contains(asJSON(out["staying"]), "operation is running") {
		t.Fatalf("while an operation runs: %s %s", resultText(res), asJSON(out))
	}
	if got := snapshotNamesOf(t, g, id); len(got) != 1 {
		t.Errorf("snapshots left: %v", got)
	}
}

// SPEC 6.12, 21.8: a delete preview and confirm page say the same things,
// including the rollback warning.
func TestBulkWillMatchesThePage(t *testing.T) {
	snap := world.Snapshot{Name: "20261102T120000Z", Manifest: world.Manifest{Label: "Before update", Reason: world.ReasonSwitch,
		TakenAt: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}}
	p := SnapshotBulkDeletePage{Frees: 4096, Items: []DeleteItem{
		{WorldID: "base", WorldName: "Base", Snapshot: snap, Rollback: "2.0.77"},
		{WorldID: "base", WorldName: "Base", Snapshot: world.Snapshot{Name: "20261102T130000Z", Manifest: world.Manifest{Label: "Two", Reason: world.ReasonManual, TakenAt: snap.TakenAt}}},
	}}
	wantWill(t, bulkWill(p), pageText(t, p))
}
