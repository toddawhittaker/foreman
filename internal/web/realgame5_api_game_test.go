//go:build game

package web

// The REST API against a real factoriotools/factorio container, over HTTP
// only (SPEC 18.10c's gate, 23): an operate token lists its tools, reads
// status, takes a snapshot, sends chat the game logs as "[name] ...",
// restarts with a countdown, and gets 404 for an admin tool. An admin
// token deletes a snapshot in two calls, the first changing nothing, and a
// code from /mcp works on /api/. Each switch leaves the other path
// working, and a token's 121st request in a minute, split across both
// paths, gets 429. Run by `make test-game`, or alone with
// `go test -tags game -run TestRealGameAPI ./internal/web/`.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

const apiGameVersion = "2.1.24"

// httpJSON sends one request to the live server and decodes its JSON answer.
func httpJSON(t *testing.T, ls *liveServer, method, path, secret, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, ls.url+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if path == PathMCP {
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	resp, err := ls.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]any{"text": string(b)}
	}
	return resp.StatusCode, out
}

// httpTool calls a tool on /api/ and returns the status and the envelope.
func httpTool(t *testing.T, ls *liveServer, secret, tool string, args map[string]any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(args)
	return httpJSON(t, ls, http.MethodPost, toolPath(tool), secret, string(b))
}

// httpMCPTool sends one tools/call to /mcp over HTTP and returns its
// structured result.
func httpMCPTool(t *testing.T, ls *liveServer, secret, tool string, args map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args}})
	status, out := httpJSON(t, ls, http.MethodPost, PathMCP, secret, string(b))
	res, _ := out["result"].(map[string]any)
	if status != http.StatusOK || res == nil {
		t.Fatalf("/mcp %s: %d %v", tool, status, out)
	}
	sc, _ := res["structuredContent"].(map[string]any)
	return sc
}

func resultOf(out map[string]any) map[string]any {
	r, _ := out["result"].(map[string]any)
	return r
}

