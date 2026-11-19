package web

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
)

// initMessage is a JSON-RPC initialize request, enough to reach the SDK.
const initMessage = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

// turnMCPOn sets the admin setting that opens /mcp (SPEC 21.2).
func turnMCPOn(t *testing.T, hn *harness) {
	t.Helper()
	if err := hn.store.SetAppSetting(context.Background(), SettingMCP, MCPOn, "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

// apiToken makes an API token for the user and returns its secret.
func apiToken(t *testing.T, hn *harness, u store.User, name, scope string) (string, store.APIToken) {
	t.Helper()
	secret, tok, err := auth.NewAPITokens(hn.store, hn.clock.Now).Create(context.Background(), u.ID, name, scope)
	if err != nil {
		t.Fatal(err)
	}
	return secret, tok
}

// mcpPost sends one raw /mcp request.
func (ls *liveServer) mcpPost(method, body string, header http.Header) reply {
	ls.t.Helper()
	h := http.Header{"Content-Type": {"application/json"}, "Accept": {"application/json, text/event-stream"}}
	for k, v := range header {
		h[k] = v
	}
	req, err := http.NewRequest(method, ls.url+PathMCP, strings.NewReader(body))
	if err != nil {
		ls.t.Fatal(err)
	}
	req.Header = h
	resp, err := ls.client.Do(req)
	if err != nil {
		ls.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return reply{StatusCode: resp.StatusCode, Header: resp.Header, body: string(b)}
}

func bearer(secret string) http.Header { return http.Header{"Authorization": {"Bearer " + secret}} }

// bearerTransport adds the token to every request the SDK client sends.
type bearerTransport struct{ secret string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.secret)
	return http.DefaultTransport.RoundTrip(r)
}

// mcpConnect opens an SDK client session with the token.
func mcpConnect(t *testing.T, url, secret string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: url + PathMCP, HTTPClient: &http.Client{Transport: bearerTransport{secret}}, DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

var (
	readTools   = []string{"list_mods", "list_players", "list_snapshots", "list_worlds", "recent_log", "server_status"}
	actionTools = []string{"cancel_stop", "restart_server", "send_chat", "start_server", "stop_server", "switch_world", "take_snapshot"}
	// adminToolNames is the admin scope's own tools (SPEC 21.7), sorted.
	adminToolNames = []string{"add_webhook", "archive_world", "ban_player", "change_game_settings", "change_mod_settings", "change_mods", "change_webhook",
		"choose_save", "clone_world", "delete_favorite", "delete_snapshots", "delete_webhook", "delete_world", "download_game_image",
		"force_stop", "foreman_settings", "generate_world", "get_game_settings", "kick_player", "list_deleted_worlds", "list_favorites",
		"list_game_versions", "list_mod_settings", "list_saves", "make_favorite", "player_action", "remove_factorio_account",
		"rename_favorite", "rename_world", "restore_snapshot", "rollback_game_version", "run_console_command", "search_mods",
		"set_factorio_account", "set_public_mods", "set_restart_schedule", "set_whitelist_switch", "unarchive_world", "update_game_version"}
)

// SPEC 21.2: FOREMAN_MCP=off wins over the admin setting.
func TestMCPForcedOffWinsOverSetting(t *testing.T) {
	ls := newLiveServer(t, map[string]string{"FOREMAN_MCP": "off"})
	u := ls.addUser("alice", testPassword)
	turnMCPOn(t, ls.harness)
	secret, _ := apiToken(t, ls.harness, u, "Claude", auth.ScopeRead)
	if rep := ls.mcpPost(http.MethodPost, initMessage, bearer(secret)); rep.StatusCode != http.StatusForbidden {
		t.Errorf("forced off: %d %q", rep.StatusCode, rep.body)
	}
}

// failingTokenStore fails the test if Foreman looks a token up.
type failingTokenStore struct {
	auth.APITokenStore
	t *testing.T
}

func (f failingTokenStore) APITokenByHash(context.Context, string) (store.APIToken, bool, error) {
	f.t.Fatal("a malformed token was looked up")
	return store.APIToken{}, false, nil
}

// SPEC 14.19: a malformed token is refused without a database lookup.
func TestMCPMalformedTokenSkipsTheStore(t *testing.T) {
	hn := newHarness(t, nil)
	s := &server{cfg: hn.cfg, log: hn.slog, now: hn.clock.Now}
	verify := s.mcpVerifier(auth.NewAPITokens(failingTokenStore{t: t}, hn.clock.Now), auth.NewAddrLimiter(hn.clock.Now))
	r := httptest.NewRequest(http.MethodPost, PathMCP, nil)
	for _, secret := range []string{"", "nope", auth.TokenPrefix, auth.TokenPrefix + strings.Repeat("A", 42), "xyz_" + strings.Repeat("A", 43)} {
		if _, err := verify(context.Background(), secret, r); err == nil {
			t.Errorf("%q was accepted", secret)
		}
	}
}

// SPEC 21.3, 12.10: a client is listed only its scope's tools, and any
// other tool is unknown.
func TestMCPToolsFollowScope(t *testing.T) {
	// The mods rig has a game, a mod planner and favorites, so every admin
	// tool is registered.
	g := newModsRig(t, false).gameRig
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	turnMCPOn(t, g.harness)
	readSecret, _ := apiToken(t, g.harness, u, "Reader", auth.ScopeRead)
	opSecret, _ := apiToken(t, g.harness, u, "Operator", auth.ScopeOperate)
	adminSecret, _ := apiToken(t, g.harness, u, "Admin", auth.ScopeAdmin)

	reader := mcpConnect(t, g.url, readSecret)
	if got := toolNames(t, reader); !slices.Equal(got, readTools) {
		t.Errorf("read scope lists %q, want %q", got, readTools)
	}
	all := slices.Sorted(slices.Values(append(slices.Clone(readTools), actionTools...)))
	if got := toolNames(t, mcpConnect(t, g.url, opSecret)); !slices.Equal(got, all) {
		t.Errorf("operate scope lists %q, want %q", got, all)
	}
	// The admin scope adds the admin tools (SPEC 21.7).
	admin := slices.Sorted(slices.Values(append(slices.Clone(all), adminToolNames...)))
	if got := toolNames(t, mcpConnect(t, g.url, adminSecret)); !slices.Equal(got, admin) {
		t.Errorf("admin scope lists %q, want %q", got, admin)
	}
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	res, err := reader.CallTool(context.Background(), &mcp.CallToolParams{Name: "start_server", Arguments: map[string]any{"world_id": id}})
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("a read token's start_server: %+v %v", res, err)
	}
	if g.ctrl.Status(id).State != game.StateStopped {
		t.Errorf("a read token started the game: %s", g.ctrl.Status(id).State)
	}
}

// SPEC 14.19: 20 failed token checks from one address in the window, then
// 429 with Retry-After, even for a working token from there.
func TestMCPAddressFailureLimit(t *testing.T) {
	ls := newLiveServer(t, nil)
	u := ls.addUser("alice", testPassword)
	turnMCPOn(t, ls.harness)
	secret, _ := apiToken(t, ls.harness, u, "Claude", auth.ScopeRead)
	bad := bearer(auth.TokenPrefix + strings.Repeat("B", 43))
	for i := range auth.LimitPerAddress {
		if rep := ls.mcpPost(http.MethodPost, initMessage, bad); rep.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failure %d: %d", i+1, rep.StatusCode)
		}
	}
	for _, h := range []http.Header{bad, bearer(secret)} {
		if rep := ls.mcpPost(http.MethodPost, initMessage, h); rep.StatusCode != http.StatusTooManyRequests || rep.Header.Get("Retry-After") == "" {
			t.Errorf("over the limit: %d, Retry-After %q", rep.StatusCode, rep.Header.Get("Retry-After"))
		}
	}
	ls.clock.Advance(auth.LimitWindow + time.Second)
	if rep := ls.mcpPost(http.MethodPost, initMessage, bearer(secret)); rep.StatusCode != http.StatusOK {
		t.Errorf("after the window: %d", rep.StatusCode)
	}
}
