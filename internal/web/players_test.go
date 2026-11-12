package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
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
