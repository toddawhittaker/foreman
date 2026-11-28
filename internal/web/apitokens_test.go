package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/store"
)

var secretRE = regexp.MustCompile(`fmn_[A-Za-z0-9_-]{43}`)

// createToken posts the create form and returns the page's secret.
func (hn *harness) createToken(c *http.Cookie, name, scope string) (string, *httptest.ResponseRecorder) {
	hn.t.Helper()
	rec := hn.post(PathAPITokens, url.Values{"name": {name}, "scope": {scope}}, withCookie(c))
	return secretRE.FindString(rec.Body.String()), rec
}

func (hn *harness) tokenNamed(name string) store.APIToken {
	hn.t.Helper()
	ts, err := hn.store.ListAPITokens(context.Background())
	if err != nil {
		hn.t.Fatal(err)
	}
	for _, t := range ts {
		if t.Name == name && t.RevokedAt.IsZero() {
			return t
		}
	}
	hn.t.Fatalf("no token %q", name)
	return store.APIToken{}
}

// SPEC 12.10 and 14.19: the create page shows the secret once, beside the
// endpoint, sent with no-store; the list never shows it again.
func TestAPITokenCreateShowsSecretOnce(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	page := hn.get(PathAPITokens, withCookie(c)).Body.String()
	if !strings.Contains(page, `value="read" id="scope-read" checked`) {
		t.Errorf("read is not picked by default")
	}
	secret, rec := hn.createToken(c, "Claude", auth.ScopeOperate)
	if rec.Code != http.StatusOK || secret == "" {
		t.Fatalf("create: %d, secret found %v", rec.Code, secret != "")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control %q, want no-store", got)
	}
	if !strings.Contains(rec.Body.String(), `>http://example.com/mcp</textarea>`) {
		t.Errorf("the endpoint is not the request's own address")
	}
	if !strings.Contains(rec.Body.String(), `>http://example.com/api/v1</textarea>`) {
		t.Errorf("the REST API base address is not the request's own address")
	}
	if _, err := hn.tokensFor(t).Check(context.Background(), secret); err != nil {
		t.Errorf("the shown secret does not check: %v", err)
	}
	list := hn.get(PathAPITokens, withCookie(c)).Body.String()
	if strings.Contains(list, secret) || !strings.Contains(list, "Claude") {
		t.Errorf("the list shows the secret or lacks the token")
	}
	if strings.Contains(hn.logs.String(), secret) {
		t.Error("the log holds the secret")
	}
	if got := strings.Join(hn.audits(AuditTokenCreate), "|"); got != "alice  token_id=1 token=\"Claude\" owner=\"alice\" scope=operate" {
		t.Errorf("audit %q", got)
	}
}

// signInAgain starts a new session for an existing user, as the fake clock
// may have outlived the first.
func (hn *harness) signInAgain(name string) *http.Cookie {
	hn.t.Helper()
	c := sessionCookie(hn.post(PathSignIn, signInForm(name, testPassword)))
	if c == nil {
		hn.t.Fatal("sign-in set no cookie")
	}
	return c
}

func (hn *harness) tokensFor(t *testing.T) *auth.APITokens {
	t.Helper()
	return auth.NewAPITokens(hn.store, hn.clock.Now)
}

// FOREMAN_PUBLIC_URL wins for the endpoint (Builder choices).
func TestAPITokenEndpointUsesPublicURL(t *testing.T) {
	hn := newHarness(t, map[string]string{"FOREMAN_PUBLIC_URL": "https://foreman.example.org/"})
	c := hn.signIn()
	_, rec := hn.createToken(c, "x", auth.ScopeRead)
	if !strings.Contains(rec.Body.String(), `>https://foreman.example.org/mcp</textarea>`) {
		t.Errorf("the endpoint ignores FOREMAN_PUBLIC_URL")
	}
	if !strings.Contains(rec.Body.String(), `>https://foreman.example.org/api/v1</textarea>`) {
		t.Errorf("the REST API base address ignores FOREMAN_PUBLIC_URL")
	}
}

