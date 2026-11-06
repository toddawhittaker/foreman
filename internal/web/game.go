package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Server control routes (SPEC 7). Each action posts to the world it acts on.
const (
	PathServer      = "/"
	PathGameStart   = "/worlds/{id}/start"
	PathGameStop    = "/worlds/{id}/stop"
	PathGameRestart = "/worlds/{id}/restart"
	PathGameForce   = "/worlds/{id}/force-stop"
	PathGameSwitch  = "/worlds/{id}/switch"
	PathGamePull    = "/worlds/{id}/pull-image"
	PathGameEvents  = "/worlds/{id}/events"
	// PathGameCancel ends the stop countdown on the world whose game is
	// stopping; in a switch that is the world switched from (SPEC 7.6).
	PathGameCancel   = "/worlds/{id}/cancel-stop"
	PathGameSnapshot = "/worlds/{id}/take-snapshot"
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
	locking   map[string]int            // those of them that hold the world's lock
	results   map[string]actionResult   // by world id
	asides    map[string][]actionResult // actions asked for while another ran
	cancelled map[string]bool           // worlds whose running action had its stop cancelled
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
	// Target is the world a switch was asked to go to, and From the world
	// it left.
	Target, From string
	// Taken is the label of the snapshot a Server page snapshot took.
	Taken string
	// Version is the game version an update was asked to go to.
	Version string
	// At is when the action ended.
	At time.Time
}

func newGameRuns() *gameRuns {
	g := &gameRuns{running: map[string]int{}, locking: map[string]int{}, results: map[string]actionResult{},
		asides: map[string][]actionResult{}, cancelled: map[string]bool{}, changed: make(chan struct{})}
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

// run starts an action in the background on one world.
func (g *gameRuns) run(id string, holdsLock bool, do func(ctx context.Context) actionResult) bool {
	return g.runOn([]string{id}, holdsLock, do)
}

// runOn starts an action in the background on every world in ids; a switch
// holds two, and each world's page shows its result. holdsLock is true for
// the actions that take the world's lock (start, stop, restart, restore,
// switch, force stop) and false for an image download. When no lock-holding
// action runs on a world, a new one forgets the world's last results, so
// the page shows only what it does. A lock-holding action asked for while
// another runs keeps its result apart, so it never hides the first one's. A
// download never takes the main place from a lock-holding action.
func (g *gameRuns) runOn(ids []string, holdsLock bool, do func(ctx context.Context) actionResult) bool {
	g.mu.Lock()
	if !g.trackLocked() {
		g.mu.Unlock()
		return false
	}
	other := make([]bool, len(ids))
	for i, id := range ids {
		other[i] = holdsLock && g.locking[id] > 0
		if holdsLock && !other[i] || !holdsLock && g.running[id] == 0 {
			delete(g.results, id)
			delete(g.asides, id)
			g.changedLocked()
		}
		g.running[id]++
		if holdsLock {
			g.locking[id]++
		}
	}
	g.mu.Unlock()
	go func() {
		defer g.wg.Done()
		res := do(g.ctx)
		res.At = time.Now()
		g.mu.Lock()
		defer g.mu.Unlock()
		for i, id := range ids {
			if g.running[id]--; g.running[id] == 0 {
				delete(g.running, id)
				delete(g.cancelled, id)
			}
			if holdsLock {
				if g.locking[id]--; g.locking[id] == 0 {
					delete(g.locking, id)
				}
			} else {
				_, has := g.results[id]
				other[i] = g.locking[id] > 0 || has
			}
			if other[i] {
				g.asides[id] = append(g.asides[id], res)
			} else {
				g.results[id] = res
			}
		}
		g.changedLocked()
	}()
	return true
}

// markCancelled notes that the action running on a world is having its stop
// countdown cancelled, so its result is on the way. It does nothing when no
// action runs, since the result is then stored already.
func (g *gameRuns) markCancelled(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running[id] > 0 {
		g.cancelled[id] = true
	}
}

// unmarkCancelled undoes markCancelled for a cancel that did not happen.
func (g *gameRuns) unmarkCancelled(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.cancelled, id)
}

