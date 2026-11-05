package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

// controlHTML renders the server page's control region.
func controlHTML(t *testing.T, p ServerPage) string {
	t.Helper()
	var b strings.Builder
	if err := newTestRenderer(t).Fragment(&b, p, "server-control"); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func pageHTML(t *testing.T, p Page) string {
	t.Helper()
	_, body := render(t, newTestRenderer(t), pageCase{status: http.StatusOK, user: "todd", page: p})
	return body
}

// DESIGN 5, A1: every action in the control region has an id that stays
// the same across live updates, so the browser can keep focus on it; the
// heading can take focus when the focused action goes away.
func TestControlActionsHaveStableIDs(t *testing.T) {
	failed := serverCase(game.StateStopped, func(p *ServerPage) {
		err := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{{Text: "mod", StartAnyway: true}}}}
		p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: err}, true
	})
	missing := serverCase(game.StateStopped, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: game.ErrImageMissing}, true
	})
	restored := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: game.ActionRestore}, true
	})
	crashed := serverCase(game.StateCrashed, func(p *ServerPage) { p.Status.CrashReason = ". Factorio said: oops" })
	cases := []struct {
		page ServerPage
		ids  []string
	}{
		{serverCase(game.StateStopped, func(*ServerPage) {}), []string{"act-start"}},
		{serverCase(game.StateRunning, func(*ServerPage) {}), []string{"act-stop", "act-restart", "act-force-stop"}},
		{failed, []string{"act-start", "act-anyway"}},
		{missing, []string{"act-start", "act-pull"}},
		{restored, []string{"act-snapshots"}},
		{crashed, []string{"act-console", "act-stop", "act-force-stop"}},
	}
	for _, c := range cases {
		html := controlHTML(t, c.page)
		for _, id := range c.ids {
			if !strings.Contains(html, `id="`+id+`"`) {
				t.Errorf("%s: no id %q in\n%s", c.page.Status.State, id, html)
			}
		}
		// Every link and button has an id.
		for _, tag := range regexp.MustCompile(`<(a|button)\b[^>]*>`).FindAllString(html, -1) {
			if !strings.Contains(tag, ` id="`) {
				t.Errorf("%s: action without an id: %s", c.page.Status.State, tag)
			}
		}
	}
	body := pageHTML(t, serverCase(game.StateRunning, func(*ServerPage) {}))
	if !strings.Contains(body, `<h2 id="control-heading" tabindex="-1">`) {
		t.Error("the Start and stop heading cannot take focus")
	}
	if !strings.Contains(body, "live.js") {
		t.Error("the server page does not load its script")
	}
}

// SPEC 8.1, A3: while the game starts or stops, the player panel says
// players are not listed, not that nobody can be online.
func TestPlayersPanelWhileStartingOrStopping(t *testing.T) {
	for state, want := range map[game.State]string{
		game.StateStarting:     "Players are not listed while the server is starting.",
		game.StateStopping:     "Players are not listed while the server is stopping.",
		game.StateStopped:      "Nobody can be online while the game is not running.",
		game.StateCrashed:      "Nobody can be online while the game is not running.",
		game.StateCrashLooping: "Nobody can be online while the game is not running.",
	} {
		var b strings.Builder
		if err := newTestRenderer(t).Fragment(&b, serverCase(state, func(*ServerPage) {}), "server-players"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), want) {
			t.Errorf("%s: %s, want %q", state, b.String(), want)
		}
	}
}

// DESIGN 7.3: a crash is a warning, not an error.
func TestCrashNoticesAreWarnings(t *testing.T) {
	for _, s := range []game.State{game.StateCrashed, game.StateCrashLooping} {
		html := controlHTML(t, serverCase(s, func(*ServerPage) {}))
		if !strings.Contains(html, `<div class="message message--warning">`) || !strings.Contains(html, "<strong>Warning:</strong>") {
			t.Errorf("%s: %s", s, html)
		}
		if strings.Contains(html, "message--error") {
			t.Errorf("%s shown as an error", s)
		}
	}
}

