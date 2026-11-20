package web

import (
	"net/url"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
)

// settingsSections are Game settings' sub-pages, each one fieldset of the one
// form, listed under Game settings in the world's page column (DESIGN 6). The
// first opens on the plain settings link.
var settingsSections = []struct {
	Key   string
	Label i18n.Msg
}{
	{"listing", i18n.M("settings.listing")},
	{"players", i18n.M("settings.joining")},
	{"gameplay", i18n.M("settings.gameplay")},
	{"saving", i18n.M("settings.saving")},
	{"network", i18n.M("settings.network")},
	{"admins", i18n.M("settings.admins.legend")},
	{"bans", i18n.M("settings.bans.legend")},
	{"whitelist", i18n.M("settings.whitelist.legend")},
}

// sectionQuery names the sub-page in a settings link.
const sectionQuery = "section"

// settingsSection returns the known sub-page v names, or the first.
func settingsSection(v string) string {
	for _, s := range settingsSections {
		if s.Key == v {
			return v
		}
	}
	return settingsSections[0].Key
}

// settingsSectionPath links to one sub-page; the first is the plain link.
func settingsSectionPath(id, section string) string {
	if section == "" || section == settingsSections[0].Key {
		return settingsPath(id)
	}
	return settingsPath(id) + "?" + sectionQuery + "=" + url.QueryEscape(section)
}

// playerSection is the sub-page a player action's result belongs on.
func playerSection(a game.PlayerAction) string {
	switch a {
	case game.PlayerBan, game.PlayerUnban:
		return "bans"
	case game.PlayerWhitelistAdd, game.PlayerWhitelistRemove:
		return "whitelist"
	}
	return "admins"
}

// SubTab is one sub-page link in a world's page column. Section is its key,
// which the draft script matches to a fieldset's data-section.
type SubTab struct {
	Href    string
	Label   i18n.Msg
	Section string
	Current bool
}

// Show reports whether the section key is on this page: one sub-page, or
// every section when ShowAll is set, so a refused form hides no problem.
func (p SettingsPage) Show(key string) bool {
	return p.ShowAll || p.Section == key
}

// FormShown reports whether any fieldset of the form is on this page. A list
// the running game keeps is a card of its own, not a fieldset.
func (p SettingsPage) FormShown() bool {
	if p.ShowAll {
		return true
	}
	switch p.Section {
	case "bans":
		return !p.LiveBans
	case "whitelist":
		return !p.LiveWhitelist
	}
	return true
}

// SectionPath is this sub-page's own link.
func (p SettingsPage) SectionPath() string { return settingsSectionPath(p.WorldID, p.Section) }
