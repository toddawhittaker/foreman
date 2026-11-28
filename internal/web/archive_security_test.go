package web

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/schedule"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Gate tests for archived worlds (SPEC 5.9, 6.5,
// 21.7): no route a person or a token can reach starts an archived world.
// archive_test.go covers the pages and tools themselves; the controller's
// own refusals are in internal/game.

// archiveOnDisk sets a world's archived flag in world.json directly. It
// reaches the one state SetArchived refuses, an archived active world, as a
// hand edit or a restored world.json from elsewhere would.
func archiveOnDisk(t *testing.T, worlds *world.Manager, id string) {
	t.Helper()
	root, err := worlds.OpenWorld(id)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	meta, err := world.ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	meta.Archived = true
	if err := world.WriteMeta(root, meta); err != nil {
		t.Fatal(err)
	}
}

// creates counts the containers Docker was asked to create.
func (g *gameRig) creates() int {
	n := 0
	for _, c := range g.fake.State().Calls {
		if strings.HasPrefix(c, "POST /containers/create") {
			n++
		}
	}
	return n
}

func (g *gameRig) isArchived(id string) bool {
	g.t.Helper()
	sum, err := g.worlds.Get(context.Background(), id)
	if err != nil {
		g.t.Fatal(err)
	}
	return sum.Archived
}

func wantArchivedRefusal(t *testing.T, what string, err error) {
	t.Helper()
	var se *game.StartError
	if !errors.As(err, &se) || se.Kind != game.StartArchived {
		t.Errorf("%s: %v, want the archived refusal", what, err)
	}
}

// mcpDidNotStart checks an action tool's answer: refused, or run and failed.
func mcpDidNotStart(t *testing.T, what string, res *mcp.CallToolResult, out map[string]any) {
	t.Helper()
	outcome, _ := out["outcome"].(map[string]any)
	if !res.IsError && (outcome == nil || outcome["ok"] != false) {
		t.Errorf("%s: %s %s, want a refusal", what, resultText(res), asJSON(out))
	}
}

// mcpTwoStep makes a two-step call and, when the preview gives a code,
// confirms it. Either step may refuse.
func mcpTwoStep(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	res, out := call(t, cs, tool, args)
	code, _ := out[confirmCodeKey].(string)
	if res.IsError || code == "" {
		return res, out
	}
	return call(t, cs, tool, withCode(args, code))
}

// SPEC 5.9: an archived world that is somehow active and stopped is not
// started by the Server page's Start or Restart, nor by start_server or
// restart_server. No container is made.
func TestArchivedActiveWorldIsNotStarted(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	archiveOnDisk(t, g.worlds, id)

	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	wantArchivedRefusal(t, "Start", g.waitResult(id, game.AuditStart))
	g.wantRedirectHome(g.post(gamePath(id, "restart"), url.Values{"countdown": {"0"}}))
	wantArchivedRefusal(t, "Restart", g.waitResult(id, game.AuditRestart))
	for _, tool := range []string{"start_server", "restart_server"} {
		res, out := call(t, cs, tool, map[string]any{"world_id": id, "countdown_seconds": 0})
		mcpDidNotStart(t, tool, res, out)
	}
	if n := g.creates(); n != 0 {
		t.Errorf("%d containers created for an archived world", n)
	}
	if !g.isArchived(id) {
		t.Error("the world was unarchived")
	}
}

// SPEC 5.9, 7.9: a switch to an archived world is refused by the switch
// page and by switch_world; the running game is not touched.
func TestSwitchToAnArchivedWorldIsRefused(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	a := g.newWorld("Alpha", "2.1.24-no-space-age.zip", false)
	b := g.newWorld("Bravo", "2.1.24-no-space-age.zip", false)
	g.startGame(a)
	if _, err := g.worlds.SetArchived(context.Background(), b, true); err != nil {
		t.Fatal(err)
	}
	creates := g.creates()

	for _, start := range []string{"", "1"} {
		rep := g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"0"}, "start": {start}})
		if rep.StatusCode < 400 {
			t.Errorf("switch page (start=%q): %d, want a refusal", start, rep.StatusCode)
		}
	}
	for _, start := range []bool{false, true} {
		res, out := call(t, cs, "switch_world", map[string]any{"world_id": b, "countdown_seconds": 0, "start": start})
		mcpDidNotStart(t, "switch_world", res, out)
	}
	time.Sleep(200 * time.Millisecond) // an accepted switch runs in the background
	if active, _, _ := g.store.ActiveWorld(context.Background()); active != a {
		t.Errorf("the active world is %s, want Alpha", active)
	}
	if g.containerStatus(a) != "running" || g.creates() != creates {
		t.Error("a refused switch stopped or recreated the game")
	}
}

