package web

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/notify"
)

// hookToken is the secret part of a test address: no page, log line or
// audit entry may hold it (SPEC 14.8).
const hookToken = "hook-token-SECRET-4417" //nolint:gosec // a made-up address part, to prove it never leaks

var hookURL = "https://discord.com/api/webhooks/123/" + hookToken

// notifyHarness is a harness whose handler has a notifier built as main
// builds it, from the environment's secret key.
type notifyHarness struct {
	*harness
	n *notify.Notifier
}

func newNotifyHarness(t *testing.T, env map[string]string) *notifyHarness {
	t.Helper()
	hn := newHarness(t, env)
	n, err := notify.New(hn.store, hn.cfg.SecretKey, hn.slog)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Deps{Config: hn.cfg, Store: hn.store, Worlds: hn.worlds, Notifier: n, Log: hn.slog, Now: hn.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	hn.h = h
	return &notifyHarness{harness: hn, n: n}
}

func hookForm(preset, raw string, events ...string) url.Values {
	if events == nil {
		for _, e := range notify.Events {
			events = append(events, string(e))
		}
	}
	return url.Values{"preset": {preset}, "url": {raw}, "events": events, "lang": {"en"}}
}

// wantNoAddress fails when the page, the log or the audit log holds the token.
func (nh *notifyHarness) wantNoAddress(where, body string) {
	nh.t.Helper()
	if strings.Contains(body, hookToken) {
		nh.t.Errorf("%s shows the address", where)
	}
	if strings.Contains(nh.logs.String(), hookToken) {
		nh.t.Errorf("the log holds the address after %s", where)
	}
	list, _ := nh.store.ListAudit(context.Background(), 100)
	for _, e := range list {
		if strings.Contains(e.Detail, hookToken) {
			nh.t.Errorf("an audit entry holds the address after %s", where)
		}
	}
}

var reValueAttr = regexp.MustCompile(`<input id="webhook-url"[^>]*value=`)

func TestNotificationsNeedSignIn(t *testing.T) {
	nh := newNotifyHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	for _, p := range []string{PathNotifications, PathNotifications + "/new", PathNotifications + "/1", PathNotifications + "/1/delete"} {
		nh.wantRedirect(nh.get(p), PathSetup)
	}
	nh.wantRedirect(nh.post(PathNotifications+"/new", hookForm("discord", hookURL)), PathSetup)
	if list, _ := nh.n.List(context.Background()); len(list) != 0 {
		t.Errorf("a signed-out post added %v", list)
	}
}

// DESIGN 8.16: without the key the page is a note and a link, no form.
func TestNotificationsWithoutKey(t *testing.T) {
	nh := newNotifyHarness(t, nil)
	c := nh.signIn()
	body := nh.get(PathNotifications, withCookie(c)).Body.String()
	if !strings.Contains(body, "<code>FOREMAN_SECRET_KEY</code>") || !strings.Contains(body, `id="act-back"`) {
		t.Errorf("no note\n%s", body)
	}
	if strings.Contains(body, "act-webhook-add") || strings.Contains(body, "<form method=\"post\" action=\"/settings/notifications") {
		t.Errorf("the page offers a form\n%s", body)
	}
	nh.wantRedirect(nh.get(PathNotifications+"/new", withCookie(c)), PathNotifications)
	nh.wantRedirect(nh.post(PathNotifications+"/new", hookForm("discord", hookURL), withCookie(c)), PathNotifications)
	nh.wantNoAddress("a post without a key", "")
}

// SPEC 8.10, 8.11, 14.8: add, list, edit and delete. Only the preset and
// host are ever shown, every change is audited, and none holds the address.
func TestWebhookLifecycle(t *testing.T) {
	nh := newNotifyHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := nh.signIn()
	ctx := context.Background()

	body := nh.get(PathNotifications, withCookie(c)).Body.String()
	if !strings.Contains(body, `id="notify-empty"`) || !strings.Contains(body, `id="act-webhook-add"`) {
		t.Errorf("empty list\n%s", body)
	}
	// The form defaults to Discord, every event and the admin's language.
	form := nh.get(PathNotifications+"/new", withCookie(c), withHeader("Accept-Language", "de")).Body.String()
	if !strings.Contains(form, `value="discord" id="preset-discord" checked`) || strings.Count(form, `name="events"`) != len(notify.Events) {
		t.Errorf("form defaults\n%s", form)
	}
	if strings.Count(form, "checked") != 1+len(notify.Events) {
		t.Errorf("want Discord and every event checked\n%s", form)
	}
	if !strings.Contains(form, `<option value="de" lang="de" selected>`) {
		t.Errorf("the language is not the admin's\n%s", form)
	}

	rec := nh.post(PathNotifications+"/new", hookForm("discord", hookURL), withCookie(c))
	nh.wantRedirect(rec, PathNotifications+"?done=added&host=discord.com&preset=discord")
	list, _ := nh.n.List(ctx)
	if len(list) != 1 || list[0].Host != "discord.com" || !list[0].AllOn() {
		t.Fatalf("stored %+v", list)
	}
	id := list[0].ID
	if got := nh.audits(AuditWebhookAdd); len(got) != 1 || got[0] != "alice  webhook 1: discord, discord.com" {
		t.Errorf("audit %q", got)
	}

	body = nh.get(PathNotifications+"?done=added&host=discord.com&preset=discord", withCookie(c)).Body.String()
	for _, want := range []string{
		"Added the Discord webhook to discord.com.", "1 webhook gets messages.", "All events",
		`<span class="ident" translate="no">discord.com</span>`, "Edit the Discord webhook to discord.com", "Delete the Discord webhook to discord.com",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list lacks %q\n%s", want, body)
		}
	}
	nh.wantNoAddress("the list", body)

	// The edit form never holds the address; leaving it empty keeps it.
	edit := nh.get(webhookPath(id), withCookie(c)).Body.String()
	if reValueAttr.MatchString(edit) || !strings.Contains(edit, "The address is kept: Discord, discord.com.") {
		t.Errorf("edit form\n%s", edit)
	}
	nh.wantNoAddress("the edit form", edit)
	f := hookForm("discord", "", "crash", "low-disk")
	f.Set("lang", "fr")
	nh.wantRedirect(nh.post(webhookPath(id), f, withCookie(c)), PathNotifications+"?done=saved&host=discord.com&preset=discord")
	got, _ := nh.n.Get(ctx, id)
	if got.Lang != "fr" || got.AllOn() || !got.On(notify.Crash) || !got.On(notify.LowDisk) || got.On(notify.Start) {
		t.Errorf("after edit %+v", got)
	}
	body = nh.get(PathNotifications, withCookie(c)).Body.String()
	if !strings.Contains(body, "Crash, low disk") || !strings.Contains(body, `<dd lang="fr">Français</dd>`) {
		t.Errorf("list after edit\n%s", body)
	}
	if got := nh.audits(AuditWebhookChange); len(got) != 1 {
		t.Errorf("audit %q", got)
	}

	// Delete asks first, then removes it.
	confirm := nh.get(webhookPath(id)+"/delete", withCookie(c)).Body.String()
	for _, want := range []string{"Delete the Discord webhook to discord.com?", "Stop sending messages to it.", "Delete webhook", "Cancel, keep the webhook"} {
		if !strings.Contains(confirm, want) {
			t.Errorf("confirm lacks %q", want)
		}
	}
	nh.wantRedirect(nh.post(webhookPath(id)+"/delete", url.Values{}, withCookie(c)), PathNotifications+"?done=deleted&host=discord.com&preset=discord")
	if list, _ := nh.n.List(ctx); len(list) != 0 {
		t.Errorf("after delete %+v", list)
	}
	if got := nh.audits(AuditWebhookDelete); len(got) != 1 {
		t.Errorf("audit %q", got)
	}
	if rec := nh.get(webhookPath(id), withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("deleted webhook: %d", rec.Code)
	}
	nh.wantNoAddress("the whole run", "")
}

