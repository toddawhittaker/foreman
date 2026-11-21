package web

// The REST API's trust boundary (SPEC 14.19, 23.3, 23.6, 23.8, 23.9, 12.10,
// 21.5), checked against the real handler. api_test.go covers the envelope
// and the plain statuses; these pin the security lines on /api/.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// apiPost is a POST of args to a tool on /api/, decoded.
func apiPost(t *testing.T, hn *harness, secret, tool string, args map[string]any) (int, apiAnswer) {
	t.Helper()
	b, _ := json.Marshal(args)
	rec := apiReq(hn, http.MethodPost, toolPath(tool), string(b), bearer(secret))
	return rec.Code, decodeAnswer(t, rec)
}

// SPEC 14.19, 23.8: with the API off, a request gets 403 whatever its
// token, and the token is never looked up: a working token's last use
// stays unset.
func TestAPISecOffMakesNoLookup(t *testing.T) {
	hn := newHarness(t, nil)
	u := mcpOn(t, hn)
	secret, tok := apiToken(t, hn, u, "Script", auth.ScopeRead)
	for _, h := range []http.Header{bearer(secret), bearer("not a token"), bearer(""), {"Authorization": {"Bearer"}}, nil} {
		if rec := apiReq(hn, http.MethodGet, PathAPITools, "", h); rec.Code != http.StatusForbidden {
			t.Errorf("off, %v: %d %q", h, rec.Code, rec.Body)
		}
	}
	if got, err := hn.store.APIToken(context.Background(), tok.ID); err != nil || !got.LastUsedAt.IsZero() {
		t.Errorf("the token was looked up while the API was off: %v %v", got.LastUsedAt, err)
	}
	// The probe works: a lookup does mark the token used.
	apiOnFor(t, hn)
	if rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret)); rec.Code != http.StatusOK {
		t.Fatalf("on: %d", rec.Code)
	}
	if got, _ := hn.store.APIToken(context.Background(), tok.ID); got.LastUsedAt.IsZero() {
		t.Error("a lookup did not mark the token used, so the check above proves nothing")
	}
}

// SPEC 23.8, 21.2: each switch, and each forcing variable, turns off only
// its own path.
func TestAPISecSwitchesAreSeparate(t *testing.T) {
	mcpCode := func(hn *harness, secret string) int { return mcpReq(hn, peerUntrusted, bearer(secret)).Code }
	apiCode := func(hn *harness, secret string) int {
		return apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret)).Code
	}
	setting := func(hn *harness, name, v string) {
		if err := hn.store.SetAppSetting(context.Background(), name, v, "alice", hn.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}

	hn := newHarness(t, nil)
	u := mcpOn(t, hn)
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	setting(hn, SettingAPI, APIOn)
	setting(hn, SettingMCP, MCPOff)
	if a, m := apiCode(hn, secret), mcpCode(hn, secret); a != http.StatusOK || m != http.StatusForbidden {
		t.Errorf("MCP switched off: API %d, MCP %d", a, m)
	}
	setting(hn, SettingMCP, MCPOn)
	setting(hn, SettingAPI, APIOff)
	if a, m := apiCode(hn, secret), mcpCode(hn, secret); a != http.StatusForbidden || m != http.StatusOK {
		t.Errorf("API switched off: API %d, MCP %d", a, m)
	}

	hn = newHarness(t, map[string]string{"FOREMAN_API": "off"})
	apiOnFor(t, hn)
	u, _, _ = hn.store.UserByUsername(context.Background(), "alice")
	secret, _ = apiToken(t, hn, u, "Script", auth.ScopeRead)
	if a, m := apiCode(hn, secret), mcpCode(hn, secret); a != http.StatusForbidden || m != http.StatusOK {
		t.Errorf("FOREMAN_API=off: API %d, MCP %d", a, m)
	}

	hn = newHarness(t, map[string]string{"FOREMAN_MCP": "off"})
	apiOnFor(t, hn)
	u, _, _ = hn.store.UserByUsername(context.Background(), "alice")
	secret, _ = apiToken(t, hn, u, "Script", auth.ScopeRead)
	if a, m := apiCode(hn, secret), mcpCode(hn, secret); a != http.StatusOK || m != http.StatusForbidden {
		t.Errorf("FOREMAN_MCP=off: API %d, MCP %d", a, m)
	}
}

