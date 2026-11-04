package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Server control routes (SPEC 7). Each action posts to the world it acts on.
const (
	PathServer      = "/"
	PathGameStart   = "/worlds/{id}/start"
	PathGameAnyway  = "/worlds/{id}/start-anyway"
	PathGameStop    = "/worlds/{id}/stop"
	PathGameRestart = "/worlds/{id}/restart"
	PathGameForce   = "/worlds/{id}/force-stop"
	PathGamePull    = "/worlds/{id}/pull-image"
	PathGameEvents  = "/worlds/{id}/events"
)

func gamePath(id, action string) string { return worldPath(id) + "/" + action }

// The stop countdown field (SPEC 7.6): whole seconds from 0 to the
// controller's limit, 60 when left empty.
var (
	maxCountdownSeconds     = int(game.MaxStopCountdown / time.Second)
	defaultCountdownSeconds = int(game.StopCountdown / time.Second)
)

// Live updates (SPEC 16.3). heartbeatEvery keeps a quiet stream inside the
// idle limit and is when the stream checks that its user is still signed
// in. playersLimit bounds the RCON call behind the player list.
var (
	heartbeatEvery = 25 * time.Second
	playersLimit   = 2 * time.Second
)

// gameRuns holds the server control actions that run past their request,
// their last results, and the live pages watching them.
type gameRuns struct {
	ctx       context.Context // ends at Close; actions run in it, and live pages end with it
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.Mutex
	closed    bool
	running   map[string]int            // actions in flight, by world id
	results   map[string]actionResult   // by world id
	asides    map[string][]actionResult // actions asked for while another ran (R38)
	changed   chan struct{}             // closed and replaced when a result is stored
	closeOnce sync.Once
}

// actionResult is how the last action on a world ended.
type actionResult struct {
	Action string
	Err    error
	// Restored and Undo name the snapshot a restore put back and the
	// "Before restore" snapshot it took first.
	Restored string
	Undo     string
}

func newGameRuns() *gameRuns {
	g := &gameRuns{running: map[string]int{}, results: map[string]actionResult{},
		asides: map[string][]actionResult{}, changed: make(chan struct{})}
	g.ctx, g.cancel = context.WithCancel(context.Background())
	return g
}

// close ends the live pages and interrupts the actions still running, and
// waits for both. The game itself is never stopped (SPEC 3.8).
func (g *gameRuns) close() {
	g.closeOnce.Do(func() {
		g.mu.Lock()
		g.closed = true
		g.mu.Unlock()
		g.cancel()
		g.wg.Wait()
	})
}

// track counts f as in flight, or reports false once close has begun.
func (g *gameRuns) track() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.trackLocked()
}

func (g *gameRuns) trackLocked() bool {
	if g.closed {
		return false
	}
	g.wg.Add(1)
	return true
}

// run starts an action in the background. When nothing else runs on the
// world it forgets the world's last results, so the page shows only what
// this action does. An action asked for while another runs keeps its
// result apart, so it never hides the first one's (R38).
func (g *gameRuns) run(id string, do func(ctx context.Context) actionResult) bool {
	g.mu.Lock()
	if !g.trackLocked() {
		g.mu.Unlock()
		return false
	}
	other := g.running[id] > 0
	g.running[id]++
	if !other {
		delete(g.results, id)
		delete(g.asides, id)
		g.changedLocked()
	}
	g.mu.Unlock()
	go func() {
		defer g.wg.Done()
		res := do(g.ctx)
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.running[id]--; g.running[id] == 0 {
			delete(g.running, id)
		}
		if other {
			g.asides[id] = append(g.asides[id], res)
		} else {
			g.results[id] = res
		}
		g.changedLocked()
	}()
	return true
}

func (g *gameRuns) changedLocked() {
	close(g.changed)
	g.changed = make(chan struct{})
}

func (g *gameRuns) result(id string) (actionResult, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	res, ok := g.results[id]
	return res, ok
}

// others returns the results of the actions asked for while another ran.
func (g *gameRuns) others(id string) []actionResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]actionResult(nil), g.asides[id]...)
}

func (g *gameRuns) watch() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.changed
}

func actor(u store.User) game.Actor { return game.Actor{UserID: u.ID, Username: u.Username} }

// home is where setup and sign-in land: the Server page, or the world list
// in a mode without server control.
func (s *server) home() string {
	if s.game == nil {
		return PathWorlds
	}
	return PathServer
}