// cancelPending reports whether a cancelled action's result has not been
// stored yet. It ends with the actions on the world, so a later snapshot or
// download never revives the message (SPEC 7.6).
func (g *gameRuns) cancelPending(id string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cancelled[id]
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
	s.mux.HandleFunc("GET "+PathGameStop, s.requireUser(s.handleStopForm))
	s.mux.HandleFunc("POST "+PathGameStop, s.requireUser(s.handleStop))
	s.mux.HandleFunc("GET "+PathGameRestart, s.requireUser(s.handleStopForm))
	s.mux.HandleFunc("POST "+PathGameRestart, s.requireUser(s.handleStop))
	s.mux.HandleFunc("GET "+PathGameForce, s.requireUser(s.handleForceStopForm))
	s.mux.HandleFunc("POST "+PathGameForce, s.requireUser(s.handleForceStop))
	s.mux.HandleFunc("POST "+PathGamePull, s.requireUser(s.handlePull))
	s.mux.HandleFunc("POST "+PathGameCancel, s.requireUser(s.handleCancelStop))
	s.mux.HandleFunc("POST "+PathGameSnapshot, s.requireUser(s.handleServerSnapshot))
	s.switchRoutes()
	s.handleLong("GET "+PathGameEvents, s.requireUser(s.handleEvents))
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
	p := ServerPage{World: sum, HasWorld: true, Status: st, ModsFolder: hostModsFolder(s.hostData, sum.ID), Now: s.now()}
	p.Result, p.HaveResult = s.runs.result(sum.ID)
	if !p.HaveResult && s.runs.cancelPending(sum.ID) {
		p.Result, p.HaveResult = cancelledByOperation(st, s.now())
	}
	p.Others = s.runs.others(sum.ID)
	s.switchNames(&p)
	if p.HaveResult && p.Result.Err == nil && p.Result.Action == game.AuditUpdate {
		p.Rollback = s.rollbackOffer(sum.ID)
	}
	if st.State == game.StateCrashLooping {
		p.StartAnywaySnap = s.startAnywaySnapshot(sum.ID)
	}
	// Said before Start is pressed (SPEC 3.7). Only local Docker is asked;
	// when it cannot answer, Start finds out and says so.
	if p.CanStart() {
		if ref, present, err := s.game.ImageFor(ctx, sum.ID); err == nil {
			p.Image, p.ImageMissing = ref, !present
		}
	}
	// Not while an operation runs: a switch would be refused (DESIGN 7.1).
	// Each switch confirmation runs its own checks (SPEC 17.7, 17.8).
	if !p.Busy() {
		p.OtherWorlds = s.otherWorlds(ctx)
	}
	if st.State == game.StateRunning {
		ctx, cancel := context.WithTimeout(ctx, playersLimit)
		defer cancel()
		players, err := s.game.Players(ctx, sum.ID)
		p.Players, p.PlayersKnown, p.PlayersError = players, err == nil, err != nil
	}
	return p
}

// otherWorlds lists the worlds the Server page offers to switch to: every
// readable one that is not the active world.
func (s *server) otherWorlds(ctx context.Context) []world.Summary {
	list, err := s.worlds.List(ctx)
	if err != nil {
		// A browser that left mid-request is not a server fault.
		if ctx.Err() == nil {
			s.log.Error("list worlds", "error", err)
		}
		return nil
	}
	var out []world.Summary
	for _, ws := range list {
		if !ws.Active && !ws.Unreadable && !ws.Newer {
			out = append(out, ws)
		}
	}
	return out
}

// startAnywaySnapshot returns a world's newest snapshot when it is a
// "Before start anyway" one, which a crash loop offers to restore
// (SPEC 17.9), or nil. With a newer snapshot the world has moved on, and
// the restore would undo more than the start.
func (s *server) startAnywaySnapshot(id string) *world.Snapshot {
	list, err := s.worlds.Snapshots(id)
	if err != nil {
		s.log.Error("list snapshots", "world", id, "error", err)
		return nil
	}
	if len(list) == 0 || list[0].Reason != game.ReasonStartAnyway {
		return nil
	}
	return &list[0]
}

func (s *server) handleServer(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.worlds.List(r.Context())
	if err != nil {
		s.fail(w, r, "find the active world", err)
		return
	}
	var sum world.Summary
	found := false
	for _, ws := range list {
		if ws.Active {
			sum, found = ws, true
		}
	}
	if !found {
		s.page(w, r, http.StatusOK, u.Username, ServerPage{})
		return
	}
	p := s.serverPage(r.Context(), sum, s.game.Status(sum.ID))
	p.FocusResult = followsOwnAction(r)
	s.page(w, r, http.StatusOK, u.Username, p)
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
			Detail: i18n.M("server.start.not_active")})
		return world.Summary{}, false
	}
	return sum, true
}

