//go:build game

package web

// The admin tools against a real factoriotools/factorio container (SPEC
// 18.10b, 21.7, 21.8): change_mods disables a probe mod placed in the
// world, restore_snapshot puts back a manual snapshot, and
// update_game_version moves the world from 2.0.77 to 2.1.24, each in two
// steps through MCP. Each takes the snapshot (reason and label) and writes
// the audit row the UI path does for the same action, with the token's
// name, and the game reaches InGame afterwards. No portal is called: the
// probe mod is a local zip. Run by `make test-game`, or alone with
// `go test -tags game -run TestRealGameMCPAdmin ./internal/web/`.

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/releases"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	adminGameFrom  = "2.0.77"
	adminGameTo    = "2.1.24"
	adminProbeMod  = "foreman-t7-probe"
	adminInGameLog = "changing state from(CreatingGame) to(InGame)"
)

func TestRealGameMCPAdmin(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && only != "2.0" && only != "2.1" {
		t.Skipf("runs on %s and %s only", adminGameFrom, adminGameTo)
	}
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	dc, err := docker.New(host)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{adminGameFrom, adminGameTo} {
		if _, err := dc.InspectImage(context.Background(), game.ImageRef(v)); err != nil {
			t.Fatalf("the game image %s is not on this host (%v). Pull it with docker pull, then run again", v, err)
		}
	}

	var ctrl *game.Controller
	makeGame := func(worlds *world.Manager, st *store.Store) *game.Controller {
		c, err := game.New(game.Config{
			Docker: dc, Worlds: worlds, Audit: st, Switches: st,
			HostDataDir: filepath.Dir(worlds.Dirs().Worlds), GameUID: os.Getuid(), GameGID: os.Getgid(),
			Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		t.Cleanup(func() { cancel(); <-done })
		ctrl = c
		return c
	}
	hn := newHarnessWith(t, nil, makeGame)
	// A handler with a mod planner (over a local fake portal that the
	// disable never needs) and a release list, so every admin tool is there.
	h, err := NewHandler(Deps{Config: hn.cfg, Store: hn.store, Worlds: hn.worlds, Game: ctrl, Log: hn.slog, Now: hn.clock.Now,
		Portal:   modportal.NewForTest(modportaltest.New(t).URL),
		Releases: &fakeReleases{versions: []string{adminGameFrom, adminGameTo}, latest: releases.Latest{Stable: adminGameTo}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	hn.h = h
	ls := newLiveServerFor(t, hn)
	u := addTestUser(t, hn)
	turnMCPOn(t, hn)
	secret, _ := apiToken(t, hn, u, "Claude", auth.ScopeAdmin)
	alice := game.Actor{UserID: u.ID, Username: u.Username}
	ctx := context.Background()

	id, err := hn.worlds.Create(ctx, "mcp-realgame4", adminGameFrom, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(game.ContainerName(id), "foreman-mcp-realgame4") {
		t.Fatalf("container name %s is not recognisable as this test's", game.ContainerName(id))
	}
	t.Cleanup(func() {
		_ = ctrl.Stop(context.Background(), id, alice, game.StopOptions{})
		if err := dc.RemoveContainer(context.Background(), game.ContainerName(id), true); err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "No such container") {
			t.Errorf("could not remove test container: %v", err)
		}
	})
	wdir := filepath.Join(hn.worlds.Dirs().Worlds, id)
	save, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", "2.0.77-no-space-age.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wdir, world.SavesDir, "main.zip"), save, 0o600); err != nil { //nolint:gosec // the test's own folder
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wdir, world.ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wdir, world.ModsDir, adminProbeMod+"_1.0.0.zip"), modZip(t, adminProbeMod, "1.0.0"), 0o600); err != nil {
		t.Fatal(err)
	}
	modList := `{"mods":[{"name":"base","enabled":true},{"name":"` + adminProbeMod + `","enabled":true}]}`
	if err := os.WriteFile(filepath.Join(wdir, world.ModsDir, world.ModListFile), []byte(modList), 0o600); err != nil {
		t.Fatal(err)
	}

	cs := mcpConnect(t, ls.url, secret)
	inGames := func() int {
		n := 0
		for _, l := range ctrl.Lines(id) {
			if strings.Contains(l, adminInGameLog) {
				n++
			}
		}
		return n
	}
	// settle waits for the action to end, starts the game if the action
	// left it stopped, and waits for a new InGame line on version.
	settle := func(t *testing.T, action string, before int, version string) {
		t.Helper()
		var st map[string]any
		waitUntil(t, 4*time.Minute, "server_status to report "+action, func() bool {
			_, st = call(t, cs, "server_status", map[string]any{"world_id": id})
			last, ok := st["last_result"].(map[string]any)
			return ok && last["action"] == action && st["running_action"] == nil
		})
		if last := st["last_result"].(map[string]any); last["ok"] != true {
			t.Fatalf("%s failed: %s", action, asJSON(st))
		}
		if ctrl.Status(id).State != game.StateRunning {
			if err := ctrl.Start(ctx, id, alice, game.StartOptions{}); err != nil {
				t.Fatalf("start after %s: %v", action, err)
			}
		}
		waitUntil(t, 3*time.Minute, "InGame after "+action, func() bool { return inGames() > before })
		if got := ctrl.Status(id).GameVersion; got != version {
			t.Fatalf("after %s the game runs %s, want %s", action, got, version)
		}
	}
	// twoStep runs a tool's preview, checks it named label, then confirms.
	twoStep := func(t *testing.T, tool string, args map[string]any, label string) {
		t.Helper()
		count := func() int {
			list, err := hn.worlds.Snapshots(id)
			if err != nil {
				t.Fatal(err)
			}
			return len(list)
		}
		snaps := count()
		res, preview := call(t, cs, tool, args)
		if res.IsError || preview["done"] != false || preview["snapshot"] != label {
			t.Fatalf("%s preview: %s %s", tool, resultText(res), asJSON(preview))
		}
		if count() != snaps {
			t.Fatalf("%s's preview took a snapshot", tool)
		}
		res, out := call(t, cs, tool, withCode(args, preview[confirmCodeKey].(string)))
		if res.IsError || out["done"] != true {
			t.Fatalf("%s confirm: %s %s", tool, resultText(res), asJSON(out))
		}
	}
	// wantSnapshotAndAudit checks the newest snapshot and the action's audit
	// rows are what the UI path writes, but under the token's name.
	wantSnapshotAndAudit := func(t *testing.T, reason, label, action string) {
		t.Helper()
		list, err := hn.worlds.Snapshots(id)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(list, func(s world.Snapshot) bool { return s.Reason == reason && s.Label == label }) {
			t.Errorf("no %s snapshot labelled %q: %+v", reason, label, list)
		}
		rows := slices.DeleteFunc(auditRows(t, hn), func(e store.AuditEntry) bool { return e.Action != action })
		if len(rows) != 1 || rows[0].TokenName != "Claude" || rows[0].Username != "alice" || rows[0].WorldID != id {
			t.Errorf("%s audit rows: %+v", action, rows)
		}
	}

	before := inGames()
	if err := ctrl.Start(ctx, id, alice, game.StartOptions{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitUntil(t, 3*time.Minute, "InGame on "+adminGameFrom, func() bool { return inGames() > before })
	if !slices.ContainsFunc(ctrl.Lines(id), func(l string) bool { return strings.Contains(l, "Loading mod "+adminProbeMod) }) {
		t.Fatal("the game did not load the probe mod")
	}
	res, out := call(t, cs, "take_snapshot", map[string]any{"world_id": id, "label": "Keep"})
	if res.IsError || out["finished"] != true {
		t.Fatalf("snapshot: %s %s", resultText(res), asJSON(out))
	}
	var keepName string
	if taken, _ := out["snapshots_taken"].([]any); len(taken) == 1 {
		keep, _ := taken[0].(map[string]any)
		keepName, _ = keep["name"].(string)
	}
	if keepName == "" {
		t.Fatalf("take_snapshot named no snapshot: %s", asJSON(out))
	}

	t.Run("change_mods disables the probe", func(t *testing.T) {
		before := inGames()
		twoStep(t, "change_mods", map[string]any{"world_id": id, "kind": "disable", "names": []string{adminProbeMod}}, world.LabelBeforeMods)
		settle(t, game.AuditMods, before, adminGameFrom)
		wantSnapshotAndAudit(t, world.ReasonMods, world.LabelBeforeMods, game.AuditMods)
		b, _ := os.ReadFile(filepath.Join(wdir, world.ModsDir, world.ModListFile)) //nolint:gosec // the test's own folder
		if !strings.Contains(strings.Join(strings.Fields(string(b)), ""), `"name":"`+adminProbeMod+`","enabled":false`) {
			t.Errorf("mod-list.json: %s", b)
		}
	})
	t.Run("restore_snapshot puts back Keep", func(t *testing.T) {
		before := inGames()
		twoStep(t, "restore_snapshot", map[string]any{"world_id": id, "snapshot": keepName}, world.LabelBeforeRestore)
		settle(t, game.AuditRestore, before, adminGameFrom)
		wantSnapshotAndAudit(t, world.ReasonBeforeRestore, world.LabelBeforeRestore, game.AuditRestore)
	})
	t.Run("update_game_version to "+adminGameTo, func(t *testing.T) {
		before := inGames()
		twoStep(t, "update_game_version", map[string]any{"world_id": id, "version": adminGameTo}, world.LabelBeforeUpdate)
		settle(t, game.AuditUpdate, before, adminGameTo)
		wantSnapshotAndAudit(t, world.ReasonGameUpdate, world.LabelBeforeUpdate, game.AuditUpdate)
	})
}
