package web

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/factorioaccount"
)

const (
	acctKey      = "keykeykeykeykeykeykeykeykeykeykey"
	acctOtherKey = "otherotherotherotherotherotherother"
	acctUser     = "portal-user-7731"
	acctToken    = "portal-token-5509"
)

// accountHarness is a harness whose handler uses the account service built
// from env, as main builds it.
type accountHarness struct {
	*harness
	svc *factorioaccount.Service
}

func newAccountHarness(t *testing.T, env map[string]string) *accountHarness {
	t.Helper()
	hn := newHarness(t, env)
	ah := &accountHarness{harness: hn}
	ah.rebuild(env)
	return ah
}

// rebuild makes a new handler over the same database, as a restart with
// other variables would.
func (ah *accountHarness) rebuild(env map[string]string) {
	ah.t.Helper()
	cfg := ah.cfg
	if env != nil {
		cfg = loadConfig(ah.t, env)
	}
	svc, err := factorioaccount.New(factorioaccount.Account{Username: cfg.FactorioUsername, Token: cfg.FactorioToken}, cfg.SecretKey, ah.store)
	if err != nil {
		ah.t.Fatal(err)
	}
	h, err := NewHandler(Deps{Config: cfg, Store: ah.store, Worlds: ah.worlds, Account: svc, Log: ah.slog, Now: ah.clock.Now})
	if err != nil {
		ah.t.Fatal(err)
	}
	ah.t.Cleanup(h.Close)
	ah.h, ah.svc = h, svc
}

