package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
)

func (g *gameRig) playerCommands(id string) []string {
	var out []string
	for _, c := range g.commands(id) {
		for _, p := range []string{"/kick", "/ban", "/unban", "/promote", "/demote", "/whitelist"} {
			if strings.HasPrefix(c, p+" ") {
				out = append(out, c)
			}
		}
	}
	return out
}

func (g *gameRig) playerAudits() int {
	n := 0
	for _, e := range g.audit() {
		if e.Action == game.AuditPlayer {
			n++
		}
	}
	return n
}

// SPEC 8.2, 14.9: a name the console would misread is refused, never sent
// or audited, and the page it came from shows it as a field error.
func TestPlayerActionRefusesAName(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	back := settingsPath(id)
	for _, name := range []string{"bob smith", " bob", "a\x00b", "x\u200b", strings.Repeat("a", 65)} {
		g.wantRedirectTo(back, g.post(playerPath(id, "promote"), url.Values{"name": {name}, "return": {back}}))
	}
	// The ban form on the Settings tab goes to the confirmation, which sends
	// a bad name back.
	rep := g.do(http.MethodGet, playerPath(id, "ban")+"?name=bob+smith&return="+url.QueryEscape(back), nil, http.Header{"Cookie": {g.cookie.String()}})
	g.wantRedirectTo(back, rep)
	if sent := g.playerCommands(id); len(sent) != 0 || g.playerAudits() != 0 {
		t.Fatalf("sent %q, audited %d", sent, g.playerAudits())
	}
	page := g.get(back)
	if !strings.Contains(page, `id="ban-name-error"`) || !strings.Contains(page, "This is not a usable player name.") {
		t.Errorf("the settings page lacks the ban name's field error:\n%s", page)
	}
	if again := g.get(back); strings.Contains(again, `id="ban-name-error"`) {
		t.Error("the result showed twice; a reload must drop it")
	}
}

// SPEC 8.2: kick and ban confirm, and a reason with a line break is refused
// on the confirmation with nothing sent; promote acts at once and is audited.
func TestPlayerActionsSendAndConfirm(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	for _, a := range []string{"promote", "unban", "whitelist-add", "nonsense"} {
		rep := g.do(http.MethodGet, playerPath(id, a)+"?name=bob", nil, http.Header{"Cookie": {g.cookie.String()}})
		if rep.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404: only kick and ban confirm", a, rep.StatusCode)
		}
	}
	if page := g.get(playerPath(id, "ban") + "?name=bob"); !strings.Contains(page, "Ban bob from Base?") || !strings.Contains(page, `id="act-confirm-ban"`) {
		t.Errorf("ban confirmation:\n%s", page)
	}
	rep := g.post(playerPath(id, "ban"), url.Values{"name": {"bob"}, "reason": {"x\n/promote mallory"}})
	if rep.StatusCode != http.StatusBadRequest || !strings.Contains(rep.body, "The reason must be one line") {
		t.Fatalf("bad reason: %d\n%s", rep.StatusCode, rep.body)
	}
	if sent := g.playerCommands(id); len(sent) != 0 {
		t.Fatalf("sent %q", sent)
	}
	g.wantRedirectHome(g.post(playerPath(id, "promote"), url.Values{"name": {"carol"}, "return": {"//evil.example"}}))
	if sent := g.playerCommands(id); len(sent) != 1 || sent[0] != "/promote carol" || g.playerAudits() != 1 {
		t.Errorf("sent %q, audited %d", sent, g.playerAudits())
	}
}

// SPEC 8.3: the whitelist switch is audited and kept per world; the next
// start passes both whitelist flags.
func TestWhitelistSwitch(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
	g.wantRedirectTo(settingsPath(id)+"?whitelist=on", g.post(settingsWhitelistPath(id), url.Values{"use_whitelist": {"on"}}))
	if page := g.get(settingsPath(id) + "?whitelist=on"); !strings.Contains(page, "only whitelisted players can join Base") {
		t.Errorf("no done message:\n%s", page)
	}
	if page := g.get(settingsPath(id) + "?whitelist=off"); strings.Contains(page, "anyone can join") {
		t.Error("a link that does not match the switch showed a result")
	}
	found := false
	for _, e := range g.audit() {
		found = found || (e.Action == AuditWhitelistSwitch && e.Detail == "on")
	}
	if !found {
		t.Error("the switch was not audited")
	}
	g.wantRedirectHome(g.post(gamePath(id, "start"), url.Values{}))
	g.waitPage(running)
	if cmd := strings.Join(g.container(id).Cmd, " "); !strings.Contains(cmd, "--use-server-whitelist --server-whitelist ") {
		t.Errorf("the start script lacks the whitelist flags: %s", cmd)
	}
}

