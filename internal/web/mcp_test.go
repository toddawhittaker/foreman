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
	actionTools = []string{"restart_server", "send_chat", "start_server", "stop_server", "switch_world", "take_snapshot"}
)

// SPEC 21.2, 14.19: while MCP is off every request gets 403, before its
// token is read, so a bad token is not even a 401.
func TestMCPOffRefusesEveryRequest(t *testing.T) {
	ls := newLiveServer(t, nil)
	u := ls.addUser("alice", testPassword)
	secret, _ := apiToken(t, ls.harness, u, "Claude", auth.ScopeRead)
	for _, h := range []http.Header{bearer(secret), bearer("fmn_nonsense"), nil} {
		if rep := ls.mcpPost(http.MethodPost, initMessage, h); rep.StatusCode != http.StatusForbidden || !strings.Contains(rep.body, "MCP access is off") {
			t.Errorf("off: %d %q", rep.StatusCode, rep.body)
		}
	}
	if rep := ls.mcpPost(http.MethodGet, "", bearer(secret)); rep.StatusCode != http.StatusForbidden {
		t.Errorf("off GET: %d", rep.StatusCode)
	}
}

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

// SPEC 21.1: GET and DELETE answer 405.
func TestMCPGetAndDeleteAre405(t *testing.T) {
	ls := newLiveServer(t, nil)
	u := ls.addUser("alice", testPassword)
	turnMCPOn(t, ls.harness)
	secret, _ := apiToken(t, ls.harness, u, "Claude", auth.ScopeRead)
	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		rep := ls.mcpPost(m, "", bearer(secret))
		if rep.StatusCode != http.StatusMethodNotAllowed || rep.Header.Get("Allow") != http.MethodPost {
			t.Errorf("%s: %d, Allow %q", m, rep.StatusCode, rep.Header.Get("Allow"))
		}
	}
}

// SPEC 14.19: only a bearer token is read. An unknown token gets a plain
// 401; an expired or revoked one a 401 naming its state. A token in the
// query string or a cookie, and a browser session, count for nothing.
func TestMCPTokenChecks(t *testing.T) {
	ls := newLiveServer(t, nil)
	session := ls.signIn()
	u, _, _ := ls.store.UserByUsername(context.Background(), "alice")
	turnMCPOn(t, ls.harness)
	secret, _ := apiToken(t, ls.harness, u, "Claude", auth.ScopeRead)
	if rep := ls.mcpPost(http.MethodPost, initMessage, bearer(secret)); rep.StatusCode != http.StatusOK {
		t.Fatalf("working token: %d %q", rep.StatusCode, rep.body)
	}
	unknown := auth.TokenPrefix + strings.Repeat("A", 43)
	for name, h := range map[string]http.Header{
		"none":    nil,
		"unknown": bearer(unknown),
		"cookie":  {"Cookie": {"token=" + secret + "; " + session.String()}},
		"basic":   {"Authorization": {"Basic " + secret}},
	} {
		rep := ls.mcpPost(http.MethodPost, initMessage, h)
		if rep.StatusCode != http.StatusUnauthorized || strings.Contains(rep.body, "expired") || strings.Contains(rep.body, "revoked") {
			t.Errorf("%s: %d %q", name, rep.StatusCode, rep.body)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, ls.url+PathMCP+"?token="+secret, strings.NewReader(initMessage))
	req.Header.Set("Content-Type", "application/json")
	if resp, err := ls.client.Do(req); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("token in query: %v %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}

	ls.clock.Advance(auth.TokenPeriod + time.Minute)
	if rep := ls.mcpPost(http.MethodPost, initMessage, bearer(secret)); rep.StatusCode != http.StatusUnauthorized || !strings.Contains(rep.body, "expired") {
		t.Errorf("expired: %d %q", rep.StatusCode, rep.body)
	}

	secret2, tok2 := apiToken(t, ls.harness, u, "Other", auth.ScopeRead)
	if _, err := auth.NewAPITokens(ls.store, ls.clock.Now).Revoke(context.Background(), tok2.ID); err != nil {
		t.Fatal(err)
	}
	if rep := ls.mcpPost(http.MethodPost, initMessage, bearer(secret2)); rep.StatusCode != http.StatusUnauthorized || !strings.Contains(rep.body, "revoked") {
		t.Errorf("revoked: %d %q", rep.StatusCode, rep.body)
	}
	if strings.Contains(ls.logs.String(), secret) || strings.Contains(ls.logs.String(), secret2) {
		t.Error("a token secret reached the log")
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
	g := newGameRig(t, image21)
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
	for _, secret := range []string{opSecret, adminSecret} {
		if got := toolNames(t, mcpConnect(t, g.url, secret)); !slices.Equal(got, all) {
			t.Errorf("listed %q, want %q", got, all)
		}
	}
	res, err := reader.CallTool(context.Background(), &mcp.CallToolParams{Name: "start_server", Arguments: map[string]any{"world_id": "x"}})
	if err == nil && (res == nil || !res.IsError) {
		t.Errorf("a read token started a server: %+v", res)
	}
}

// SPEC 14.19: one JSON-RPC message per body; a batch is refused.
func TestMCPRefusesBatch(t *testing.T) {
	ls := newLiveServer(t, nil)
	u := ls.addUser("alice", testPassword)
	turnMCPOn(t, ls.harness)
	secret, _ := apiToken(t, ls.harness, u, "Claude", auth.ScopeRead)
	if rep := ls.mcpPost(http.MethodPost, " ["+initMessage+"]", bearer(secret)); rep.StatusCode != http.StatusBadRequest {
		t.Errorf("batch: %d %q", rep.StatusCode, rep.body)
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

// SPEC 14.19: a token may make 120 requests a minute.
func TestMCPPerTokenRateLimit(t *testing.T) {
	ls := newLiveServer(t, nil)
	u := ls.addUser("alice", testPassword)
	turnMCPOn(t, ls.harness)
	secret, _ := apiToken(t, ls.harness, u, "Claude", auth.ScopeRead)
	for i := range auth.TokenRequestsPerMinute {
		if rep := ls.mcpPost(http.MethodPost, initMessage, bearer(secret)); rep.StatusCode != http.StatusOK {
			t.Fatalf("request %d: %d", i+1, rep.StatusCode)
		}
	}
	rep := ls.mcpPost(http.MethodPost, initMessage, bearer(secret))
	if rep.StatusCode != http.StatusTooManyRequests || rep.Header.Get("Retry-After") == "" {
		t.Errorf("request 121: %d, Retry-After %q", rep.StatusCode, rep.Header.Get("Retry-After"))
	}
}

// SPEC 14.7, 14.19: a request from a foreign origin is refused.
func TestMCPRefusesForeignOrigin(t *testing.T) {
	ls := newLiveServer(t, nil)
	u := ls.addUser("alice", testPassword)
	turnMCPOn(t, ls.harness)
	secret, _ := apiToken(t, ls.harness, u, "Claude", auth.ScopeRead)
	h := bearer(secret)
	h.Set("Origin", "https://evil.example")
	if rep := ls.mcpPost(http.MethodPost, initMessage, h); rep.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: %d", rep.StatusCode)
	}
}