func TestRealGameAPI(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && !strings.HasPrefix(apiGameVersion, only) {
		t.Skipf("runs on %s only", apiGameVersion)
	}
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	dc, err := docker.New(host)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dc.InspectImage(context.Background(), game.ImageRef(apiGameVersion)); err != nil {
		t.Fatalf("the game image %s is not on this host (%v). Pull it with docker pull, then run again", apiGameVersion, err)
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
	apiOnFor(t, hn)
	opSecret, _ := apiToken(t, hn, u, "Claude", auth.ScopeOperate)
	adminSecret, _ := apiToken(t, hn, u, "Admin", auth.ScopeAdmin)
	alice := game.Actor{UserID: u.ID, Username: u.Username}

	ctx := context.Background()
	id, err := hn.worlds.Generate(ctx, world.NewWorld{Name: "api-realgame5", GameVersion: apiGameVersion})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(game.ContainerName(id), "foreman-api-realgame5") {
		t.Fatalf("container name %s is not recognisable as this test's", game.ContainerName(id))
	}
	t.Cleanup(func() {
		_ = ctrl.Stop(context.Background(), id, alice, game.StopOptions{})
		if err := dc.RemoveContainer(context.Background(), game.ContainerName(id), true); err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "No such container") {
			t.Errorf("could not remove test container: %v", err)
		}
	})
	if err := ctrl.Start(ctx, id, alice, game.StartOptions{}); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The operate token lists its tools: the operate set, no admin tool.
	status, out := httpJSON(t, ls, http.MethodGet, PathAPITools, opSecret, "")
	tools, _ := out["tools"].([]any)
	var names []string
	for _, x := range tools {
		if m, ok := x.(map[string]any); ok {
			names = append(names, m["name"].(string))
		}
	}
	if status != http.StatusOK || !slices.Contains(names, "restart_server") || slices.Contains(names, "delete_snapshots") {
		t.Fatalf("operate list: %d %q", status, names)
	}

	// Status of the running game.
	status, out = httpTool(t, ls, opSecret, "server_status", map[string]any{"world_id": id})
	if st := resultOf(out); status != http.StatusOK || st["state"] != string(game.StateRunning) || st["game_version"] != apiGameVersion {
		t.Fatalf("status: %d %s", status, asJSON(out))
	}

	// A snapshot, audited under the token's name.
	status, out = httpTool(t, ls, opSecret, "take_snapshot", map[string]any{"world_id": id, "label": "API real game"})
	if status != http.StatusOK || resultOf(out)["finished"] != true {
		t.Fatalf("snapshot: %d %s", status, asJSON(out))
	}
	if !slices.ContainsFunc(auditRows(t, hn), func(e store.AuditEntry) bool {
		return e.TokenName == "Claude" && strings.Contains(e.Action, "snapshot")
	}) {
		t.Errorf("no snapshot audit under the token's name: %+v", auditRows(t, hn))
	}

	// Chat the game logs with the token's prefix.
	status, out = httpTool(t, ls, opSecret, "send_chat", map[string]any{"world_id": id, "message": "hello over REST"})
	if status != http.StatusOK || resultOf(out)["sent"] != "[Claude] hello over REST" {
		t.Fatalf("chat: %d %s", status, asJSON(out))
	}
	waitUntil(t, 20*time.Second, "the chat line in the game's log", func() bool {
		return slices.ContainsFunc(ctrl.Lines(id), func(l string) bool { return strings.Contains(l, "[Claude] hello over REST") })
	})

	// An admin tool is unknown to the operate token, and nothing happens.
	snaps, _ := hn.worlds.Snapshots(id)
	if status, out := httpTool(t, ls, opSecret, "delete_snapshots", map[string]any{"world_id": id, "snapshots": []string{snaps[0].Name}}); status != http.StatusNotFound {
		t.Errorf("an operate token's admin tool: %d %s", status, asJSON(out))
	}
	if now, _ := hn.worlds.Snapshots(id); len(now) != len(snaps) {
		t.Errorf("the refused delete removed a snapshot")
	}

	// A restart with a countdown, then reported by server_status.
	startedAt := ctrl.Status(id).StartedAt
	status, out = httpTool(t, ls, opSecret, "restart_server", map[string]any{"world_id": id, "countdown_seconds": 3})
	if status != http.StatusOK {
		t.Fatalf("restart: %d %s", status, asJSON(out))
	}
	waitUntil(t, 3*time.Minute, "server_status to report the restart", func() bool {
		_, out = httpTool(t, ls, opSecret, "server_status", map[string]any{"world_id": id})
		st := resultOf(out)
		last, ok := st["last_result"].(map[string]any)
		return ok && last["action"] == game.AuditRestart && st["running_action"] == nil
	})
	if st := resultOf(out); st["last_result"].(map[string]any)["ok"] != true || st["state"] != string(game.StateRunning) {
		t.Errorf("after the restart: %s", asJSON(out))
	}
	if !ctrl.Status(id).StartedAt.After(startedAt) {
		t.Errorf("the game was not started again")
	}

	// The admin token deletes a snapshot in two calls; the first changes
	// nothing.
	if status, out := httpTool(t, ls, opSecret, "take_snapshot", map[string]any{"world_id": id, "label": "second"}); status != http.StatusOK {
		t.Fatalf("second snapshot: %d %s", status, asJSON(out))
	}
	snaps, _ = hn.worlds.Snapshots(id)
	if len(snaps) < 2 {
		t.Fatalf("snapshots: %d", len(snaps))
	}
	first := snaps[0].Name
	args := map[string]any{"world_id": id, "snapshots": []string{first}}
	status, out = httpTool(t, ls, adminSecret, "delete_snapshots", args)
	code, _ := resultOf(out)[confirmCodeKey].(string)
	if status != http.StatusOK || resultOf(out)["done"] != false || code == "" {
		t.Fatalf("delete preview: %d %s", status, asJSON(out))
	}
	if now, _ := hn.worlds.Snapshots(id); len(now) != len(snaps) {
		t.Fatalf("the preview deleted a snapshot")
	}
	args[confirmCodeKey] = code
	if status, out := httpTool(t, ls, adminSecret, "delete_snapshots", args); status != http.StatusOK || resultOf(out)["done"] != true {
		t.Fatalf("delete confirm: %d %s", status, asJSON(out))
	}
	now, _ := hn.worlds.Snapshots(id)
	if slices.ContainsFunc(now, func(s world.Snapshot) bool { return s.Name == first }) {
		t.Errorf("the confirmed delete left %s", first)
	}

	// A code from /mcp works on /api/.
	second := now[0].Name
	args = map[string]any{"world_id": id, "snapshots": []string{second}}
	sc := httpMCPTool(t, ls, adminSecret, "delete_snapshots", args)
	code, _ = sc[confirmCodeKey].(string)
	if sc["done"] != false || code == "" {
		t.Fatalf("/mcp preview: %s", asJSON(sc))
	}
	args[confirmCodeKey] = code
	if status, out := httpTool(t, ls, adminSecret, "delete_snapshots", args); status != http.StatusOK || resultOf(out)["done"] != true {
		t.Fatalf("/api/ confirm of an /mcp code: %d %s", status, asJSON(out))
	}
	if now, _ := hn.worlds.Snapshots(id); slices.ContainsFunc(now, func(s world.Snapshot) bool { return s.Name == second }) {
		t.Errorf("the cross-endpoint delete left %s", second)
	}

	// Each switch off leaves the other path working.
	initBody := initMessage
	set := func(name, v string) {
		if err := hn.store.SetAppSetting(ctx, name, v, "alice", hn.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	set(SettingAPI, APIOff)
	if s, _ := httpJSON(t, ls, http.MethodGet, PathAPITools, opSecret, ""); s != http.StatusForbidden {
		t.Errorf("API off: /api/ %d", s)
	}
	if s, _ := httpJSON(t, ls, http.MethodPost, PathMCP, opSecret, initBody); s != http.StatusOK {
		t.Errorf("API off: /mcp %d", s)
	}
	set(SettingAPI, APIOn)
	set(SettingMCP, MCPOff)
	if s, _ := httpJSON(t, ls, http.MethodPost, PathMCP, opSecret, initBody); s != http.StatusForbidden {
		t.Errorf("MCP off: /mcp %d", s)
	}
	if s, _ := httpJSON(t, ls, http.MethodGet, PathAPITools, opSecret, ""); s != http.StatusOK {
		t.Errorf("MCP off: /api/ %d", s)
	}
	set(SettingMCP, MCPOn)

	// A fresh token's 121st request in a minute, split across both, is 429.
	limited, _ := apiToken(t, hn, u, "Limited", auth.ScopeRead)
	hn.clock.Advance(time.Minute + time.Second)
	for i := range auth.TokenRequestsPerMinute {
		var s int
		if i%2 == 0 {
			s, _ = httpJSON(t, ls, http.MethodPost, PathMCP, limited, initBody)
		} else {
			s, _ = httpJSON(t, ls, http.MethodGet, PathAPITools, limited, "")
		}
		if s != http.StatusOK {
			t.Fatalf("request %d: %d", i+1, s)
		}
	}
	if s, _ := httpJSON(t, ls, http.MethodGet, PathAPITools, limited, ""); s != http.StatusTooManyRequests {
		t.Errorf("the 121st request: %d", s)
	}
}
