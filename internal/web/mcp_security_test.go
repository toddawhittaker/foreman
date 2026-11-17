package web

// Black-box tests of SPEC 14.19 ("API tokens and the MCP endpoint"), one
// line at a time, against the real handler. Each test names the line it
// pins. mcp_test.go covers the basic cases; these fill the gaps.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/store"
)

// mcpReq is an /mcp request served in-process, from the given peer.
func mcpReq(hn *harness, peer string, header http.Header, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	hn.t.Helper()
	r := httptest.NewRequest(http.MethodPost, PathMCP, strings.NewReader(initMessage))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range header {
		r.Header[k] = v
	}
	r.RemoteAddr = peer
	for _, m := range mods {
		m(r)
	}
	return hn.serve(r)
}

// mcpTokenCount names each test token uniquely.
var mcpTokenCount int

// mcpUser is a harness user with MCP on and a token of the given scope.
func mcpUser(t *testing.T, hn *harness, scope string) (string, store.APIToken) {
	t.Helper()
	u, ok, _ := hn.store.UserByUsername(context.Background(), "alice")
	if !ok {
		u = addTestUser(t, hn)
	}
	turnMCPOn(t, hn)
	mcpTokenCount++
	return apiToken(t, hn, u, fmt.Sprintf("T%d", mcpTokenCount), scope)
}

// 14.19 Format: "fmn_" then 43 base64url characters, 32 random bytes.
// Storage: only the SHA-256 hash is stored, and the token is found by it.
func TestMCPSecFormatAndStorage(t *testing.T) {
	hn := newHarness(t, nil)
	u := addTestUser(t, hn)
	re := regexp.MustCompile(`^fmn_[A-Za-z0-9_-]{43}$`)
	seen := map[string]bool{}
	for i := range 20 {
		secret, tok := apiToken(t, hn, u, fmt.Sprintf("t%d", i), auth.ScopeRead)
		if !re.MatchString(secret) {
			t.Fatalf("secret %d does not have the format", i)
		}
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(secret, "fmn_"))
		if err != nil || len(raw) != 32 {
			t.Fatalf("secret %d is not 32 bytes of base64url: %d %v", i, len(raw), err)
		}
		if seen[secret] {
			t.Fatal("two tokens share a secret")
		}
		seen[secret] = true
		sum := sha256.Sum256([]byte(secret))
		got, found, err := hn.store.APITokenByHash(context.Background(), hex.EncodeToString(sum[:]))
		if err != nil || !found || got.ID != tok.ID {
			t.Fatalf("token %d is not found by its SHA-256: %v %v", i, found, err)
		}
		if strings.Contains(fmt.Sprintf("%+v", got), secret) {
			t.Fatal("the stored row holds the secret")
		}
	}
}

