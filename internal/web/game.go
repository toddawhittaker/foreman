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
	playersLimit   = 5 * time.Second
)

// gameRuns holds the server control actions that run past their request,
// their last results, and the live pages watching them.
type gameRuns struct {
	ctx       context.Context // ends at Close; actions run in it
	cancel    context.CancelFunc
	live      context.Context // ends at Close, first; live pages end with it
	stopLive  context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.Mutex
	closed    bool
	results   map[string]actionResult // by world id
	changed   chan struct{}           // closed and replaced when a result is stored
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
	g := &gameRuns{results: map[string]actionResult{}, changed: make(chan struct{})}
	g.ctx, g.cancel = context.WithCancel(context.Background())
	g.live, g.stopLive = context.WithCancel(context.Background())
	return g
}

// close ends the live pages first, then interrupts the actions still
// running, and waits for both. The game itself is never stopped (SPEC 3.8).
func (g *gameRuns) close() {
	g.closeOnce.Do(func() {
		g.mu.Lock()
		g.closed = true
		g.mu.Unlock()
		g.stopLive()
		g.cancel()
		g.wg.Wait()
	})
}

// track counts f as in flight, or reports false once close has begun.
func (g *gameRuns) track() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.wg.Add(1)
	return true
}

// run starts an action in the background and forgets the world's last
// result, so the page shows only what this action does.
func (g *gameRuns) run(id string, do func(ctx context.Context) actionResult) bool {
	if !g.track() {
		return false
	}
	g.store(id, nil)
	go func() {
		defer g.wg.Done()
		res := do(g.ctx)
		g.store(id, &res)
	}()
	return true
}

func (g *gameRuns) store(id string, res *actionResult) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if res == nil {
		delete(g.results, id)
	} else {
		g.results[id] = *res
	}
	close(g.changed)
	g.changed = make(chan struct{})
}

func (g *gameRuns) result(id string) (actionResult, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	res, ok := g.results[id]
	return res, ok
}

func (g *gameRuns) watch() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.changed
}

func actor(u store.User) game.Actor { return game.Actor{UserID: u.ID, Username: u.Username} }

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
		errors.Is(err, game.ErrDownloading), errors.Is(err, game.ErrForeign),
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

// ServerPage is the server's status and controls for the active world
// (SPEC 7.1, 7.2, 8.1, DESIGN 7.5, 7.6).
type ServerPage struct {
	World    world.Summary
	HasWorld bool
	Status   game.Status
	// Players is who is online, when PlayersKnown; PlayersError says the
	// game could not be asked.
	Players      []game.Player
	PlayersKnown bool
	PlayersError bool
	// Result is how the last action ended, when HaveResult.
	Result     actionResult
	HaveResult bool
}

func (ServerPage) templateName() string { return "server" }
func (ServerPage) navItem() string      { return PathServer }

func (p ServerPage) liveURL() string {
	if !p.HasWorld {
		return ""
	}
	return gamePath(p.World.ID, "events")
}

// Paths the server page links and posts to.
func (p ServerPage) WorldPath() string     { return worldPath(p.World.ID) }
func (p ServerPage) SnapshotsPath() string { return snapshotsPath(p.World.ID) }
func (p ServerPage) StartPath() string     { return gamePath(p.World.ID, "start") }
func (p ServerPage) StopPath() string      { return gamePath(p.World.ID, "stop") }
func (p ServerPage) RestartPath() string   { return gamePath(p.World.ID, "restart") }
func (p ServerPage) ForceStopPath() string { return gamePath(p.World.ID, "force-stop") }
func (p ServerPage) PullPath() string      { return gamePath(p.World.ID, "pull-image") }

// StateWord is the badge's word (DESIGN 7.5).
func (p ServerPage) StateWord() string { return stateWord(p.Status.State) }

func stateWord(s game.State) string {
	switch s {
	case game.StateStopped:
		return "Stopped"
	case game.StateStarting:
		return "Starting"
	case game.StateRunning:
		return "Running"
	case game.StateStopping:
		return "Stopping"
	case game.StateCrashed:
		return "Crashed"
	case game.StateCrashLooping:
		return "Crash-looping"
	}
	return "Unknown"
}

