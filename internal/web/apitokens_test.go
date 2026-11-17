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
	if !strings.Contains(rec.Body.String(), `value="http://example.com/mcp"`) {
		t.Errorf("the endpoint is not the request's own address")
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
	if got := strings.Join(hn.audits(AuditTokenCreate), "|"); got != "alice  token=Claude owner=alice scope=operate" {
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
	if !strings.Contains(rec.Body.String(), `value="https://foreman.example.org/mcp"`) {
		t.Errorf("the endpoint ignores FOREMAN_PUBLIC_URL")
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
	rec := hn.post(tokenPath(tok.ID, "renew"), url.Values{"return": {"/worlds"}}, withCookie(c))
	hn.wantRedirect(rec, "/worlds")
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
	if got := hn.audits(AuditTokenRevoke); len(got) != 1 || got[0] != "alice  token=Claude owner=alice scope=read" {
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
	if body := hn.get(PathAPITokens+"?done=mcp-on", withCookie(c)).Body.String(); !strings.Contains(body, `name="mcp" value="off"`) || !strings.Contains(body, `id="done-message"`) {
		t.Error("the page does not show MCP on")
	}
	hn.post(pathAPITokenMCP, url.Values{"mcp": {"off"}}, withCookie(c))
	if got := strings.Join(hn.audits(AuditMCPSet), "|"); got != "alice  on|alice  off" && got != "alice  off|alice  on" {
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
