package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/saveplayers"
	"github.com/toddawhittaker/foreman/internal/world"
)

// panelHTML renders the panel's body as the world page loads it.
func panelHTML(t *testing.T, loc i18n.Locale, p SavePlayersPanel) string {
	t.Helper()
	r, err := sharedRenderer()
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := r.Fragment(&b, loc, p, "save-players-body"); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func viewOf(state string, players ...game.SavePlayer) game.SavePlayersView {
	return game.SavePlayersView{State: state, Players: players}
}

var testWorld = world.Summary{ID: "base", Name: "Base"}

func mustHave(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing %q in:\n%s", w, body)
		}
	}
}

func mustNotHave(t *testing.T, body string, no ...string) {
	t.Helper()
	for _, w := range no {
		if strings.Contains(body, w) {
			t.Errorf("unexpected %q in:\n%s", w, body)
		}
	}
}

// SPEC 8.14: a read that worked lists every player with play time and last
// seen; time from before Foreman tracked the world says so; a player in the
// game now says online; "Read again" is always there.
func TestSavePlayersReadList(t *testing.T) {
	seen := time.Date(2026, 11, 20, 18, 30, 0, 0, time.UTC)
	v := viewOf(game.SavePlayersReady,
		game.SavePlayer{Name: "OrcScorpion", PlayTime: 276*time.Hour + 40*time.Minute, BeforeForeman: true},
		game.SavePlayer{Name: "Tracked", PlayTime: 95 * time.Minute, LastSeen: seen},
		game.SavePlayer{Name: "Here", PlayTime: 30 * time.Second, Online: true, LastSeen: seen})
	v.GameTime, v.GameTimeKnown = 301*time.Hour+5*time.Minute, true
	body := panelHTML(t, i18n.English, panelOf(testWorld, v, false, true, false, 0))
	mustHave(t, body, `id="save-players-table"`, "3 players in this save", "OrcScorpion", "276 h 40 min",
		"Played before Foreman tracked this world", "1 h 35 min", "0 min", "Online now", "Last seen", `datetime="2026-11-20T18:30:00Z"`,
		"Total play time of this save: 301 h 5 min", `id="save-players-read"`, "Read again", `action="/worlds/base/players/read"`, `hx-target="#save-players"`)
	mustNotHave(t, body, "of memory", "uses now", `id="save-players-inexact"`, `hx-trigger="every`)
	if strings.Count(body, "Played before Foreman tracked") != 1 {
		t.Errorf("the note belongs to the one player who played before:\n%s", body)
	}
}

// SPEC 8.14: a save nobody joined says so, and a one-player list is singular.
func TestSavePlayersNobodyAndOne(t *testing.T) {
	body := panelHTML(t, i18n.English, panelOf(testWorld, viewOf(game.SavePlayersReady), false, true, false, 0))
	mustHave(t, body, "No one has joined this save.", "Read again")
	mustNotHave(t, body, "<table")
	one := panelHTML(t, i18n.English, panelOf(testWorld, viewOf(game.SavePlayersReady, game.SavePlayer{Name: "Solo", PlayTime: time.Hour}), false, true, false, 0))
	mustHave(t, one, "1 player in this save")
}

// SPEC 8.14 (drift): a record that may have missed time says so, only when
// it is a read's list.
func TestSavePlayersInexact(t *testing.T) {
	body := panelHTML(t, i18n.English, panelOf(testWorld, viewOf(game.SavePlayersReady, game.SavePlayer{Name: "A", PlayTime: time.Hour}), true, true, false, 0))
	mustHave(t, body, `id="save-players-inexact"`, "may be a little off", "Read again")
	for _, st := range []string{game.SavePlayersReading, game.SavePlayersTrackedOnly, game.SavePlayersNone} {
		p := panelOf(testWorld, viewOf(st), true, true, false, 0)
		if p.Inexact {
			t.Errorf("state %q shows inexact", st)
		}
	}
}

// SPEC 8.14, 8.16: while a read runs there is no button, the status is read
// out, and the panel asks again by itself.
func TestSavePlayersReading(t *testing.T) {
	p := panelOf(testWorld, viewOf(game.SavePlayersReading), false, true, false, 0)
	body := panelHTML(t, i18n.English, p)
	mustHave(t, body, `role="status"`, "Reading the newest save", `hx-get="/worlds/base/players"`, `hx-trigger="every 3s"`, `hx-target="#save-players"`)
	mustNotHave(t, body, "<button", "<table", "autofocus")
	p.Focus = true
	mustHave(t, panelHTML(t, i18n.English, p), "autofocus")
}