// DESIGN 8.12: a result shows once, only on the page the action went back
// to, and not at all after about a minute, so a stale Undo never appears.
func TestPlayerResultShowsOnlyOnItsPage(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	set := settingsPath(id)
	g.wantRedirectHome(g.post(playerPath(id, "promote"), url.Values{"name": {"carol"}}))
	if page := g.get(set); strings.Contains(page, "carol is now an admin") {
		t.Error("a result meant for the Server page showed on the Settings tab")
	}
	g.wantRedirectTo(set, g.post(playerPath(id, "demote"), url.Values{"name": {"carol"}, "return": {set}}))
	g.clock.Advance(2 * time.Minute)
	if page := g.get(set); strings.Contains(page, "carol is no longer an admin") {
		t.Error("a result two minutes old was shown")
	}
	g.wantRedirectTo(set, g.post(playerPath(id, "promote"), url.Values{"name": {"carol"}, "return": {set}}))
	page := g.get(set)
	if !strings.Contains(page, `id="player-done"`) || !strings.Contains(page, "carol is now an admin of Base.") {
		t.Fatalf("the result is missing:\n%s", page)
	}
	if strings.Count(page, "autofocus") != 1 {
		t.Errorf("%d autofocus attributes, want one", strings.Count(page, "autofocus"))
	}
}

// A done action shows the game's reply, which may be text the captures
// never saw (SPEC 22.3: marked as English).
func TestPlayerResultShowsTheGamesReply(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	set := settingsPath(id)
	g.wantRedirectTo(set, g.post(playerPath(id, "ban"), url.Values{"name": {"bob"}, "return": {set}}))
	if page := g.get(set); !strings.Contains(page, `<span lang="en">Added bob to the banlist.</span>`) {
		t.Errorf("the reply is missing:\n%s", page)
	}
}

// SPEC 17.1: a refused name is in the error summary and focused, both for a
// name typed in a field and for one with no field, which is a page error.
func TestPlayerNameErrorsAreSummarised(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	set := settingsPath(id)
	rep := g.do(http.MethodGet, playerPath(id, "ban")+"?name=a+b&return="+url.QueryEscape(set), nil, http.Header{"Cookie": {g.cookie.String()}})
	g.wantRedirectTo(set, rep)
	page := g.get(set)
	if !strings.Contains(page, `id="error-summary"`) || !strings.Contains(page, `href="#ban-name"`) {
		t.Errorf("no summary for the ban name:\n%s", page)
	}
	g.wantRedirectTo(set, g.post(playerPath(id, "unban"), url.Values{"name": {"a b"}, "return": {set}}))
	page = g.get(set)
	if !strings.Contains(page, `id="player-error"`) || !strings.Contains(page, "This is not a usable player name.") {
		t.Errorf("a refused name with no field showed nothing:\n%s", page)
	}
}

// The game compares player names without regard to case.
func TestPlayerNamesIgnoreCase(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	if err := g.fake.Join(game.ContainerName(id), "alice"); err != nil {
		t.Fatal(err)
	}
	if page := g.get(playerPath(id, "kick") + "?name=ALICE"); strings.Contains(page, `id="player-gone"`) || !strings.Contains(page, `id="act-confirm-kick"`) {
		t.Errorf("ALICE was taken as gone while alice is on:\n%s", page)
	}
	if got := banReasonIn([]byte(`[{"username": "Bob", "reason": "griefing"}]`), "bob"); got != "griefing" {
		t.Errorf("reason %q", got)
	}
}

// settingsPlayerCase is the Settings tab of a running world whose game keeps
// both lists, showing the result of action.
func settingsPlayerCase(action game.PlayerAction, failed bool) SettingsPage {
	p := settingsFormCase(false)
	p.Running, p.LiveBans, p.LiveWhitelist, p.UseWhitelist = true, true, true, true
	p.BanList = []gamesettings.Ban{{Name: "bob", Reason: "griefing"}, {Name: "carl"}}
	p.WhitelistNames = []string{"hank"}
	p.Player = &PlayerResult{WorldID: "base", WorldName: longName, Action: action, Name: "carol", Reason: "spam", Reply: "Added carol to the banlist."}
	if failed {
		p.Player.Error, p.Player.Reply = i18n.M("players.error.refused", "name", "carol", "world", longName), "carol wasn't banned."
	}
	return p
}
