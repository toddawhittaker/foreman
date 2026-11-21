package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
)

// apiOnFor turns both switches on, so a test can use either path.
func apiOnFor(t *testing.T, hn *harness) {
	t.Helper()
	mcpOn(t, hn)
	if err := hn.store.SetAppSetting(context.Background(), SettingAPI, APIOn, "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

// apiReq serves one /api/ request in process.
func apiReq(hn *harness, method, path, body string, header http.Header) *httptest.ResponseRecorder {
	hn.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		r.Header[k] = v
	}
	r.RemoteAddr = peerUntrusted
	return hn.serve(r)
}

func toolPath(name string) string { return "/api/v1/tools/" + name }

// apiAnswer is the REST envelope (SPEC 23.5).
type apiAnswer struct {
	Message string         `json:"message"`
	Error   string         `json:"error"`
	Result  map[string]any `json:"result"`
}

func decodeAnswer(t *testing.T, rec *httptest.ResponseRecorder) apiAnswer {
	t.Helper()
	var a apiAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("not JSON (%d): %q", rec.Code, rec.Body)
	}
	return a
}

// mcpCall sends one raw tools/call to /mcp and returns its structured result.
func mcpCall(t *testing.T, hn *harness, secret, tool string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	msg, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args}})
	r := httptest.NewRequest(http.MethodPost, PathMCP, strings.NewReader(string(msg)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Authorization", "Bearer "+secret)
	r.RemoteAddr = peerUntrusted
	rec := hn.serve(r)
	var answer struct {
		Result struct {
			Structured map[string]any `json:"structuredContent"`
			IsError    bool           `json:"isError"`
		} `json:"result"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &answer) != nil {
		t.Fatalf("/mcp %s: %d %q", tool, rec.Code, rec.Body)
	}
	return answer.Result.Structured, answer.Result.IsError
}

// SPEC 23.1, 23.5: the list holds the scope's tools with their schemas, and
// a call answers 200 with message and result; an empty body means {}.
func TestAPIListAndCall(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)

	rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret))
	var list struct{ Tools []apiTool }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil {
		t.Fatalf("list: %d %q", rec.Code, rec.Body)
	}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
		if tool.Description == "" || tool.InputSchema == nil || !tool.ReadOnly || tool.TwoStep {
			t.Errorf("read tool %s: %+v", tool.Name, tool)
		}
	}
	slices.Sort(names)
	ls := newLiveServerFor(t, hn)
	if want := toolNames(t, mcpConnect(t, ls.url, secret)); !slices.Equal(names, want) {
		t.Errorf("read scope lists %q, /mcp lists %q", names, want)
	}

	for _, c := range []struct{ body, ct string }{{"", ""}, {"{}", "application/json"}, {" { } ", "application/json; charset=utf-8"}} {
		r := httptest.NewRequest(http.MethodPost, toolPath("list_worlds"), strings.NewReader(c.body))
		if c.ct != "" {
			r.Header.Set("Content-Type", c.ct)
		}
		r.Header.Set("Authorization", "Bearer "+secret)
		r.RemoteAddr = peerUntrusted
		rec := hn.serve(r)
		a := decodeAnswer(t, rec)
		if rec.Code != http.StatusOK || a.Message == "" || a.Result["worlds"] == nil || a.Error != "" {
			t.Errorf("list_worlds with %q: %d %q", c.body, rec.Code, rec.Body)
		}
	}
}

// SPEC 23.1, 23.2, 23.4, 23.5: each refusal's status.
func TestAPIStatuses(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	auth := bearer(secret)
	withType := func(ct string) http.Header {
		h := bearer(secret)
		h.Set("Content-Type", ct)
		return h
	}
	cases := []struct {
		name, method, path, body string
		header                   http.Header
		want                     int
	}{
		{"tool outside the scope", http.MethodPost, toolPath("start_server"), "", auth, http.StatusNotFound},
		{"no such tool", http.MethodPost, toolPath("nope"), "", auth, http.StatusNotFound},
		{"another API path", http.MethodGet, "/api/v2/tools", "", auth, http.StatusNotFound},
		{"plain text", http.MethodPost, toolPath("list_worlds"), "{}", withType("text/plain"), http.StatusUnsupportedMediaType},
		{"a form", http.MethodPost, toolPath("list_worlds"), "{}", withType("application/x-www-form-urlencoded"), http.StatusUnsupportedMediaType},
		{"an array", http.MethodPost, toolPath("list_worlds"), "[]", auth, http.StatusBadRequest},
		{"a number", http.MethodPost, toolPath("list_worlds"), "3", auth, http.StatusBadRequest},
		{"broken JSON", http.MethodPost, toolPath("list_worlds"), `{"a":`, auth, http.StatusBadRequest},
		{"two objects", http.MethodPost, toolPath("list_worlds"), `{}{}`, auth, http.StatusBadRequest},
		{"GET a tool", http.MethodGet, toolPath("list_worlds"), "", auth, http.StatusMethodNotAllowed},
		{"POST the list", http.MethodPost, PathAPITools, "", auth, http.StatusMethodNotAllowed},
		{"PUT a tool", http.MethodPut, toolPath("list_worlds"), "{}", auth, http.StatusMethodNotAllowed},
		{"a tool's refusal", http.MethodPost, toolPath("list_snapshots"), `{"world_id":"nope"}`, auth, http.StatusUnprocessableEntity},
		{"arguments off the schema", http.MethodPost, toolPath("list_snapshots"), `{"world_id":7}`, auth, http.StatusUnprocessableEntity},
		{"no token", http.MethodPost, toolPath("list_worlds"), "", nil, http.StatusUnauthorized},
	}
	for _, c := range cases {
		rec := apiReq(hn, c.method, c.path, c.body, c.header)
		if rec.Code != c.want {
			t.Errorf("%s: %d %q, want %d", c.name, rec.Code, rec.Body, c.want)
			continue
		}
		if c.want == http.StatusMethodNotAllowed && rec.Header().Get("Allow") == "" {
			t.Errorf("%s: no Allow header", c.name)
		}
		if c.want == http.StatusNotFound || c.want == http.StatusBadRequest || c.want == http.StatusUnsupportedMediaType || c.want == http.StatusUnprocessableEntity {
			if a := decodeAnswer(t, rec); a.Error == "" {
				t.Errorf("%s: no error text: %q", c.name, rec.Body)
			}
		}
	}
	if a := decodeAnswer(t, apiReq(hn, http.MethodPost, toolPath("list_snapshots"), `{"world_id":"nope"}`, auth)); !strings.Contains(a.Error, "no world has the id") || a.Result != nil {
		t.Errorf("a tool refusal without a result: %+v", a)
	}

	pad := func(n int) string { return "{" + strings.Repeat(" ", n-2) + "}" }
	if rec := apiReq(hn, http.MethodPost, toolPath("list_worlds"), pad(mcpMaxBody), auth); rec.Code != http.StatusOK {
		t.Errorf("exactly 4 MiB: %d %q", rec.Code, rec.Body)
	}
	if rec := apiReq(hn, http.MethodPost, toolPath("list_worlds"), pad(mcpMaxBody+1), auth); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("4 MiB + 1: %d %q", rec.Code, rec.Body)
	}
}

// SPEC 23.6: a confirm code is bound to its token, tool and arguments, not
// to the endpoint; a moved state gives 422 with a fresh preview and code.
func TestAPIConfirmAcrossEndpoints(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeAdmin)
	post := func(args map[string]any) (int, apiAnswer) {
		b, _ := json.Marshal(args)
		rec := apiReq(hn, http.MethodPost, toolPath("set_foreman_settings"), string(b), bearer(secret))
		return rec.Code, decodeAnswer(t, rec)
	}

	// A code from /mcp works on /api/.
	out, _ := mcpCall(t, hn, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 30})
	code, _ := out[confirmCodeKey].(string)
	if out["done"] != false || code == "" {
		t.Fatalf("/mcp preview: %v", out)
	}
	if status, a := post(map[string]any{"stop_countdown_seconds": 30, confirmCodeKey: code}); status != http.StatusOK || a.Result["done"] != true {
		t.Fatalf("/api/ confirm of an /mcp code: %d %+v", status, a)
	}

	// And a code from /api/ works on /mcp.
	status, a := post(map[string]any{"stop_countdown_seconds": 40})
	code, _ = a.Result[confirmCodeKey].(string)
	if status != http.StatusOK || a.Result["done"] != false || code == "" {
		t.Fatalf("/api/ preview: %d %+v", status, a)
	}
	if out, isErr := mcpCall(t, hn, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 40, confirmCodeKey: code}); isErr || out["done"] != true {
		t.Fatalf("/mcp confirm of an /api/ code: %v", out)
	}

	// A preview whose state moved before the confirm.
	_, a = post(map[string]any{"stop_countdown_seconds": 50})
	stale, _ := a.Result[confirmCodeKey].(string)
	_, a = post(map[string]any{"stop_countdown_seconds": 60})
	other, _ := a.Result[confirmCodeKey].(string)
	if status, a := post(map[string]any{"stop_countdown_seconds": 60, confirmCodeKey: other}); status != http.StatusOK || a.Result["done"] != true {
		t.Fatalf("second confirm: %d %+v", status, a)
	}
	status, a = post(map[string]any{"stop_countdown_seconds": 50, confirmCodeKey: stale})
	fresh, _ := a.Result[confirmCodeKey].(string)
	if status != http.StatusUnprocessableEntity || a.Error == "" || fresh == "" || fresh == stale {
		t.Errorf("moved state: %d %+v", status, a)
	}
}

// SPEC 23.8, 14.19: with REST API access off, every /api/ request gets 403
// before its token is read, so guesses neither get a 401 nor count toward
// the address's failures, and /mcp keeps working.
func TestAPIOffBeforeTokenRead(t *testing.T) {
	hn := newHarness(t, nil)
	u := mcpOn(t, hn)
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	bad := bearer(auth.TokenPrefix + strings.Repeat("D", 43))
	for range auth.LimitPerAddress + 5 {
		if rec := apiReq(hn, http.MethodGet, PathAPITools, "", bad); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "off") {
			t.Fatalf("off: %d %q", rec.Code, rec.Body)
		}
	}
	for _, path := range []string{PathAPITools, toolPath("list_worlds"), "/api/anything"} {
		for _, h := range []http.Header{bearer(secret), nil} {
			if rec := apiReq(hn, http.MethodPost, path, "", h); rec.Code != http.StatusForbidden {
				t.Errorf("off, %s: %d", path, rec.Code)
			}
		}
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("/mcp with the API off: %d %q", rec.Code, rec.Body)
	}
	apiOnFor(t, hn)
	if rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("on again, same address: %d", rec.Code)
	}
	// MCP off leaves the API working.
	if err := hn.store.SetAppSetting(context.Background(), SettingMCP, MCPOff, "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("API with MCP off: %d", rec.Code)
	}
}

// SPEC 23.8: FOREMAN_API=off wins over the setting.
func TestAPIForcedOff(t *testing.T) {
	hn := newHarness(t, map[string]string{"FOREMAN_API": "off"})
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	if rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret)); rec.Code != http.StatusForbidden {
		t.Errorf("forced off: %d %q", rec.Code, rec.Body)
	}
}

// SPEC 23.9: one token has one budget across /mcp and /api/, and failed
// token checks on either count toward one address limit.
func TestAPISharedLimits(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	for i := range auth.TokenRequestsPerMinute {
		var code int
		if i%2 == 0 {
			code = mcpReq(hn, peerUntrusted, bearer(secret)).Code
		} else {
			code = apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret)).Code
		}
		if code != http.StatusOK {
			t.Fatalf("request %d: %d", i+1, code)
		}
	}
	if rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("API after the shared minute's budget: %d", rec.Code)
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("MCP after the shared minute's budget: %d", rec.Code)
	}

	hn.clock.Advance(time.Minute + time.Second)
	bad := bearer(auth.TokenPrefix + strings.Repeat("B", 43))
	for i := range auth.LimitPerAddress {
		if rec := apiReq(hn, http.MethodGet, PathAPITools, "", bad); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: %d", i+1, rec.Code)
		}
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("MCP after the API's failures: %d", rec.Code)
	}
}

// slowBody holds the body back longer than the server's write timeout.
type slowBody struct {
	r     io.Reader
	delay time.Duration
	slept bool
}

func (b *slowBody) Read(p []byte) (int, error) {
	if !b.slept {
		b.slept = true
		time.Sleep(b.delay)
	}
	return b.r.Read(p)
}

// SPEC 21.5, 23.2: a call may outlast the server's write timeout, as an
// action's wait does, and its answer still arrives whole.
func TestAPIOutlastsTheWriteTimeout(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	srv := httptest.NewUnstartedServer(hn.h)
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+toolPath("list_worlds"), &slowBody{r: strings.NewReader("{}"), delay: 500 * time.Millisecond})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("the answer was lost: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"worlds"`) {
		t.Errorf("status %d, %v\n%s", resp.StatusCode, err, body)
	}
}
