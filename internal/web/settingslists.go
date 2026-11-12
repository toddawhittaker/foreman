package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The ban list and whitelist on the Settings tab (SPEC 8.3, DESIGN 8.13).
// While the game keeps a list, it is changed by player actions sent to the
// game; otherwise Foreman writes the file through the settings form, after
// the settings snapshot.

// PathSettingsWhitelist turns the world's whitelist switch on or off.
const PathSettingsWhitelist = "/worlds/{id}/settings/whitelist"

// AuditWhitelistSwitch records a change to a world's whitelist switch; the
// detail is "on" or "off".
const AuditWhitelistSwitch = "whitelist-switch"

// switchQuery names the query value that shows the result of a switch change.
const switchQuery = "whitelist"

func settingsWhitelistPath(id string) string { return settingsPath(id) + "/whitelist" }

// settingsLists are a world's ban list and whitelist as they are now.
type settingsLists struct {
	banCfg, wlCfg        world.ConfigRead
	bans                 []gamesettings.Ban
	whitelist            []string
	bansBroken, wlBroken bool
	// running is true when the game runs, so it keeps the ban list.
	running bool
	// kept is true when the running game was started with the whitelist
	// switch on, so it keeps the whitelist.
	kept bool
	// use is the world's switch, which the game reads at its next start.
	use bool
}

func (h *settingsHandlers) loadLists(ctx context.Context, sum world.Summary, running bool) (settingsLists, error) {
	s := h.s
	l := settingsLists{running: running, use: sum.UseWhitelist}
	var err error
	if l.banCfg, err = s.worlds.ReadConfig(sum.ID, world.ServerBanListFile); err != nil {
		return l, err
	}
	if l.wlCfg, err = s.worlds.ReadConfig(sum.ID, world.ServerWhiteListFile); err != nil {
		return l, err
	}
	if l.banCfg.Found {
		if l.bans, err = gamesettings.ParseBanList(l.banCfg.Data); err != nil {
			l.bans, l.bansBroken = nil, true
		}
	}
	if l.wlCfg.Found {
		if l.whitelist, err = gamesettings.ParseWhitelist(l.wlCfg.Data); err != nil {
			l.whitelist, l.wlBroken = nil, true
		}
	}
	if running && s.game != nil {
		// The save asks again, so a page shown in the wrong mode cannot make
		// Foreman write a file the game keeps.
		if l.kept, err = s.game.WhitelistKept(ctx, sum.ID); err != nil {
			s.log.Warn("read whitelist switch of the running game", "world", sum.ID, "error", err)
		}
	}
	return l, nil
}

func (l settingsLists) banNames() []string { return gamesettings.BanNames(l.bans) }

// bansEditable and whitelistEditable are true when the list is a box of the
// settings form: Foreman writes the file, since the game does not keep it.
func (l settingsLists) bansEditable() bool      { return !l.bansBroken && !l.running }
func (l settingsLists) whitelistEditable() bool { return !l.wlBroken && !l.kept }

func (l settingsLists) fill(p *SettingsPage) {
	p.BanList, p.BansBroken, p.LiveBans = l.bans, l.bansBroken, l.running
	p.Bans = strings.Join(l.banNames(), "\n")
	p.WhitelistNames, p.WhitelistBroken, p.LiveWhitelist = l.whitelist, l.wlBroken, l.kept
	p.Whitelist = strings.Join(l.whitelist, "\n")
	p.UseWhitelist = l.use
	p.WhitelistRestart = l.running && l.use != l.kept
}

// refillLists puts the boxes of a previewed change back in the form.
func refillLists(p *SettingsPage, c *pendingChange) {
	if c == nil || c.Form == nil {
		return
	}
	if c.Form.Bans.Posted {
		p.Bans = strings.Join(c.Form.Bans.Names, "\n")
	}
	if c.Form.Whitelist.Posted {
		p.Whitelist = strings.Join(c.Form.Whitelist.Names, "\n")
	}
}

// listEdit is one list box as posted.
type listEdit struct {
	Posted bool
	// Base is the list the form showed; Names are the box's names.
	Base, Names []string
	err         i18n.Msg
}

// postedList reads a list box: one name per line, checked as the admins box
// is (SPEC 14.9). A box that was not posted, or is not editable, changes
// nothing.
func postedList(text string, posted bool, base []string) listEdit {
	if !posted {
		return listEdit{}
	}
	names, bad := adminLines(text)
	if bad > 0 {
		return listEdit{err: i18n.M("settings.admins.bad_name", "line", bad, "max", gamesettings.MaxNameBytes)}
	}
	return listEdit{Posted: true, Base: base, Names: names}
}

