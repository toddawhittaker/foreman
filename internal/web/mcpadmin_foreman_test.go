package web

import (
	"context"
	"html"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/auth"
	"github.com/toddawhittaker/foreman/internal/factorioaccount"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/schedule"
	"github.com/toddawhittaker/foreman/internal/secret"
	"github.com/toddawhittaker/foreman/internal/store"
)

// foremanRig is a live server with a notifier, a scheduler and an account
// service, and an admin token named Claude connected over MCP.
type foremanRig struct {
	*liveServer
	cs  *mcp.ClientSession
	n   *notify.Notifier
	sc  *schedule.Scheduler
	svc *factorioaccount.Service
}

// foremanSecrets are the values no result, error, log line or audit entry
// may hold (SPEC 14.8).
var foremanSecrets = []string{hookToken, "webhooks/123", acctUser, acctToken}

func newForemanRig(t *testing.T, env map[string]string) *foremanRig {
	t.Helper()
	hn := newHarness(t, env)
	n, err := notify.New(hn.store, hn.cfg.SecretKey, hn.slog)
	if err != nil {
		t.Fatal(err)
	}
	sc, err := schedule.New(context.Background(), schedule.Config{
		Game: noGame{}, Store: hn.store, Now: hn.clock.Now, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Active: func(context.Context) (string, string, bool, error) { return "", "", false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := factorioaccount.New(factorioaccount.Account{Username: hn.cfg.FactorioUsername, Token: hn.cfg.FactorioToken}, hn.cfg.SecretKey, hn.store)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Deps{Config: hn.cfg, Store: hn.store, Worlds: hn.worlds, Notifier: n, Scheduler: sc, Account: svc, Log: hn.slog, Now: hn.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	hn.h = h
	ls := newLiveServerFor(t, hn)
	hn.signIn()
	u, _, err := hn.store.UserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	turnMCPOn(t, hn)
	secretValue, _ := apiToken(t, hn, u, "Claude", auth.ScopeAdmin)
	return &foremanRig{liveServer: ls, cs: mcpConnect(t, ls.url, secretValue), n: n, sc: sc, svc: svc}
}

// keyed is the environment that lets webhooks and an account be stored.
var keyed = map[string]string{"FOREMAN_SECRET_KEY": acctKey}

func (r *foremanRig) auditRows(action string) []store.AuditEntry {
	r.t.Helper()
	list, err := r.store.ListAudit(context.Background(), store.AuditFilter{Limit: 500})
	if err != nil {
		r.t.Fatal(err)
	}
	var out []store.AuditEntry
	for _, e := range list {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

// publicOn reads the public mods setting.
func (r *foremanRig) publicOn() bool {
	v, found, err := r.store.AppSetting(context.Background(), SettingPublicMods)
	if err != nil {
		r.t.Fatal(err)
	}
	return found && v == PublicModsOn
}

// auditCount is every audit row, to show a call changed nothing.
func (r *foremanRig) auditCount() int {
	r.t.Helper()
	list, err := r.store.ListAudit(context.Background(), store.AuditFilter{Limit: 1000})
	if err != nil {
		r.t.Fatal(err)
	}
	return len(list)
}

// wantNoSecrets fails when text, the log or any audit entry holds a secret.
func (r *foremanRig) wantNoSecrets(where, text string) {
	r.t.Helper()
	list, err := r.store.ListAudit(context.Background(), store.AuditFilter{Limit: 1000})
	if err != nil {
		r.t.Fatal(err)
	}
	var audit strings.Builder
	for _, e := range list {
		audit.WriteString(e.Detail + "\n")
	}
	for _, v := range foremanSecrets {
		if strings.Contains(text, v) {
			r.t.Errorf("%s holds %q", where, v)
		}
		if strings.Contains(r.logs.String(), v) {
			r.t.Errorf("the log holds %q after %s", v, where)
		}
		if strings.Contains(audit.String(), v) {
			r.t.Errorf("the audit log holds %q after %s", v, where)
		}
	}
}

// run calls a tool and checks neither its result nor its error holds a secret.
func (r *foremanRig) run(t *testing.T, name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	res, out := call(t, r.cs, name, args)
	r.wantNoSecrets(name, resultText(res)+asJSON(out))
	return res, out
}

func hookArgs(raw string) map[string]any {
	return map[string]any{"preset": "discord", "url": raw, "lang": "en"}
}

// SPEC 21.8, 21.7: only delete_webhook and set_foreman_settings are two-step; every other Foreman-wide
// tool carries an explicit false destructive hint.
func TestMCPForemanToolsAnnotations(t *testing.T) {
	r := newForemanRig(t, keyed)
	tools, err := r.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"foreman_settings": false, "set_restart_schedule": false, "add_webhook": false, "change_webhook": false,
		"delete_webhook": true, "set_factorio_account": false, "remove_factorio_account": false, "set_public_mods": false, "set_foreman_settings": true}
	seen := 0
	for _, tl := range tools.Tools {
		two, ok := want[tl.Name]
		if !ok {
			continue
		}
		seen++
		if tl.Name == "foreman_settings" {
			// A read tool: read-only says it changes nothing.
			if tl.Annotations == nil || !tl.Annotations.ReadOnlyHint {
				t.Errorf("foreman_settings: annotations %+v, want read-only", tl.Annotations)
			}
			continue
		}
		if tl.Annotations == nil || tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint != two {
			t.Errorf("%s: annotations %+v, want destructive %v", tl.Name, tl.Annotations, two)
		}
	}
	if seen != len(want) {
		t.Errorf("found %d of %d Foreman-wide tools", seen, len(want))
	}
}

// SPEC 21.7, 14.8: the settings tool shows the schedule, each webhook's
// preset and host, where the account comes from and the public mods page,
// and never an address, a username or a token.
func TestMCPForemanSettings(t *testing.T) {
	r := newForemanRig(t, keyed)
	ctx := context.Background()
	if _, err := r.n.Add(ctx, notify.Setup{Preset: notify.Discord, URL: secret.New(hookURL), Events: defaultEvents(), Lang: "en"}, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := r.svc.Set(ctx, factorioaccount.Account{Username: secret.New(acctUser), Token: secret.New(acctToken)}, "alice"); err != nil {
		t.Fatal(err)
	}
	res, out := r.run(t, "foreman_settings", map[string]any{})
	if res.IsError {
		t.Fatalf("foreman_settings: %s", resultText(res))
	}
	text := asJSON(out)
	for _, want := range []string{`"configured":false`, `"time":"04:00"`, `"zone":"UTC"`, `"host":"discord.com"`, `"preset":"discord"`, `"can_store":true`, `"source":"stored"`, `"public_mods":false`} {
		if !strings.Contains(text, want) {
			t.Errorf("settings lack %s: %s", want, text)
		}
	}
	if strings.Contains(text, "url") || strings.Contains(text, "username") || strings.Contains(text, "token") {
		t.Errorf("settings name an address, a username or a token: %s", text)
	}
}

func TestMCPForemanSettingsWithoutKey(t *testing.T) {
	r := newForemanRig(t, nil)
	_, out := r.run(t, "foreman_settings", map[string]any{})
	if text := asJSON(out); !strings.Contains(text, `"source":"none"`) || !strings.Contains(text, `"can_store":false`) || strings.Contains(text, `"can_store":true`) {
		t.Errorf("settings: %s", text)
	}
}

// SPEC 7.12, 12.9: the schedule is saved and refused as its page does, and
// audited with the token's name.
func TestMCPSetRestartSchedule(t *testing.T) {
	r := newForemanRig(t, nil)
	res, out := r.run(t, "set_restart_schedule", map[string]any{"enabled": true, "time": "03:30", "zone": "Europe/London", "limit_hours": 2})
	if res.IsError || out["enabled"] != true || out["time"] != "03:30" || out["zone"] != "Europe/London" || out["limit_hours"] != float64(2) || out["next_restart"] == "" {
		t.Fatalf("set: %s %s", resultText(res), asJSON(out))
	}
	saved, found := r.sc.Settings()
	if !found || !saved.Enabled || saved.Minute != 210 || saved.Zone != "Europe/London" || saved.LimitHours != 2 || saved.UpdatedBy != "alice" {
		t.Errorf("saved %+v", saved)
	}
	rows := r.auditRows(AuditScheduleSet)
	if len(rows) != 1 || rows[0].TokenName != "Claude" || rows[0].Username != "alice" || rows[0].Detail != "every day at 03:30 Europe/London, waiting up to 2 h" {
		t.Errorf("audit %+v", rows)
	}

	// A field left out keeps its saved value.
	res, out = r.run(t, "set_restart_schedule", map[string]any{"enabled": true, "time": "05:00"})
	if res.IsError || out["time"] != "05:00" || out["zone"] != "Europe/London" || out["limit_hours"] != float64(2) {
		t.Errorf("a partial change: %s %s", resultText(res), asJSON(out))
	}
	// Off ignores the other fields, as the page does.
	res, out = r.run(t, "set_restart_schedule", map[string]any{"enabled": false, "time": "nonsense"})
	if res.IsError || out["enabled"] != false || out["time"] != "05:00" || out["next_restart"] != nil {
		t.Errorf("off: %s %s", resultText(res), asJSON(out))
	}
	if rows := r.auditRows(AuditScheduleSet); len(rows) != 3 || rows[0].Detail != "off" {
		t.Errorf("audit after off %+v", rows)
	}
}

// SPEC 7.12: each refusal is the page's.
func TestMCPSetRestartScheduleRefusals(t *testing.T) {
	r := newForemanRig(t, nil)
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"time":  {map[string]any{"enabled": true, "time": "25:00"}, "time"},
		"zone":  {map[string]any{"enabled": true, "zone": "Mars/Base"}, "zone"},
		"limit": {map[string]any{"enabled": true, "limit_hours": 13}, "12"},
		"minus": {map[string]any{"enabled": true, "limit_hours": -1}, "12"},
	} {
		before := r.auditCount()
		res, _ := r.run(t, "set_restart_schedule", tc.args)
		if !res.IsError || !strings.Contains(resultText(res), tc.want) || !strings.Contains(resultText(res), "was not saved") {
			t.Errorf("%s: %s", name, resultText(res))
		}
		if _, found := r.sc.Settings(); found || r.auditCount() != before {
			t.Errorf("%s: a refused schedule was saved or audited", name)
		}
	}
}

// SPEC 8.10, 12.9: a webhook is added, changed and refused as its page
// does; its address goes in and never comes out, and the audit names the
// token.
func TestMCPWebhookAddAndChange(t *testing.T) {
	r := newForemanRig(t, keyed)
	res, out := r.run(t, "add_webhook", map[string]any{"preset": "discord", "url": hookURL, "events": []string{"start", "stop"}, "lang": "de"})
	hook, _ := out["webhook"].(map[string]any)
	if res.IsError || hook == nil || hook["host"] != "discord.com" || hook["language"] != "de" || asJSON(hook["events"]) != `["start","stop"]` || hook["unreadable"] != false {
		t.Fatalf("add: %s %s", resultText(res), asJSON(out))
	}
	list, _ := r.n.List(context.Background())
	if len(list) != 1 || list[0].Host != "discord.com" {
		t.Fatalf("stored %+v", list)
	}
	id := list[0].ID
	rows := r.auditRows(AuditWebhookAdd)
	if len(rows) != 1 || rows[0].TokenName != "Claude" || rows[0].Username != "alice" || rows[0].Detail != "webhook 1: discord, discord.com" {
		t.Errorf("add audit %+v", rows)
	}

	// A change without an address keeps the stored one; left-out fields keep.
	res, out = r.run(t, "change_webhook", map[string]any{"webhook_id": id, "events": []string{"crash"}})
	hook, _ = out["webhook"].(map[string]any)
	if res.IsError || hook == nil || asJSON(hook["events"]) != `["crash"]` || hook["language"] != "de" || hook["unreadable"] != false {
		t.Fatalf("change: %s %s", resultText(res), asJSON(out))
	}
	rows = r.auditRows(AuditWebhookChange)
	if len(rows) != 1 || rows[0].TokenName != "Claude" || rows[0].Detail != "webhook 1: discord, discord.com" {
		t.Errorf("change audit %+v", rows)
	}
	// A new address of another host replaces the stored one.
	res, out = r.run(t, "change_webhook", map[string]any{"webhook_id": id, "url": "https://discord.com/api/webhooks/456/other-token-9921"})
	if res.IsError {
		t.Errorf("new address: %s %s", resultText(res), asJSON(out))
	}
	// withKey, checkSetup: the page's reasons.
	for name, tc := range map[string]struct {
		tool string
		args map[string]any
		want string
	}{
		"unknown preset":       {"add_webhook", map[string]any{"preset": "slack", "url": hookURL}, "Choose a service"},
		"no address":           {"add_webhook", map[string]any{"preset": "discord"}, "address"},
		"wrong scheme":         {"add_webhook", map[string]any{"preset": "discord", "url": "ftp://discord.com/api/webhooks/1/x"}, "address"},
		"not discord":          {"add_webhook", map[string]any{"preset": "discord", "url": "https://example.com/hook-" + hookToken}, "Discord"},
		"no events":            {"add_webhook", map[string]any{"preset": "discord", "url": hookURL, "events": []string{}}, "event"},
		"unknown event":        {"add_webhook", map[string]any{"preset": "discord", "url": hookURL, "events": []string{"nope"}}, "not an event"},
		"unknown lang":         {"add_webhook", map[string]any{"preset": "discord", "url": hookURL, "lang": "xx"}, "anguage"},
		"preset needs address": {"change_webhook", map[string]any{"webhook_id": id, "preset": "ntfy"}, "address"},
		"unknown id":           {"change_webhook", map[string]any{"webhook_id": 99}, "no webhook has the id 99"},
	} {
		before := r.auditCount()
		res, _ := r.run(t, tc.tool, tc.args)
		if !res.IsError || !strings.Contains(resultText(res), tc.want) {
			t.Errorf("%s: %s", name, resultText(res))
		}
		if got, _ := r.n.List(context.Background()); len(got) != 1 || r.auditCount() != before {
			t.Errorf("%s: a refused call stored or audited something", name)
		}
	}
}

// SPEC 8.10: without a secret key nothing is stored, as the page says.
func TestMCPWebhookNeedsKey(t *testing.T) {
	r := newForemanRig(t, nil)
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"add_webhook", hookArgs(hookURL)},
		{"change_webhook", map[string]any{"webhook_id": 1}},
		{"delete_webhook", map[string]any{"webhook_id": 1}},
	} {
		res, _ := r.run(t, tc.tool, tc.args)
		if !res.IsError || !strings.Contains(resultText(res), "FOREMAN_SECRET_KEY") {
			t.Errorf("%s: %s", tc.tool, resultText(res))
		}
	}
}

// SPEC 21.8: delete_webhook's first call changes nothing; the second, with
// the code, deletes and audits it under the token.
func TestMCPDeleteWebhookTwoSteps(t *testing.T) {
	r := newForemanRig(t, keyed)
	ctx := context.Background()
	hook, err := r.n.Add(ctx, notify.Setup{Preset: notify.Discord, URL: secret.New(hookURL), Events: defaultEvents(), Lang: "en"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"webhook_id": hook.ID}
	before := r.auditCount()
	res, out := r.run(t, "delete_webhook", args)
	if res.IsError || out["done"] != false || out["confirm_code"] == "" || len(webhookDeleteWill()) != 2 || !strings.Contains(asJSON(out["will"]), "Stop sending messages to it.") {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(out))
	}
	if list, _ := r.n.List(ctx); len(list) != 1 || r.auditCount() != before {
		t.Fatal("the preview deleted the webhook or wrote an audit row")
	}
	// A code is for these arguments only.
	code, _ := out["confirm_code"].(string)
	res, _ = r.run(t, "delete_webhook", map[string]any{"webhook_id": hook.ID + 1, "confirm_code": code})
	if !res.IsError {
		t.Error("a code was accepted for another webhook")
	}
	if list, _ := r.n.List(ctx); len(list) != 1 {
		t.Fatal("a refused confirm deleted the webhook")
	}

	_, res2, done := confirmCall(t, r.cs, "delete_webhook", args)
	r.wantNoSecrets("delete_webhook", resultText(res2)+asJSON(done))
	if res2.IsError || done["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res2), asJSON(done))
	}
	if list, _ := r.n.List(ctx); len(list) != 0 {
		t.Errorf("webhook still stored: %+v", list)
	}
	rows := r.auditRows(AuditWebhookDelete)
	if len(rows) != 1 || rows[0].TokenName != "Claude" || rows[0].Username != "alice" || rows[0].Detail != "webhook 1: discord, discord.com" {
		t.Errorf("delete audit %+v", rows)
	}
	// Gone, so the page's refusal.
	if res, _ := r.run(t, "delete_webhook", args); !res.IsError || !strings.Contains(resultText(res), "no webhook has the id") {
		t.Errorf("a second delete: %s", resultText(res))
	}
}

// SPEC 21.8, ruling 2: a webhook that changed since the preview is not
// deleted; the call returns a fresh preview and code.
func TestMCPDeleteWebhookMovedState(t *testing.T) {
	r := newForemanRig(t, keyed)
	ctx := context.Background()
	hook, err := r.n.Add(ctx, notify.Setup{Preset: notify.Discord, URL: secret.New(hookURL), Events: defaultEvents(), Lang: "en"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"webhook_id": hook.ID}
	code := askCode(t, r.cs, "delete_webhook", args)
	if _, err := r.n.Update(ctx, hook.ID, notify.Setup{Preset: notify.Discord, Events: map[notify.Event]bool{notify.Start: true}, Lang: "en"}, "alice"); err != nil {
		t.Fatal(err)
	}
	res, out := r.run(t, "delete_webhook", withCode(args, code))
	fresh, _ := out["confirm_code"].(string)
	if !res.IsError || out["done"] != false || fresh == "" || fresh == code {
		t.Fatalf("moved: %s %s", resultText(res), asJSON(out))
	}
	if list, _ := r.n.List(ctx); len(list) != 1 {
		t.Fatal("a moved preview deleted the webhook")
	}
	res, out = r.run(t, "delete_webhook", withCode(args, fresh))
	if res.IsError || out["done"] != true {
		t.Errorf("the fresh code: %s %s", resultText(res), asJSON(out))
	}
}

// Preview parity (epic plan): each sentence the preview gives is on the
// page the UI shows, rendered in English from the same page struct.
func TestMCPDeleteWebhookPreviewMatchesThePage(t *testing.T) {
	r := newForemanRig(t, keyed)
	hook, err := r.n.Add(context.Background(), notify.Setup{Preset: notify.Discord, URL: secret.New(hookURL), Events: defaultEvents(), Lang: "en"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	_, out := r.run(t, "delete_webhook", map[string]any{"webhook_id": hook.ID})
	will, _ := out["will"].([]any)
	var page strings.Builder
	p := WebhookDeletePage{Hook: webhookView(i18n.English, hook)}
	tpl, err := newTestRenderer(t).template(i18n.English, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := tpl.ExecuteTemplate(&page, "content", view{Page: p}); err != nil {
		t.Fatal(err)
	}
	text := html.UnescapeString(page.String())
	if len(will) == 0 {
		t.Fatal("the preview says nothing will happen")
	}
	for _, w := range will {
		if !strings.Contains(text, w.(string)) {
			t.Errorf("the page lacks the preview's sentence %q\n%s", w, text)
		}
	}
}

// SPEC 9.11, 14.8: the account is stored write-only and audited without its
// values; nothing it was given comes back.
func TestMCPFactorioAccount(t *testing.T) {
	r := newForemanRig(t, keyed)
	res, out := r.run(t, "set_factorio_account", map[string]any{"username": acctUser, "token": acctToken})
	if res.IsError || asJSON(out) != `{"source":"stored"}` || resultText(res) != "The factorio.com account was stored." {
		t.Fatalf("set: %s %s", resultText(res), asJSON(out))
	}
	acct, src, meta, err := r.svc.Get(context.Background())
	if err != nil || src != factorioaccount.Stored || acct.Username.Reveal() != acctUser || acct.Token.Reveal() != acctToken || meta.UpdatedBy != "alice" {
		t.Errorf("stored %v %v %+v", src, err, meta)
	}
	rows := r.auditRows(AuditAccountSet)
	if len(rows) != 1 || rows[0].TokenName != "Claude" || rows[0].Username != "alice" || rows[0].Detail != "" {
		t.Errorf("set audit %+v", rows)
	}
	_, out = r.run(t, "foreman_settings", map[string]any{})
	if !strings.Contains(asJSON(out), `"source":"stored"`) {
		t.Errorf("settings: %s", asJSON(out))
	}

	res, out = r.run(t, "remove_factorio_account", map[string]any{})
	if res.IsError || asJSON(out) != `{"source":"none"}` {
		t.Fatalf("remove: %s %s", resultText(res), asJSON(out))
	}
	if _, src, _, _ := r.svc.Get(context.Background()); src != factorioaccount.None {
		t.Errorf("source after remove: %v", src)
	}
	rows = r.auditRows(AuditAccountRemove)
	if len(rows) != 1 || rows[0].TokenName != "Claude" || rows[0].Detail != "" {
		t.Errorf("remove audit %+v", rows)
	}
	// Nothing stored: nothing removed and nothing audited.
	res, out = r.run(t, "remove_factorio_account", map[string]any{})
	if res.IsError || asJSON(out) != `{"source":"none"}` || !strings.Contains(resultText(res), "nothing was removed") || len(r.auditRows(AuditAccountRemove)) != 1 {
		t.Errorf("a second remove: %s %s", resultText(res), asJSON(out))
	}
}

// SPEC 9.11: the account page's refusals, and no value echoed in any.
func TestMCPFactorioAccountRefusals(t *testing.T) {
	r := newForemanRig(t, keyed)
	for name, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"no username": {map[string]any{"username": "", "token": acctToken}, "username"},
		"no token":    {map[string]any{"username": acctUser, "token": " "}, "token"},
		"control":     {map[string]any{"username": acctUser + "\n", "token": acctToken + "\x00"}, "control"},
		"too long":    {map[string]any{"username": strings.Repeat("u", 300) + acctUser, "token": acctToken}, "256"},
	} {
		before := r.auditCount()
		res, out := r.run(t, "set_factorio_account", tc.args)
		if !res.IsError || !strings.Contains(strings.ToLower(resultText(res)), tc.want) || out != nil {
			t.Errorf("%s: %s %v", name, resultText(res), out)
		}
		if _, src, _, _ := r.svc.Get(context.Background()); src != factorioaccount.None || r.auditCount() != before {
			t.Errorf("%s: a refused account was stored or audited", name)
		}
	}

	// Without a key nothing can be stored.
	r = newForemanRig(t, nil)
	res, _ := r.run(t, "set_factorio_account", map[string]any{"username": acctUser, "token": acctToken})
	if !res.IsError || !strings.Contains(resultText(res), "no secret key") || len(r.auditRows(AuditAccountSet)) != 0 {
		t.Errorf("no key: %s", resultText(res))
	}
}

// SPEC 9.11: an account set by the environment cannot be replaced or removed.
func TestMCPFactorioAccountFromEnvironment(t *testing.T) {
	r := newForemanRig(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey, "FOREMAN_FACTORIO_USERNAME": acctUser, "FOREMAN_FACTORIO_TOKEN": acctToken})
	res, out := r.run(t, "set_factorio_account", map[string]any{"username": "other-user", "token": "other-token"})
	if !res.IsError || !strings.Contains(resultText(res), "environment") || strings.Contains(resultText(res)+asJSON(out), "other-") {
		t.Errorf("set over the environment: %s", resultText(res))
	}
	res, out = r.run(t, "remove_factorio_account", map[string]any{})
	if res.IsError || asJSON(out) != `{"source":"environment"}` || !strings.Contains(resultText(res), "nothing was removed") {
		t.Errorf("remove over the environment: %s %s", resultText(res), asJSON(out))
	}
	_, out = r.run(t, "foreman_settings", map[string]any{})
	if !strings.Contains(asJSON(out), `"source":"environment"`) {
		t.Errorf("settings: %s", asJSON(out))
	}
	if len(r.auditRows(AuditAccountSet))+len(r.auditRows(AuditAccountRemove)) != 0 {
		t.Error("a refused account change was audited")
	}
}

// SPEC 9.16, 12.9: the public mods page is switched and audited under the
// token's name.
func TestMCPSetPublicMods(t *testing.T) {
	r := newForemanRig(t, nil)
	res, out := r.run(t, "set_public_mods", map[string]any{"on": true})
	if res.IsError || out["on"] != true || !r.publicOn() {
		t.Fatalf("on: %s %s", resultText(res), asJSON(out))
	}
	rows := r.auditRows(AuditPublicModsSet)
	if len(rows) != 1 || rows[0].TokenName != "Claude" || rows[0].Username != "alice" || rows[0].Detail != PublicModsOn {
		t.Errorf("audit %+v", rows)
	}
	_, out = r.run(t, "foreman_settings", map[string]any{})
	if !strings.Contains(asJSON(out), `"public_mods":true`) {
		t.Errorf("settings: %s", asJSON(out))
	}
	res, out = r.run(t, "set_public_mods", map[string]any{"on": false})
	if res.IsError || out["on"] != false || r.publicOn() {
		t.Errorf("off: %s %s", resultText(res), asJSON(out))
	}
	if rows := r.auditRows(AuditPublicModsSet); len(rows) != 2 || rows[0].Detail != PublicModsOff {
		t.Errorf("audit after off %+v", rows)
	}
}

// SPEC 14.19: a delete_webhook confirm refused because the webhook is gone
// still spends its code.
func TestMCPDeleteWebhookGoneStillSpendsCode(t *testing.T) {
	r := newForemanRig(t, keyed)
	ctx := context.Background()
	hook, err := r.n.Add(ctx, notify.Setup{Preset: notify.Discord, URL: secret.New(hookURL), Events: defaultEvents(), Lang: "en"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"webhook_id": hook.ID}
	code := askCode(t, r.cs, "delete_webhook", args)
	if err := r.n.Delete(ctx, hook.ID); err != nil {
		t.Fatal(err)
	}
	if res, _ := r.run(t, "delete_webhook", withCode(args, code)); !res.IsError || strings.Contains(resultText(res), errConfirmRefused.Error()) {
		t.Fatalf("confirm for a gone webhook: %s", resultText(res))
	}
	if res, _ := r.run(t, "delete_webhook", withCode(args, code)); !res.IsError || !strings.Contains(resultText(res), errConfirmRefused.Error()) {
		t.Errorf("the code was not spent: %s", resultText(res))
	}
}

// SPEC 21.7, 14.8: add_webhook and change_webhook take Pushover's two
// keys, and no answer, log line or audit entry holds either.
func TestMCPWebhookPushover(t *testing.T) {
	r := newForemanRig(t, keyed)
	const tok, usr = "mcpTOKENsecret4417", "mcpUSERsecret9921" //nolint:gosec // made up, to prove they never leak
	check := func(where string, res *mcp.CallToolResult, out map[string]any) {
		t.Helper()
		for _, k := range []string{tok, usr} {
			if strings.Contains(resultText(res), k) || strings.Contains(asJSON(out), k) || strings.Contains(r.logs.String(), k) {
				t.Errorf("%s: a key leaked", where)
			}
		}
		all, _ := r.store.ListAudit(context.Background(), store.AuditFilter{Limit: 500})
		for _, row := range all {
			if strings.Contains(row.Detail, tok) || strings.Contains(row.Detail, usr) {
				t.Errorf("%s: an audit entry holds a key", where)
			}
		}
	}
	res, out := r.run(t, "add_webhook", map[string]any{"preset": "pushover", "app_token": tok})
	if !res.IsError {
		t.Errorf("one key was accepted: %s", resultText(res))
	}
	check("refused add", res, out)
	res, out = r.run(t, "add_webhook", map[string]any{"preset": "pushover", "app_token": tok, "user_key": usr})
	hook, _ := out["webhook"].(map[string]any)
	if res.IsError || hook == nil || hook["preset"] != "pushover" || hook["host"] != "api.pushover.net" {
		t.Fatalf("add: %s %s", resultText(res), asJSON(out))
	}
	check("add", res, out)
	id := hook["id"]
	res, out = r.run(t, "change_webhook", map[string]any{"webhook_id": id, "user_key": "newUSER5"})
	if res.IsError {
		t.Errorf("change: %s", resultText(res))
	}
	check("change", res, out)
	res, out = r.run(t, "foreman_settings", map[string]any{})
	check("settings", res, out)
}