func (s *server) gameRoutes() {
	if s.game == nil {
		// No server control in this mode: the home page is the world list.
		s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, PathWorlds, http.StatusSeeOther)
		})
		return
	}
	s.mux.HandleFunc("GET /{$}", s.requireUser(s.handleServer))
	s.mux.HandleFunc("POST "+PathGameStart, s.requireUser(s.handleStart))
	s.mux.HandleFunc("GET "+PathGameAnyway, s.requireUser(s.handleStartAnywayForm))
	s.mux.HandleFunc("GET "+PathGameStop, s.requireUser(s.handleStopForm))
	s.mux.HandleFunc("POST "+PathGameStop, s.requireUser(s.handleStop))
	s.mux.HandleFunc("GET "+PathGameRestart, s.requireUser(s.handleStopForm))
	s.mux.HandleFunc("POST "+PathGameRestart, s.requireUser(s.handleStop))
	s.mux.HandleFunc("GET "+PathGameForce, s.requireUser(s.handleForceStopForm))
	s.mux.HandleFunc("POST "+PathGameForce, s.requireUser(s.handleForceStop))
	s.mux.HandleFunc("POST "+PathGamePull, s.requireUser(s.handlePull))
	s.handleLong("GET "+PathGameEvents, s.requireUser(s.handleEvents))
}

// gameError turns an error from server control into a status and words for
// the admin. Anything it does not know goes to worldError; a 500 means the
// caller logs the error and shows no detail.
func gameError(err error) (int, string) {
	switch {
	case errors.Is(err, game.ErrAlreadyRunning), errors.Is(err, game.ErrNotRunning),
		errors.Is(err, game.ErrNotReady), errors.Is(err, game.ErrImageMissing),
		errors.Is(err, game.ErrRCONUnreachable), errors.Is(err, game.ErrSaveTimeout),
		errors.Is(err, game.ErrStartFailed), errors.Is(err, game.ErrCancelled),
		errors.Is(err, game.ErrDownloading), errors.Is(err, game.ErrForeign), errors.Is(err, game.ErrPaused),
		errors.Is(err, game.ErrSaveNewerThanImage), errors.Is(err, world.ErrSaveInFuture):
		return http.StatusConflict, sentence(err)
	case errors.Is(err, game.ErrCountdown):
		return http.StatusBadRequest, sentence(err)
	}
	// The Docker client wraps a failed connection in *url.Error; its detail
	// stays in the log.
	if _, ok := errors.AsType[*url.Error](err); ok {
		return http.StatusServiceUnavailable, "Foreman cannot reach Docker, so nothing was done. Check that Docker is running and that Foreman can reach it, then try again."
	}
	return worldError(err)
}

// gameRefusal is refusal for server control errors.
func (s *server) gameRefusal(w http.ResponseWriter, r *http.Request, u store.User, what string, err error) (status int, msg string, ok bool) {
	status, msg = gameError(err)
	switch status {
	case http.StatusNotFound:
		s.notFound(w, r, u)
		return 0, "", false
	case http.StatusInternalServerError:
		s.fail(w, r, what, err)
		return 0, "", false
	}
	return status, msg, true
}

// activeWorld returns the active world; found is false when none is.
func (s *server) activeWorld(ctx context.Context) (world.Summary, bool, error) {
	list, err := s.worlds.List(ctx)
	if err != nil {
		return world.Summary{}, false, err
	}
	for _, w := range list {
		if w.Active {
			return w, true, nil
		}
	}
	return world.Summary{}, false, nil
}

// serverPage builds the server page for a world from its status, asking
// the game who is online when it is running.
func (s *server) serverPage(ctx context.Context, sum world.Summary, st game.Status) ServerPage {
	p := ServerPage{World: sum, HasWorld: true, Status: st}
	p.Result, p.HaveResult = s.runs.result(sum.ID)
	p.Others = s.runs.others(sum.ID)
	if st.State == game.StateRunning {
		ctx, cancel := context.WithTimeout(ctx, playersLimit)
		defer cancel()
		players, err := s.game.Players(ctx, sum.ID)
		p.Players, p.PlayersKnown, p.PlayersError = players, err == nil, err != nil
	}
	return p
}

func (s *server) handleServer(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, found, err := s.activeWorld(r.Context())
	if err != nil {
		s.fail(w, r, "find the active world", err)
		return
	}
	if !found {
		s.page(w, r, http.StatusOK, u.Username, ServerPage{})
		return
	}
	s.page(w, r, http.StatusOK, u.Username, s.serverPage(r.Context(), sum, s.game.Status(sum.ID)))
}