// SPEC 14.19 Origin: any Origin is refused unless it equals
// FOREMAN_PUBLIC_URL's origin, on every /api/ path, before the token is
// read.
func TestAPISecOrigin(t *testing.T) {
	withOrigin := func(secret string, o ...string) http.Header {
		return http.Header{"Origin": o, "Authorization": {"Bearer " + secret}}
	}
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	for _, o := range []string{"http://example.com", "http://localhost", "null", "", "http://example.com:80"} {
		if rec := apiReq(hn, http.MethodGet, PathAPITools, "", withOrigin(secret, o)); rec.Code != http.StatusForbidden {
			t.Errorf("no public URL, Origin %q: %d", o, rec.Code)
		}
	}

	hn = newHarness(t, map[string]string{"FOREMAN_PUBLIC_URL": "https://foreman.example/sub/"})
	apiOnFor(t, hn)
	u, _, _ = hn.store.UserByUsername(context.Background(), "alice")
	secret, _ = apiToken(t, hn, u, "Script", auth.ScopeRead)
	for o, want := range map[string]int{
		"https://foreman.example":      http.StatusOK,
		"https://foreman.example/":     http.StatusForbidden,
		"http://foreman.example":       http.StatusForbidden,
		"https://foreman.example:8443": http.StatusForbidden,
		"https://evil.foreman.example": http.StatusForbidden,
		"https://foreman.example.evil": http.StatusForbidden,
		"null":                         http.StatusForbidden,
	} {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, PathAPITools, ""},
			{http.MethodPost, toolPath("list_worlds"), "{}"},
		} {
			rec := apiReq(hn, c.method, c.path, c.body, withOrigin(secret, o))
			if rec.Code != want {
				t.Errorf("Origin %q on %s: %d, want %d", o, c.path, rec.Code, want)
			}
		}
	}
	if rec := apiReq(hn, http.MethodGet, PathAPITools, "", withOrigin(secret, "https://foreman.example", "https://evil.example")); rec.Code != http.StatusForbidden {
		t.Errorf("two Origins: %d", rec.Code)
	}
	// Refused before the token is read: bad tokens behind a foreign Origin
	// do not count toward the address's failures.
	for range auth.LimitPerAddress + 2 {
		apiReq(hn, http.MethodGet, PathAPITools, "", withOrigin("fmn_x", "https://evil.example"))
	}
	if rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("after foreign-origin tries: %d", rec.Code)
	}
}