func loadConfig(t *testing.T, env map[string]string) *config.Config {
	t.Helper()
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func accountForm(user, token string) url.Values {
	return url.Values{"username": {user}, "token": {token}}
}

// wantNoSecrets fails when body or the log holds either value.
func (ah *accountHarness) wantNoSecrets(where, body string, values ...string) {
	ah.t.Helper()
	for _, v := range values {
		if strings.Contains(body, v) {
			ah.t.Errorf("%s shows %q", where, v)
		}
		if strings.Contains(ah.logs.String(), v) {
			ah.t.Errorf("the log holds %q after %s", v, where)
		}
	}
}

func TestAccountPageNeedsSignIn(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	ah.wantRedirect(ah.get(PathAccount), PathSetup)
	ah.wantRedirect(ah.post(PathAccount, accountForm(acctUser, acctToken)), PathSetup)
	if _, src, _, _ := ah.svc.Get(context.Background()); src != factorioaccount.None {
		t.Errorf("a signed-out post stored an account: %v", src)
	}
}

// SPEC 9.11: with no account anywhere, the page says downloads are off and
// offers both fields; the token is a password field.
func TestAccountPageWithNoAccountOffersTheForm(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := ah.signIn()
	rec := ah.get(PathAccount, withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"No account is set, so mod downloads are off.",
		`name="username"`, `name="token" type="password" required autocomplete="off"`,
		`<button type="submit" class="button button--primary">Save account</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q\n%s", want, body)
		}
	}
	for _, bad := range []string{" disabled", "Remove account", "Set by environment"} {
		if strings.Contains(body, bad) {
			t.Errorf("page holds %q\n%s", bad, body)
		}
	}
	if regexp.MustCompile(`<input id="account-[^>]*value=`).MatchString(body) {
		t.Errorf("a field has a value attribute\n%s", body)
	}
	// The user menu links to the page from every page.
	if !strings.Contains(ah.get(PathWorlds, withCookie(c)).Body.String(), `href="`+PathAccount+`"`) {
		t.Error("the user menu does not link to the account page")
	}
}

// SPEC 9.11: an account from the environment wins, is not changeable, and
// is never shown.
func TestAccountFromTheEnvironmentHasNoForm(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_FACTORIO_USERNAME": acctUser, "FOREMAN_FACTORIO_TOKEN": acctToken})
	c := ah.signIn()
	rec := ah.get(PathAccount, withCookie(c))
	body := rec.Body.String()
	for _, want := range []string{"Set by environment", "<code>FOREMAN_FACTORIO_USERNAME</code>", "<code>FOREMAN_FACTORIO_TOKEN</code>"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(body, `name="token"`) || strings.Contains(body, `action="`+PathAccount+`"`) {
		t.Errorf("the page offers a form for an environment account\n%s", body)
	}
	ah.wantNoSecrets("the environment page", body, acctUser, acctToken)

	// A forged post is refused, and nothing is stored or audited.
	rec = ah.post(PathAccount, accountForm("other-user", "other-token"), withCookie(c))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "comes from the environment") {
		t.Errorf("post: %d\n%s", rec.Code, rec.Body)
	}
	ah.wantNoSecrets("a post over the environment account", rec.Body.String(), acctUser, acctToken, "other-user", "other-token")
	if got := ah.audits(AuditAccountSet); len(got) != 0 {
		t.Errorf("audit entries %v", got)
	}
}

// SPEC 9.11: without a secret key nothing can be stored, and the page says
// how to fix that.
func TestAccountWithoutASecretKeyIsDisabled(t *testing.T) {
	ah := newAccountHarness(t, nil)
	c := ah.signIn()
	body := ah.get(PathAccount, withCookie(c)).Body.String()
	for _, want := range []string{"<code>FOREMAN_SECRET_KEY</code>", "<code>FOREMAN_FACTORIO_USERNAME</code>", "<code>FOREMAN_FACTORIO_TOKEN</code>"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if n := strings.Count(body, " disabled"); n != 3 {
		t.Errorf("%d disabled controls, want both fields and the button\n%s", n, body)
	}

	rec := ah.post(PathAccount, accountForm(acctUser, acctToken), withCookie(c))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "no secret key is set") {
		t.Errorf("post: %d\n%s", rec.Code, rec.Body)
	}
	ah.wantNoSecrets("a post without a key", rec.Body.String(), acctUser, acctToken)
}

// SPEC 9.11, 11.4, 12.9: save, see the stored line, replace, remove. No
// page, log line or audit entry carries either value.
func TestAccountSaveReplaceRemove(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := ah.signIn()
	ctx := context.Background()

	rec := ah.post(PathAccount, accountForm("  "+acctUser+" ", acctToken), withCookie(c))
	ah.wantRedirect(rec, PathAccount+"?done=saved")
	acct, src, meta, err := ah.svc.Get(ctx)
	if err != nil || src != factorioaccount.Stored || acct.Username.Reveal() != acctUser || acct.Token.Reveal() != acctToken || meta.UpdatedBy != "alice" {
		t.Fatalf("stored account: %v %v %+v %v", acct, src, meta, err)
	}
	if got := ah.audits(AuditAccountSet); len(got) != 1 || got[0] != "alice  " {
		t.Errorf("audit entries %q", got)
	}

	body := ah.get(PathAccount+"?done=saved", withCookie(c)).Body.String()
	for _, want := range []string{"<strong>Done:</strong> The account is saved.", "Stored on", `<time datetime="`, "by alice", "Replace account", "Remove account"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q\n%s", want, body)
		}
	}
	ah.wantNoSecrets("the stored page", body, acctUser, acctToken)

	ah.wantRedirect(ah.post(PathAccount, accountForm("second-user-1", "second-token-2"), withCookie(c)), PathAccount+"?done=saved")
	if acct, _, _, _ := ah.svc.Get(ctx); acct.Token.Reveal() != "second-token-2" {
		t.Error("replace kept the old token")
	}

	ah.wantRedirect(ah.post(PathAccount, url.Values{"action": {"remove"}}, withCookie(c)), PathAccount+"?done=removed")
	if _, src, _, _ := ah.svc.Get(ctx); src != factorioaccount.None {
		t.Errorf("source after remove: %v", src)
	}
	if got := ah.audits(AuditAccountRemove); len(got) != 1 || got[0] != "alice  " {
		t.Errorf("remove audit entries %q", got)
	}
	body = ah.get(PathAccount+"?done=removed", withCookie(c)).Body.String()
	if !strings.Contains(body, "<strong>Done:</strong> The account is removed.") || !strings.Contains(body, "No account is set") {
		t.Errorf("page after remove:\n%s", body)
	}
	ah.wantNoSecrets("the removed page", body, acctUser, acctToken, "second-user-1", "second-token-2")
	ah.wantNoSecrets("the whole run", "", acctUser, acctToken, "second-user-1", "second-token-2")
	// The page never repeats an unknown "done" value.
	if body := ah.get(PathAccount+"?done=%3Cb%3E", withCookie(c)).Body.String(); strings.Contains(body, "<b>") || strings.Contains(body, "Done:") {
		t.Errorf("an unknown done value changed the page:\n%s", body)
	}
}

// Removing when nothing is stored changes nothing and writes no audit entry.
func TestAccountRemoveWithNothingStored(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := ah.signIn()
	ah.wantRedirect(ah.post(PathAccount, url.Values{"action": {"remove"}}, withCookie(c)), PathAccount+"?done=removed")
	if got := ah.audits(AuditAccountRemove); len(got) != 0 {
		t.Errorf("audit entries %q", got)
	}
}