// files works out the list files a form edit changes, applied to the lists as
// they are now: only names the person added or removed are changed, and a
// ban kept keeps its reason.
func (l settingsLists) files(e formEdit) []pendingFile {
	var files []pendingFile
	if e.Bans.Posted && !l.bansBroken {
		add, remove := gamesettings.AdminDiff(e.Bans.Base, e.Bans.Names)
		now := l.banNames()
		target := applyAdminChange(now, add, remove)
		if a, r := gamesettings.AdminDiff(now, target); len(a)+len(r) > 0 {
			reasons := map[string]string{}
			for _, b := range l.bans {
				reasons[strings.ToLower(b.Name)] = b.Reason
			}
			bans := make([]gamesettings.Ban, len(target))
			for i, n := range target {
				bans[i] = gamesettings.Ban{Name: n, Reason: reasons[strings.ToLower(n)]}
			}
			files = append(files, pendingFile{File: world.ServerBanListFile, Hash: l.banCfg.Hash, New: gamesettings.BanListJSON(bans)})
		}
	}
	if e.Whitelist.Posted && !l.wlBroken {
		add, remove := gamesettings.AdminDiff(e.Whitelist.Base, e.Whitelist.Names)
		target := applyAdminChange(l.whitelist, add, remove)
		if a, r := gamesettings.AdminDiff(l.whitelist, target); len(a)+len(r) > 0 {
			files = append(files, pendingFile{File: world.ServerWhiteListFile, Hash: l.wlCfg.Hash, New: gamesettings.NameListJSON(target)})
		}
	}
	return files
}

// fillSwitchDone shows the result of a switch change the browser was sent
// back from, and reports whether the link asked for one. The result shows
// only when the switch is as the link says.
func fillSwitchDone(p *SettingsPage, sw settingsWorld, v string) bool {
	if v != "on" && v != "off" {
		return false
	}
	if (v == "on") != sw.lists.use {
		return true
	}
	if sw.lists.use {
		p.Done = i18n.M("settings.whitelist.done_on", "world", sw.sum.Name)
	} else {
		p.Done = i18n.M("settings.whitelist.done_off", "world", sw.sum.Name)
	}
	p.DoneRestart = sw.lists.running && sw.lists.use != sw.lists.kept
	return true
}

// handleWhitelistSwitch sets the world's whitelist switch. It changes only
// how the game is started, so it takes no snapshot; it is recorded in the
// audit log first (SPEC 8.3, 12.9).
func (h *settingsHandlers) handleWhitelistSwitch(w http.ResponseWriter, r *http.Request, u store.User) {
	s := h.s
	sw, ok := h.load(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	if !sw.readOnly.IsZero() {
		p := sw.formPage(sw.formText(), sw.admins)
		p.Error = sw.readOnly
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	on := r.PostFormValue("use_whitelist") != ""
	detail := "off"
	if on {
		detail = "on"
	}
	err := s.store.AddAudit(r.Context(), store.AuditEntry{
		Time: s.now(), UserID: u.ID, Username: u.Username, Action: AuditWhitelistSwitch,
		WorldID: sw.sum.ID, Detail: detail,
	})
	if err == nil {
		err = s.worlds.SetUseWhitelist(sw.sum.ID, on)
	}
	if err != nil {
		status, msg := worldError(err)
		if status == http.StatusInternalServerError || msg.IsZero() {
			s.log.Error("set whitelist switch", "world", sw.sum.ID, "error", err)
			status, msg = http.StatusInternalServerError, i18n.M("settings.error.failed")
		}
		p := sw.formPage(sw.formText(), sw.admins)
		p.Error = msg
		s.page(w, r, status, u.Username, p)
		return
	}
	http.Redirect(w, r, settingsPath(sw.sum.ID)+"?"+switchQuery+"="+detail, http.StatusSeeOther)
}

// PlayerDone is the player action result to show as Done, or nil.
func (p SettingsPage) PlayerDone() *PlayerResult {
	if p.Player == nil || !p.Player.Error.IsZero() {
		return nil
	}
	return p.Player
}

// PlayerFailed is the player action result to show as an error at the top,
// or nil. A name refused by the check is a field error instead.
func (p SettingsPage) PlayerFailed() *PlayerResult {
	if p.Player == nil || p.Player.Error.IsZero() || p.Player.NameError {
		return nil
	}
	return p.Player
}

// nameError is the field error of a name typed for action, and the name.
func (p SettingsPage) nameError(action game.PlayerAction) (i18n.Msg, string) {
	if p.Player == nil || !p.Player.NameError || p.Player.Action != action {
		return i18n.Msg{}, ""
	}
	return p.Player.Error, p.Player.Name
}

func (p SettingsPage) BanNameError() i18n.Msg { m, _ := p.nameError(game.PlayerBan); return m }
func (p SettingsPage) BanNameTyped() string   { _, n := p.nameError(game.PlayerBan); return n }
func (p SettingsPage) WhitelistNameError() i18n.Msg {
	m, _ := p.nameError(game.PlayerWhitelistAdd)
	return m
}
func (p SettingsPage) WhitelistNameTyped() string {
	_, n := p.nameError(game.PlayerWhitelistAdd)
	return n
}