// SPEC 23.3, 14.19: only the bearer header carries a token. One in the
// query string or a cookie is ignored, and a browser session or trusted
// header gives no identity.
func TestAPISecOnlyTheBearerHeader(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	for name, path := range map[string]string{
		"query token":        PathAPITools + "?token=" + secret,
		"query access_token": PathAPITools + "?access_token=" + secret,
		"query on a tool":    toolPath("list_worlds") + "?token=" + secret,
	} {
		method := http.MethodGet
		if strings.Contains(path, "/tools/") {
			method = http.MethodPost
		}
		if rec := apiReq(hn, method, path, "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	for name, h := range map[string]http.Header{
		"cookie token":       {"Cookie": {"token=" + secret}},
		"cookie bearer":      {"Cookie": {"Authorization=Bearer " + secret}},
		"X-Api-Key":          {"X-Api-Key": {secret}},
		"bare secret":        {"Authorization": {secret}},
		"query + bad bearer": {"Authorization": {"Bearer fmn_x"}},
	} {
		path := PathAPITools
		if name == "query + bad bearer" {
			path += "?token=" + secret
		}
		if rec := apiReq(hn, http.MethodGet, path, "", h); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}

	ls := newLiveServer(t, nil)
	session := ls.signIn()
	apiOnFor(t, ls.harness)
	req, _ := http.NewRequest(http.MethodGet, ls.url+PathAPITools, nil)
	req.Header.Set("Cookie", session.String())
	resp, err := ls.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a session cookie on /api/: %d", resp.StatusCode)
	}

	hh := headerHarness(t, "headers", "")
	for _, name := range []string{SettingMCP, SettingAPI} {
		if err := hh.store.SetAppSetting(context.Background(), name, "on", "system", hh.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if rec := hh.get(PathWorlds, asUser("alice")...); rec.Code != http.StatusOK {
		t.Fatalf("the header sign-in itself: %d", rec.Code)
	}
	r := httptest.NewRequest(http.MethodGet, PathAPITools, nil)
	r.RemoteAddr = peerProxy
	for _, m := range asUser("alice") {
		m(r)
	}
	if rec := hh.serve(r); rec.Code != http.StatusUnauthorized {
		t.Errorf("trusted headers on /api/: %d %q", rec.Code, rec.Body)
	}
}

// SPEC 14.19: an expired token gets a 401 naming "expired" until renewed;
// a revoked one a 401 naming "revoked"; an unknown one names neither.
func TestAPISec401NamesTheState(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, tok := apiToken(t, hn, u, "Script", auth.ScopeRead)
	tokens := auth.NewAPITokens(hn.store, hn.clock.Now)
	get := func() *httptest.ResponseRecorder {
		return apiReq(hn, http.MethodPost, toolPath("list_worlds"), "{}", bearer(secret))
	}
	rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(auth.TokenPrefix+strings.Repeat("Z", 43)))
	if b := strings.ToLower(rec.Body.String()); rec.Code != http.StatusUnauthorized || strings.Contains(b, "expired") || strings.Contains(b, "revoked") {
		t.Errorf("unknown: %d %q", rec.Code, b)
	}
	hn.clock.Advance(auth.TokenPeriod + time.Second)
	if rec := get(); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("expired: %d %q", rec.Code, rec.Body)
	}
	if _, err := tokens.Renew(context.Background(), tok.ID, tok.UserID); err != nil {
		t.Fatal(err)
	}
	if rec := get(); rec.Code != http.StatusOK {
		t.Errorf("renewed: %d %q", rec.Code, rec.Body)
	}
	if _, err := tokens.Revoke(context.Background(), tok.ID); err != nil {
		t.Fatal(err)
	}
	if rec := get(); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "revoked") {
		t.Errorf("revoked: %d %q", rec.Code, rec.Body)
	}
}

// SPEC 12.10, 14.19: a revoke takes effect at once, so a call whose body
// arrives after the revoke is refused, and the tool never runs.
func TestAPISecRevokeWhileBodyIsHeldBack(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, tok := apiToken(t, hn, u, "Script", auth.ScopeAdmin)
	before := auditCount(t, hn)

	// The body signals when the handler first reads it, which is after the
	// first token check passed.
	body := newHeldBody()
	body.r = strings.NewReader(`{"stop_countdown_seconds":45}`)
	r := httptest.NewRequest(http.MethodPost, toolPath("set_foreman_settings"), body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+secret)
	r.RemoteAddr = peerUntrusted
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { hn.h.ServeHTTP(rec, r); close(done) }()
	select {
	case <-body.reading:
	case <-done:
		t.Fatalf("answered %d before reading the body: %q", rec.Code, rec.Body)
	}
	if _, err := auth.NewAPITokens(hn.store, hn.clock.Now).Revoke(context.Background(), tok.ID); err != nil {
		close(body.release)
		<-done
		t.Fatal(err)
	}
	close(body.release)
	<-done
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "revoked") {
		t.Errorf("a revoked token's late body: %d %q", rec.Code, rec.Body)
	}
	// The revoke itself may audit; the tool must not have.
	if n := auditCount(t, hn); n > before+1 {
		t.Errorf("the refused call wrote audit entries: %d to %d", before, n)
	}
}