// SPEC 8.14, 8.16: every failure id has its own words, the tracked-since
// date shows, no seeded totals are invented, and the admin can read again.
func TestSavePlayersTrackedOnly(t *testing.T) {
	since := time.Date(2026, 11, 21, 10, 5, 0, 0, time.UTC)
	words := map[string]string{
		saveplayers.ResultImageMissing: "game image is not downloaded",
		saveplayers.ResultOutOfMemory:  "ran out of memory",
		saveplayers.ResultTimeout:      "took too long",
		saveplayers.ResultModsDiffer:   "are not this world",
		saveplayers.ResultSaveNewer:    "a newer game version made it",
		saveplayers.ResultBadOutput:    "no result Foreman can use",
		"something-new":                "The save could not be read.",
	}
	for id, want := range words {
		v := viewOf(game.SavePlayersTrackedOnly, game.SavePlayer{Name: "Seen", PlayTime: 10 * time.Minute, LastSeen: since})
		v.Failure, v.TrackedSince = id, since
		body := panelHTML(t, i18n.English, panelOf(testWorld, v, false, true, false, 0))
		mustHave(t, body, want, `id="save-players-tracked"`, "Foreman has tracked this world since", `datetime="2026-11-21T10:05:00Z"`,
			"Seen", "Read again")
		mustNotHave(t, body, "Played before Foreman")
	}
	// Nobody joined since tracking began.
	v := viewOf(game.SavePlayersTrackedOnly)
	v.Failure, v.TrackedSince = saveplayers.ResultModsDiffer, since
	mustHave(t, panelHTML(t, i18n.English, panelOf(testWorld, v, false, true, false, 0)), "No one has joined this save.")
}

// SPEC 8.14, 8.16: a save nothing has read is offered a first read; a world
// with no save says there is nothing to read and offers no button; a game
// version that cannot be read says so and offers no button.
func TestSavePlayersNeverNoSaveUnsupported(t *testing.T) {
	never := panelHTML(t, i18n.English, panelOf(testWorld, viewOf(game.SavePlayersNone), false, true, false, 0))
	mustHave(t, never, "has not read the players of the newest save", "Read players from the newest save")
	mustNotHave(t, never, "Read again", "<table")

	none := panelHTML(t, i18n.English, panelOf(testWorld, viewOf(game.SavePlayersNone), false, false, false, 0))
	mustHave(t, none, "no save yet")
	mustNotHave(t, none, "<button")

	v := viewOf(game.SavePlayersNotSupported, game.SavePlayer{Name: "Seen", PlayTime: time.Hour})
	v.TrackedSince = time.Date(2026, 11, 21, 10, 5, 0, 0, time.UTC)
	un := panelHTML(t, i18n.English, panelOf(testWorld, v, false, true, false, 0))
	mustHave(t, un, "cannot be read from a save of this game version", "Foreman has tracked this world since", "Seen")
	mustNotHave(t, un, "<button")

	mustHave(t, panelHTML(t, i18n.English, panelOf(testWorld, viewOf(""), false, true, false, 0)), "cannot be shown right now")
}

// SPEC 8.14: while a game runs, the button says a read needs about as much
// memory as the game uses now, and shows that figure; stopped, it says
// nothing of memory.
func TestSavePlayersMemoryNote(t *testing.T) {
	v := viewOf(game.SavePlayersNone)
	run := panelHTML(t, i18n.English, panelOf(testWorld, v, false, true, true, 6_900_000_000))
	mustHave(t, run, `aria-describedby="save-players-memory"`, `id="save-players-memory"`,
		"as much memory as the game uses now: 6.9 GB", "Read players from the newest save")
	noFigure := panelHTML(t, i18n.English, panelOf(testWorld, v, false, true, true, 0))
	mustHave(t, noFigure, "as much memory as the game uses now.")
	mustNotHave(t, noFigure, " GB", " MB")
	stopped := panelHTML(t, i18n.English, panelOf(testWorld, v, false, true, false, 6_900_000_000))
	mustNotHave(t, stopped, "memory", "aria-describedby")
	// Read again carries the note too.
	again := panelHTML(t, i18n.English, panelOf(testWorld, viewOf(game.SavePlayersReady), false, true, true, 2_000_000_000))
	mustHave(t, again, "Read again", "2.0 GB")
}

// A refused click shows why, as an alert, above the list.
func TestSavePlayersRefused(t *testing.T) {
	p := panelOf(testWorld, viewOf(game.SavePlayersNone), false, true, false, 0)
	msg, ok := readError(game.ErrSaveCopyOutOfMemory)
	if !ok {
		t.Fatal("ErrSaveCopyOutOfMemory is not worded")
	}
	p.Refused = msg
	mustHave(t, panelHTML(t, i18n.English, p), `role="alert"`, "The players were not read.", "ran out of memory")
	if _, ok := readError(game.ErrNotRunning); ok {
		t.Error("an error that is not the read's own was worded as one")
	}
}