// SPEC 12.10: a FOREMAN_PUBLIC_URL with a path prefix puts both addresses
// under it.
func TestAPITokenAddressesKeepPublicURLPath(t *testing.T) {
	hn := newHarness(t, map[string]string{"FOREMAN_PUBLIC_URL": "https://example.org/foreman"})
	c := hn.signIn()
	_, rec := hn.createToken(c, "x", auth.ScopeRead)
	for _, want := range []string{"https://example.org/foreman/mcp", "https://example.org/foreman/api/v1"} {
		if !strings.Contains(rec.Body.String(), ">"+want+"</textarea>") {
			t.Errorf("the page lacks %s", want)
		}
	}
}

// S1: behind a trusted proxy the scheme comes from X-Forwarded-Proto, so a
// client is never told to send its token in clear; a forged header from any
// other peer is ignored.
func TestAPITokenEndpointScheme(t *testing.T) {
	hn := newHarness(t, map[string]string{"FOREMAN_TRUSTED_PROXIES": "10.0.0.0/8"})
	c := hn.signIn()
	proto := withHeader("X-Forwarded-Proto", "https")
	cases := []struct {
		name string
		mods []func(*http.Request)
		want string
	}{
		{"trusted proxy", []func(*http.Request){withPeer("10.1.2.3:5555"), proto}, "https://example.com"},
		{"untrusted peer", []func(*http.Request){proto}, "http://example.com"},
		{"trusted, odd value", []func(*http.Request){withPeer("10.1.2.3:5555"), withHeader("X-Forwarded-Proto", "gopher")}, "http://example.com"},
	}
	for i, k := range cases {
		mods := append([]func(*http.Request){withCookie(c)}, k.mods...)
		rec := hn.post(PathAPITokens, url.Values{"name": {"t" + string(rune('a'+i))}, "scope": {"read"}}, mods...)
		if !strings.Contains(rec.Body.String(), ">"+k.want+"/mcp</textarea>") || !strings.Contains(rec.Body.String(), ">"+k.want+"/api/v1</textarea>") {
			t.Errorf("%s: want %s (status %d)", k.name, k.want, rec.Code)
		}
	}
}

// SPEC 12.10: a bad or taken name is a field error, and nothing is made.
func TestAPITokenCreateNameErrors(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.createToken(c, "Claude", auth.ScopeRead)
	for _, name := range []string{"", strings.Repeat("a", 33), "a[b]", "claude"} {
		secret, rec := hn.createToken(c, name, auth.ScopeRead)
		if rec.Code != http.StatusUnprocessableEntity || secret != "" || !strings.Contains(rec.Body.String(), `id="token-name-error"`) {
			t.Errorf("name %q: %d, secret %v", name, rec.Code, secret != "")
		}
	}
	if _, rec := hn.createToken(c, "ok", "root"); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad scope: %d", rec.Code)
	}
	if ts, _ := hn.store.ListAPITokens(context.Background()); len(ts) != 1 {
		t.Errorf("%d tokens, want 1", len(ts))
	}
}

// SPEC 12.10: renew restarts the 30 days, keeps the secret, and returns to
// the page the banner was on (14.17); a hostile return goes to the list.
func TestAPITokenRenew(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	secret, _ := hn.createToken(c, "Claude", auth.ScopeRead)
	tok := hn.tokenNamed("Claude")
	hn.clock.Advance(31 * 24 * time.Hour)
	if _, err := hn.tokensFor(t).Check(context.Background(), secret); err == nil {
		t.Fatal("the token works after 31 days")
	}
	c = hn.signInAgain("alice")
	rec := hn.post(tokenPath(tok.ID, "renew"), url.Values{"return": {"/worlds?done=renewed&name=Old&x=1"}}, withCookie(c))
	hn.wantRedirect(rec, "/worlds?done=renewed&name=Claude&x=1")
	if body := hn.get("/worlds?done=renewed&name=Claude", withCookie(c)).Body.String(); !strings.Contains(body, `id="renewed-message"`) || !strings.Contains(body, "Renewed Claude.") {
		t.Error("the page renewed from shows no Done message")
	}
	if _, err := hn.tokensFor(t).Check(context.Background(), secret); err != nil {
		t.Errorf("the renewed token is refused: %v", err)
	}
	rec = hn.post(tokenPath(tok.ID, "renew"), url.Values{"return": {"//evil.example/"}}, withCookie(c))
	hn.wantRedirect(rec, doneURL(tokenDoneRenewed, "Claude"))
	if n := len(hn.audits(AuditTokenRenew)); n != 2 {
		t.Errorf("%d renew audits, want 2", n)
	}
}