// SPEC 23.9, 14.19: at most 4 requests of one token run at once, counting
// /mcp and /api/ together.
func TestAPISecConcurrencyShared(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	send := func(path string, body io.Reader) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, body)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		r.Header.Set("Authorization", "Bearer "+secret)
		r.RemoteAddr = peerUntrusted
		rec := httptest.NewRecorder()
		hn.h.ServeHTTP(rec, r)
		return rec
	}
	type held struct {
		body *heldBody
		done chan int
	}
	var open []held
	t.Cleanup(func() {
		for _, h := range open {
			select {
			case <-h.body.release:
			default:
				close(h.body.release)
			}
			<-h.done
		}
	})
	for i := range auth.TokenConcurrent {
		h := held{newHeldBody(), make(chan int, 1)}
		path := PathMCP
		if i%2 == 1 {
			// Half the held requests are on /api/.
			path = toolPath("list_worlds")
			h.body.r = strings.NewReader("{}")
		}
		open = append(open, h)
		go func() { h.done <- send(path, h.body).Code }()
		select {
		case <-h.body.reading:
		case code := <-h.done:
			t.Fatalf("held request %d was answered %d before holding a slot", i+1, code)
		}
	}
	for _, path := range []string{toolPath("list_worlds"), PathMCP} {
		body := "{}"
		if path == PathMCP {
			body = initMessage
		}
		if rec := send(path, strings.NewReader(body)); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
			t.Errorf("5th at once on %s: %d", path, rec.Code)
		}
	}
}

// SPEC 23.9: failed token checks on /mcp count toward the same address
// limit that refuses /api/ (api_test.go covers the other direction).
func TestAPISecAddressLimitFromMCP(t *testing.T) {
	hn := newHarness(t, nil)
	apiOnFor(t, hn)
	u, _, _ := hn.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, hn, u, "Script", auth.ScopeRead)
	bad := bearer(auth.TokenPrefix + strings.Repeat("C", 43))
	for i := range auth.LimitPerAddress {
		if rec := mcpReq(hn, peerUntrusted, bad); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: %d", i+1, rec.Code)
		}
	}
	rec := apiReq(hn, http.MethodGet, PathAPITools, "", bearer(secret))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("/api/ after /mcp's failures: %d", rec.Code)
	}
}