// SPEC 5.9, 7.12: a restart of a running world that is archived is refused
// before the game stops, from the Server page, from restart_server and from
// the restart schedule, so players are not thrown out for nothing.
func TestRunningArchivedWorldIsNotRestarted(t *testing.T) {
	for _, how := range []string{"page", "restart_server", "schedule"} {
		t.Run(how, func(t *testing.T) {
			g, cs := mcpRig(t, auth.ScopeAdmin, image21)
			id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
			g.startGame(id)
			archiveOnDisk(t, g.worlds, id)
			creates := g.creates()
			switch how {
			case "page":
				g.wantRedirectHome(g.post(gamePath(id, "restart"), url.Values{"countdown": {"0"}}))
				wantArchivedRefusal(t, how, g.waitResult(id, game.AuditRestart))
			case "restart_server":
				res, out := call(t, cs, "restart_server", map[string]any{"world_id": id, "countdown_seconds": 0})
				mcpDidNotStart(t, how, res, out)
			case "schedule":
				// The call the restart schedule makes (internal/schedule), with
				// no countdown so a wrong answer cannot wait a minute.
				err := g.ctrl.Restart(context.Background(), id, schedule.Actor,
					game.StopOptions{WarnEmpty: true, OnlyRunning: true}, game.StartOptions{})
				wantArchivedRefusal(t, how, err)
			}
			if s := g.containerStatus(id); s != "running" {
				t.Errorf("the refused restart left the game %s", s)
			}
			if g.creates() != creates {
				t.Error("the refused restart made a container")
			}
		})
	}
}

// relaunchRig is a world, its MCP session, and a snapshot to restore.
type relaunchRig struct {
	g    *gameRig
	cs   *mcp.ClientSession
	id   string
	snap string
}

func restoreRig(t *testing.T) relaunchRig {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	snap, err := g.worlds.TakeSnapshot(id, "Keep", "alice")
	if err != nil {
		t.Fatal(err)
	}
	return relaunchRig{g: g, cs: cs, id: id, snap: snap.Name}
}

func modRig(t *testing.T) relaunchRig {
	g, cs := modsMCP(t, true)
	return relaunchRig{g: g.gameRig, cs: cs, id: g.world20("Base", "alpha_1.0.0")}
}

func updateRig(t *testing.T) relaunchRig {
	g, cs := t5VersionRig(t)
	g.addImage(newVersion)
	return relaunchRig{g: g, cs: cs, id: g.newWorld("Base", fixtureNoSA, false)}
}

// SPEC 5.9: a restore, a mod change or an update of a running world that is
// archived never starts it again afterwards, through the pages or MCP.
func TestRunningArchivedWorldIsNotRelaunched(t *testing.T) {
	cases := []struct {
		name   string
		make   func(t *testing.T) relaunchRig
		act    func(t *testing.T, r relaunchRig)
		action string
	}{
		{"restore page", restoreRig, func(t *testing.T, r relaunchRig) {
			r.g.wantRedirectHome(r.g.post(restorePath(r.id, r.snap), url.Values{"countdown": {"0"}}))
		}, game.ActionRestore},
		{"restore_snapshot", restoreRig, func(t *testing.T, r relaunchRig) {
			mcpTwoStep(t, r.cs, "restore_snapshot", map[string]any{"world_id": r.id, "snapshot": r.snap, "countdown_seconds": 0})
		}, game.ActionRestore},
		{"mods page", modRig, func(t *testing.T, r relaunchRig) {
			v := planValues(t, r.g.get(kindPlanPath(r.id, mods.KindDisable, "alpha")))
			v.Set("countdown", "0")
			v.Set("confirm-drops", "1")
			r.g.wantRedirectHome(r.g.post(modsPath(r.id)+"/plan", v))
		}, game.AuditMods},
		{"change_mods", modRig, func(t *testing.T, r relaunchRig) {
			mcpTwoStep(t, r.cs, "change_mods", map[string]any{"world_id": r.id, "kind": "disable", "names": []string{"alpha"}, "countdown_seconds": 0})
		}, game.AuditMods},
		{"update page", updateRig, func(t *testing.T, r relaunchRig) {
			r.g.wantRedirectHome(r.g.post(updateURL(r.id), url.Values{"game_version": {newVersion}, "countdown": {"0"}}))
		}, game.AuditUpdate},
		{"update_game_version", updateRig, func(t *testing.T, r relaunchRig) {
			mcpTwoStep(t, r.cs, "update_game_version", map[string]any{"world_id": r.id, "version": newVersion, "countdown_seconds": 0})
		}, game.AuditUpdate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := c.make(t)
			r.g.startGame(r.id)
			archiveOnDisk(t, r.g.worlds, r.id)
			creates := r.g.creates()
			c.act(t, r)
			if err := r.g.waitResult(r.id, c.action); err == nil {
				t.Errorf("the %s reported success on an archived world", c.action)
			}
			if n := r.g.creates() - creates; n != 0 {
				t.Errorf("the %s started the archived world again (%d containers made)", c.action, n)
			}
			if !r.g.isArchived(r.id) {
				t.Error("the world is no longer archived")
			}
		})
	}
}