// SPEC 12.10: revoke goes through a confirm page and is final.
func TestAPITokenRevoke(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	secret, _ := hn.createToken(c, "Claude", auth.ScopeRead)
	tok := hn.tokenNamed("Claude")
	ask := hn.get(tokenPath(tok.ID, "revoke"), withCookie(c))
	if ask.Code != http.StatusOK || !strings.Contains(ask.Body.String(), `id="act-token-revoke"`) {
		t.Fatalf("confirm page: %d", ask.Code)
	}
	if _, err := hn.tokensFor(t).Check(context.Background(), secret); err != nil {
		t.Fatal("opening the confirm page revoked the token")
	}
	hn.wantRedirect(hn.post(tokenPath(tok.ID, "revoke"), url.Values{}, withCookie(c)), doneURL(tokenDoneRevoked, "Claude"))
	if _, err := hn.tokensFor(t).Check(context.Background(), secret); err == nil {
		t.Error("the revoked token still works")
	}
	if rec := hn.post(tokenPath(tok.ID, "renew"), url.Values{}, withCookie(c)); rec.Code != http.StatusConflict {
		t.Errorf("renewing a revoked token: %d", rec.Code)
	}
	if got := hn.audits(AuditTokenRevoke); len(got) != 1 || got[0] != `alice  token_id=1 token="Claude" owner="alice" scope=read` {
		t.Errorf("audit %q", got)
	}
	if rec := hn.get(tokenPath(999, "revoke"), withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown token: %d", rec.Code)
	}
}

// SPEC 21.2: the switch turns MCP on and off, audited; FOREMAN_MCP=off
// shows that and refuses the switch.
func TestAPITokenMCPSwitch(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	if body := hn.get(PathAPITokens, withCookie(c)).Body.String(); !strings.Contains(body, `name="mcp" value="on"`) {
		t.Fatal("MCP is not off by default")
	}
	hn.wantRedirect(hn.post(pathAPITokenMCP, url.Values{"mcp": {"on"}}, withCookie(c)), PathAPITokens+"?done=mcp-on")
	if body := hn.get(PathAPITokens+"?done=mcp-on", withCookie(c)).Body.String(); !strings.Contains(body, `name="mcp" value="off"`) || !strings.Contains(body, `class="toast toast--success" data-toast`) {
		t.Error("the page does not show MCP on")
	}
	hn.post(pathAPITokenMCP, url.Values{"mcp": {"off"}}, withCookie(c))
	if got := strings.Join(hn.audits(AuditMCPSet), "|"); got != "alice  off|alice  on" { // newest first
		t.Errorf("audit %q", got)
	}
	if rec := hn.post(pathAPITokenMCP, url.Values{"mcp": {"yes"}}, withCookie(c)); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad value: %d", rec.Code)
	}
}

func TestAPITokenMCPForcedOff(t *testing.T) {
	hn := newHarness(t, map[string]string{"FOREMAN_MCP": "off"})
	c := hn.signIn()
	body := hn.get(PathAPITokens, withCookie(c)).Body.String()
	if !strings.Contains(body, "FOREMAN_MCP=off") || strings.Contains(body, `name="mcp"`) {
		t.Error("the forced-off page does not say so, or offers the switch")
	}
	if rec := hn.post(pathAPITokenMCP, url.Values{"mcp": {"on"}}, withCookie(c)); rec.Code != http.StatusConflict {
		t.Errorf("switching while forced off: %d", rec.Code)
	}
	if v, found, _ := hn.store.AppSetting(context.Background(), SettingMCP); found && v == MCPOn {
		t.Error("the setting was turned on while forced off")
	}
	if len(hn.audits(AuditMCPSet)) != 0 {
		t.Error("a refused switch was audited")
	}
}