// SPEC 23.6, 21.8: a preview on /api/ changes nothing; a code is refused
// for another tool or other arguments, and any use spends it.
func TestAPISecConfirmCodes(t *testing.T) {
	g := newGameRig(t, image21)
	apiOnFor(t, g.harness)
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, g.harness, u, "Script", auth.ScopeAdmin)
	settings := func() string {
		_, a := apiPost(t, g.harness, secret, "foreman_settings", map[string]any{})
		return asJSON(a.Result)
	}
	refused := func(what string, status int, a apiAnswer) {
		t.Helper()
		if status != http.StatusUnprocessableEntity || a.Error != errConfirmRefused.Error() {
			t.Errorf("%s: %d %+v", what, status, a)
		}
	}
	preview := func(args map[string]any) string {
		t.Helper()
		status, a := apiPost(t, g.harness, secret, "set_foreman_settings", args)
		code, _ := a.Result[confirmCodeKey].(string)
		if status != http.StatusOK || a.Result["done"] != false || code == "" {
			t.Fatalf("preview: %d %+v", status, a)
		}
		return code
	}

	was, audits := settings(), auditCount(t, g.harness)
	code := preview(map[string]any{"stop_countdown_seconds": 33})
	preview(map[string]any{"stop_countdown_seconds": 33})
	if now := settings(); now != was {
		t.Errorf("a preview changed the settings:\n%s\n%s", was, now)
	}
	if n := auditCount(t, g.harness); n != audits {
		t.Errorf("a preview wrote %d audit entries", n-audits)
	}

	// Other arguments: refused, and the code is spent for its own call.
	status, a := apiPost(t, g.harness, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 34, confirmCodeKey: code})
	refused("other arguments", status, a)
	status, a = apiPost(t, g.harness, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 33, confirmCodeKey: code})
	refused("after a use with other arguments", status, a)

	// Another tool: refused, and spent.
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	code = preview(map[string]any{"stop_countdown_seconds": 35})
	status, a = apiPost(t, g.harness, secret, "force_stop", map[string]any{"world_id": id, confirmCodeKey: code})
	refused("another tool", status, a)
	status, a = apiPost(t, g.harness, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 35, confirmCodeKey: code})
	refused("after a use on another tool", status, a)

	// A good use spends it too: the same call twice works once.
	code = preview(map[string]any{"stop_countdown_seconds": 36})
	if status, a := apiPost(t, g.harness, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 36, confirmCodeKey: code}); status != http.StatusOK || a.Result["done"] != true {
		t.Fatalf("confirm: %d %+v", status, a)
	}
	status, a = apiPost(t, g.harness, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 36, confirmCodeKey: code})
	refused("second use", status, a)

	// A code from /mcp spent there is spent on /api/ as well.
	out, _ := mcpCall(t, g.harness, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 37})
	code, _ = out[confirmCodeKey].(string)
	if out, isErr := mcpCall(t, g.harness, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 37, confirmCodeKey: code}); isErr || out["done"] != true {
		t.Fatalf("/mcp confirm: %v", out)
	}
	status, a = apiPost(t, g.harness, secret, "set_foreman_settings", map[string]any{"stop_countdown_seconds": 37, confirmCodeKey: code})
	refused("an /mcp code reused on /api/", status, a)
}

// SPEC 21.5, 23.2: the token snapshot cap counts snapshots taken through
// /mcp and /api/ together, and the /api/ snapshot's audit entry names the
// token (12.9).
func TestAPISecSnapshotCapShared(t *testing.T) {
	g := newGameRig(t, image21)
	apiOnFor(t, g.harness)
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	secret, _ := apiToken(t, g.harness, u, "Scripted", auth.ScopeOperate)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	for i := range world.MaxTokenSnapshots {
		args := map[string]any{"world_id": id, "label": fmt.Sprint("s", i)}
		if i%2 == 0 {
			if out, isErr := mcpCall(t, g.harness, secret, "take_snapshot", args); isErr || out["finished"] != true {
				t.Fatalf("/mcp snapshot %d: %v", i, out)
			}
			continue
		}
		if status, a := apiPost(t, g.harness, secret, "take_snapshot", args); status != http.StatusOK || a.Result["finished"] != true {
			t.Fatalf("/api/ snapshot %d: %d %+v", i, status, a)
		}
	}
	status, a := apiPost(t, g.harness, secret, "take_snapshot", map[string]any{"world_id": id})
	if status != http.StatusUnprocessableEntity || !strings.Contains(a.Error, "limit") {
		t.Errorf("/api/ past the shared cap: %d %+v", status, a)
	}
	if _, isErr := mcpCall(t, g.harness, secret, "take_snapshot", map[string]any{"world_id": id}); !isErr {
		t.Error("/mcp past the shared cap was not refused")
	}
	if list, _ := g.worlds.Snapshots(id); len(list) != world.MaxTokenSnapshots {
		t.Errorf("snapshots: %d, want %d", len(list), world.MaxTokenSnapshots)
	}

	rows, err := g.store.ListAudit(context.Background(), store.AuditFilter{Limit: 10000})
	if err != nil {
		t.Fatal(err)
	}
	named := 0
	for _, e := range rows {
		if strings.Contains(e.Action, "snapshot") && e.TokenName == "Scripted" {
			named++
		}
	}
	if named < world.MaxTokenSnapshots {
		t.Errorf("%d snapshot audit entries name the token, want %d:\n%s", named, world.MaxTokenSnapshots, asJSON(rows))
	}
}

