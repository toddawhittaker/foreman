package web

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

// connectedButton is the Make admin button of a player, named by the player.
func connectedButton(name string) string {
	return `name="make_admin" value="` + name + `">Make admin<span class="visually-hidden"> ` + name + `</span></button>`
}

// join puts players into the running game of id.
func (g *gameRig) join(id string, players ...string) {
	g.t.Helper()
	for _, p := range players {
		if err := g.fake.Join(game.ContainerName(id), p); err != nil {
			g.t.Fatal(err)
		}
	}
}

// SPEC 11.1, 8.1: a running world lists who is connected and not yet an admin,
// each with a Make admin button named for the player. A stopped world, and a
// running one with nobody on, list no one.
func TestSettingsConnectedNow(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	g.writeConfig(id, world.ServerSettingsFile, `{"name": "Old"}`)
	g.writeConfig(id, world.ServerAdminListFile, `["Bob"]`)
	path := settingsPath(id)

	if body := g.get(path); strings.Contains(body, "Connected now") || strings.Contains(body, "make_admin") {
		t.Errorf("nobody is on, yet a list shows:\n%s", body)
	}
	g.join(id, "alice", "bob", "Mary Ann")
	body := g.get(path)
	if !strings.Contains(body, "Connected now") || !strings.Contains(body, connectedButton("alice")) {
		t.Errorf("alice is not offered:\n%s", body)
	}
	if strings.Contains(body, connectedButton("bob")) {
		t.Error("an admin is offered Make admin, matched without regard to case")
	}
	if strings.Contains(body, `value="Mary Ann"`) {
		t.Error("a name the game could not be sent is offered")
	}

	g2 := newGameRig(t, image21)
	id2 := g2.newWorld("Base", "2.1.24-no-space-age.zip", false)
	if body := g2.get(settingsPath(id2)); strings.Contains(body, "Connected now") {
		t.Errorf("a stopped world lists players:\n%s", body)
	}
}

// SPEC 11.1, 11.3: Make admin adds the name to the admins typed so far and
// shows the normal preview; confirming promotes both by RCON.
func TestSettingsMakeAdminGoesThroughPreview(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	g.writeConfig(id, world.ServerSettingsFile, `{"name": "Old"}`)
	g.join(id, "alice")
	path := settingsPath(id)

	rep := g.post(path, url.Values{"name": {"Old"}, "description": {""}, "admins": {"carol"}, "make_admin": {"alice"}})
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("preview %d:\n%s", rep.StatusCode, rep.body)
	}
	for _, want := range []string{"Added admin alice", "Added admin carol", "Changes to admins apply at once."} {
		if !strings.Contains(rep.body, want) {
			t.Errorf("preview lacks %q:\n%s", want, rep.body)
		}
	}
	if cmds := g.commands(id); slices.Contains(cmds, "/promote alice") {
		t.Fatal("promoted before the confirmation")
	}
	conf := g.post(path+"/confirm", url.Values{"token": {tokenOf(t, rep.body)}})
	if conf.StatusCode != http.StatusSeeOther {
		t.Fatalf("confirm %d:\n%s", conf.StatusCode, conf.body)
	}
	if cmds := g.commands(id); !slices.Contains(cmds, "/promote alice") || !slices.Contains(cmds, "/promote carol") {
		t.Errorf("RCON commands %q: want both promotes", cmds)
	}
}

// SPEC 11.1: a made admin goes through the same name check as a typed one.
func TestSettingsMakeAdminChecksTheName(t *testing.T) {
	r := newSettingsRig(t)
	form := settingsFormValues()
	form.Set("make_admin", "Mary Ann")
	rec := r.postAs(r.cookie, r.path(""), form)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "is not a usable player name.") {
		t.Errorf("%d:\n%s", rec.Code, rec.Body.String())
	}
	if len(r.snapshots()) != 0 || strings.Contains(r.read(world.ServerSettingsFile), "Mary") {
		t.Error("a refused name changed the world")
	}
}

// SPEC 7.6, 14.17: the Restart link after a save opens the stop page and
// Cancel there goes back to the settings; a link from nowhere safe, and the
// Server page's own Restart, cancel to the Server page.
func TestStopPageCancelReturnsWhereItCameFrom(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.startRunning()
	restart := gamePath(id, "restart")
	settings := settingsPath(id)

	done := g.get(settings + "?saved=1&restart=1")
	link := `href="` + restart + `?from=%2Fworlds%2F` + id + `%2Fsettings"`
	if !strings.Contains(done, link) {
		t.Errorf("the Restart link after a save lacks %s:\n%s", link, done)
	}

	cancel := func(body, to string) bool {
		return strings.Contains(body, `<a href="`+to+`">Cancel, keep it running</a>`)
	}
	from := restart + "?from=" + url.QueryEscape(settings)
	body := g.get(from)
	if !cancel(body, settings) || !strings.Contains(body, `action="`+restart+`?from=%2Fworlds%2F`+id+`%2Fsettings"`) {
		t.Errorf("Cancel does not return to the settings:\n%s", body)
	}
	rep := g.post(from, url.Values{"countdown": {"x"}})
	if rep.StatusCode != http.StatusBadRequest || !cancel(rep.body, settings) {
		t.Errorf("a refused countdown lost the way back: %d\n%s", rep.StatusCode, rep.body)
	}
	for _, q := range []string{"", "?from=//evil.example", "?from=https://evil.example/", `?from=/\evil.example`} {
		if body := g.get(restart + q); !cancel(body, PathServer) {
			t.Errorf("%q: Cancel is not the Server page:\n%s", q, body)
		}
	}
	if body := g.get(gamePath(id, "stop") + "?from=" + url.QueryEscape(settings)); !cancel(body, settings) {
		t.Errorf("the stop page ignores where it came from:\n%s", body)
	}
}
