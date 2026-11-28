package web

import (
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorioaccount"
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
		{GeneralPage{Saved: true, Trims: true, Trim: trimCount{Snapshots: 3, Worlds: 1}}, false, "Saved. The new settings apply from now on."},
		{SchedulePage{Saved: true, On: true, Next: time.Date(2026, 11, 3, 4, 30, 0, 0, time.UTC)}, false, "Saved. The next restart is 2026-11-03 04:30 UTC."},
		{SchedulePage{Saved: true}, false, "Saved. No restart is scheduled."},
		{NotificationsPage{Done: i18n.M("notify.done.added", "preset", "Discord", "host", "discord.com")}, false, "Added the Discord webhook to discord.com."},
		{PublicModsSettingsPage{Done: "on"}, false, "Turned the public mods page on."},
		{APITokensPage{Done: "revoked", DoneName: "laptop"}, false, "laptop"},
		{APITokensPage{Done: "api-off"}, false, "Turned REST API access off."},
		{UsersPage{Done: "password", MyKind: "password"}, false, "Your password is changed. You were signed out everywhere else."},
		{UsersPage{Error: i18n.M("users.password.busy"), MyKind: "password"}, true, "Foreman is busy checking other passwords. Try again in a moment."},
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

// The trim counts are the only record of what a save will delete, and a toast
// closes, so the page itself says them in plain text after the save; the
// toast stays short.
func TestGeneralSaveThatTrimsSaysSoInThePage(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, user: "alice", page: GeneralPage{Saved: true, Trims: true, Trim: trimCount{Snapshots: 3, Worlds: 1}}})
	toast := between(body, `<div id="toasts" class="toasts">`, "</div></div>")
	if strings.Contains(toast, "snapshots") || strings.Contains(toast, "Worlds affected") {
		t.Errorf("the toast carries the trim counts: %q", toast)
	}
	note := between(body, `id="general-trim"`, "</div>")
	for _, want := range []string{"3 automatic snapshots will be removed", "Worlds affected: 1."} {
		if !strings.Contains(note, want) {
			t.Errorf("the saved page's trim note lacks %q: %q", want, note)
		}
	}
}

// The confirm pages name their question by the heading, so a screen reader
// hears the question when it lands in the confirm section, as on Force stop,
// Switch and a player action.
func TestConfirmPagesAreNamedByTheirHeading(t *testing.T) {
	r := newTestRenderer(t)
	for _, p := range []Page{
		UserDeletePage{User: UserRow{ID: 3, Username: "dave", Kind: SignInProxy}, Tokens: 2},
		APITokenRevokePage{Token: TokenRow{ID: 1, Owner: "bob", Name: "Claude", Scope: "read"}},
		WebhookDeletePage{Hook: WebhookView{ID: 1, Preset: i18n.M("notify.preset.discord"), Host: "discord.com"}},
		GeneralConfirmPage{Trim: trimCount{Snapshots: 3, Worlds: 2}, Hidden: []GeneralHidden{{Name: "snapshot_retention", Value: "2"}}},
	} {
		_, body := render(t, r, pageCase{status: 200, user: "alice", page: p})
		h1 := between(body, "<h1", "</h1>")
		if !strings.Contains(h1, `<span id="confirm-heading">`) {
			t.Errorf("%T: the heading has no confirm-heading: %q", p, h1)
		}
		if !strings.Contains(body, `<section class="confirm" aria-labelledby="confirm-heading">`) {
			t.Errorf("%T: the confirm section is not named by its heading", p)
		}
	}
}

// The public page's one plate is named by the heading above it; an
// aria-label that repeats the h1 only says it twice.
func TestPublicModsPlateRepeatsNoLabel(t *testing.T) {
	body := renderPublic(t, PublicModsData{})
	if strings.Contains(body, `aria-label="Public mods"`) {
		t.Error("a plate repeats the page heading as its label")
	}
	body = renderPublic(t, PublicModsData{Worlds: []PublicWorld{{Name: "Base", GameVersion: "2.0.1"}}})
	if strings.Contains(body, `aria-label="Public mods"`) {
		t.Error("a plate repeats the page heading as its label")
	}
}

// DESIGN 7.1: a form's foot has Cancel first, then the action; and the
// Required note leads the form, before any note about why it reopened, as on
// the new-entry form.
func TestAccountReplaceFormOrder(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, user: "alice", page: AccountPage{Source: factorioaccount.Stored, CanStore: true, TokenError: i18n.M("account.error.nokey"), Return: "/worlds"}})
	form := between(body, `aria-labelledby="account-replace-summary"`, "</form>")
	cancel, replace := strings.Index(form, `class="button button--ghost"`), strings.Index(form, `type="submit"`)
	if cancel < 0 || replace < 0 || cancel > replace {
		t.Errorf("Cancel at %d, Replace at %d: %q", cancel, replace, form)
	}
	note, retype := strings.Index(form, `required-note`), strings.Index(form, "message--note")
	if note < 0 || retype < 0 || note > retype {
		t.Errorf("the Required note at %d, the retype note at %d", note, retype)
	}
}