// addTestUser adds alice with no password.
func addTestUser(t *testing.T, hn *harness) store.User {
	t.Helper()
	u, err := hn.store.CreateUser(context.Background(), "alice", "", hn.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// 14.19 Where the token goes: only Authorization: Bearer. Other headers
// and schemes holding a working secret count for nothing.
func TestMCPSecOnlyBearerHeader(t *testing.T) {
	hn := newHarness(t, nil)
	secret, _ := mcpUser(t, hn, auth.ScopeRead)
	for name, h := range map[string]http.Header{
		"X-Api-Key":     {"X-Api-Key": {secret}},
		"Token scheme":  {"Authorization": {"Token " + secret}},
		"bare secret":   {"Authorization": {secret}},
		"proxy header":  {"Proxy-Authorization": {"Bearer " + secret}},
		"x-auth-token":  {"X-Auth-Token": {secret}},
		"Basic encoded": {"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte("alice:"+secret))}},
	} {
		if rec := mcpReq(hn, peerUntrusted, h); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	// A query token is ignored even beside a bad bearer.
	rec := mcpReq(hn, peerUntrusted, bearer("fmn_x"), func(r *http.Request) { r.URL.RawQuery = "access_token=" + secret })
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("query beside a bad bearer: %d", rec.Code)
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("the bearer itself: %d %s", rec.Code, rec.Body)
	}
}

// 14.19 Where the token goes: an unknown or malformed token gets a plain
// 401; an Expired one a 401 naming "expired" that a renew lifts; a Revoked
// one a 401 naming "revoked" for good.
func TestMCPSec401Wording(t *testing.T) {
	hn := newHarness(t, nil)
	secret, tok := mcpUser(t, hn, auth.ScopeRead)
	tokens := auth.NewAPITokens(hn.store, hn.clock.Now)
	for _, s := range []string{"fmn_short", auth.TokenPrefix + strings.Repeat("Z", 43), strings.ToUpper(secret[:4]) + secret[4:], secret + "A"} {
		rec := mcpReq(hn, peerUntrusted, bearer(s))
		b := strings.ToLower(rec.Body.String())
		if rec.Code != http.StatusUnauthorized || strings.Contains(b, "expired") || strings.Contains(b, "revoked") {
			t.Errorf("unknown or malformed: %d %q", rec.Code, b)
		}
	}
	hn.clock.Advance(auth.TokenPeriod + time.Second)
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("expired: %d %q", rec.Code, rec.Body)
	}
	if _, err := tokens.Renew(context.Background(), tok.ID, tok.UserID); err != nil {
		t.Fatal(err)
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("renewed, same secret: %d %q", rec.Code, rec.Body)
	}
	if _, err := tokens.Revoke(context.Background(), tok.ID); err != nil {
		t.Fatal(err)
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "revoked") {
		t.Errorf("revoked: %d %q", rec.Code, rec.Body)
	}
	if _, err := tokens.Renew(context.Background(), tok.ID, tok.UserID); err == nil {
		t.Error("a revoked token was renewed")
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked after a renew try: %d", rec.Code)
	}
}

// 14.19 Where the token goes: a token is never written to a log; the
// access log records the path only. Every refusal path is exercised.
func TestMCPSecTokenNeverLogged(t *testing.T) {
	hn := newHarness(t, nil)
	secret, tok := mcpUser(t, hn, auth.ScopeRead)
	q := func(r *http.Request) { r.URL.RawQuery = "token=" + secret }
	mcpReq(hn, peerUntrusted, bearer(secret), q)
	mcpReq(hn, peerUntrusted, bearer(secret), withHeader("Origin", "https://evil.example"))
	mcpReq(hn, peerUntrusted, http.Header{"Cookie": {"token=" + secret}})
	bad := auth.TokenPrefix + strings.Repeat("Q", 43)
	for range auth.LimitPerAddress + 2 {
		mcpReq(hn, "192.0.2.77:1", bearer(bad), q)
	}
	hn.clock.Advance(auth.TokenPeriod + time.Second)
	mcpReq(hn, peerUntrusted, bearer(secret), q)
	if _, err := auth.NewAPITokens(hn.store, hn.clock.Now).Revoke(context.Background(), tok.ID); err != nil {
		t.Fatal(err)
	}
	mcpReq(hn, peerUntrusted, bearer(secret), q)
	logs := hn.logs.String()
	for _, s := range []string{secret, secret[4:20], bad, auth.TokenPrefix} {
		if strings.Contains(logs, s) {
			t.Errorf("the log holds %q", s[:8])
		}
	}
}

// 14.19 Limits: 20 failed checks per address in 15 minutes, counting IPv6
// by its /64: the 21st from anywhere in that /64 gets 429 with Retry-After,
// even with a working token; another /64 is untouched.
func TestMCPSecAddressLimitIPv6Slash64(t *testing.T) {
	hn := newHarness(t, nil)
	secret, _ := mcpUser(t, hn, auth.ScopeRead)
	bad := bearer(auth.TokenPrefix + strings.Repeat("B", 43))
	for i := range auth.LimitPerAddress {
		peer := fmt.Sprintf("[2001:db8:1:2::%x]:4000", i+1)
		if rec := mcpReq(hn, peer, bad); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d: %d", i+1, rec.Code)
		}
	}
	rec := mcpReq(hn, "[2001:db8:1:2:ffff:1:2:3]:4000", bearer(secret))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("same /64: %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if rec := mcpReq(hn, "[2001:db8:1:3::1]:4000", bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("next /64: %d", rec.Code)
	}
	// IPv4 addresses are counted one by one.
	if rec := mcpReq(hn, "192.0.2.99:1", bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("IPv4 peer: %d", rec.Code)
	}
	// The window is 15 minutes.
	hn.clock.Advance(auth.LimitWindow - time.Minute)
	if rec := mcpReq(hn, "[2001:db8:1:2::1]:4000", bearer(secret)); rec.Code != http.StatusTooManyRequests {
		t.Errorf("inside the window: %d", rec.Code)
	}
	hn.clock.Advance(2 * time.Minute)
	if rec := mcpReq(hn, "[2001:db8:1:2::1]:4000", bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("after the window: %d", rec.Code)
	}
}

// 14.19 Limits, per address: behind a trusted proxy the client's own
// address (X-Forwarded-For) is counted, so one client cannot lock out the
// proxy's other clients; from an untrusted peer the header is ignored, so
// rotating it does not escape the limit.
func TestMCPSecAddressLimitProxyAware(t *testing.T) {
	hn := newHarness(t, map[string]string{"FOREMAN_TRUSTED_PROXIES": "10.0.0.0/8"})
	secret, _ := mcpUser(t, hn, auth.ScopeRead)
	bad := bearer(auth.TokenPrefix + strings.Repeat("C", 43))
	xff := func(a string) func(*http.Request) { return withHeader("X-Forwarded-For", a) }
	for range auth.LimitPerAddress {
		mcpReq(hn, peerProxy, bad, xff("203.0.113.7"))
	}
	if rec := mcpReq(hn, peerProxy, bearer(secret), xff("203.0.113.7")); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the guessing client: %d", rec.Code)
	}
	if rec := mcpReq(hn, peerProxy, bearer(secret), xff("203.0.113.8")); rec.Code != http.StatusOK {
		t.Errorf("another client of the same proxy: %d", rec.Code)
	}
	for i := range auth.LimitPerAddress {
		mcpReq(hn, peerUntrusted, bad, xff(fmt.Sprintf("198.51.100.%d", i+1)))
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret), xff("198.51.100.200")); rec.Code != http.StatusTooManyRequests {
		t.Errorf("an untrusted peer rotating X-Forwarded-For: %d", rec.Code)
	}
}