// storedSetting is an app setting's stored value, "" when never saved.
func storedSetting(hn *harness, name string) string {
	hn.t.Helper()
	v, _, err := hn.store.AppSetting(context.Background(), name)
	if err != nil {
		hn.t.Fatal(err)
	}
	return v
}

// SPEC 23.8: REST API access is off by default, has its own switch beside
// MCP's, takes effect at once and is audited as api-set.
func TestAPITokenAPISwitch(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	if body := hn.get(PathAPITokens, withCookie(c)).Body.String(); !strings.Contains(body, `name="api" value="on"`) || !strings.Contains(body, `name="mcp" value="on"`) {
		t.Fatal("REST API access is not off by default, beside MCP's switch")
	}
	if storedSetting(hn, SettingAPI) == APIOn {
		t.Fatal("the setting is on before the switch is used")
	}
	hn.wantRedirect(hn.post(pathAPITokenAPI, url.Values{"api": {"on"}}, withCookie(c)), PathAPITokens+"?done=api-on")
	if storedSetting(hn, SettingAPI) != APIOn {
		t.Error("the switch did not turn REST API access on")
	}
	if storedSetting(hn, SettingMCP) == MCPOn {
		t.Error("the REST API switch turned MCP on")
	}
	body := hn.get(PathAPITokens+"?done=api-on", withCookie(c)).Body.String()
	if !strings.Contains(body, `name="api" value="off"`) || !strings.Contains(body, `name="mcp" value="on"`) || !strings.Contains(body, "Turned REST API access on.") {
		t.Error("the page does not show REST API on and MCP still off")
	}
	hn.wantRedirect(hn.post(pathAPITokenAPI, url.Values{"api": {"off"}}, withCookie(c)), PathAPITokens+"?done=api-off")
	if storedSetting(hn, SettingAPI) == APIOn {
		t.Error("the switch did not turn REST API access off")
	}
	if got := strings.Join(hn.audits(AuditAPISet), "|"); got != "alice  off|alice  on" { // newest first
		t.Errorf("audit %q", got)
	}
	if len(hn.audits(AuditMCPSet)) != 0 {
		t.Error("the REST API switch wrote an mcp-set entry")
	}
	if rec := hn.post(pathAPITokenAPI, url.Values{"api": {"yes"}}, withCookie(c)); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad value: %d", rec.Code)
	}
}

// SPEC 23.8: FOREMAN_API=off shows that, offers no switch, refuses a post
// and writes nothing.
func TestAPITokenAPIForcedOff(t *testing.T) {
	hn := newHarness(t, map[string]string{"FOREMAN_API": "off"})
	c := hn.signIn()
	body := hn.get(PathAPITokens, withCookie(c)).Body.String()
	if !strings.Contains(body, "FOREMAN_API=off") || strings.Contains(body, `name="api"`) {
		t.Error("the forced-off page does not say so, or offers the switch")
	}
	if !strings.Contains(body, `name="mcp" value="on"`) {
		t.Error("FOREMAN_API=off took away the MCP switch")
	}
	if rec := hn.post(pathAPITokenAPI, url.Values{"api": {"on"}}, withCookie(c)); rec.Code != http.StatusConflict {
		t.Errorf("switching while forced off: %d", rec.Code)
	}
	if v, found, _ := hn.store.AppSetting(context.Background(), SettingAPI); found && v == APIOn {
		t.Error("the setting was turned on while forced off")
	}
	if storedSetting(hn, SettingAPI) == APIOn {
		t.Error("REST API access is on while forced off")
	}
	if len(hn.audits(AuditAPISet)) != 0 {
		t.Error("a refused switch was audited")
	}
}