// A9: lists with their markers removed keep the list role for VoiceOver.
func TestUnmarkedListsKeepTheirRole(t *testing.T) {
	starting := serverCase(game.StateStarting, func(p *ServerPage) {
		p.Status.Operation = &game.Operation{Action: game.AuditStart, Steps: []game.Step{{Name: game.StepLoad, State: game.StepProgress}}}
	})
	worlds := WorldsPage{Worlds: []world.Summary{{ID: "base", Name: "Base"}}}
	console := ConsolePage{World: world.Summary{ID: "base", Name: "Base"}, HasWorld: true,
		History: []consoleEntry{{Command: "/time", Last: true}}}
	for name, want := range map[string]struct {
		page Page
		list string
	}{
		"steps":   {starting, `<ol class="steps" role="list">`},
		"tabs":    {starting, `<ul class="tabs" role="list">`},
		"worlds":  {worlds, `<ul class="worlds" role="list">`},
		"console": {console, `<ol class="console-history" role="list">`},
	} {
		if body := pageHTML(t, want.page); !strings.Contains(body, want.list) {
			t.Errorf("%s: no %s", name, want.list)
		}
	}
}

// A paused container and a game that is not ready are refusals the admin
// can act on, worded plainly.
func TestPausedAndNotReadyAreWorded(t *testing.T) {
	for _, err := range []error{game.ErrPaused, game.ErrNotReady} {
		if status, msg := gameError(err); status != http.StatusConflict || msg != sentence(err) {
			t.Errorf("%v: %d %q", err, status, msg)
		}
		if got := failureText(err); got != sentence(err) {
			t.Errorf("%v shown as %q", err, got)
		}
	}
}

// notActive stands for the controller's refusal to start a world that is
// not the active one, which names the world.
type notActive string

func (e notActive) Error() string        { return string(e) + " is not the active world. Switch to it first" }
func (e notActive) Is(target error) bool { return target == game.ErrWorldNotActive }

// SPEC 7.3, 7.9: a start of a world that is not active is a refusal with
// its own words, not a 500.
func TestNotActiveIsWorded(t *testing.T) {
	for _, c := range []struct {
		err  error
		want string
	}{
		{notActive("Bravo"), "Bravo is not the active world. Switch to it first."},
		{fmt.Errorf("start: %w", notActive("Bravo")), "Bravo is not the active world."},
	} {
		status, msg := gameError(c.err)
		if status != http.StatusConflict || !strings.Contains(msg, c.want) {
			t.Errorf("%v: %d %q, want 409 containing %q", c.err, status, msg, c.want)
		}
		if got := failureText(c.err); got != msg {
			t.Errorf("%v shown as %q", c.err, got)
		}
	}
}

// SPEC 7.7: a forced stop that interrupted a switch shows the operation's
// error, which names the world it stopped, not the bare cancellation.
func TestForcedStopInterruptingASwitchNamesTheWorld(t *testing.T) {
	opError := game.ErrCancelled.Error() + ". It stopped the game of Alpha."
	st := game.Status{Operation: &game.Operation{Action: game.AuditSwitch, Done: true, Error: opError}}
	p := ServerPage{World: worldSummary("alpha"), HasWorld: true, Status: st, HaveResult: true,
		Result: actionResult{Action: game.AuditSwitch, Err: game.ErrCancelled}}
	f := p.Failure()
	if f == nil || f.Message != "A forced stop interrupted this. It stopped the game of Alpha." || f.In != "operation" {
		t.Fatalf("failure: %+v", f)
	}
	if got := resultAnnouncement(p); got != "Error: "+f.Message {
		t.Errorf("announcement: %q", got)
	}

	// Without an operation error to name, the plain cancellation shows.
	p.Status = game.Status{}
	if f := p.Failure(); f == nil || f.Message != sentence(game.ErrCancelled) {
		t.Errorf("plain: %+v", f)
	}
}

// networkRefusal stands for a game network refusal, which carries its own
// plain-English text and matches game.ErrNetwork.
type networkRefusal string

func (e networkRefusal) Error() string        { return string(e) }
func (e networkRefusal) Is(target error) bool { return target == game.ErrNetwork }