// DESIGN 8.16: a refused form names what to fix without echoing the
// address, and the field comes back empty.
func TestWebhookFormRefusals(t *testing.T) {
	nh := newNotifyHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := nh.signIn()
	for _, tc := range []struct {
		form url.Values
		want string
	}{
		{hookForm("discord", ""), "Enter the webhook address."},
		{hookForm("discord", "http://discord.com/api/webhooks/1/"+hookToken), "Enter an address that starts with https://."},
		{hookForm("discord", "https://example.com/"+hookToken), "Enter a Discord webhook address."},
		{hookForm("ntfy", "https://ntfy.sh/"), "Enter the address of an ntfy topic"},
		{hookForm("generic", "https://u:"+hookToken+"@example.com/"), "without a user name or password"},
		{hookForm("generic", "https://example.com/a b"+hookToken), "Enter a whole web address"},
		{hookForm("generic", "https://example.com/"+strings.Repeat("a", 3000)+hookToken), "at most 2,048 characters"},
		{hookForm("discord", hookURL, "nonsense"), "Choose at least one event."},
		{hookForm("carrier-pigeon", hookURL), "Choose a service."},
	} {
		rec := nh.post(PathNotifications+"/new", tc.form, withCookie(c))
		body := rec.Body.String()
		if rec.Code != http.StatusBadRequest || !strings.Contains(body, tc.want) || !strings.Contains(body, `id="error-summary"`) {
			t.Errorf("%v: %d, want %q\n%s", tc.form, rec.Code, tc.want, body)
		}
		if reValueAttr.MatchString(body) {
			t.Errorf("%v: the address came back in the field", tc.form)
		}
		nh.wantNoAddress("a refused form", body)
	}
	// A good address refused for another field must be typed again.
	body := nh.post(PathNotifications+"/new", hookForm("discord", hookURL, "nonsense"), withCookie(c)).Body.String()
	if !strings.Contains(body, "Type the address again.") {
		t.Errorf("no retype message\n%s", body)
	}
	f := hookForm("discord", hookURL)
	f.Set("lang", "xx")
	if body := nh.post(PathNotifications+"/new", f, withCookie(c)).Body.String(); !strings.Contains(body, "Choose a language from the list.") {
		t.Errorf("bad language\n%s", body)
	}
	if list, _ := nh.n.List(context.Background()); len(list) != 0 {
		t.Errorf("a refused form stored %v", list)
	}
	if got := nh.audits(AuditWebhookAdd); len(got) != 0 {
		t.Errorf("a refused form was audited: %q", got)
	}
}