// SPEC 12.10, "Reminder": the banner shows from 5 days before expiry and
// while Expired, one line per token, on every signed-in page, and goes
// once renewed or revoked.
func TestAPITokenBanner(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.createToken(c, "Claude", auth.ScopeRead)
	hn.createToken(c, "Other", auth.ScopeRead)
	tok := hn.tokenNamed("Claude")
	banner := func(path string) string {
		body := hn.get(path, withCookie(c)).Body.String()
		if !strings.Contains(body, `id="token-banner"`) {
			return ""
		}
		return body
	}
	hn.clock.Advance(24*24*time.Hour + 23*time.Hour)
	c = hn.signInAgain("alice")
	if b := banner(PathWorlds); b != "" {
		t.Fatal("the banner shows more than 5 days before expiry")
	}
	hn.clock.Advance(2 * time.Hour)
	b := banner(PathWorlds)
	if b == "" || !strings.Contains(b, "Your API token Claude expires") || !strings.Contains(b, "Your API token Other expires") {
		t.Fatalf("no banner lines 5 days before expiry")
	}
	if !strings.Contains(b, `<input type="hidden" name="return" value="/worlds">`) {
		t.Error("the banner's Renew does not return to the page")
	}
	hn.clock.Advance(10 * 24 * time.Hour)
	c = hn.signInAgain("alice")
	if b := banner(PathAPITokens); !strings.Contains(b, "Your API token Claude expired") {
		t.Error("no banner while Expired")
	}
	if b := banner(PathAPITokens); b == "" {
		t.Fatal("no banner on the tokens page")
	} else if !strings.Contains(b, `class="state state--token-expired"`) {
		t.Error("no Expired card")
	}
	hn.post(tokenPath(tok.ID, "renew"), url.Values{}, withCookie(c))
	if b := banner(PathWorlds); strings.Contains(b, "Claude") {
		t.Error("the banner stays after renewing")
	}
	other := hn.tokenNamed("Other")
	hn.post(tokenPath(other.ID, "revoke"), url.Values{}, withCookie(c))
	if b := banner(PathWorlds); b != "" {
		t.Error("the banner stays after revoking")
	}
}

// SPEC 12.10: the banner is the owner's alone; another user's expiring
// token never shows in it.
func TestAPITokenBannerIsOwners(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.createToken(c, "Claude", auth.ScopeRead)
	hn.addUser("bob", testPassword)
	hn.clock.Advance(26 * 24 * time.Hour)
	bob := hn.signInAgain("bob")
	if body := hn.get(PathWorlds, withCookie(bob)).Body.String(); strings.Contains(body, `id="token-banner"`) {
		t.Error("bob sees alice's token in his banner")
	}
	alice := hn.signInAgain("alice")
	if body := hn.get(PathWorlds, withCookie(alice)).Body.String(); !strings.Contains(body, `id="token-banner"`) {
		t.Error("alice sees no banner")
	}
	if body := hn.get(PathWorlds, withCookie(alice), withHeader("HX-Request", "true")).Body.String(); strings.Contains(body, `id="token-banner"`) {
		t.Error("an htmx request drew the banner")
	}
}