// SPEC 8.14: a player's name is the player's own text, never markup.
func TestSavePlayersNameIsEscaped(t *testing.T) {
	body := panelHTML(t, i18n.English, panelOf(testWorld, viewOf(game.SavePlayersReady,
		game.SavePlayer{Name: `<script>alert(1)</script>`, PlayTime: time.Hour}), false, true, false, 0))
	mustNotHave(t, body, "<script>")
	mustHave(t, body, "&lt;script&gt;")
}

// SPEC 22: every language words every state without a missing key, and the
// plural of the count follows the language.
func TestSavePlayersInEveryLanguage(t *testing.T) {
	seen := time.Date(2026, 11, 20, 18, 30, 0, 0, time.UTC)
	views := []game.SavePlayersView{
		viewOf(game.SavePlayersReady, game.SavePlayer{Name: "A", PlayTime: 3 * time.Hour, BeforeForeman: true},
			game.SavePlayer{Name: "B", PlayTime: time.Minute, LastSeen: seen}, game.SavePlayer{Name: "C", PlayTime: time.Hour, Online: true}),
		viewOf(game.SavePlayersReading), viewOf(game.SavePlayersNone), viewOf(game.SavePlayersNotSupported),
		{State: game.SavePlayersTrackedOnly, Failure: saveplayers.ResultTimeout, TrackedSince: seen},
	}
	for _, loc := range i18n.Languages {
		for _, v := range views {
			p := panelOf(testWorld, v, true, true, true, 3_000_000_000)
			p.Refused = failureWords(saveplayers.ResultBadOutput)
			body := panelHTML(t, loc.Tag, p)
			if strings.Contains(body, "saveplayers.") || strings.Contains(body, "common.") {
				t.Errorf("%s %s: a key shows as text:\n%s", loc.Tag, v.State, body)
			}
		}
	}
}

// SPEC 8.14: the world page holds the panel's place and its anchor, for the
// Server page's link; the body loads from the players path.
func TestWorldPageHoldsThePlayersPanel(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id := hn.seedSnapshotWorld()
	page := hn.get(worldPath(id), withCookie(c)).Body.String()
	mustHave(t, page, `id="save-players"`, "Players in this save", `hx-get="/worlds/`+id+`/players"`, `hx-trigger="load"`, `hx-swap="innerHTML"`)
}

// SPEC 8.14: the panel and its button are the signed-in admin's; without a
// game on this Foreman the panel says so and the button is not there.
func TestSavePlayersHandlers(t *testing.T) {
	hn := newHarness(t, nil)
	id := hn.seedSnapshotWorld()
	hx := withHeader("HX-Request", "true")

	// An htmx request that is signed out is sent to sign in, and sees nothing.
	for _, rec := range []*httptest.ResponseRecorder{hn.get(playersPath(id), hx), hn.post(playersReadPath(id), url.Values{}, hx)} {
		if rec.Header().Get("HX-Redirect") == "" || strings.Contains(rec.Body.String(), "save-players") {
			t.Errorf("signed out: %d %v\n%s", rec.Code, rec.Header(), rec.Body)
		}
	}

	c := hn.signIn()
	rec := hn.get(playersPath(id), withCookie(c), hx)
	if rec.Code != http.StatusOK {
		t.Fatalf("panel: %d\n%s", rec.Code, rec.Body)
	}
	mustHave(t, rec.Body.String(), `id="save-players-heading"`, "cannot be shown right now")
	mustNotHave(t, rec.Body.String(), "<button", "<html")

	if rec := hn.get(playersPath(id), withCookie(c)); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != worldPath(id)+"#save-players" {
		t.Errorf("without script: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := hn.get(playersPath("nope"), withCookie(c), hx); rec.Code != http.StatusNotFound {
		t.Errorf("unknown world: %d", rec.Code)
	}
	if rec := hn.post(playersReadPath("nope"), url.Values{}, withCookie(c), hx); rec.Code != http.StatusNotFound {
		t.Errorf("read of an unknown world: %d", rec.Code)
	}
	// No game on this Foreman: nothing to read with.
	if rec := hn.post(playersReadPath(id), url.Values{}, withCookie(c), hx); rec.Code != http.StatusNotFound {
		t.Errorf("read without a game: %d", rec.Code)
	}
}

// playTime says hours and minutes, and never a negative time.
func TestPlayTime(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0 min", 59 * time.Second: "0 min", 90 * time.Minute: "1 h 30 min",
		276*time.Hour + 40*time.Minute: "276 h 40 min", -time.Hour: "0 min",
	} {
		if got := i18n.English.Format(playTime(d)); got != want {
			t.Errorf("playTime(%v) = %q, want %q", d, got, want)
		}
	}
}