// A refused game network says what to change, not "see the log".
func TestNetworkRefusalIsShown(t *testing.T) {
	err := fmt.Errorf("start: %w", networkRefusal("FOREMAN_GAME_NETWORK must be set when Foreman runs in a container: choose a network"))
	want := "Foreman could not"
	if status, msg := gameError(err); status != http.StatusConflict || msg != sentence(err) {
		t.Errorf("gameError: %d %q", status, msg)
	}
	if got := failureText(err); got != sentence(err) || strings.HasPrefix(got, want) {
		t.Errorf("shown as %q", got)
	}
}

var reAnnounce = regexp.MustCompile(`<div hx-swap-oob="innerHTML:#announcer">([^<]*)</div>`)

// DESIGN 7.5, A2: a live page reads out a state change, and a failed or
// finished operation in its place; the first send is silent.
func TestLiveAnnouncements(t *testing.T) {
	r := newTestRenderer(t)
	send := func(l *liveStream, p ServerPage) string {
		t.Helper()
		rec := httptest.NewRecorder()
		l.w, l.rc = rec, http.NewResponseController(rec)
		if err := l.send(p); err != nil {
			t.Fatal(err)
		}
		var said []string
		for _, m := range reAnnounce.FindAllStringSubmatch(rec.Body.String(), -1) {
			said = append(said, m[1])
		}
		return strings.Join(said, " | ")
	}
	failed := func(s game.State, err error) ServerPage {
		return serverCase(s, func(p *ServerPage) { p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: err}, true })
	}
	startFailed := fmt.Errorf("%w: it could not load the save", game.ErrStartFailed)

	l := &liveStream{render: r, sent: map[string]string{}}
	if got := send(l, failed(game.StateStopped, startFailed)); got != "" {
		t.Errorf("first send said %q", got)
	}

	l = &liveStream{render: r, sent: map[string]string{}}
	send(l, serverCase(game.StateStopped, func(*ServerPage) {}))
	if got := send(l, serverCase(game.StateStarting, func(*ServerPage) {})); got != "Server is now starting" {
		t.Errorf("starting: %q", got)
	}
	want := "Error: The game did not start: it could not load the save."
	if got := send(l, failed(game.StateStopped, startFailed)); got != want {
		t.Errorf("failed start: %q, want %q", got, want)
	}
	if got := send(l, failed(game.StateStopped, startFailed)); got != "" {
		t.Errorf("the same failure again: %q", got)
	}
	if got := send(l, failed(game.StateRunning, startFailed)); got != "Server is now running" {
		t.Errorf("a state change after a failure: %q", got)
	}
	if got := send(l, failed(game.StateCrashed, startFailed)); got != "Server is now crashed" {
		t.Errorf("a crash after a failure: %q", got)
	}

	// A new result and a state change together: the result is said, and the
	// state is on the page.
	l = &liveStream{render: r, sent: map[string]string{}}
	send(l, serverCase(game.StateRunning, func(*ServerPage) {}))
	if got := send(l, failed(game.StateStopped, startFailed)); got != want {
		t.Errorf("a new failure with a state change: %q, want %q", got, want)
	}

	l = &liveStream{render: r, sent: map[string]string{}}
	send(l, serverCase(game.StateStopping, func(*ServerPage) {}))
	restored := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: game.ActionRestore}, true
	})
	if got := send(l, restored); got != "The world is restored." {
		t.Errorf("restore: %q", got)
	}

	l = &liveStream{render: r, sent: map[string]string{}}
	send(l, serverCase(game.StateRunning, func(*ServerPage) {}))
	other := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Others = []actionResult{{Action: game.AuditStop, Err: game.ErrNotRunning}}
	})
	if got := send(l, other); got != "Error: The stop asked for during another action did not finish: The game is not running." {
		t.Errorf("a refused stop: %q", got)
	}
}

// The error of an action asked for while another ran is shown on its
// own, and never in place of the first action's result.
func TestOtherActionsErrorShownApart(t *testing.T) {
	p := serverCase(game.StateStopped, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: game.AuditRestart, Err: game.ErrSaveTimeout}, true
		p.Others = []actionResult{{Action: game.AuditStop, Err: game.ErrNotRunning}}
	})
	html := controlHTML(t, p)
	for _, want := range []string{"did not finish saving in time", "The stop asked for during another action did not finish: The game is not running."} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in\n%s", want, html)
		}
	}
}