// GameVersion is the version the running game reports, else the world's.
func (p ServerPage) GameVersion() string {
	if p.Status.GameVersion != "" {
		return p.Status.GameVersion
	}
	return p.World.GameVersion
}

// Running reports whether the game has a run to show uptime for.
func (p ServerPage) Running() bool { return !p.Status.StartedAt.IsZero() }

// Uptime says how long the game has run, in whole minutes, and the same as
// an ISO 8601 duration for the time element.
func (p ServerPage) Uptime() string    { return durationText(p.Status.Uptime) }
func (p ServerPage) UptimeISO() string { return durationISO(p.Status.Uptime) }

// CPU is the game's CPU use, as a percentage of one core.
func (p ServerPage) CPU() string { return fmt.Sprintf("%.1f%%", p.Status.CPUPercent) }

// Busy reports whether a start, stop, restart or restore is running.
func (p ServerPage) Busy() bool { return p.Status.Operation != nil && !p.Status.Operation.Done }

// The actions the page offers in the current state. An action that would
// be refused is left out, and ActionsNote says why (DESIGN 7.1).
func (p ServerPage) CanStart() bool {
	s := p.Status.State
	return !p.Busy() && (s == game.StateStopped || s == game.StateCrashLooping)
}

func (p ServerPage) CanStop() bool {
	s := p.Status.State
	return !p.Busy() && (s == game.StateRunning || s == game.StateCrashed)
}

func (p ServerPage) CanRestart() bool { return !p.Busy() && p.Status.State == game.StateRunning }

func (p ServerPage) CanForceStop() bool {
	switch p.Status.State {
	case game.StateStopped, game.StateCrashLooping, game.StateUnknown:
		return false
	}
	return true
}

// ActionsNote explains the actions left out, or is "".
func (p ServerPage) ActionsNote() string {
	switch {
	case p.Status.State == game.StateUnknown:
		return "Foreman cannot start or stop the game until it can ask Docker about it."
	case p.Busy():
		return "Wait for this to finish, or use Force stop to stop the game now without saving."
	case p.Status.State == game.StateStarting || p.Status.State == game.StateStopping:
		return "The game is not in game, so it cannot be warned or saved. Wait, or use Force stop."
	}
	return ""
}

// ShowOperation reports whether the step list is shown: while the
// operation runs, and after it failed.
func (p ServerPage) ShowOperation() bool { return showOp(p.Status.Operation) }

// ShowDownload is ShowOperation for the image download.
func (p ServerPage) ShowDownload() bool { return showOp(p.Status.Download) }

func showOp(o *game.Operation) bool { return o != nil && (!o.Done || o.Error != "") }

// OperationHeading names the operation in the step list.
func (p ServerPage) OperationHeading() string {
	o := p.Status.Operation
	failed := o.Done && o.Error != ""
	switch o.Action {
	case game.AuditStart:
		return choose(failed, "The start failed", "Starting the server")
	case game.AuditStop:
		return choose(failed, "The stop failed", "Stopping the server")
	case game.AuditRestart:
		return choose(failed, "The restart failed", "Restarting the server")
	case game.ActionRestore:
		return choose(failed, "The restore failed", "Restoring a snapshot")
	}
	return choose(failed, "This failed", "Working")
}

