// Package schedule restarts the active world at a set time (SPEC 7.12). It
// is a leaf package that uses only the game controller's public methods.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/store"
)

// Limits of a schedule (SPEC 7.12).
const (
	// Warning is the fixed countdown before a scheduled restart.
	Warning           = 60 * time.Second
	DefaultLimitHours = 1
	MaxLimitHours     = 12
	// DefaultMinute is 04:00, the time a new schedule offers.
	DefaultMinute = 4 * 60
)

// How often the loop looks again.
const (
	pollIdle      = time.Minute
	pollWaiting   = 30 * time.Second
	pollCountdown = 5 * time.Second
	// askTimeout bounds one /players online.
	askTimeout = 10 * time.Second
	// missedAfter: a scheduled time the loop reaches later than this was
	// missed (Foreman or its host was not running) and is not made up.
	missedAfter = 10 * time.Minute
)

// Actor is who the audit log names for the schedule's restarts and cancels.
var Actor = game.Actor{Username: "restart schedule"}

// Errors Set returns for a schedule it refuses.
var (
	ErrMinute = errors.New("the time must be between 00:00 and 23:59")
	ErrZone   = errors.New("the time zone is not one Foreman knows")
	ErrLimit  = fmt.Errorf("the wait limit must be a whole number of hours from 0 to %d", MaxLimitHours)
)

// Game is the part of the game controller the schedule uses.
type Game interface {
	Status(id string) game.Status
	Players(ctx context.Context, id string) ([]game.Player, error)
	Restart(ctx context.Context, id string, by game.Actor, stop game.StopOptions, opts game.StartOptions) error
	CancelStop(ctx context.Context, id string, by game.Actor) error
}

// Store keeps the schedule. *store.Store satisfies it.
type Store interface {
	RestartSchedule(ctx context.Context) (store.RestartSchedule, bool, error)
	SetRestartSchedule(ctx context.Context, r store.RestartSchedule, now time.Time) error
}

// Emitter sends events. A nil *notify.Notifier is a valid one.
type Emitter interface{ Emit(notify.Message) }

// ActiveWorld names the active world; ok is false when no world is active.
type ActiveWorld func(ctx context.Context) (id, name string, ok bool, err error)

// Config is what a Scheduler needs. Now defaults to time.Now.
type Config struct {
	Game     Game
	Store    Store
	Active   ActiveWorld
	Notifier Emitter
	Log      *slog.Logger
	Now      func() time.Time
}

// Phase is where today's scheduled restart stands.
type Phase string

const (
	// PhaseNone: no scheduled restart is under way and none was skipped.
	PhaseNone Phase = ""
	// PhaseWaiting: the set time has passed and the schedule waits for
	// nobody to be online.
	PhaseWaiting Phase = "waiting"
	// PhaseCountdown: nobody was online, so the restart's warning runs.
	PhaseCountdown Phase = "countdown"
	// PhaseSkipped: today's restart was skipped.
	PhaseSkipped Phase = "skipped"
)

// SkipReason says why a restart was skipped.
type SkipReason string

const (
	SkipPlayers SkipReason = "players"
	SkipAdmin   SkipReason = "admin"
)

// State is what the Server page shows about the schedule (DESIGN 8.15).
type State struct {
	// Next is the next scheduled restart; zero when the schedule is off.
	Next  time.Time
	Phase Phase
	// WorldID is the world the wait, countdown or skip is about.
	WorldID string
	// Since is the set time the wait began at; Until is when it is skipped
	// if players are still online. Both are kept through the countdown.
	Since, Until time.Time
	// Unasked: the game could not be asked who is online, so the wait
	// counts someone as online.
	Unasked bool
	// Joined is a player who joined during the warning, so the wait began
	// again; "" when the warning was cut short for another reason.
	Joined string
	// CountdownEnds is when the warning ends, in PhaseCountdown.
	CountdownEnds time.Time
	// SkippedAt and Reason are set in PhaseSkipped.
	SkippedAt time.Time
	Reason    SkipReason
}

// Scheduler runs the restart schedule.
type Scheduler struct {
	cfg  Config
	wake chan struct{}

	mu       sync.Mutex
	settings store.RestartSchedule
	found    bool
	loc      *time.Location
	next     time.Time
	st       State
	changed  chan struct{}
	// The world under way, its name for events, and when its current run
	// began, so a restart by hand ends the wait.
	worldName string
	startedAt time.Time
	// cancelled is set when the schedule itself pressed Cancel stop, so a
	// cancel it did not make is known to be an admin's.
	cancelled bool
	restarts  sync.WaitGroup
}

// New loads the schedule. It restarts nothing until Run.
func New(ctx context.Context, cfg Config) (*Scheduler, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	s := &Scheduler{cfg: cfg, wake: make(chan struct{}, 1), changed: make(chan struct{})}
	r, found, err := cfg.Store.RestartSchedule(ctx)
	if err != nil {
		return nil, err
	}
	if !found {
		r = store.RestartSchedule{Minute: DefaultMinute, Zone: UTC, LimitHours: DefaultLimitHours}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applyLocked(r, found, cfg.Now())
	return s, nil
}

// Settings returns the schedule; found is false when none was ever saved,
// and the defaults are returned.
func (s *Scheduler) Settings() (r store.RestartSchedule, found bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings, s.found
}

// State returns where the schedule stands now.
func (s *Scheduler) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// Changed returns a channel closed at the next change of State.
func (s *Scheduler) Changed() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changed
}