// SPEC 9.11: a stored account the key cannot open asks for the account
// again, and does not crash.
func TestAccountUnreadableAsksAgain(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := ah.signIn()
	ah.wantRedirect(ah.post(PathAccount, accountForm(acctUser, acctToken), withCookie(c)), PathAccount+"?done=saved")

	ah.rebuild(map[string]string{"FOREMAN_SECRET_KEY": acctOtherKey})
	body := ah.get(PathAccount, withCookie(c)).Body.String()
	for _, want := range []string{"could not be read", "Enter the account again to replace it.", `name="token"`} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "Remove account") {
		t.Error("an unreadable account offers Remove")
	}
	ah.wantNoSecrets("the unreadable page", body, acctUser, acctToken)

	ah.wantRedirect(ah.post(PathAccount, accountForm("new-user-1", "new-token-1"), withCookie(c)), PathAccount+"?done=saved")
	if _, src, _, _ := ah.svc.Get(context.Background()); src != factorioaccount.Stored {
		t.Errorf("source after re-entering: %v", src)
	}
}

// SPEC 11.4: a refused form lists every problem and puts nothing back in
// the fields, not even what was typed.
func TestAccountRefusedFormShowsNothingBack(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := ah.signIn()

	rec := ah.post(PathAccount, accountForm("  ", acctToken), withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "Enter the factorio.com username") || !strings.Contains(body, "Type both again.") {
		t.Errorf("empty username: %d\n%s", rec.Code, body)
	}
	if strings.Contains(body, "Enter the factorio.com token") {
		t.Error("a given token was called missing")
	}
	ah.wantNoSecrets("the refused form", body, acctToken)

	rec = ah.post(PathAccount, accountForm(acctUser, ""), withCookie(c))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Enter the factorio.com token") {
		t.Errorf("empty token: %d\n%s", rec.Code, rec.Body)
	}
	ah.wantNoSecrets("the refused form", rec.Body.String(), acctUser)

	rec = ah.post(PathAccount, accountForm(acctUser, "bad\x07token"), withCookie(c))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Remove control characters") {
		t.Errorf("control character: %d\n%s", rec.Code, rec.Body)
	}
	long := strings.Repeat("x", maxAccountFieldChars+1)
	rec = ah.post(PathAccount, accountForm(long, acctToken), withCookie(c))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Use at most 256 characters") {
		t.Errorf("long username: %d\n%s", rec.Code, rec.Body)
	}
	ah.wantNoSecrets("the refused form", rec.Body.String(), long, acctToken)
	if _, src, _, _ := ah.svc.Get(context.Background()); src != factorioaccount.None {
		t.Errorf("a refused form stored an account: %v", src)
	}
}

// SPEC 14.3: a post from another site is refused.
func TestAccountPostFromAnotherSiteIsRefused(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := ah.signIn()
	cross := withHeader("Sec-Fetch-Site", "cross-site")
	for _, form := range []url.Values{accountForm(acctUser, acctToken), {"action": {"remove"}}} {
		if rec := ah.post(PathAccount, form, withCookie(c), cross); rec.Code != http.StatusForbidden {
			t.Errorf("cross-site post: %d", rec.Code)
		}
	}
	if _, src, _, _ := ah.svc.Get(context.Background()); src != factorioaccount.None {
		t.Errorf("a cross-site post stored an account: %v", src)
	}
}

// SPEC 14.17: the page goes back to a local path after saving, and never to
// anywhere else, from the link and from a forged form field alike.
func TestAccountReturnPath(t *testing.T) {
	ah := newAccountHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := ah.signIn()
	const back = "/worlds/base/mods?q=a%20b"

	body := ah.get(PathAccount+"?return="+url.QueryEscape(back), withCookie(c)).Body.String()
	if !strings.Contains(body, `<input type="hidden" name="return" value="/worlds/base/mods?q=a%20b">`) || !strings.Contains(body, `href="/worlds/base/mods?q=a%20b">Cancel</a>`) {
		t.Errorf("the page does not carry the return path\n%s", body)
	}

	form := accountForm(acctUser, acctToken)
	form.Set("return", back)
	ah.wantRedirect(ah.post(PathAccount, form, withCookie(c)), back)

	for _, bad := range []string{"//evil.example/", "/\\evil.example", "https://evil.example/", "/\t/evil.example", "evil", PathSignIn} {
		body := ah.get(PathAccount+"?return="+url.QueryEscape(bad), withCookie(c)).Body.String()
		// The language menu carries this page's own address; no other return field may exist.
		for _, m := range regexp.MustCompile(`name="return" value="([^"]*)"`).FindAllStringSubmatch(body, -1) {
			if !strings.HasPrefix(m[1], PathAccount) {
				t.Errorf("%q: the page carries the return path %q", bad, m[1])
			}
		}
		if strings.Contains(body, "Cancel") {
			t.Errorf("%q: the page links back to a refused path", bad)
		}
		form.Set("return", bad)
		ah.wantRedirect(ah.post(PathAccount, form, withCookie(c)), PathAccount+"?done=saved")
	}

	// Removing keeps the path for the page it lands on.
	rm := url.Values{"action": {"remove"}, "return": {back}}
	ah.wantRedirect(ah.post(PathAccount, rm, withCookie(c)), PathAccount+"?done=removed&return="+url.QueryEscape(back))
}
