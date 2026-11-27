package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
)

// playtimeHTML renders the Server page's play time region and the region
// with the link to the save's players, one after the other.
func playtimeHTML(t *testing.T, p ServerPage) string {
	t.Helper()
	var b strings.Builder
	for _, region := range []string{"server-playtime", "server-saveplayers"} {
		if err := newTestRenderer(t).Fragment(&b, i18n.English, p, region); err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

func withSavePlayers(v game.SavePlayersView) func(*ServerPage) {
	return func(p *ServerPage) { p.SavePlayers, p.HaveSavePlayers = v, true }
}

// SPEC 8.15: the total is shown as hours and minutes, and the count links to
// the list on the world's Overview page.
func TestPlayTimeShowsHoursMinutesAndALinkToTheList(t *testing.T) {
	p := serverCase(game.StateStopped, withSavePlayers(game.SavePlayersView{
		State: game.SavePlayersReady, GameTime: 276*time.Hour + 40*time.Minute, GameTimeKnown: true,
		Players: []game.SavePlayer{{Name: "alice"}, {Name: "bob"}, {Name: "carol"}},
	}))
	html := playtimeHTML(t, p)
	for _, want := range []string{"276 h 40 min", `href="/worlds/base#save-players"`, ">3 players in this save</a>"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q:\n%s", want, html)
		}
	}
}

// Hours are not folded into days: the figure is what the game's /time says.
func TestPlayTimeOverADayStaysInHours(t *testing.T) {
	p := serverCase(game.StateStopped, withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady, GameTime: 50*time.Hour + 5*time.Minute + 59*time.Second, GameTimeKnown: true}))
	if html := playtimeHTML(t, p); !strings.Contains(html, "50 h 5 min") {
		t.Errorf("not in hours and minutes:\n%s", html)
	}
}

func TestOnePlayerIsSingular(t *testing.T) {
	p := serverCase(game.StateStopped, withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady, Players: []game.SavePlayer{{Name: "alice"}}}))
	if html := playtimeHTML(t, p); !strings.Contains(html, ">1 player in this save</a>") {
		t.Errorf("not singular:\n%s", html)
	}
}

// SPEC 8.15: with no record there is no figure and no count, whatever the
// reason: nothing read, no save, a game that cannot be read, a failed read.
func TestNoRecordShowsNoInventedFigures(t *testing.T) {
	cases := map[string]func(*ServerPage){
		"view not available": func(*ServerPage) {},
		"none":               withSavePlayers(game.SavePlayersView{State: game.SavePlayersNone}),
		"not supported":      withSavePlayers(game.SavePlayersView{State: game.SavePlayersNotSupported}),
		"failed, no ticks":   withSavePlayers(game.SavePlayersView{State: game.SavePlayersTrackedOnly, Failure: "mods-differ"}),
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			html := playtimeHTML(t, serverCase(game.StateStopped, edit))
			for _, bad := range []string{"<dl", "<a ", " h ", "players in this save"} {
				if strings.Contains(html, bad) {
					t.Errorf("shows %q with no record:\n%s", bad, html)
				}
			}
			for _, wrapper := range []string{`class="play-time"`, `class="save-players-count"`} {
				if !strings.Contains(html, wrapper) {
					t.Errorf("the %s wrapper must always be there, so a change back to nothing still sends an event:\n%s", wrapper, html)
				}
			}
		})
	}
}

// While a read runs there is no figure yet; the page says the save is being read.
func TestPlayTimeSaysNotReadYetWhileReading(t *testing.T) {
	html := playtimeHTML(t, serverCase(game.StateStopped, withSavePlayers(game.SavePlayersView{State: game.SavePlayersReading})))
	if !strings.Contains(html, "Being read") || strings.Contains(html, "<a ") {
		t.Errorf("a read in progress shows %s", html)
	}
}