// Set checks and saves the schedule. A wait under way ends when the
// schedule is turned off; a countdown already running carries on.
func (s *Scheduler) Set(ctx context.Context, r store.RestartSchedule) error {
	if r.Minute < 0 || r.Minute >= 24*60 {
		return ErrMinute
	}
	if !ValidZone(r.Zone) {
		return ErrZone
	}
	if r.LimitHours < 0 || r.LimitHours > MaxLimitHours {
		return ErrLimit
	}
	now := s.cfg.Now()
	if err := s.cfg.Store.SetRestartSchedule(ctx, r, now); err != nil {
		return err
	}
	r.UpdatedAt = now.UTC().Truncate(time.Second)
	s.mu.Lock()
	s.applyLocked(r, true, now)
	if !r.Enabled && s.st.Phase == PhaseWaiting {
		s.st = State{}
	}
	s.st.Next = s.next
	s.changedLocked()
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

func (s *Scheduler) applyLocked(r store.RestartSchedule, found bool, now time.Time) {
	s.settings, s.found = r, found
	s.next = time.Time{}
	loc, err := time.LoadLocation(r.Zone)
	if err != nil {
		// A zone that went away from the system's list: nothing runs
		// until the schedule is saved again.
		s.cfg.Log.Error("the restart schedule's time zone cannot be read, so no restart is scheduled", "zone", r.Zone, "error", err)
		s.loc = nil
	} else {
		s.loc = loc
	}
	if r.Enabled && s.loc != nil {
		s.next = nextAfter(now, r.Minute, s.loc)
	}
	s.st.Next = s.next
}

// changedLocked wakes everyone watching State.
func (s *Scheduler) changedLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}

// Run follows the schedule until ctx ends, then waits for a restart it
// began to end.
func (s *Scheduler) Run(ctx context.Context) {
	defer s.restarts.Wait()
	for {
		t := time.NewTimer(s.step(ctx))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-s.wake:
		case <-t.C:
		}
		t.Stop()
	}
}

// step does what the schedule needs now and returns how long to wait
// before the next step.
func (s *Scheduler) step(ctx context.Context) time.Duration {
	s.mu.Lock()
	switch s.st.Phase {
	case PhaseWaiting:
		s.mu.Unlock()
		return s.wait(ctx)
	case PhaseCountdown:
		s.mu.Unlock()
		return s.watchCountdown(ctx)
	}
	now := s.cfg.Now()
	if s.next.IsZero() {
		s.mu.Unlock()
		return pollIdle
	}
	if now.Before(s.next) {
		s.mu.Unlock()
		return min(s.next.Sub(now), pollIdle)
	}
	due := s.next
	s.next = nextAfter(due, s.settings.Minute, s.loc)
	limit := time.Duration(s.settings.LimitHours) * time.Hour
	// A new set time ends yesterday's skipped note.
	s.st = State{Next: s.next}
	s.changedLocked()
	s.mu.Unlock()
	if now.Sub(due) > missedAfter {
		s.cfg.Log.Info("a scheduled restart was missed while Foreman was not running, and is not made up", "time", due)
		return pollIdle
	}
	return s.begin(ctx, due, limit)
}

// begin starts the wait for nobody to be online, if the active world's game
// is running. A game that is stopped, starting, crashed or crash-looping is
// left alone (SPEC 7.12).
func (s *Scheduler) begin(ctx context.Context, due time.Time, limit time.Duration) time.Duration {
	id, name, ok, err := s.cfg.Active(ctx)
	if err != nil {
		s.cfg.Log.Error("the scheduled restart could not find the active world, so it did not run", "error", err)
		return pollIdle
	}
	if !ok {
		return pollIdle
	}
	st := s.cfg.Game.Status(id)
	if st.State != game.StateRunning {
		s.cfg.Log.Info("the scheduled restart did not run because the game is not running", "world", id, "state", string(st.State))
		return pollIdle
	}
	s.mu.Lock()
	s.st = State{Next: s.next, Phase: PhaseWaiting, WorldID: id, Since: due, Until: due.Add(limit)}
	s.worldName, s.startedAt = name, st.StartedAt
	s.changedLocked()
	s.mu.Unlock()
	return s.wait(ctx)
}