// 14.19 Limits: 120 requests a minute per token, in a fixed window, then
// 429 with Retry-After; the count is per token and the next window opens.
func TestMCPSecPerTokenMinute(t *testing.T) {
	hn := newHarness(t, nil)
	secret, _ := mcpUser(t, hn, auth.ScopeRead)
	other, _ := mcpUser(t, hn, auth.ScopeRead)
	for i := range auth.TokenRequestsPerMinute {
		if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusOK {
			t.Fatalf("request %d: %d", i+1, rec.Code)
		}
	}
	hn.clock.Advance(30 * time.Second)
	rec := mcpReq(hn, peerUntrusted, bearer(secret))
	ra := rec.Header().Get("Retry-After")
	if rec.Code != http.StatusTooManyRequests || ra == "" || ra == "0" {
		t.Errorf("request 121: %d, Retry-After %q", rec.Code, ra)
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(other)); rec.Code != http.StatusOK {
		t.Errorf("another token: %d", rec.Code)
	}
	// A rate refusal is not a failed token check: the address is not locked.
	for range auth.LimitPerAddress + 5 {
		mcpReq(hn, peerUntrusted, bearer(secret))
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(other)); rec.Code != http.StatusOK {
		t.Errorf("rate refusals locked the address: %d", rec.Code)
	}
	hn.clock.Advance(31 * time.Second)
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("next minute: %d", rec.Code)
	}
}