// SPEC 7.4, 8.9, 17.4: a refusal with a choice is a Warning whose box is the
// confirmation, with no advice to fix the world first; a plain refusal is an
// Error with the blocked-start note. The check step reads "Refused" in words.
func TestRefusalBoxAndStepWord(t *testing.T) {
	refused := func(anyway bool, action string, state game.State) ServerPage {
		return serverCase(state, func(p *ServerPage) {
			err := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemModMissing,
				Text: "a.zip uses m, but this world has no file for it.", Mods: []string{"m"}, StartAnyway: anyway}}}}
			p.Status.Operation = &game.Operation{Action: action, Done: true, Error: err.Error(), Steps: []game.Step{
				{Name: game.StepCheck, State: game.StepFailed}, {Name: game.StepLoad, State: game.StepSkipped}}}
			p.Result, p.HaveResult = actionResult{Action: action, Err: err}, true
		})
	}
	cases := []struct {
		name       string
		page       ServerPage
		want, nots []string
	}{
		{"start anyway", refused(true, game.AuditStart, game.StateStopped),
			[]string{`step-state--warning">Refused<`, "<strong>Warning:</strong>", `"Before start anyway"`,
				`action="/worlds/base/start"`, `name="anyway" value="1"`, ">Start anyway</button>", "To undo, restore that snapshot"},
			[]string{"Fix the world first", "Try to start again", "button--primary", `name="countdown"`, "Failed"}},
		{"restart anyway", refused(true, game.AuditRestart, game.StateRunning),
			[]string{`action="/worlds/base/restart"`, `name="countdown" value="60"`, "60-second countdown", ">Restart anyway</button>"},
			[]string{"Fix the world first"}},
		{"restart anyway of a stopped game", refused(true, game.AuditRestart, game.StateStopped),
			[]string{">Restart anyway</button>", `"Before start anyway"`},
			[]string{"countdown, save the game and stop it"}},
		{"save newer than the world", serverCase(game.StateStopped, func(p *ServerPage) {
			p.Status.Operation = &game.Operation{Action: game.AuditStart, Done: true, Error: errSaveNewer.Error(), Steps: []game.Step{
				{Name: game.StepCheck, State: game.StepFailed}}}
			p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: errSaveNewer}, true
		}), []string{`step-state--failed">Refused<`}, []string{"Failed"}},
		{"changed since shown", serverCase(game.StateStopped, func(p *ServerPage) {
			err := &game.PrecheckError{Changed: true, Check: world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemSaveUnreadable, Text: "x", StartAnyway: true}}}}
			p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: err}, true
		}), []string{"changed since the page showed it", "Read the message again", `name="seen" value="save-unreadable:;running=false"`}, nil},
		{"plain refusal", refused(false, game.AuditStart, game.StateStopped),
			[]string{`step-state--failed">Refused<`, "<strong>Error:</strong>", "Fix the world first", "Try to start again"},
			[]string{"Start anyway", `name="anyway"`, "Failed"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			html := controlHTML(t, c.page)
			for _, w := range c.want {
				if !strings.Contains(html, w) {
					t.Errorf("lacks %q:\n%s", w, html)
				}
			}
			for _, n := range c.nots {
				if strings.Contains(html, n) {
					t.Errorf("has %q:\n%s", n, html)
				}
			}
		})
	}
}

// DESIGN 7.5: the read-out of a refusal with a choice says the choice is
// there, so a screen reader user knows to look for the button.
func TestRefusalAnnouncementNamesTheChoice(t *testing.T) {
	for action, verb := range map[string]string{game.AuditStart: "start anyway", game.AuditRestart: "restart anyway", game.AuditSwitch: "switch anyway"} {
		p := serverCase(game.StateStopped, func(p *ServerPage) {
			err := &game.PrecheckError{Check: world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemSaveUnreadable, Text: "x", StartAnyway: true}}}}
			p.Result, p.HaveResult = actionResult{Action: action, Err: err, Target: "other"}, true
		})
		if got, want := resultAnnouncement(p), "You can "+verb+"; the button is below."; !strings.Contains(got, want) {
			t.Errorf("%s: %q lacks %q", action, got, want)
		}
	}
}