// SPEC 12.10: last use is shown to the minute.
func TestAPITokenLastUsedToTheMinute(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.createToken(c, "Claude", auth.ScopeRead)
	tok := hn.tokenNamed("Claude")
	if err := hn.store.TouchAPIToken(context.Background(), tok.ID, time.Date(2026, 11, 2, 13, 7, 30, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	body := hn.get(PathAPITokens, withCookie(c)).Body.String()
	if !strings.Contains(body, `<span class="nowrap">13:07 UTC</span>`) {
		t.Error("last use is not shown as 13:07 UTC")
	}
}

// A crafted link cannot show a Renewed message for a token the user does
// not own, or one that is revoked.
func TestAPITokenRenewedMessageNotSpoofed(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.createToken(c, "Claude", auth.ScopeRead)
	for _, name := range []string{"Your account is locked, call 555-0100", "claude-x"} {
		body := hn.get("/worlds?"+url.Values{"done": {"renewed"}, "name": {name}}.Encode(), withCookie(c)).Body.String()
		if strings.Contains(body, `id="renewed-message"`) {
			t.Errorf("name %q showed a Renewed message", name)
		}
	}
	tok := hn.tokenNamed("Claude")
	hn.post(tokenPath(tok.ID, "revoke"), url.Values{}, withCookie(c))
	if body := hn.get("/worlds?done=renewed&name=Claude", withCookie(c)).Body.String(); strings.Contains(body, `id="renewed-message"`) {
		t.Error("a revoked token's name showed a Renewed message")
	}
}

// SPEC 12.10, 14.17: a Renewed message shows only for a working token, so
// the name of an expired one cannot be used to show it either.
func TestAPITokenRenewedMessageOnlyForWorkingToken(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.createToken(c, "Claude", auth.ScopeRead)
	shown := func() bool {
		return strings.Contains(hn.get("/worlds?done=renewed&name=Claude", withCookie(c)).Body.String(), `id="renewed-message"`)
	}
	if !shown() {
		t.Fatal("a working token's Renewed message is not shown")
	}
	hn.clock.Advance(auth.TokenPeriod + time.Hour)
	c = hn.signInAgain("alice")
	if shown() {
		t.Error("an expired token's name showed a Renewed message")
	}
}

// SPEC 12.10: a token works while MCP or REST API access is on, so the
// create form and the created page warn only when both are off, a forced-off
// one counting as off.
func TestAPITokenWarnsOnlyWhenBothAccessesAreOff(t *testing.T) {
	const words = "MCP and REST API access are both off, so this token will not work until an admin turns one on."
	cases := []struct {
		name string
		env  map[string]string
		mcp  bool
		api  bool
		warn bool
	}{
		{"both off", nil, false, false, true},
		{"only MCP on", nil, true, false, false},
		{"only REST on", nil, false, true, false},
		{"both on", nil, true, true, false},
		{"REST on but MCP forced off", map[string]string{"FOREMAN_MCP": "off"}, false, true, false},
		{"MCP on but REST forced off", map[string]string{"FOREMAN_API": "off"}, true, false, false},
		{"both forced off", map[string]string{"FOREMAN_MCP": "off", "FOREMAN_API": "off"}, false, false, true},
	}
	for _, k := range cases {
		t.Run(k.name, func(t *testing.T) {
			hn := newHarness(t, k.env)
			c := hn.signIn()
			ctx := context.Background()
			if k.mcp {
				turnMCPOn(t, hn)
			}
			if k.api {
				if err := hn.store.SetAppSetting(ctx, SettingAPI, APIOn, "alice", hn.clock.Now()); err != nil {
					t.Fatal(err)
				}
			}
			form := hn.get(PathAPITokens, withCookie(c)).Body.String()
			_, rec := hn.createToken(c, "x", auth.ScopeRead)
			created := rec.Body.String()
			for _, p := range []struct{ name, body, id string }{{"create form", form, `id="token-access-off"`}, {"created page", created, `id="access-off-note"`}} {
				if got := strings.Contains(p.body, p.id); got != k.warn {
					t.Errorf("%s: warning shown %v, want %v", p.name, got, k.warn)
				}
				if got := strings.Contains(p.body, words); got != k.warn {
					t.Errorf("%s: wording shown %v, want %v", p.name, got, k.warn)
				}
			}
		})
	}
}

// The created page's note links an admin to each switch that can be used.
func TestAPITokenWarningLinksOnlyUsableSwitches(t *testing.T) {
	hn := newHarness(t, map[string]string{"FOREMAN_API": "off"})
	c := hn.signIn()
	_, rec := hn.createToken(c, "x", auth.ScopeRead)
	body := rec.Body.String()
	if !strings.Contains(body, `#mcp-access`) || strings.Contains(body, `#api-access`) {
		t.Errorf("with REST forced off the note should link MCP's switch only:\n%s", body)
	}
	hn = newHarness(t, nil)
	c = hn.signIn()
	_, rec = hn.createToken(c, "x", auth.ScopeRead)
	if body := rec.Body.String(); !strings.Contains(body, `#mcp-access`) || !strings.Contains(body, `#api-access`) {
		t.Errorf("with both off the note should link both switches:\n%s", body)
	}
}