// Changing the service on the edit page needs the address again, since
// it is checked against the service.
func TestWebhookEditPresetNeedsAddress(t *testing.T) {
	nh := newNotifyHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := nh.signIn()
	nh.post(PathNotifications+"/new", hookForm("discord", hookURL), withCookie(c))
	rec := nh.post(webhookPath(1), hookForm("ntfy", ""), withCookie(c))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Type the address again to change the service.") {
		t.Errorf("%d\n%s", rec.Code, rec.Body)
	}
	nh.wantRedirect(nh.post(webhookPath(1), hookForm("ntfy", "https://ntfy.sh/"+hookToken), withCookie(c)), PathNotifications+"?done=saved&host=ntfy.sh&preset=ntfy")
	nh.wantNoAddress("an edit", "")
}

func TestWebhookUnknownIDs(t *testing.T) {
	nh := newNotifyHarness(t, map[string]string{"FOREMAN_SECRET_KEY": acctKey})
	c := nh.signIn()
	for _, p := range []string{"/9", "/0", "/-1", "/abc", "/9/delete"} {
		if rec := nh.get(PathNotifications+p, withCookie(c)); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d", p, rec.Code)
		}
	}
	if rec := nh.post(PathNotifications+"/9/delete", url.Values{}, withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("POST delete: %d", rec.Code)
	}
}

// A Done message only names a known preset and a plain host.
func TestWebhookDoneIgnoresOddLinks(t *testing.T) {
	for _, q := range []string{"done=added&preset=evil&host=x.com", "done=added&preset=discord&host=<b>", "done=other&preset=discord&host=x.com"} {
		v, _ := url.ParseQuery(q)
		if m := webhookDone(v); !m.IsZero() {
			t.Errorf("%s: %v", q, m)
		}
	}
}