// startable returns the world in the path when it is the active one, the
// only world a start may run (SPEC 7.3). Otherwise it answers the request.
func (s *server) startable(w http.ResponseWriter, r *http.Request, u store.User) (world.Summary, bool) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return world.Summary{}, false
	}
	if !sum.Active {
		s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict,
			Detail: "Only the active world can be started. This world is not the active one."})
		return world.Summary{}, false
	}
	return sum, true
}

// begin runs an action in the background and sends the admin to the
// server page, which follows it live.
func (s *server) begin(w http.ResponseWriter, r *http.Request, id string, do func(ctx context.Context) actionResult) {
	if !s.runs.run(id, do) {
		s.page(w, r, http.StatusServiceUnavailable, "", ErrorPage{Status: http.StatusServiceUnavailable,
			Detail: "Foreman is shutting down. Try again once it is back."})
		return
	}
	http.Redirect(w, r, PathServer, http.StatusSeeOther)
}

// finish logs an action's error that the admin cannot fix, then returns
// the result to keep.
func (s *server) finish(id, action string, err error) actionResult {
	if err != nil {
		if status, _ := gameError(err); status == http.StatusInternalServerError {
			s.log.Error("server control", "world", id, "action", action, "error", err)
		}
	}
	return actionResult{Action: action, Err: err}
}

// handleStart starts the active world (SPEC 7.3). "anyway" starts despite
// the pre-start check's problems; the confirmation page sends it (7.4).
func (s *server) handleStart(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.startable(w, r, u)
	if !ok {
		return
	}
	opts := game.StartOptions{Anyway: r.PostFormValue("anyway") == "1"}
	by := actor(u)
	s.begin(w, r, sum.ID, func(ctx context.Context) actionResult {
		return s.finish(sum.ID, game.AuditStart, s.game.Start(ctx, sum.ID, by, opts))
	})
}

// CountdownForm is the stop countdown choice (SPEC 7.6).
type CountdownForm struct {
	Countdown      string
	NoCountdown    bool
	CountdownError string
}

// readCountdown reads the countdown fields. A problem is worded for the
// field; an empty field means the default countdown (R40).
func readCountdown(r *http.Request) (CountdownForm, game.StopOptions) {
	f := CountdownForm{Countdown: strings.TrimSpace(r.PostFormValue("countdown")), NoCountdown: r.PostFormValue("no_countdown") != ""}
	if f.NoCountdown {
		return f, game.StopOptions{}
	}
	if f.Countdown == "" {
		return f, game.StopOptions{Countdown: game.StopCountdown}
	}
	n, err := strconv.Atoi(f.Countdown)
	if err != nil || n < 0 || n > maxCountdownSeconds {
		f.CountdownError = fmt.Sprintf("Enter a whole number of seconds from 0 to %d", maxCountdownSeconds)
		return f, game.StopOptions{}
	}
	return f, game.StopOptions{Countdown: time.Duration(n) * time.Second}
}

func defaultCountdown() CountdownForm {
	return CountdownForm{Countdown: strconv.Itoa(defaultCountdownSeconds)}
}

// StopPage asks how to warn players before a stop or restart (SPEC 7.5,
// 7.6, 7.8). With Anyway it also confirms a restart despite the pre-start
// check's problems (7.4).
type StopPage struct {
	World   world.Summary
	Restart bool
	CountdownForm
	Anyway bool
	// CheckProblems are the pre-start check's findings, for Anyway.
	CheckProblems []string
	// Refusal replaces the form when the restart cannot go ahead anyway.
	Refusal string
}

func (StopPage) templateName() string { return "stop" }
func (StopPage) navItem() string      { return PathServer }
func (p StopPage) hasErrors() bool    { return p.CountdownError != "" }

// Problems lists the countdown field's error for the summary.
func (p StopPage) Problems() []Problem {
	return problems(Problem{"countdown", p.CountdownError})
}

// Path is where the form posts.
func (p StopPage) Path() string {
	if p.Restart {
		return gamePath(p.World.ID, "restart")
	}
	return gamePath(p.World.ID, "stop")
}

// Verb is the action's name, for headings and the button.
func (p StopPage) Verb() string { return choose(p.Restart, "Restart", "Stop") }

func (s *server) handleStopForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	restart := strings.HasSuffix(r.URL.Path, "/restart")
	s.page(w, r, http.StatusOK, u.Username, StopPage{World: sum, Restart: restart, CountdownForm: defaultCountdown()})
}

