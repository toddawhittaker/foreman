//go:build game

package web

// MCP against a real factoriotools/factorio container (SPEC 21.4 to 21.6,
// 12.10, 18.10's gate in small): an operate token reads server_status,
// takes a snapshot audited under the token's name, says chat the game logs
// with the "[name] " prefix, and restarts with a countdown; a read token is
// refused the restart. Run by `make test-game`, or alone with
// `go test -tags game -run TestRealGameMCP ./internal/web/`; `make verify`
// runs neither.

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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

const mcpGameVersion = "2.1.24"

func TestRealGameMCP(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && !strings.HasPrefix(mcpGameVersion, only) {
		t.Skipf("runs on %s only", mcpGameVersion)
	}
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	dc, err := docker.New(host)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dc.InspectImage(context.Background(), game.ImageRef(mcpGameVersion)); err != nil {
		t.Fatalf("the game image %s is not on this host (%v). Pull it with docker pull, then run again", mcpGameVersion, err)
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
	ls := newLiveServerFor(t, hn)
	u := addTestUser(t, hn)
	turnMCPOn(t, hn)
	opSecret, _ := apiToken(t, hn, u, "Claude", auth.ScopeOperate)
	readSecret, _ := apiToken(t, hn, u, "Reader", auth.ScopeRead)
	alice := game.Actor{UserID: u.ID, Username: u.Username}

	ctx := context.Background()
	id, err := hn.worlds.Generate(ctx, world.NewWorld{Name: "mcp-realgame3", GameVersion: mcpGameVersion})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(game.ContainerName(id), "foreman-mcp-realgame3") {
		t.Fatalf("container name %s is not recognisable as this test's", game.ContainerName(id))
	}
	t.Cleanup(func() {
		_ = ctrl.Stop(context.Background(), id, alice, game.StopOptions{})
		if err := dc.RemoveContainer(context.Background(), game.ContainerName(id), true); err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "No such container") {
			t.Errorf("could not remove test container: %v", err)
		}
	})
	if sum, err := hn.worlds.Get(ctx, id); err != nil || !sum.Active {
		t.Fatalf("the only world is not active: %+v %v", sum, err)
	}
	if err := ctrl.Start(ctx, id, alice, game.StartOptions{}); err != nil {
		t.Fatalf("start: %v", err)
	}

	cs := mcpConnect(t, ls.url, opSecret)

	// server_status reads the running game.
	_, st := call(t, cs, "server_status", map[string]any{"world_id": id})
	if st["state"] != string(game.StateRunning) || st["game_version"] != mcpGameVersion {
		t.Fatalf("status: %s", asJSON(st))
	}

	// take_snapshot: the game saves first; the audit row has the token's name.
	res, out := call(t, cs, "take_snapshot", map[string]any{"world_id": id, "label": "MCP real game"})
	if res.IsError || out["finished"] != true {
		t.Fatalf("snapshot: %s %s", resultText(res), asJSON(out))
	}
	if !slices.ContainsFunc(auditRows(t, hn), func(e store.AuditEntry) bool {
		return e.TokenName == "Claude" && strings.Contains(e.Action, "snapshot")
	}) {
		t.Errorf("no snapshot audit under the token's name: %+v", auditRows(t, hn))
	}

	// send_chat: the game's own log shows the prefixed line.
	res, out = call(t, cs, "send_chat", map[string]any{"world_id": id, "message": "hello from the test"})
	if res.IsError || out["sent"] != "[Claude] hello from the test" {
		t.Fatalf("chat: %s %s", resultText(res), asJSON(out))
	}
	waitUntil(t, 20*time.Second, "the chat line in the game's log", func() bool {
		return slices.ContainsFunc(ctrl.Lines(id), func(l string) bool { return strings.Contains(l, "[Claude] hello from the test") })
	})

	// A read token is refused restart_server, and the game is untouched.
	reader := mcpConnect(t, ls.url, readSecret)
	if r, err := reader.CallTool(ctx, &mcp.CallToolParams{Name: "restart_server", Arguments: map[string]any{"world_id": id}}); err == nil && !r.IsError {
		t.Errorf("a read token restarted the game: %s", resultText(r))
	}
	startedAt := ctrl.Status(id).StartedAt

	// restart_server with a short countdown: finished, or still running and
	// then reported by server_status.
	res, out = call(t, cs, "restart_server", map[string]any{"world_id": id, "countdown_seconds": 3})
	if res.IsError {
		t.Fatalf("restart: %s %s", resultText(res), asJSON(out))
	}
	t.Logf("restart answered: %s", resultText(res))
	if out["finished"] == false && !strings.Contains(resultText(res), "still running") {
		t.Errorf("an unfinished restart does not say so: %s", resultText(res))
	}
	waitUntil(t, 3*time.Minute, "server_status to report the restart", func() bool {
		_, st = call(t, cs, "server_status", map[string]any{"world_id": id})
		last, ok := st["last_result"].(map[string]any)
		return ok && last["action"] == game.AuditRestart && st["running_action"] == nil
	})
	if last := st["last_result"].(map[string]any); last["ok"] != true || st["state"] != string(game.StateRunning) {
		t.Errorf("after the restart: %s", asJSON(st))
	}
	if !ctrl.Status(id).StartedAt.After(startedAt) {
		t.Errorf("the game was not started again")
	}
	if !slices.ContainsFunc(auditRows(t, hn), func(e store.AuditEntry) bool {
		return e.TokenName == "Claude" && e.Action == game.AuditRestart
	}) {
		t.Errorf("no restart audit under the token's name")
	}
}

func auditRows(t *testing.T, hn *harness) []store.AuditEntry {
	t.Helper()
	list, err := hn.store.ListAudit(context.Background(), store.AuditFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func waitUntil(t *testing.T, limit time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