// wait looks whether anyone is online. Nobody: the countdown begins.
// Someone, or no answer: the wait goes on until its limit, then today's
// restart is skipped.
func (s *Scheduler) wait(ctx context.Context) time.Duration {
	s.mu.Lock()
	id, until, startedAt := s.st.WorldID, s.st.Until, s.startedAt
	s.mu.Unlock()

	st := s.cfg.Game.Status(id)
	// Stopped, crashed, or restarted by hand: there is nothing left to do today.
	if st.State != game.StateRunning || !st.StartedAt.Equal(startedAt) {
		s.endToday()
		return pollIdle
	}
	busy := st.Operation != nil && !st.Operation.Done
	names, err := s.online(ctx, id)
	now := s.cfg.Now()
	if err == nil && len(names) == 0 && !busy {
		s.startCountdown(ctx, id, now)
		return pollCountdown
	}
	if !now.Before(until) {
		s.mu.Lock()
		s.skipLocked(SkipPlayers, now)
		s.mu.Unlock()
		return pollIdle
	}
	s.mu.Lock()
	if unasked := err != nil; unasked != s.st.Unasked {
		s.st.Unasked = unasked
		s.changedLocked()
	}
	s.mu.Unlock()
	return min(pollWaiting, until.Sub(now))
}

// online asks the game who is online. An error counts as someone online.
func (s *Scheduler) online(ctx context.Context, id string) ([]game.Player, error) {
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	return s.cfg.Game.Players(ctx, id)
}

// startCountdown restarts the game with the fixed warning. The restart
// never asks to start anyway, so a start the pre-start check would refuse
// never happens, and the game is left as it is.
func (s *Scheduler) startCountdown(ctx context.Context, id string, now time.Time) {
	s.mu.Lock()
	s.st.Phase, s.st.CountdownEnds, s.st.Unasked, s.st.Joined = PhaseCountdown, now.Add(Warning), false, ""
	s.cancelled = false
	s.changedLocked()
	s.mu.Unlock()
	s.restarts.Go(func() {
		err := s.cfg.Game.Restart(ctx, id, Actor, game.StopOptions{Countdown: Warning}, game.StartOptions{})
		s.restartEnded(err)
	})
}

// watchCountdown presses Cancel stop when someone is online during the
// warning; the game tells them in chat, and the wait begins again.
func (s *Scheduler) watchCountdown(ctx context.Context) time.Duration {
	s.mu.Lock()
	id := s.st.WorldID
	s.mu.Unlock()
	names, err := s.online(ctx, id)
	if err == nil && len(names) == 0 {
		return pollCountdown
	}
	s.mu.Lock()
	if s.st.Phase != PhaseCountdown {
		s.mu.Unlock()
		return pollCountdown
	}
	s.cancelled = true
	if len(names) > 0 {
		s.st.Joined = names[0].Name
	}
	s.mu.Unlock()
	err = s.cfg.Game.CancelStop(ctx, id, Actor)
	if err != nil {
		// No countdown to cancel: it has not begun or has just ended. A
		// later look cancels one that begins.
		s.mu.Lock()
		s.cancelled = false
		s.mu.Unlock()
		if !errors.Is(err, game.ErrNoCountdown) {
			s.cfg.Log.Error("the scheduled restart could not cancel its countdown", "world", id, "error", err)
		}
	}
	return pollCountdown
}

// restartEnded settles the day when the restart ends.
func (s *Scheduler) restartEnded(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.st.Phase != PhaseCountdown {
		return
	}
	switch {
	case err == nil:
		s.emitLocked(false)
		s.st = State{Next: s.next}
	case errors.Is(err, game.ErrStopCancelled) && s.cancelled:
		s.st.Phase, s.st.CountdownEnds = PhaseWaiting, time.Time{}
	case errors.Is(err, game.ErrStopCancelled):
		// An admin pressed Cancel stop, which skips today's restart (DESIGN 8.15).
		s.skipLocked(SkipAdmin, s.cfg.Now())
		return
	default:
		// The restart's own result shows on the Server page.
		s.cfg.Log.Error("the scheduled restart failed", "world", s.st.WorldID, "error", err)
		s.st = State{Next: s.next}
	}
	s.changedLocked()
}

// skipLocked ends today's restart without running it, and says so. Only a
// skip for players sends an event: its message names that reason, and an
// admin who cancelled knows already.
func (s *Scheduler) skipLocked(why SkipReason, now time.Time) {
	s.cfg.Log.Info("today's scheduled restart was skipped", "world", s.st.WorldID, "reason", string(why))
	if why == SkipPlayers {
		s.emitLocked(true)
	}
	s.st = State{Next: s.next, Phase: PhaseSkipped, WorldID: s.st.WorldID, SkippedAt: now, Reason: why}
	s.changedLocked()
}

// endToday ends a wait the game no longer needs, without an event.
func (s *Scheduler) endToday() {
	s.mu.Lock()
	s.st = State{Next: s.next}
	s.changedLocked()
	s.mu.Unlock()
}

// emitLocked sends the scheduled-restart event for the world under way.
// Emit never blocks (SPEC 8.12), so it is safe under the lock.
func (s *Scheduler) emitLocked(skipped bool) {
	if s.cfg.Notifier == nil {
		return
	}
	s.cfg.Notifier.Emit(notify.Message{Event: notify.ScheduledRestart, World: notify.World{ID: s.st.WorldID, Name: s.worldName}, Skipped: skipped})
}