// handleStop stops or restarts a world's game gracefully (SPEC 7.6, 7.8).
func (s *server) handleStop(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	restart := strings.HasSuffix(r.URL.Path, "/restart")
	var sum world.Summary
	var ok bool
	if restart {
		sum, ok = s.startable(w, r, u)
	} else {
		sum, ok = s.findWorld(w, r, u)
	}
	if !ok {
		return
	}
	form, stop := readCountdown(r)
	anyway := restart && r.PostFormValue("anyway") == "1"
	if form.CountdownError != "" {
		p := StopPage{World: sum, Restart: restart, CountdownForm: form, Anyway: anyway}
		if anyway {
			p = s.anywayProblems(p)
		}
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	by := actor(u)
	s.begin(w, r, sum.ID, func(ctx context.Context) actionResult {
		if restart {
			err := s.game.Restart(ctx, sum.ID, by, stop, game.StartOptions{Anyway: anyway})
			return s.finish(sum.ID, game.AuditRestart, err)
		}
		return s.finish(sum.ID, game.AuditStop, s.game.Stop(ctx, sum.ID, by, stop))
	})
}

// StartAnywayPage confirms a start despite the pre-start check (SPEC 7.4,
// DESIGN 7.4): it names what the game will delete and the snapshot taken
// first.
type StartAnywayPage struct {
	World         world.Summary
	CheckProblems []string
	// Refusal replaces the form when the start cannot go ahead anyway.
	Refusal string
}

func (StartAnywayPage) templateName() string { return "startanyway" }
func (StartAnywayPage) navItem() string      { return PathServer }

// Path is where the confirmation posts.
func (p StartAnywayPage) Path() string { return gamePath(p.World.ID, "start") }

// anywayCheck runs the pre-start check again for a confirmation page. It
// changes nothing. refusal is set when "Start anyway" is not allowed.
func (s *server) anywayCheck(id string) (problems []string, refusal string, err error) {
	check, err := s.worlds.PreStartCheck(id)
	if err != nil {
		return nil, "", err
	}
	for _, pr := range check.Problems {
		problems = append(problems, pr.Text)
	}
	switch {
	case check.OK():
		refusal = "The pre-start check now finds no problems, so there is nothing to confirm. Start the server from the Server page."
	case !check.CanStartAnyway():
		refusal = "At least one of these problems cannot be started past. Fix the world first."
	}
	return problems, refusal, nil
}

func (s *server) anywayProblems(p StopPage) StopPage {
	problems, refusal, err := s.anywayCheck(p.World.ID)
	if err != nil {
		s.log.Error("pre-start check", "world", p.World.ID, "error", err)
		refusal = "Foreman could not run the pre-start check. The details are in Foreman's log."
	}
	p.CheckProblems, p.Refusal = problems, refusal
	return p
}

func (s *server) handleStartAnywayForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.startable(w, r, u)
	if !ok {
		return
	}
	if r.URL.Query().Get("then") == "restart" {
		p := s.anywayProblems(StopPage{World: sum, Restart: true, Anyway: true, CountdownForm: defaultCountdown()})
		s.page(w, r, http.StatusOK, u.Username, p)
		return
	}
	problems, refusal, err := s.anywayCheck(sum.ID)
	if err != nil {
		s.notFoundOrFail(w, r, u, "pre-start check", err)
		return
	}
	s.page(w, r, http.StatusOK, u.Username, StartAnywayPage{World: sum, CheckProblems: problems, Refusal: refusal})
}

// ForceStopPage confirms a forced stop (SPEC 7.7).
type ForceStopPage struct {
	World world.Summary
}

func (ForceStopPage) templateName() string { return "forcestop" }
func (ForceStopPage) navItem() string      { return PathServer }

// Path is where the confirmation posts.
func (p ForceStopPage) Path() string { return gamePath(p.World.ID, "force-stop") }

func (s *server) handleForceStopForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	s.page(w, r, http.StatusOK, u.Username, ForceStopPage{World: sum})
}

// handleForceStop stops the game without saving (SPEC 7.7).
func (s *server) handleForceStop(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	by := actor(u)
	s.begin(w, r, sum.ID, func(ctx context.Context) actionResult {
		return s.finish(sum.ID, game.AuditForceStop, s.game.ForceStop(ctx, sum.ID, by))
	})
}

// handlePull downloads the world's pinned game image (SPEC 3.7).
func (s *server) handlePull(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	by := actor(u)
	s.begin(w, r, sum.ID, func(ctx context.Context) actionResult {
		return s.finish(sum.ID, game.AuditImageDownload, s.game.PullImage(ctx, sum.ID, by))
	})
}
