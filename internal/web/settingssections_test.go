package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

// DESIGN 6: a sub-page key that is not one of Game settings' sections opens
// the first, so a crafted link cannot name a section that does not exist.
func TestSettingsSection(t *testing.T) {
	for in, want := range map[string]string{
		"": "listing", "listing": "listing", "players": "players", "whitelist": "whitelist",
		"bogus": "listing", "Players": "listing", "../admins": "listing",
	} {
		if got := settingsSection(in); got != want {
			t.Errorf("settingsSection(%q) = %q, want %q", in, got, want)
		}
	}
}

// A player action's result shows on the sub-page of the list it changed.
func TestPlayerSection(t *testing.T) {
	for a, want := range map[game.PlayerAction]string{
		game.PlayerBan: "bans", game.PlayerUnban: "bans",
		game.PlayerWhitelistAdd: "whitelist", game.PlayerWhitelistRemove: "whitelist",
		game.PlayerPromote: "admins", game.PlayerDemote: "admins", game.PlayerKick: "admins",
	} {
		if got := playerSection(a); got != want {
			t.Errorf("playerSection(%q) = %q, want %q", a, got, want)
		}
	}
}

// One sub-page shows its own fieldset; ShowAll shows every one, and a list
// the running game keeps is a card, so its sub-page has no form.
func TestSettingsShowAndFormShown(t *testing.T) {
	p := SettingsPage{Section: "players"}
	if !p.Show("players") || p.Show("listing") || !p.FormShown() {
		t.Errorf("one sub-page: Show players %v, listing %v, FormShown %v", p.Show("players"), p.Show("listing"), p.FormShown())
	}
	all := SettingsPage{Section: "players", ShowAll: true}
	for _, s := range settingsSections {
		if !all.Show(s.Key) {
			t.Errorf("ShowAll hides %s", s.Key)
		}
	}
	for _, c := range []struct {
		page SettingsPage
		want bool
	}{
		{SettingsPage{Section: "bans"}, true},
		{SettingsPage{Section: "bans", LiveBans: true}, false},
		{SettingsPage{Section: "whitelist", LiveWhitelist: true}, false},
		{SettingsPage{Section: "whitelist", LiveBans: true}, true},
		{SettingsPage{Section: "bans", LiveBans: true, ShowAll: true}, true},
	} {
		if got := c.page.FormShown(); got != c.want {
			t.Errorf("FormShown(%+v) = %v, want %v", c.page, got, c.want)
		}
	}
}

var reSectionInput = regexp.MustCompile(`<input type="hidden" name="section" value="([^"]+)">`)

func postedSection(t *testing.T, body string) string {
	t.Helper()
	m := reSectionInput.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// A save made from a sub-page comes back to it; the first sub-page is the
// plain link, so it adds nothing.
func TestSettingsSaveReturnsToItsSubPage(t *testing.T) {
	r := newSettingsRig(t)
	for section, want := range map[string]string{"players": "&section=players", "listing": ""} {
		form := settingsFormValues()
		form.Set("max_players", map[string]string{"players": "9", "listing": "10"}[section])
		form.Set("section", section)
		token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())
		rec := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s: confirm %d:\n%s", section, rec.Code, rec.Body.String())
		}
		to := rec.Header().Get("Location")
		if want != "" && !strings.HasSuffix(to, want) {
			t.Errorf("%s: redirect %q lacks %q", section, to, want)
		}
		if want == "" && strings.Contains(to, "section=") {
			t.Errorf("%s: redirect %q names a section", section, to)
		}
	}
}

// A preview whose file changed before the save is shown again, and its save
// still returns to the sub-page it came from.
func TestSettingsRepreviewKeepsSubPage(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("max_players", "9")
	form.Set("section", "saving")
	token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())
	r.write(world.ServerSettingsFile, strings.Replace(settingsFile, `"autosave_slots": 5`, `"autosave_slots": 7`, 1))
	again := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {token}})
	if again.Code != http.StatusOK {
		t.Fatalf("re-preview %d:\n%s", again.Code, again.Body.String())
	}
	rec := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, again.Body.String())}})
	if to := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.HasSuffix(to, "&section=saving") {
		t.Errorf("confirm %d to %q", rec.Code, to)
	}
}

// "Go back and edit" opens the sub-page the preview came from; a sub-page
// named in the link wins, so the page column can move between them.
func TestSettingsEditTokenRestoresSubPage(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("max_players", "9")
	form.Set("section", "players")
	token := tokenOf(t, r.postAs(r.cookie, r.path(""), form).Body.String())

	body := r.getAs(r.cookie, r.path("?edit="+url.QueryEscape(token))).Body.String()
	if got := postedSection(t, body); got != "players" {
		t.Errorf("edit page section %q, want players", got)
	}
	if !strings.Contains(body, `<fieldset id="settings-joining" data-section="players">`) || !strings.Contains(body, `<fieldset id="settings-listing" data-section="listing" hidden>`) {
		t.Errorf("edit page does not show only the players sub-page:\n%s", body)
	}
	// The draft script runs on an edit too, so changes made on top of the
	// preview are kept across sub-pages.
	if !strings.Contains(body, `data-draft="alice"`) {
		t.Error("the edit page's form has no draft")
	}

	body = r.getAs(r.cookie, r.path("?section=admins&edit="+url.QueryEscape(token))).Body.String()
	if got := postedSection(t, body); got != "admins" {
		t.Errorf("named sub-page %q, want admins", got)
	}
}

// A refused form shows every section, since its problems may be on any of
// them, but keeps the sub-page it was posted from for the page column and
// the next preview. It has no draft, so a problem is not hidden by one.
func TestSettingsRefusedFormKeepsSubPage(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("max_players", "ten")
	form.Set("section", "players")
	rec := r.postAs(r.cookie, r.path(""), form)
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	if got := postedSection(t, body); got != "players" {
		t.Errorf("refused form section %q, want players", got)
	}
	for _, id := range []string{"settings-listing", "settings-joining", "settings-gameplay", "settings-saving", "settings-admins"} {
		if regexp.MustCompile(`<fieldset id="` + id + `"[^>]* hidden`).MatchString(body) {
			t.Errorf("the refused form hides %s", id)
		}
	}
	if !strings.Contains(body, `href="`+settingsSectionPath(r.id, "players")+`" data-section="players" aria-current="page"`) {
		t.Errorf("the page column does not mark the players sub-page:\n%s", body)
	}
	if strings.Contains(body, "data-draft=") {
		t.Error("the refused form keeps a draft")
	}
}

// The draft script finds its marks in the page: each sub-page link and each
// fieldset names its section, and each link has hidden words for the dot.
func TestSettingsSubPageMarks(t *testing.T) {
	r := newSettingsRig(t)
	body := r.getAs(r.cookie, r.path("?section=gameplay")).Body.String()
	for _, s := range settingsSections {
		if !strings.Contains(body, `data-section="`+s.Key+`"`) {
			t.Errorf("no mark for %s", s.Key)
		}
	}
	if n := strings.Count(body, "data-unsaved-note hidden>"); n < len(settingsSections) {
		t.Errorf("%d hidden unsaved notes, want at least %d", n, len(settingsSections))
	}
	if !strings.Contains(body, `id="settings-unsaved" role="status"></p>`) {
		t.Error("the unsaved count is not an empty status region")
	}
}
