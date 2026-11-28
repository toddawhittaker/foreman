package web

import (
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// DESIGN 6, "Toasts": a settings page that comes back after a save or a
// refusal says so in a toast last in main, and keeps no message of its own
// above its plates, so they stay where they were.
func TestSettingsResultsAreToasts(t *testing.T) {
	r := newTestRenderer(t)
	for _, c := range []struct {
		page  Page
		error bool
		want  string
	}{
		{GeneralPage{Saved: true}, false, "Saved. The new settings apply from now on."},
		{GeneralPage{Saved: true, Trims: true, Trim: trimCount{Snapshots: 3, Worlds: 1}}, false, "Worlds affected: 1."},
		{SchedulePage{Saved: true, On: true, Next: time.Date(2026, 11, 3, 4, 30, 0, 0, time.UTC)}, false, `Saved. The next restart is <time datetime="2026-11-03T04:30:00Z">2026-11-03 04:30 UTC</time>.`},
		{SchedulePage{Saved: true}, false, "Saved. No restart is scheduled."},
		{NotificationsPage{Done: i18n.M("notify.done.added")}, false, ""},
		{PublicModsSettingsPage{Done: "on"}, false, ""},
		{APITokensPage{Done: "revoked", DoneName: "laptop"}, false, "laptop"},
		{APITokensPage{Done: "api-off"}, false, ""},
		{UsersPage{Done: "password", MyKind: "password"}, false, ""},
		{UsersPage{Error: i18n.M("users.error.busy"), MyKind: "password"}, true, ""},
		{AccountPage{Done: i18n.M("account.done.saved")}, false, "The account is saved."},
		{AccountPage{Error: i18n.M("account.error.nokey")}, true, "no secret key is set"},
	} {
		_, body := render(t, r, pageCase{status: 200, user: "alice", page: c.page})
		region := between(body, `<div id="toasts" class="toasts">`, "</div></div>")
		kind := `class="toast toast--success"`
		if c.error {
			kind = `class="toast toast--error"`
		}
		if !strings.Contains(region, kind) || !strings.Contains(region, c.want) {
			t.Errorf("%T: toast region %q, want %s with %q", c.page, region, kind, c.want)
		}
		main := body[strings.Index(body, "<main"):strings.Index(body, `<div id="toasts"`)]
		for _, old := range []string{"message--success", `id="done-message"`, "-refusal", "autofocus"} {
			if strings.Contains(main, old) {
				t.Errorf("%T: the page still carries %q", c.page, old)
			}
		}
	}
	for _, p := range []Page{GeneralPage{}, SchedulePage{}, NotificationsPage{}, PublicModsSettingsPage{}, APITokensPage{}, UsersPage{}, AccountPage{}} {
		_, body := render(t, r, pageCase{status: 200, user: "alice", page: p})
		if !strings.Contains(body, `<div id="toasts" class="toasts"></div>`) {
			t.Errorf("%T: a page with nothing to say has a toast", p)
		}
	}
}

// DESIGN 6, "The page header": every Settings tab draws its crumb, title and
// tab row in the one header, so the tab row sits in one place on each.
func TestSettingsPagesUseThePageHeader(t *testing.T) {
	r := newTestRenderer(t)
	for _, p := range []Page{
		GeneralPage{}, SchedulePage{}, NotificationsPage{}, WebhookFormPage{}, PublicModsSettingsPage{},
		APITokensPage{}, UsersPage{}, AccountPage{}, AuditLogPage{},
	} {
		_, body := render(t, r, pageCase{status: 200, user: "alice", page: p})
		head := between(body, `<header class="pagehead">`, `</header>`)
		if !strings.Contains(head, `<nav class="crumb"`) || !strings.Contains(head, "<h1>") || !strings.Contains(head, `class="subnav subnav--settings"`) {
			t.Errorf("%T: header %q", p, head)
		}
		if strings.Count(body, "<h1") != 1 || strings.Contains(body, `class="page-head`) {
			t.Errorf("%T: a title outside the page header", p)
		}
	}
	// Notifications' Add a webhook is the page's own button, in the header.
	_, body := render(t, r, pageCase{status: 200, user: "alice", page: NotificationsPage{}})
	if actions := between(body, `<div class="pagehead__actions">`, `</div>`); !strings.Contains(actions, `id="act-webhook-add"`) {
		t.Errorf("Add a webhook is not among the page's buttons: %q", actions)
	}
}