// begin runs an action in the background and sends the admin to the
// server page, which follows it live.
func (s *server) begin(w http.ResponseWriter, r *http.Request, id string, holdsLock bool, do func(ctx context.Context) actionResult) {
	s.beginOn(w, r, []string{id}, holdsLock, do)
}

// beginOn is begin for an action on several worlds. It reports whether the
// action started; if not, the page has been answered.
func (s *server) beginOn(w http.ResponseWriter, r *http.Request, ids []string, holdsLock bool, do func(ctx context.Context) actionResult) bool {
	return s.beginTo(w, r, PathServer, ids, holdsLock, do)
}

// beginTo is beginOn sending the admin to the page at to.
func (s *server) beginTo(w http.ResponseWriter, r *http.Request, to string, ids []string, holdsLock bool, do func(ctx context.Context) actionResult) bool {
	if !s.runs.runOn(ids, holdsLock, do) {
		s.page(w, r, http.StatusServiceUnavailable, "", ErrorPage{Status: http.StatusServiceUnavailable,
			Detail: i18n.M("server.shutting_down")})
		return false
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
	return true
}

// ownResultQuery marks a Server page load that follows the admin's own
// action, whose result the page then moves focus to.
const (
	ownResultQuery = "?result=1"
	// PathServerResult is the Server page after the admin's own action.
	PathServerResult = PathServer + ownResultQuery
)

func followsOwnAction(r *http.Request) bool {
	return r.URL.RawQuery == strings.TrimPrefix(ownResultQuery, "?")
}

// finish logs an action's error that the admin cannot fix, then returns
// the result to keep.
func (s *server) finish(id, action string, err error) actionResult {
	if err != nil {
		status, _ := gameError(err)
		// A download's cause is not shown, so it is logged unless it is the
		// plain "no such image".
		de, failedDownload := errors.AsType[*game.ImageDownloadError](err)
		// Shutdown cancels a running action; that is not a fault.
		if errors.Is(err, context.Canceled) {
			return actionResult{Action: action, Err: err}
		}
		if status == http.StatusInternalServerError || failedDownload && !de.NotFound {
			s.log.Error("server control", "world", id, "action", action, "error", err)
		} else if errors.Is(err, game.ErrRCONUnreachable) {
			// The page shows the plain refusal; the transport's own error is
			// kept here.
			s.log.Warn("server control", "world", id, "action", action, "error", err)
		}
	}
	return actionResult{Action: action, Err: err}
}

// handleStart starts the active world (SPEC 7.3). "anyway" starts despite
// the pre-start check's problems; the refusal box sends it (7.4).
func (s *server) handleStart(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.startable(w, r, u)
	if !ok {
		return
	}
	opts := anywayOptions(r)
	by := actor(u)
	s.begin(w, r, sum.ID, true, func(ctx context.Context) actionResult {
		return s.finish(sum.ID, game.AuditStart, s.game.Start(ctx, sum.ID, by, opts))
	})
}

// CountdownForm is the stop countdown choice (SPEC 7.6), and who would
// be warned.
type CountdownForm struct {
	Countdown      string
	CountdownError i18n.Msg
	Online         Online
}

// readCountdown reads the countdown field. A problem is worded for the
// field; an empty field means the default countdown, and 0 means no warning
// (SPEC 7.6).
func readCountdown(r *http.Request) (CountdownForm, game.StopOptions) {
	f := CountdownForm{Countdown: strings.TrimSpace(r.PostFormValue("countdown"))}
	if f.Countdown == "" {
		return f, game.StopOptions{Countdown: game.StopCountdown}
	}
	n, err := strconv.Atoi(f.Countdown)
	if err != nil || n < 0 || n > maxCountdownSeconds {
		f.CountdownError = i18n.M("server.countdown.invalid", "max", maxCountdownSeconds)
		return f, game.StopOptions{}
	}
	return f, game.StopOptions{Countdown: time.Duration(n) * time.Second}
}

func defaultCountdown() CountdownForm {
	return CountdownForm{Countdown: strconv.Itoa(defaultCountdownSeconds)}
}

// Online is who is in the game, for a form that stops it (SPEC 7.6, 7.7).
// Asked is false when the game is not running, so the form says nothing.
type Online struct {
	Asked, Failed bool
	Names         []string
}

// online asks the game who is online, when it is running.
func (s *server) online(ctx context.Context, id string) Online {
	if s.game.Status(id).State != game.StateRunning {
		return Online{}
	}
	ctx, cancel := context.WithTimeout(ctx, playersLimit)
	defer cancel()
	players, err := s.game.Players(ctx, id)
	o := Online{Asked: true, Failed: err != nil}
	for _, pl := range players {
		o.Names = append(o.Names, pl.Name)
	}
	return o
}

// Text names who is online.
func (o Online) Text() i18n.Msg {
	switch {
	case o.Failed:
		return i18n.M("server.online.failed")
	case len(o.Names) == 0:
		return i18n.M("server.online.nobody")
	}
	return i18n.M("server.online.names", "count", len(o.Names), "names", strings.Join(o.Names, ", "))
}

// Disconnected says who a forced stop disconnects.
func (o Online) Disconnected() i18n.Msg {
	switch {
	case !o.Asked || o.Failed:
		return i18n.M("server.online.disconnected_unknown")
	case len(o.Names) == 0:
		return i18n.M("server.online.nobody")
	}
	return i18n.M("server.online.disconnected", "names", strings.Join(o.Names, ", "))
}

// StopPage asks how to warn players before a stop or restart (SPEC 7.5,
// 7.6, 7.8).
type StopPage struct {
	World   world.Summary
	Restart bool
	CountdownForm
}

func (StopPage) templateName() string { return "stop" }
func (StopPage) navItem() string      { return PathServer }
func (p StopPage) hasErrors() bool    { return !p.CountdownError.IsZero() }

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

func (s *server) handleStopForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	restart := strings.HasSuffix(r.URL.Path, "/restart")
	form := defaultCountdown()
	form.Online = s.online(r.Context(), sum.ID)
	s.page(w, r, http.StatusOK, u.Username, StopPage{World: sum, Restart: restart, CountdownForm: form})
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
	if !form.CountdownError.IsZero() {
		form.Online = s.online(r.Context(), sum.ID)
		s.page(w, r, http.StatusBadRequest, u.Username, StopPage{World: sum, Restart: restart, CountdownForm: form})
		return
	}
	by, opts := actor(u), anywayOptions(r)
	s.begin(w, r, sum.ID, true, func(ctx context.Context) actionResult {
		if restart {
			err := s.game.Restart(ctx, sum.ID, by, stop, opts)
			return s.finish(sum.ID, game.AuditRestart, err)
		}
		return s.finish(sum.ID, game.AuditStop, s.game.Stop(ctx, sum.ID, by, stop))
	})
}