// SPEC 14.8, 14.19, 23.2: no token secret appears in any /api/ answer or
// log line, on any path: success, refusals, a foreign Origin, a query or
// cookie token, rate and address limits, expiry and revoke.
func TestAPISecNoSecretAnywhere(t *testing.T) {
	g := newGameRig(t, image21)
	apiOnFor(t, g.harness)
	u, _, _ := g.store.UserByUsername(context.Background(), "alice")
	secret, tok := apiToken(t, g.harness, u, "Script", auth.ScopeAdmin)
	other, _ := apiToken(t, g.harness, u, "Other", auth.ScopeOperate)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	bad := auth.TokenPrefix + strings.Repeat("Q", 43)
	var bodies []string
	keep := func(rec *httptest.ResponseRecorder) { bodies = append(bodies, rec.Body.String()) }

	keep(apiReq(g.harness, http.MethodGet, PathAPITools, "", bearer(secret)))
	keep(apiReq(g.harness, http.MethodGet, PathAPITools, "", bearer(other)))
	for _, tool := range []string{"list_worlds", "server_status", "list_snapshots", "foreman_settings", "list_api_tokens", "take_snapshot"} {
		keep(apiReq(g.harness, http.MethodPost, toolPath(tool), `{"world_id":"`+id+`"}`, bearer(secret)))
	}
	keep(apiReq(g.harness, http.MethodPost, toolPath("set_foreman_settings"), `{"stop_countdown_seconds":20}`, bearer(secret)))
	keep(apiReq(g.harness, http.MethodPost, toolPath("nope"), "{}", bearer(secret)))
	keep(apiReq(g.harness, http.MethodPost, toolPath("list_worlds"), "[", bearer(secret)))
	keep(apiReq(g.harness, http.MethodPost, toolPath("list_worlds"), "{}", http.Header{"Origin": {"https://evil.example"}, "Authorization": {"Bearer " + secret}}))
	keep(apiReq(g.harness, http.MethodGet, PathAPITools+"?token="+secret, "", nil))
	keep(apiReq(g.harness, http.MethodGet, PathAPITools, "", http.Header{"Cookie": {"token=" + secret}}))
	for range auth.TokenRequestsPerMinute {
		keep(apiReq(g.harness, http.MethodGet, PathAPITools+"?token="+other, "", bearer(other)))
	}
	r := httptest.NewRequest(http.MethodGet, PathAPITools, nil)
	r.Header.Set("Authorization", "Bearer "+bad)
	r.RemoteAddr = "192.0.2.77:1"
	for range auth.LimitPerAddress + 2 {
		keep(g.serve(r.Clone(context.Background())))
	}
	g.clock.Advance(auth.TokenPeriod + time.Second)
	keep(apiReq(g.harness, http.MethodGet, PathAPITools, "", bearer(secret)))
	if _, err := auth.NewAPITokens(g.store, g.clock.Now).Revoke(context.Background(), tok.ID); err != nil {
		t.Fatal(err)
	}
	keep(apiReq(g.harness, http.MethodGet, PathAPITools, "", bearer(secret)))

	secrets := []string{secret, secret[len(auth.TokenPrefix):], other, other[len(auth.TokenPrefix):], bad}
	wantNoSecretIn(t, "an /api/ answer", secrets, bodies...)
	wantNoSecretIn(t, "the log", secrets, g.logs.String())
	if logs := g.logs.String(); strings.Contains(logs, auth.TokenPrefix) {
		t.Error("the log holds the token prefix")
	}
}