// SPEC 5.9, 6.5: a restore through the snapshot page or
// restore_snapshot keeps the world's archive state as it is now, whichever
// state the snapshot was taken in.
func TestRestoreKeepsTheArchiveStateThroughThePagesAndMCP(t *testing.T) {
	g, cs := mcpRig(t, auth.ScopeAdmin, image21)
	g.newWorld("Alpha", "2.1.24-no-space-age.zip", false)
	b := g.newWorld("Bravo", "2.1.24-no-space-age.zip", false)
	ctx := context.Background()
	unarchivedSnap, err := g.worlds.TakeSnapshot(b, "Before archive", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.worlds.SetArchived(ctx, b, true); err != nil {
		t.Fatal(err)
	}
	archivedSnap, err := g.worlds.TakeSnapshot(b, "While archived", "alice")
	if err != nil {
		t.Fatal(err)
	}

	if rep := g.post(restorePath(b, unarchivedSnap.Name), url.Values{}); rep.StatusCode >= 400 {
		t.Fatalf("restore page: %d\n%.400s", rep.StatusCode, rep.body)
	}
	if !g.isArchived(b) {
		t.Error("the restore page unarchived the world")
	}
	if res, out := mcpTwoStep(t, cs, "restore_snapshot", map[string]any{"world_id": b, "snapshot": unarchivedSnap.Name}); res.IsError {
		t.Fatalf("restore_snapshot: %s %s", resultText(res), asJSON(out))
	}
	if !g.isArchived(b) {
		t.Error("restore_snapshot unarchived the world")
	}

	if _, err := g.worlds.SetArchived(ctx, b, false); err != nil {
		t.Fatal(err)
	}
	if res, out := mcpTwoStep(t, cs, "restore_snapshot", map[string]any{"world_id": b, "snapshot": archivedSnap.Name}); res.IsError {
		t.Fatalf("restore_snapshot: %s %s", resultText(res), asJSON(out))
	}
	if g.isArchived(b) {
		t.Error("restoring a snapshot taken while archived archived the world")
	}
}

// SPEC 5.9: archiving a world and switching to it at the same moment never
// leave it both archived and active: both take the world's lock.
func TestArchiveAndSwitchRaceLeavesNoArchivedActiveWorld(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", "2.1.24-no-space-age.zip", false)
	b := g.newWorld("Bravo", "2.1.24-no-space-age.zip", false)
	ctx := context.Background()
	by := game.Actor{Username: "alice"}
	for i := range 50 {
		var wg sync.WaitGroup
		gate := make(chan struct{})
		wg.Go(func() { <-gate; _, _ = g.worlds.SetArchived(ctx, b, true) })
		wg.Go(func() { <-gate; _ = g.ctrl.Switch(ctx, a, b, by, game.StopOptions{}, game.StartOptions{}, false) })
		close(gate)
		wg.Wait()
		active, _, _ := g.store.ActiveWorld(ctx)
		if active == b && g.isArchived(b) {
			t.Fatalf("round %d: Bravo is both active and archived", i)
		}
		// Back to the start: Alpha active, Bravo not archived.
		if active == b {
			if err := g.ctrl.Switch(ctx, b, a, by, game.StopOptions{}, game.StartOptions{}, false); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := g.worlds.SetArchived(ctx, b, false); err != nil {
			t.Fatal(err)
		}
	}
}