// anywayOptions reads a go-ahead anyway and the fingerprint of what its box
// showed; the controller refuses it when that no longer holds (SPEC 7.4).
func anywayOptions(r *http.Request) game.StartOptions {
	return game.StartOptions{Anyway: r.PostFormValue("anyway") == "1", Seen: r.PostFormValue("seen")}
}

// ForceStopPage confirms a forced stop (SPEC 7.7).
type ForceStopPage struct {
	World  world.Summary
	Online Online
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
	s.page(w, r, http.StatusOK, u.Username, ForceStopPage{World: sum, Online: s.online(r.Context(), sum.ID)})
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
	s.begin(w, r, sum.ID, true, func(ctx context.Context) actionResult {
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
	s.begin(w, r, sum.ID, false, func(ctx context.Context) actionResult {
		return s.finish(sum.ID, game.AuditImageDownload, s.game.PullImage(ctx, sum.ID, by))
	})
}

// handleCancelStop ends the stop countdown on the world in the path, so the
// action that began it ends with nothing changed (SPEC 7.6). It returns once
// that action has ended, so the Server page shows how it ended.
func (s *server) handleCancelStop(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	// A countdown that ended just before the cancel has nothing to cancel;
	// the Server page shows how the action went on.
	s.runs.markCancelled(sum.ID)
	err := s.game.CancelStop(r.Context(), sum.ID, actor(u))
	if err != nil {
		s.runs.unmarkCancelled(sum.ID)
	}
	if err != nil && !errors.Is(err, game.ErrNoCountdown) {
		if status, msg, ok := s.gameRefusal(w, r, u, "cancel the stop", err); ok {
			s.page(w, r, status, u.Username, ErrorPage{Status: status, Detail: msg})
		}
		return
	}
	http.Redirect(w, r, PathServerResult, http.StatusSeeOther)
}
