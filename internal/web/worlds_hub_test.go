package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
)

func TestBusyText(t *testing.T) {
	names := map[string]string{"a": "Alpha", "b": "Bravo"}
	steps := []game.Step{{Name: "Warn players", State: game.StepDone}, {Name: "Take a snapshot of both worlds", State: game.StepProgress}}
	for _, tc := range []struct {
		name string
		op   *game.Operation
		want string
	}{
		{"none", nil, ""},
		{"done", &game.Operation{Action: game.AuditSwitch, Done: true, Switch: &game.SwitchInfo{From: "a", To: "b"}}, ""},
		{"switch with its step", &game.Operation{Action: game.AuditSwitch, Steps: steps, Switch: &game.SwitchInfo{From: "a", To: "b"}},
			"Switching from Alpha to Bravo. Now: Take a snapshot of both worlds."},
		{"unknown world falls back to its id", &game.Operation{Action: game.AuditSwitch, Switch: &game.SwitchInfo{From: "a", To: "gone"}},
			"Switching from Alpha to gone."},
		{"other operation", &game.Operation{Action: game.AuditRestart}, "An operation is running on this world: restart."},
		{"action named in words", &game.Operation{Action: game.AuditForceStop}, "An operation is running on this world: forced stop."},
		{"action without a name", &game.Operation{Action: "mystery"}, "An operation is running on this world: action."},
	} {
		name := func(id string) string {
			if n := names[id]; n != "" {
				return n
			}
			return id
		}
		if got := i18n.English.Format(busyText(tc.op, name)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// With nothing running, a world's page asks nothing of the world store: the
// busy line is not worth listing every world.
func TestWorldBusyReadsNoWorldsWhenIdle(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Alpha", fixtureNoSA, false)
	s := &server{game: g.ctrl} // no world store: a read of it would panic
	if got := s.worldBusy(id); !got.IsZero() {
		t.Errorf("busy %q on an idle world", got)
	}
}

// SPEC 7.9: the switch is busy on both worlds and both pages show it; a
// world it is not about is left unmarked. SPEC 17.8: a delete that would be
// refused says why before the name is typed.
func TestWorldsShowARunningSwitch(t *testing.T) {
	g := newGameRig(t, image21)
	a := g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	c := g.newWorld("Charlie", fixtureNoSA, false)

	list := g.get(PathWorlds)
	g.wantContains("idle list", list, `href="`+switchPath(b)+`" class="button">Switch<span class="visually-hidden"> to Bravo</span>`, `href="`+switchPath(c)+`" class="button">Switch<span class="visually-hidden"> to Charlie</span>`)
	g.wantMissing("idle list", list, `href="`+switchPath(a)+`"`, "Busy:", "Switching is unavailable")

	g.startGame(a)
	g.wantRedirectHome(g.post(switchPath(b), url.Values{"from": {a}, "running": {"1"}, "countdown": {"30"}}))
	g.waitPage("Switching from Alpha to Bravo")

	list = g.get(PathWorlds)
	if n := strings.Count(list, "<strong>Busy:</strong> Switching from Alpha to Bravo."); n != 2 {
		t.Errorf("want both worlds marked, got %d marks:\n%s", n, list)
	}
	g.wantContains("busy list", list, "Switching is unavailable while an operation runs on the server")
	g.wantMissing("busy list", list, `href="`+switchPath(b)+`"`, `href="`+switchPath(c)+`"`)

	for _, id := range []string{a, b} {
		g.wantContains("involved world", g.get(worldPath(id)), "Switching from Alpha to Bravo.",
			`<a href="/">Go to the Server page</a>`)
	}
	// Both the note and the delete refusal link to the page that follows it.
	if n := strings.Count(g.get(worldPath(b)), `<a href="/">Go to the Server page</a>`); n != 2 {
		t.Errorf("the busy world's page has %d links to the Server page, want 2", n)
	}
	g.wantMissing("active world's delete refusal", g.get(worldPath(a)), "cannot be deleted until it ends. <a")
	g.wantMissing("unrelated world", g.get(worldPath(c)), "Switching from Alpha to Bravo.", "Go to the Server page")

	del := g.get(deletePath(b))
	g.wantContains("delete while busy", del, "Switching from Alpha to Bravo.", "This world cannot be deleted until it ends.")
	g.wantMissing("delete while busy", del, `name="confirm_name"`)
	page := g.get(worldPath(b))
	g.wantContains("world page", page, "This world cannot be deleted until it ends.")
	g.wantMissing("world page", page, `href="`+deletePath(b)+`"`)
	g.wantContains("unrelated delete", g.get(deletePath(c)), `name="confirm_name"`)
}
