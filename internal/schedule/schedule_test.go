package schedule

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/store"
)

// fakeGame is the controller as the schedule sees it. A restart runs until
// the test ends it with finish, or until CancelStop.
type fakeGame struct {
	mu         sync.Mutex
	status     game.Status
	players    []game.Player
	playersErr error
	restarts   int
	stops      []game.StopOptions
	starts     []game.StartOptions
	actors     []game.Actor
	cancels    int
	countdown  chan error
	entered    chan struct{}
}

func newFakeGame() *fakeGame {
	return &fakeGame{
		status:  game.Status{State: game.StateRunning, StartedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		entered: make(chan struct{}, 10),
	}
}

func (f *fakeGame) Status(string) game.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeGame) Players(context.Context, string) ([]game.Player, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.players, f.playersErr
}

func (f *fakeGame) Restart(_ context.Context, _ string, by game.Actor, stop game.StopOptions, opts game.StartOptions) error {
	f.mu.Lock()
	f.restarts++
	f.stops, f.starts, f.actors = append(f.stops, stop), append(f.starts, opts), append(f.actors, by)
	c := make(chan error, 1)
	f.countdown = c
	f.mu.Unlock()
	f.entered <- struct{}{}
	return <-c
}

func (f *fakeGame) CancelStop(_ context.Context, _ string, by game.Actor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.countdown == nil {
		return game.ErrNoCountdown
	}
	f.cancels++
	f.actors = append(f.actors, by)
	f.countdown <- game.ErrStopCancelled
	f.countdown = nil
	return nil
}

func (f *fakeGame) setPlayers(names ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.players = nil
	for _, n := range names {
		f.players = append(f.players, game.Player{Name: n})
	}
}

func (f *fakeGame) set(fn func(*fakeGame)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeGame) count() (restarts, cancels int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.restarts, f.cancels
}

// waitEntered waits for the schedule's restart to reach the fake.
func (f *fakeGame) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the restart was never called")
	}
}

// finish ends the running restart with err, as the game would.
func (f *fakeGame) finish(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.countdown <- err
	f.countdown = nil
}

type emits struct {
	mu   sync.Mutex
	msgs []notify.Message
}

func (e *emits) Emit(m notify.Message) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.msgs = append(e.msgs, m)
}

func (e *emits) all() []notify.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]notify.Message(nil), e.msgs...)
}

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

type rig struct {
	t     *testing.T
	s     *Scheduler
	g     *fakeGame
	emits *emits
	clock *clock
	st    *store.Store
	ctx   context.Context
}

// at is a time on 2026-11-12 in UTC.
func at(hh, mm int) time.Time { return time.Date(2026, 11, 12, hh, mm, 0, 0, time.UTC) }