// 14.19 Limits: at most 4 requests of one token running at once; the 5th
// gets 429 with Retry-After, and a slot frees when one ends. Requests are
// held open by bodies that have not finished arriving.
func TestMCPSecPerTokenConcurrency(t *testing.T) {
	ls := newLiveServer(t, nil)
	secret, _ := mcpUser(t, ls.harness, auth.ScopeRead)
	type held struct {
		w    *io.PipeWriter
		done chan int
	}
	var open []held
	for range auth.TokenConcurrent {
		pr, pw := io.Pipe()
		h := held{pw, make(chan int, 1)}
		req, _ := http.NewRequest(http.MethodPost, ls.url+PathMCP, pr)
		req.Header = http.Header{"Content-Type": {"application/json"}, "Accept": {"application/json, text/event-stream"}, "Authorization": {"Bearer " + secret}}
		go func() {
			resp, err := ls.client.Do(req)
			if err != nil {
				h.done <- -1
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			h.done <- resp.StatusCode
		}()
		open = append(open, h)
	}
	t.Cleanup(func() {
		for _, h := range open {
			_ = h.w.Close()
		}
	})
	// The held requests take their slots as they arrive; wait for the 5th
	// to be refused.
	var rep reply
	deadline := time.Now().Add(10 * time.Second)
	for {
		rep = ls.mcpPost(http.MethodPost, initMessage, bearer(secret))
		if rep.StatusCode == http.StatusTooManyRequests || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rep.StatusCode != http.StatusTooManyRequests || rep.Header.Get("Retry-After") == "" {
		t.Fatalf("5th at once: %d, Retry-After %q", rep.StatusCode, rep.Header.Get("Retry-After"))
	}
	_, _ = io.WriteString(open[0].w, initMessage)
	_ = open[0].w.Close()
	if code := <-open[0].done; code != http.StatusOK {
		t.Fatalf("a held request: %d", code)
	}
	if rep := ls.mcpPost(http.MethodPost, initMessage, bearer(secret)); rep.StatusCode != http.StatusOK {
		t.Errorf("after one ended: %d %q", rep.StatusCode, rep.body)
	}
}

// 14.19 Limits: a request body is at most 4 MiB.
func TestMCPSecBodyLimit(t *testing.T) {
	ls := newLiveServer(t, nil)
	secret, _ := mcpUser(t, ls.harness, auth.ScopeRead)
	pad := func(n int) string { return initMessage + strings.Repeat(" ", n-len(initMessage)) }
	if rep := ls.mcpPost(http.MethodPost, pad(mcpMaxBody), bearer(secret)); rep.StatusCode != http.StatusOK {
		t.Errorf("exactly 4 MiB: %d %q", rep.StatusCode, rep.body)
	}
	if rep := ls.mcpPost(http.MethodPost, pad(mcpMaxBody+1), bearer(secret)); rep.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("4 MiB + 1: %d %q", rep.StatusCode, rep.body)
	}
	if mcpMaxBody != 4<<20 {
		t.Errorf("mcpMaxBody is %d", mcpMaxBody)
	}
}

// 14.19 Limits: a body holds one JSON-RPC message; any batch is refused.
func TestMCPSecOneMessage(t *testing.T) {
	ls := newLiveServer(t, nil)
	secret, _ := mcpUser(t, ls.harness, auth.ScopeRead)
	for _, body := range []string{"[" + initMessage + "]", "\r\n\t[" + initMessage + "," + initMessage + "]", "[]"} {
		if rep := ls.mcpPost(http.MethodPost, body, bearer(secret)); rep.StatusCode != http.StatusBadRequest {
			t.Errorf("batch %q: %d", body[:4], rep.StatusCode)
		}
	}
}

// 14.19 Off means off: 403 before the token is read, so guesses while off
// neither get a 401 nor count toward the address's failures.
func TestMCPSecOffBeforeTokenRead(t *testing.T) {
	hn := newHarness(t, nil)
	secret, _ := mcpUser(t, hn, auth.ScopeRead)
	if err := hn.store.SetAppSetting(context.Background(), SettingMCP, "off", "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	bad := bearer(auth.TokenPrefix + strings.Repeat("D", 43))
	for range auth.LimitPerAddress + 5 {
		if rec := mcpReq(hn, peerUntrusted, bad); rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "off") {
			t.Fatalf("off: %d %q", rec.Code, rec.Body)
		}
	}
	turnMCPOn(t, hn)
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("on again, same address: %d", rec.Code)
	}
}