// A zero time with the figure known is a real figure (a new world), not a gap.
func TestZeroPlayTimeIsStillAFigure(t *testing.T) {
	html := playtimeHTML(t, serverCase(game.StateStopped, withSavePlayers(game.SavePlayersView{State: game.SavePlayersReady, GameTimeKnown: true})))
	if !strings.Contains(html, "0 h 0 min") {
		t.Errorf("a known zero is not shown:\n%s", html)
	}
}

// Tracked-only still counts the players it has seen, with their play time.
func TestTrackedOnlyShowsWhatIsKnown(t *testing.T) {
	p := serverCase(game.StateRunning, withSavePlayers(game.SavePlayersView{
		State: game.SavePlayersTrackedOnly, Failure: "mods-differ", GameTime: 90 * time.Minute, GameTimeKnown: true,
		Players: []game.SavePlayer{{Name: "alice"}, {Name: "bob"}},
	}))
	html := playtimeHTML(t, p)
	if !strings.Contains(html, "1 h 30 min") || !strings.Contains(html, "2 players in this save") {
		t.Errorf("tracked figures missing:\n%s", html)
	}
}

// The link takes focus, so the region is its own event and is sent only when
// it changes, never with every status update.
func TestPlayTimeRegionIsSentOnlyWhenItChanges(t *testing.T) {
	r := newTestRenderer(t)
	l := &liveStream{render: r, sent: map[string]string{}}
	rec := httptest.NewRecorder()
	l.w, l.rc = rec, http.NewResponseController(rec)
	view := game.SavePlayersView{State: game.SavePlayersReady, GameTime: time.Hour, GameTimeKnown: true, Players: []game.SavePlayer{{Name: "alice"}}}
	if err := l.send(serverCase(game.StateRunning, withSavePlayers(view))); err != nil {
		t.Fatal(err)
	}
	if body := rec.Body.String(); !strings.Contains(body, "event: playtime\n") || !strings.Contains(body, "event: saveplayers\n") {
		t.Fatalf("the first send lacks a region:\n%s", rec.Body.String())
	}
	rec.Body.Reset()
	// Another region changes (CPU), the play time does not: no playtime event.
	if err := l.send(serverCase(game.StateRunning, func(p *ServerPage) {
		withSavePlayers(view)(p)
		p.Status.HaveUsage, p.Status.CPUPercent = true, 12
	})); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.Body.String(), "event: playtime\n") || strings.Contains(rec.Body.String(), "event: saveplayers\n") {
		t.Errorf("an unchanged play time or count was sent again:\n%s", rec.Body.String())
	}
	// A new figure is sent, and the link, which takes focus, is not: it is
	// redrawn only when the count changes.
	rec.Body.Reset()
	view.GameTime = 2 * time.Hour
	if err := l.send(serverCase(game.StateRunning, withSavePlayers(view))); err != nil {
		t.Fatal(err)
	}
	if body := rec.Body.String(); !strings.Contains(body, "event: playtime\n") || !strings.Contains(body, "2 h 0 min") || strings.Contains(body, "event: saveplayers\n") {
		t.Errorf("a changed play time must be sent alone:\n%s", body)
	}
	rec.Body.Reset()
	view.Players = append(view.Players, game.SavePlayer{Name: "bob"})
	if err := l.send(serverCase(game.StateRunning, withSavePlayers(view))); err != nil {
		t.Fatal(err)
	}
	if body := rec.Body.String(); !strings.Contains(body, "event: saveplayers\n") || !strings.Contains(body, "2 players in this save") || strings.Contains(body, "event: playtime\n") {
		t.Errorf("a changed count must be sent alone:\n%s", body)
	}
}

// A change to the players record redraws a live page; lines and the rest do not.
func TestOnlyStatusAndPlayersRecordWakeTheServerPage(t *testing.T) {
	for kind, want := range map[game.UpdateKind]bool{
		game.UpdateStatus: true, game.UpdateSavePlayers: true, game.UpdateLine: false, game.UpdateFactoryMap: false,
	} {
		if got := wakesServerPage(kind); got != want {
			t.Errorf("wakesServerPage(%v) = %v, want %v", kind, got, want)
		}
	}
}