// newRig makes a scheduler at now whose schedule, when r is not nil, is
// saved before it loads.
func newRig(t *testing.T, now time.Time, r *store.RestartSchedule) *rig {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if r != nil {
		if err := st.SetRestartSchedule(context.Background(), *r, now); err != nil {
			t.Fatal(err)
		}
	}
	x := &rig{t: t, g: newFakeGame(), emits: &emits{}, clock: &clock{t: now}, st: st, ctx: context.Background()}
	x.s, err = New(x.ctx, Config{
		Game: x.g, Store: st, Notifier: x.emits, Now: x.clock.Now,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Active: func(context.Context) (string, string, bool, error) {
			return "w1", "Family base", true, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// nightly is 04:00 UTC with a one-hour limit.
func nightly(limit int) *store.RestartSchedule {
	return &store.RestartSchedule{Enabled: true, Minute: 4 * 60, Zone: UTC, LimitHours: limit, UpdatedBy: "todd"}
}

// nightlyAt is a schedule at minute in zone with a one-hour limit.
func nightlyAt(minute int, zone string) *store.RestartSchedule {
	return &store.RestartSchedule{Enabled: true, Minute: minute, Zone: zone, LimitHours: 1, UpdatedBy: "todd"}
}

// stepAt runs one step of the loop at t.
func (x *rig) stepAt(t time.Time) time.Duration {
	x.clock.set(t)
	return x.s.step(x.ctx)
}

// waitPhase waits for the restart goroutine to move the state to want.
func (x *rig) waitPhase(want Phase) State {
	x.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		ch := x.s.Changed()
		if st := x.s.State(); st.Phase == want {
			return st
		}
		select {
		case <-ch:
		case <-deadline:
			x.t.Fatalf("phase = %q, want %q", x.s.State().Phase, want)
		}
	}
}

func TestRunsWhenNobodyIsOnline(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	if got := x.s.State().Next; !got.Equal(at(4, 0)) {
		t.Fatalf("next = %v, want 04:00", got)
	}
	if d := x.stepAt(at(3, 59)); d > time.Minute {
		t.Errorf("a step a minute early waits %v, want at most a minute", d)
	}
	if r, _ := x.g.count(); r != 0 {
		t.Fatal("restarted before the set time")
	}
	x.stepAt(at(4, 0))
	st := x.s.State()
	if st.Phase != PhaseCountdown || !st.CountdownEnds.Equal(at(4, 1)) || st.WorldID != "w1" {
		t.Fatalf("state = %+v, want a countdown ending 04:01", st)
	}
	x.g.waitEntered(t)
	x.g.set(func(f *fakeGame) {
		if f.stops[0].Countdown != 60*time.Second {
			t.Errorf("countdown = %v, want 60 s", f.stops[0].Countdown)
		}
		if f.starts[0].Anyway {
			t.Error("a scheduled restart used start anyway")
		}
		if f.actors[0] != Actor {
			t.Errorf("actor = %+v", f.actors[0])
		}
	})
	x.g.finish(nil)
	st = x.waitPhase(PhaseNone)
	if !st.Next.Equal(at(4, 0).AddDate(0, 0, 1)) {
		t.Errorf("next = %v, want tomorrow 04:00", st.Next)
	}
	got := x.emits.all()
	if len(got) != 1 || got[0].Event != notify.ScheduledRestart || got[0].Skipped || got[0].World != (notify.World{ID: "w1", Name: "Family base"}) {
		t.Errorf("events = %+v, want one scheduled restart that ran", got)
	}
}

func TestNeverStartsAGameThatIsNotRunning(t *testing.T) {
	for _, state := range []game.State{game.StateStopped, game.StateCrashed, game.StateCrashLooping, game.StateStarting, game.StateStopping, game.StateUnknown} {
		t.Run(string(state), func(t *testing.T) {
			x := newRig(t, at(3, 0), nightly(1))
			x.g.set(func(f *fakeGame) { f.status.State = state })
			x.stepAt(at(4, 0))
			x.stepAt(at(4, 1))
			if r, _ := x.g.count(); r != 0 {
				t.Errorf("restarted a %s game", state)
			}
			if st := x.s.State(); st.Phase != PhaseNone || !st.Next.Equal(at(4, 0).AddDate(0, 0, 1)) {
				t.Errorf("state = %+v, want nothing under way and tomorrow next", st)
			}
			if len(x.emits.all()) != 0 {
				t.Error("an event was sent for a game that was not running")
			}
		})
	}
}

func TestWaitsForNobodyOnlineThenRuns(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.g.setPlayers("alice")
	x.stepAt(at(4, 0))
	st := x.s.State()
	if st.Phase != PhaseWaiting || !st.Since.Equal(at(4, 0)) || !st.Until.Equal(at(5, 0)) || st.Unasked {
		t.Fatalf("state = %+v, want waiting until 05:00", st)
	}
	x.stepAt(at(4, 30))
	if r, _ := x.g.count(); r != 0 || x.s.State().Phase != PhaseWaiting {
		t.Fatal("did not keep waiting while alice was online")
	}
	x.g.setPlayers()
	x.stepAt(at(4, 40))
	if x.s.State().Phase != PhaseCountdown {
		t.Fatalf("phase = %q, want countdown once nobody is online", x.s.State().Phase)
	}
	x.g.waitEntered(t)
	x.g.finish(nil)
	x.waitPhase(PhaseNone)
}

func TestSkipsWhenPlayersStayPastTheLimit(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.g.setPlayers("alice")
	x.stepAt(at(4, 0))
	x.stepAt(at(4, 59))
	if x.s.State().Phase != PhaseWaiting {
		t.Fatal("skipped before the limit")
	}
	x.stepAt(at(5, 0))
	st := x.s.State()
	if st.Phase != PhaseSkipped || st.Reason != SkipPlayers || !st.SkippedAt.Equal(at(5, 0)) || !st.Next.Equal(at(4, 0).AddDate(0, 0, 1)) {
		t.Fatalf("state = %+v, want skipped for players, next tomorrow", st)
	}
	got := x.emits.all()
	if len(got) != 1 || !got[0].Skipped || got[0].Event != notify.ScheduledRestart {
		t.Errorf("events = %+v, want one skipped scheduled restart", got)
	}
	if r, _ := x.g.count(); r != 0 {
		t.Error("restarted with a player online")
	}
}

func TestSkippedStaysUntilTheNextSetTime(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.g.setPlayers("alice")
	x.stepAt(at(4, 0))
	x.stepAt(at(5, 0))
	x.stepAt(at(20, 0))
	if x.s.State().Phase != PhaseSkipped {
		t.Fatal("the skipped note went before the next set time")
	}
	x.stepAt(at(4, 0).AddDate(0, 0, 1))
	if st := x.s.State(); st.Phase != PhaseWaiting {
		t.Errorf("state = %+v, want the next day's wait", st)
	}
}

func TestLimitZeroSkipsAtOnce(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(0))
	x.g.setPlayers("alice")
	x.stepAt(at(4, 0))
	if st := x.s.State(); st.Phase != PhaseSkipped || st.Reason != SkipPlayers {
		t.Fatalf("state = %+v, want skipped at once", st)
	}
	if got := x.emits.all(); len(got) != 1 || !got[0].Skipped {
		t.Errorf("events = %+v", got)
	}
}

func TestLimitZeroRunsWhenNobodyIsOnline(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(0))
	x.stepAt(at(4, 0))
	if x.s.State().Phase != PhaseCountdown {
		t.Fatal("a zero limit did not restart an empty server")
	}
	x.g.waitEntered(t)
	x.g.finish(nil)
	x.waitPhase(PhaseNone)
}

func TestRCONFailureCountsAsSomeoneOnline(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.g.set(func(f *fakeGame) { f.playersErr = game.ErrRCONUnreachable })
	x.stepAt(at(4, 0))
	st := x.s.State()
	if st.Phase != PhaseWaiting || !st.Unasked {
		t.Fatalf("state = %+v, want waiting with the game unasked", st)
	}
	x.stepAt(at(5, 0))
	if st := x.s.State(); st.Phase != PhaseSkipped {
		t.Fatalf("state = %+v, want skipped", st)
	}
	if r, _ := x.g.count(); r != 0 {
		t.Error("restarted without knowing who was online")
	}
}

func TestRCONFailureDuringTheCountdownCancelsIt(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.stepAt(at(4, 0))
	x.g.waitEntered(t)
	x.g.set(func(f *fakeGame) { f.playersErr = game.ErrRCONUnreachable })
	x.stepAt(at(4, 0).Add(5 * time.Second))
	st := x.waitPhase(PhaseWaiting)
	if st.Joined != "" {
		t.Errorf("joined = %q, want nobody named", st.Joined)
	}
	if _, c := x.g.count(); c != 1 {
		t.Errorf("cancels = %d, want 1", c)
	}
}

func TestJoinDuringTheCountdownCancelsAndWaitsAgain(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.stepAt(at(4, 0))
	x.g.waitEntered(t)
	x.g.setPlayers("bob")
	x.stepAt(at(4, 0).Add(20 * time.Second))
	st := x.waitPhase(PhaseWaiting)
	if st.Joined != "bob" || !st.Until.Equal(at(5, 0)) {
		t.Errorf("state = %+v, want waiting again until 05:00 after bob joined", st)
	}
	x.g.set(func(f *fakeGame) {
		if f.actors[1] != Actor {
			t.Errorf("cancel by %+v, want the schedule", f.actors[1])
		}
	})
	if len(x.emits.all()) != 0 {
		t.Error("an event was sent for a countdown the schedule cancelled")
	}
	// Bob leaves: the countdown begins again.
	x.g.setPlayers()
	x.stepAt(at(4, 10))
	if x.s.State().Phase != PhaseCountdown {
		t.Fatal("did not count down again once bob left")
	}
	x.g.waitEntered(t)
	x.g.finish(nil)
	x.waitPhase(PhaseNone)
	if r, c := x.g.count(); r != 2 || c != 1 {
		t.Errorf("restarts, cancels = %d, %d; want 2, 1", r, c)
	}
}

func TestAnAdminsCancelSkipsToday(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.stepAt(at(4, 0))
	x.g.waitEntered(t)
	x.g.finish(game.ErrStopCancelled)
	st := x.waitPhase(PhaseSkipped)
	if st.Reason != SkipAdmin {
		t.Errorf("reason = %q, want admin", st.Reason)
	}
	if got := x.emits.all(); len(got) != 1 || !got[0].Skipped {
		t.Errorf("events = %+v, want one skipped", got)
	}
	x.stepAt(at(4, 30))
	if r, _ := x.g.count(); r != 1 {
		t.Error("restarted again after an admin cancelled")
	}
}

func TestAFailedRestartEndsTheDay(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.stepAt(at(4, 0))
	x.g.waitEntered(t)
	x.g.finish(game.ErrStartFailed)
	x.waitPhase(PhaseNone)
	x.stepAt(at(4, 30))
	if r, _ := x.g.count(); r != 1 {
		t.Error("tried again the same day")
	}
}

func TestARestartByHandEndsTheWait(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.g.setPlayers("alice")
	x.stepAt(at(4, 0))
	x.g.set(func(f *fakeGame) { f.status.StartedAt = at(4, 5) })
	x.stepAt(at(4, 6))
	if st := x.s.State(); st.Phase != PhaseNone {
		t.Errorf("state = %+v, want the wait ended", st)
	}
	if len(x.emits.all()) != 0 {
		t.Error("an event was sent for a wait a restart by hand ended")
	}
}

func TestAStopDuringTheWaitEndsIt(t *testing.T) {
	for _, state := range []game.State{game.StateStopped, game.StateCrashed, game.StateCrashLooping} {
		x := newRig(t, at(3, 0), nightly(1))
		x.g.setPlayers("alice")
		x.stepAt(at(4, 0))
		x.g.set(func(f *fakeGame) { f.status.State = state; f.players = nil })
		x.stepAt(at(4, 10))
		if r, _ := x.g.count(); r != 0 || x.s.State().Phase != PhaseNone {
			t.Errorf("%s: restarts = %d, state = %+v", state, r, x.s.State())
		}
	}
}

func TestWaitsWhileAnotherOperationRuns(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.g.set(func(f *fakeGame) { f.status.Operation = &game.Operation{} })
	x.stepAt(at(4, 0))
	if r, _ := x.g.count(); r != 0 || x.s.State().Phase != PhaseWaiting {
		t.Fatal("began a restart while another operation ran")
	}
	x.g.set(func(f *fakeGame) { f.status.Operation.Done = true })
	x.stepAt(at(4, 1))
	if x.s.State().Phase != PhaseCountdown {
		t.Fatal("did not restart once the operation ended")
	}
	x.g.waitEntered(t)
	x.g.finish(nil)
	x.waitPhase(PhaseNone)
}

func TestAMissedRestartIsNotMadeUp(t *testing.T) {
	// Foreman starts after the set time: the next is tomorrow.
	x := newRig(t, at(4, 30), nightly(1))
	if got := x.s.State().Next; !got.Equal(at(4, 0).AddDate(0, 0, 1)) {
		t.Fatalf("next = %v, want tomorrow 04:00", got)
	}
	x.stepAt(at(4, 31))
	if r, _ := x.g.count(); r != 0 || x.s.State().Phase != PhaseNone {
		t.Fatal("made up a missed restart at startup")
	}

	// The loop wakes long after the set time, as after the host slept.
	y := newRig(t, at(3, 0), nightly(1))
	y.stepAt(at(4, 0).Add(missedAfter + time.Minute))
	if r, _ := y.g.count(); r != 0 || y.s.State().Phase != PhaseNone {
		t.Fatal("made up a restart missed while the loop did not run")
	}
	if got := y.s.State().Next; !got.Equal(at(4, 0).AddDate(0, 0, 1)) {
		t.Errorf("next = %v, want tomorrow", got)
	}
}

func TestTurningOffEndsAWait(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	x.g.setPlayers("alice")
	x.stepAt(at(4, 0))
	off := *nightly(1)
	off.Enabled = false
	if err := x.s.Set(x.ctx, off); err != nil {
		t.Fatal(err)
	}
	if st := x.s.State(); st.Phase != PhaseNone || !st.Next.IsZero() {
		t.Errorf("state = %+v, want nothing scheduled", st)
	}
	x.stepAt(at(4, 0).AddDate(0, 0, 1))
	if x.s.State().Phase != PhaseNone {
		t.Error("an off schedule began a wait")
	}
}

func TestSetSavesAndRefuses(t *testing.T) {
	x := newRig(t, at(3, 0), nil)
	r, found := x.s.Settings()
	if found || r.Enabled || r.Minute != DefaultMinute || r.Zone != UTC || r.LimitHours != DefaultLimitHours {
		t.Fatalf("defaults = %+v, %v", r, found)
	}
	if !x.s.State().Next.IsZero() {
		t.Error("a schedule never saved has a next restart")
	}
	for _, c := range []struct {
		r    store.RestartSchedule
		want error
	}{
		{store.RestartSchedule{Minute: 24 * 60, Zone: UTC}, ErrMinute},
		{store.RestartSchedule{Minute: -1, Zone: UTC}, ErrMinute},
		{store.RestartSchedule{Zone: "Mars/Olympus"}, ErrZone},
		{store.RestartSchedule{Zone: "Local"}, ErrZone},
		{store.RestartSchedule{Zone: ""}, ErrZone},
		{store.RestartSchedule{Zone: "../etc/passwd"}, ErrZone},
		{store.RestartSchedule{Zone: UTC, LimitHours: 13}, ErrLimit},
		{store.RestartSchedule{Zone: UTC, LimitHours: -1}, ErrLimit},
	} {
		if err := x.s.Set(x.ctx, c.r); !errors.Is(err, c.want) {
			t.Errorf("Set(%+v) = %v, want %v", c.r, err, c.want)
		}
	}
	want := store.RestartSchedule{Enabled: true, Minute: 2*60 + 30, Zone: "Europe/London", LimitHours: 12, UpdatedBy: "todd"}
	if err := x.s.Set(x.ctx, want); err != nil {
		t.Fatal(err)
	}
	saved, found, err := x.st.RestartSchedule(x.ctx)
	if err != nil || !found || saved.Minute != want.Minute || saved.Zone != want.Zone || saved.LimitHours != 12 || !saved.Enabled || saved.UpdatedBy != "todd" {
		t.Errorf("saved = %+v, %v, %v", saved, found, err)
	}
	if got, _ := x.s.Settings(); got != saved {
		t.Errorf("settings = %+v, want %+v", got, saved)
	}
	// 02:30 in London in November is 02:30 UTC.
	if got := x.s.State().Next; !got.Equal(at(2, 30).AddDate(0, 0, 1)) {
		t.Errorf("next = %v", got)
	}
}

func TestRunStopsWithItsContext(t *testing.T) {
	x := newRig(t, at(3, 0), nightly(1))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { x.s.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}