func choose(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

// stepList is one step list and the failure that may sit in it.
type stepList struct {
	Page    ServerPage
	Op      *game.Operation
	Failure *Failure
	In      string // the Failure.In value that puts the failure here
}

// OperationList and DownloadList are the two step lists the page shows.
func (p ServerPage) OperationList() stepList {
	return stepList{Page: p, Op: p.Status.Operation, Failure: p.Failure(), In: "operation"}
}

func (p ServerPage) DownloadList() stepList {
	return stepList{Page: p, Op: p.Status.Download, Failure: p.Failure(), In: "download"}
}

// Since says how long a player has been online.
func (ServerPage) Since(d time.Duration) string { return durationText(d) }

// StepWord is the word beside a step (DESIGN 7.6).
func (ServerPage) StepWord(s game.StepState) string {
	switch s {
	case game.StepDone:
		return "Done"
	case game.StepProgress:
		return "In progress"
	case game.StepFailed:
		return "Failed"
	case game.StepSkipped:
		return "Skipped"
	}
	return "Waiting"
}

// StepClass is the step word's modifier class, or "".
func (ServerPage) StepClass(s game.StepState) string {
	switch s {
	case game.StepDone, game.StepProgress, game.StepFailed:
		return "step-state--" + string(s)
	}
	return ""
}

// Failure is the last action's error, worded for the admin, with what the
// page can offer next.
type Failure struct {
	Message string
	// Problems are the pre-start check's findings (SPEC 7.4).
	Problems []string
	// AnywayPath confirms "Start anyway", when every problem allows it.
	AnywayPath string
	// Download offers to download the missing game image (SPEC 3.7).
	Download bool
	// Undo names the "Before restore" snapshot a failed restore took.
	Undo string
	// In says which step list holds the message: "operation", "download",
	// or "" for on its own.
	In string
}

// Failure returns the last action's error, or nil.
func (p ServerPage) Failure() *Failure {
	if !p.HaveResult || p.Result.Err == nil {
		return nil
	}
	err := p.Result.Err
	f := &Failure{Undo: p.Result.Undo}
	var pe *game.PrecheckError
	switch {
	case errors.As(err, &pe):
		f.Message = "Foreman did not start the game, because the save that would load does not match the world:"
		for _, pr := range pe.Check.Problems {
			f.Problems = append(f.Problems, pr.Text)
		}
		if pe.Check.CanStartAnyway() {
			f.AnywayPath = gamePath(p.World.ID, "start-anyway")
			if p.Result.Action == game.AuditRestart {
				f.AnywayPath += "?then=restart"
			}
		}
	case errors.Is(err, game.ErrImageMissing):
		f.Message, f.Download = sentence(err), true
	default:
		f.Message = failureText(err)
	}
	text := err.Error()
	switch {
	case p.Status.Operation != nil && p.Status.Operation.Done && p.Status.Operation.Error == text:
		f.In = "operation"
	case p.Status.Download != nil && p.Status.Download.Done && p.Status.Download.Error == text:
		f.In = "download"
	}
	return f
}

// failureText words an action's error; one the admin cannot fix is logged
// when the action ends and is not shown.
func failureText(err error) string {
	status, msg := gameError(err)
	if status == http.StatusInternalServerError || msg == "" {
		return "Foreman could not finish this. The details are in Foreman's log."
	}
	return msg
}

// Done says what the last action did, when that is not otherwise visible.
func (p ServerPage) Done() string {
	if !p.HaveResult || p.Result.Err != nil || p.Result.Action != game.ActionRestore {
		return ""
	}
	return "The world is restored. Foreman took the snapshot \"Before restore\" first; to undo the restore, restore that snapshot from the Snapshots page."
}

// Announcement is the sentence a live page reads out when the state
// changes (DESIGN 7.5).
func announcement(s game.State) string {
	if s == game.StateUnknown {
		return "Foreman cannot tell the server's state"
	}
	return "Server is now " + strings.ToLower(stateWord(s))
}

// durationText says a duration in whole minutes, hours and days.
func durationText(d time.Duration) string {
	m := int(d / time.Minute)
	switch {
	case m < 1:
		return "less than 1 min"
	case m < 60:
		return fmt.Sprintf("%d min", m)
	case m < 24*60:
		return fmt.Sprintf("%d h %d min", m/60, m%60)
	}
	return fmt.Sprintf("%d d %d h", m/(24*60), m/60%24)
}

// durationISO is d as an ISO 8601 duration in whole minutes, for datetime.
func durationISO(d time.Duration) string {
	m := int(d / time.Minute)
	return fmt.Sprintf("PT%dH%dM", m/60, m%60)
}

// serverPage builds the server page for a world from its status, asking
// the game who is online when it is running.
func (s *server) serverPage(ctx context.Context, sum world.Summary, st game.Status) ServerPage {
	p := ServerPage{World: sum, HasWorld: true, Status: st}
	p.Result, p.HaveResult = s.runs.result(sum.ID)
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
// field; zero seconds means no countdown, since the controller reads zero
// as "the default".
func readCountdown(r *http.Request) (CountdownForm, game.StopOptions) {
	f := CountdownForm{Countdown: strings.TrimSpace(r.PostFormValue("countdown")), NoCountdown: r.PostFormValue("no_countdown") != ""}
	if f.NoCountdown {
		return f, game.StopOptions{NoCountdown: true}
	}
	if f.Countdown == "" {
		return f, game.StopOptions{}
	}
	n, err := strconv.Atoi(f.Countdown)
	if err != nil || n < 0 || n > maxCountdownSeconds {
		f.CountdownError = fmt.Sprintf("Enter a whole number of seconds from 0 to %d", maxCountdownSeconds)
		return f, game.StopOptions{}
	}
	if n == 0 {
		return f, game.StopOptions{NoCountdown: true}
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

// handlePull downloads the world's pinned game image (R7, SPEC 3.7).
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

// liveRegions are the parts of the server page a live update replaces.
// Each is sent only when it changed, so an update to one never moves focus
// out of another.
var liveRegions = []string{"facts", "players", "control"}

// handleEvents streams a world's server page as server-sent events (SPEC
// 16.3): each region's HTML whenever it changes, starting with all of them,
// so a page that reconnects misses nothing. The stream ends when its user
// is no longer signed in, when the page falls too far behind, and first of
// all at shutdown.
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	if !s.runs.track() {
		http.Error(w, "Foreman is shutting down.", http.StatusServiceUnavailable)
		return
	}
	defer s.runs.wg.Done()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // proxies must pass each event on at once
	w.WriteHeader(http.StatusOK)
	out := &liveStream{w: w, rc: http.NewResponseController(w), render: s.render, sent: map[string]string{}}

	view, updates, stop := s.game.Watch(sum.ID)
	defer stop()
	results := s.runs.watch()
	beat := time.NewTicker(heartbeatEvery)
	defer beat.Stop()
	st, changed := view.Status, true
	for {
		if changed {
			if err := out.send(s.serverPage(r.Context(), sum, st)); err != nil {
				return
			}
		}
		changed = true
		select {
		case <-r.Context().Done():
			return
		case <-s.runs.live.Done():
			return
		case up, open := <-updates:
			if !open {
				return
			}
			if up.Kind == game.UpdateStatus {
				st = up.Status
			} else {
				changed = false
			}
		case <-results:
			results = s.runs.watch()
			st = s.game.Status(sum.ID)
		case <-beat.C:
			if _, state, err := s.identify(r); err != nil || state != isSignedIn {
				return
			}
			if err := out.comment(); err != nil {
				return
			}
			changed = false
		}
	}
}

// liveStream writes server-sent events.
type liveStream struct {
	w      http.ResponseWriter
	rc     *http.ResponseController
	render *Renderer
	sent   map[string]string // the last HTML sent for each region
	state  game.State        // the last state sent; "" before the first
}

// send writes each region of p that changed since the last send. When the
// state changed, the facts region carries a sentence for #announcer.
func (l *liveStream) send(p ServerPage) error {
	wrote := false
	for _, region := range liveRegions {
		var b strings.Builder
		if err := l.render.Fragment(&b, p, "server-"+region); err != nil {
			return err
		}
		html := b.String()
		if l.sent[region] == html {
			continue
		}
		l.sent[region] = html
		if region == "facts" && l.state != "" && l.state != p.Status.State {
			html += `<div hx-swap-oob="innerHTML:#announcer">` + announcement(p.Status.State) + `</div>`
		}
		if err := l.event(region, html); err != nil {
			return err
		}
		wrote = true
	}
	l.state = p.Status.State
	if !wrote {
		return nil
	}
	return l.rc.Flush()
}

// event writes one event; each line of data gets its own field.
func (l *liveStream) event(name, data string) error {
	var b strings.Builder
	b.WriteString("event: " + name + "\n")
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r", ""), "\n") {
		b.WriteString("data: " + line + "\n")
	}
	b.WriteString("\n")
	_, err := l.w.Write([]byte(b.String()))
	return err
}

// comment keeps a quiet stream alive; browsers ignore it.
func (l *liveStream) comment() error {
	if _, err := l.w.Write([]byte(": still here\n\n")); err != nil {
		return err
	}
	return l.rc.Flush()
}