// 14.19 Origin: refused unless it equals FOREMAN_PUBLIC_URL's origin. The
// DNS-rebinding case is an Origin equal to the request's own Host.
func TestMCPSecOrigin(t *testing.T) {
	hostIs := func(h string) func(*http.Request) { return func(r *http.Request) { r.Host = h } }
	origin := func(o string) http.Header { return http.Header{"Origin": {o}} }

	hn := newHarness(t, nil)
	secret, _ := mcpUser(t, hn, auth.ScopeRead)
	for _, o := range []string{"http://rebind.example", "http://rebind.example:8080", "http://localhost", "null", ""} {
		h := origin(o)
		h.Set("Authorization", "Bearer "+secret)
		if rec := mcpReq(hn, peerUntrusted, h, hostIs("rebind.example")); rec.Code != http.StatusForbidden {
			t.Errorf("no public URL, Origin %q, Host rebind.example: %d", o, rec.Code)
		}
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret), hostIs("rebind.example")); rec.Code != http.StatusOK {
		t.Errorf("no Origin at all: %d", rec.Code)
	}

	hn = newHarness(t, map[string]string{"FOREMAN_PUBLIC_URL": "https://foreman.example/sub/"})
	secret, _ = mcpUser(t, hn, auth.ScopeRead)
	cases := map[string]int{
		"https://foreman.example":      http.StatusOK,
		"https://foreman.example/":     http.StatusForbidden,
		"http://foreman.example":       http.StatusForbidden,
		"https://foreman.example:8443": http.StatusForbidden,
		"https://evil.foreman.example": http.StatusForbidden,
		"https://foreman.example.evil": http.StatusForbidden,
		"https://FOREMAN.example":      http.StatusForbidden,
		"http://rebind.example":        http.StatusForbidden,
	}
	for o, want := range cases {
		h := origin(o)
		h.Set("Authorization", "Bearer "+secret)
		if rec := mcpReq(hn, peerUntrusted, h, hostIs("rebind.example")); rec.Code != want {
			t.Errorf("Origin %q: %d, want %d", o, rec.Code, want)
		}
	}
	h := http.Header{"Origin": {"https://foreman.example", "https://evil.example"}, "Authorization": {"Bearer " + secret}}
	if rec := mcpReq(hn, peerUntrusted, h); rec.Code != http.StatusForbidden {
		t.Errorf("two Origins: %d", rec.Code)
	}
	// A foreign Origin is refused before the token is read: no failure counts.
	bad := http.Header{"Origin": {"https://evil.example"}, "Authorization": {"Bearer fmn_x"}}
	for range auth.LimitPerAddress + 1 {
		mcpReq(hn, peerUntrusted, bad)
	}
	if rec := mcpReq(hn, peerUntrusted, bearer(secret)); rec.Code != http.StatusOK {
		t.Errorf("after foreign-origin tries: %d", rec.Code)
	}
}

// 14.19 / 21.1: only POST; GET, DELETE and others answer 405 with Allow,
// with or without a token.
func TestMCPSecMethods(t *testing.T) {
	hn := newHarness(t, nil)
	secret, _ := mcpUser(t, hn, auth.ScopeRead)
	for _, m := range []string{http.MethodGet, http.MethodDelete, http.MethodPut, http.MethodPatch} {
		for _, h := range []http.Header{bearer(secret), nil} {
			rec := mcpReq(hn, peerUntrusted, h, func(r *http.Request) { r.Method = m })
			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
				t.Errorf("%s: %d Allow %q", m, rec.Code, rec.Header().Get("Allow"))
			}
		}
	}
}

// 14.19 Forward-auth proxies / 12.10: trusted headers are never read on
// /mcp, and a browser session gives no identity there either.
func TestMCPSecNoCookieOrTrustedHeaderIdentity(t *testing.T) {
	hn := headerHarness(t, "headers", "")
	turnMCPOnFresh(t, hn)
	if rec := hn.get(PathWorlds, asUser("alice")...); rec.Code != http.StatusOK {
		t.Fatalf("the header sign-in itself: %d", rec.Code)
	}
	rec := mcpReq(hn, peerProxy, nil, asUser("alice")...)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("trusted headers on /mcp: %d %q", rec.Code, rec.Body)
	}
	rec = mcpReq(hn, peerProxy, bearer("fmn_x"), asUser("alice")...)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("trusted headers beside a bad bearer: %d", rec.Code)
	}

	ls := newLiveServer(t, nil)
	session := ls.signIn()
	turnMCPOn(t, ls.harness)
	rep := ls.mcpPost(http.MethodPost, initMessage, http.Header{"Cookie": {session.String()}})
	if rep.StatusCode != http.StatusUnauthorized {
		t.Errorf("a session cookie on /mcp: %d", rep.StatusCode)
	}
}

// turnMCPOnFresh turns MCP on in a harness with no users yet.
func turnMCPOnFresh(t *testing.T, hn *harness) {
	t.Helper()
	if err := hn.store.SetAppSetting(context.Background(), SettingMCP, MCPOn, "system", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
}
