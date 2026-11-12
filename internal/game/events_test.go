package game

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
	"github.com/toddawhittaker/foreman/internal/notify"
)

// recNotifier records what it is sent. While hang is open, Emit blocks.
type recNotifier struct {
	mu   sync.Mutex
	msgs []notify.Message
	hang chan struct{}
}

func (n *recNotifier) Emit(m notify.Message) {
	if n.hang != nil {
		<-n.hang
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.msgs = append(n.msgs, m)
}

func (n *recNotifier) all() []notify.Message {
	n.mu.Lock()
	defer n.mu.Unlock()
	return slices.Clone(n.msgs)
}

func (n *recNotifier) events() []notify.Event {
	var out []notify.Event
	for _, m := range n.all() {
		out = append(out, m.Event)
	}
	return out
}

func (n *recNotifier) count(e notify.Event) int {
	k := 0
	for _, m := range n.all() {
		if m.Event == e {
			k++
		}
	}
	return k
}

// waitEvents waits until the notifier has been sent want, in order, and
// checks that nothing more arrives in the next few polls.
func waitEvents(t *testing.T, n *recNotifier, want ...notify.Event) {
	t.Helper()
	eventually(t, func() bool { return len(n.events()) >= len(want) }, "events %v, got %v", want, func() any { return n.events() })
	time.Sleep(150 * time.Millisecond)
	if got := n.events(); !slices.Equal(got, want) {
		t.Fatalf("events %v, want %v", got, want)
	}
}

func notifiedRig(t *testing.T) (*rig, *recNotifier) {
	r := newRig(t, rigOptions{})
	n := &recNotifier{}
	r.c.SetNotifier(n)
	return r, n
}

// SPEC 8.10: a start and a stop are each reported once, naming the world.
func TestEventsStartAndStop(t *testing.T) {
	r, n := notifiedRig(t)
	id := r.newWorld("Nauvis base")
	r.start(id)
	waitEvents(t, n, notify.Start)
	if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	waitEvents(t, n, notify.Start, notify.Stop)
	for _, m := range n.all() {
		if m.World.ID != id || m.World.Name != "Nauvis base" || m.Time.IsZero() {
			t.Errorf("event %s names %+v at %v", m.Event, m.World, m.Time)
		}
	}
}

// SPEC 8.10: a restart is a stop and then a start; a forced stop is a stop.
func TestEventsRestartAndForceStop(t *testing.T) {
	r, n := notifiedRig(t)
	id := r.newWorld("Restarted")
	r.start(id)
	if err := r.c.Restart(context.Background(), id, admin, StopOptions{}, StartOptions{}); err != nil {
		t.Fatal(err)
	}
	waitEvents(t, n, notify.Start, notify.Stop, notify.Start)
	if err := r.c.ForceStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	waitEvents(t, n, notify.Start, notify.Stop, notify.Start, notify.Stop)
}

// SPEC 7.6: a stop canceled during its countdown reports nothing.
func TestEventsCanceledStopReportsNothing(t *testing.T) {
	r, n := notifiedRig(t)
	id := r.newWorld("Kept")
	r.start(id)
	if err := r.game.Join(ContainerName(id), "dana"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- r.c.Stop(context.Background(), id, admin, StopOptions{Countdown: time.Minute}) }()
	eventually(t, func() bool { _, ok := r.c.Status(id).Operation.CountdownEnds(); return ok }, "the countdown")
	if err := r.c.CancelStop(context.Background(), id, admin); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrStopCancelled) {
		t.Fatalf("stop = %v", err)
	}
	waitEvents(t, n, notify.Start, notify.PlayerJoin)
}

// SPEC 8.10: joins and leaves come from the game's log lines.
func TestEventsJoinAndLeave(t *testing.T) {
	r, n := notifiedRig(t)
	id := r.newWorld("Players")
	r.start(id)
	if err := r.game.Join(ContainerName(id), "carol"); err != nil {
		t.Fatal(err)
	}
	if err := r.game.Leave(ContainerName(id), "carol"); err != nil {
		t.Fatal(err)
	}
	waitEvents(t, n, notify.Start, notify.PlayerJoin, notify.PlayerLeave)
	for _, m := range n.all()[1:] {
		if m.Player != "carol" {
			t.Errorf("%s names player %q", m.Event, m.Player)
		}
	}
}

// SPEC 8.7, 8.9, 8.10: a crash is reported with its reason, and a crash
// loop once more.
func TestEventsCrashAndCrashLoop(t *testing.T) {
	r, n := notifiedRig(t)
	id := r.newWorld("Crashy")
	r.start(id)
	if err := r.game.Crash(ContainerName(id), 137, true); err != nil {
		t.Fatal(err)
	}
	r.waitState(id, StateRunning) // Docker brought it back
	waitEvents(t, n, notify.Start, notify.Crash, notify.Start)
	if got := n.all()[1].Reason; got != crashreasons.OutOfMemory {
		t.Errorf("crash reason %q, want %q", got, crashreasons.OutOfMemory)
	}

	r.game.SetCrashLoop(ContainerName(id), true)
	r.waitState(id, StateCrashLooping)
	eventually(t, func() bool { return r.container(id).Status == "exited" }, "the container to stop")
	waitEvents(t, n, notify.Start, notify.Crash, notify.Start, notify.Crash, notify.CrashLoop)
	if got, want := n.all()[4].Reason, r.c.Status(id).Crash.Kind; got != want {
		t.Errorf("crash loop reason %q, the status says %q", got, want)
	}

	// Starting it again by hand is a start, not a stop of the crash loop.
	r.game.SetCrashLoop(ContainerName(id), false)
	r.start(id)
	waitEvents(t, n, notify.Start, notify.Crash, notify.Start, notify.Crash, notify.CrashLoop, notify.Start)
}

// Invariant: after a Foreman restart nothing that happened before it is
// sent again, and what happens after it is sent once.
func TestEventsNotRepeatedAfterReattach(t *testing.T) {
	r, n := notifiedRig(t)
	id := r.newWorld("Survive")
	crashed := r.newWorld("Crashed before")
	r.start(id)
	for _, p := range []string{"carol", "dave"} {
		if err := r.game.Join(ContainerName(id), p); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.game.Leave(ContainerName(id), "dave"); err != nil {
		t.Fatal(err)
	}
	r.startAsActive(crashed)
	r.game.SetCrashLoop(ContainerName(crashed), true)
	r.waitState(crashed, StateCrashLooping)
	eventually(t, func() bool { return r.container(crashed).Status == "exited" }, "the container to stop")
	r.game.SetCrashLoop(ContainerName(crashed), false)
	eventually(t, func() bool { return n.count(notify.CrashLoop) == 1 }, "the first crash loop")
	r.stop()

	n2 := &recNotifier{}
	c2 := r.newController(rigOptions{})
	c2.SetNotifier(n2)
	r.run(c2)
	eventually(t, func() bool { return c2.Status(id).State == StateRunning }, "re-attach")
	eventually(t, func() bool { return len(c2.Lines(id)) > 0 }, "the replayed log")
	time.Sleep(300 * time.Millisecond) // many polls, and the whole log replayed
	if got := n2.events(); len(got) != 0 {
		t.Fatalf("after the restart Foreman sent again: %v", got)
	}

	if err := r.game.Leave(ContainerName(id), "carol"); err != nil {
		t.Fatal(err)
	}
	waitEvents(t, n2, notify.PlayerLeave)
	if err := c2.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatal(err)
	}
	waitEvents(t, n2, notify.PlayerLeave, notify.Stop)
}

// Invariant: a crash Foreman finds Docker restarting when it comes back
// was reported by the Foreman that saw it, and is not reported again.
func TestEventsCrashBeforeRestartNotRepeated(t *testing.T) {
	r, n := notifiedRig(t)
	id := r.newWorld("Restarting")
	r.start(id)
	r.stop()
	// The crash happens while no Foreman watches; Docker restarts the game
	// into a failed load.
	r.game.SetCrashLoop(ContainerName(id), true)
	eventually(t, func() bool { return r.container(id).RestartCount >= 1 }, "Docker to restart it")
	n2 := &recNotifier{}
	c2 := r.newController(rigOptions{})
	c2.SetNotifier(n2)
	r.run(c2)
	eventually(t, func() bool { s := c2.Status(id).State; return s == StateCrashed || s == StateCrashLooping }, "the crash to show")
	time.Sleep(200 * time.Millisecond)
	for _, e := range n2.events() {
		if e != notify.CrashLoop {
			t.Errorf("the restarted Foreman sent %s for what happened before it", e)
		}
	}
	if got := n.events(); !slices.Equal(got, []notify.Event{notify.Start}) {
		t.Errorf("first Foreman sent %v", got)
	}
}

// SPEC 8.12: a notifier that hangs never delays a start, a stop or the
// monitor, and a full queue drops events rather than waiting.
func TestEmitNeverBlocks(t *testing.T) {
	r := newRig(t, rigOptions{})
	n := &recNotifier{hang: make(chan struct{})}
	defer close(n.hang)
	r.c.SetNotifier(n)
	id := r.newWorld("Hung")
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.start(id)
		for i := range 2 * eventBuffer {
			p := "p" + string(rune('a'+i%26)) + string(rune('a'+i/26))
			if err := r.game.Join(ContainerName(id), p); err != nil {
				t.Error(err)
				return
			}
		}
		for range 2 * eventBuffer {
			r.c.Emit(notify.Message{Event: notify.ScheduledRestart, World: notify.World{ID: id}})
		}
		if err := r.c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a hung notifier held up the game")
	}
	r.waitState(id, StateStopped)
}

// SPEC 8.13: low disk is reported once each time free space falls below
// 4 GB, and clears only above 4.5 GB.
func TestLowDiskHysteresis(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	var free atomic.Uint64
	free.Store(10_000_000_000)
	r.c.freeBytes = func(string) (uint64, error) { return free.Load(), nil }
	n := &recNotifier{}
	r.c.SetNotifier(n)
	r.stop = r.run(r.c)
	id := r.newWorld("Disk")
	r.start(id)
	polls := func() { time.Sleep(150 * time.Millisecond) }
	set := func(v uint64) { free.Store(v); polls() }
	low := func() bool { return r.c.Status(id).LowDisk }

	set(LowDiskBelow + 1)
	if low() || n.count(notify.LowDisk) != 0 {
		t.Fatal("warned above 4 GB")
	}
	set(LowDiskBelow - 1)
	eventually(t, low, "low disk shown")
	if st := r.c.Status(id); st.FreeBytes != LowDiskBelow-1 {
		t.Errorf("free bytes %d", st.FreeBytes)
	}
	set(1_000_000_000)
	set(LowDiskBelow + 100_000_000) // between the marks: still low, no new event
	if !low() {
		t.Error("cleared between 4 and 4.5 GB")
	}
	set(LowDiskBelow - 1)
	if got := n.count(notify.LowDisk); got != 1 {
		t.Fatalf("%d low-disk events in one crossing, want 1", got)
	}
	var m notify.Message
	for _, x := range n.all() {
		if x.Event == notify.LowDisk {
			m = x
		}
	}
	if m.FreeBytes != LowDiskBelow-1 || m.World.ID != id {
		t.Errorf("low disk event %+v", m)
	}
	set(LowDiskClear + 1)
	eventually(t, func() bool { return !low() }, "low disk cleared")
	set(LowDiskBelow - 1)
	eventually(t, func() bool { return n.count(notify.LowDisk) == 2 }, "a second crossing reported")
}

// SPEC 8.7: a start Docker refuses because the port is taken names that
// reason on the error and on the status, and reports no start or crash.
func TestStartPortInUse(t *testing.T) {
	r, n := notifiedRig(t)
	id := r.newWorld("Port")
	r.game.FailStartPortInUse(ContainerName(id), "34197")
	err := r.c.Start(context.Background(), id, admin, StartOptions{})
	se, ok := errors.AsType[*StartError](err)
	if !ok || se.Kind != StartDockerRefused || se.Reason.Kind != crashreasons.PortInUse {
		t.Fatalf("start = %#v, want a port-in-use StartError", err)
	}
	if !errors.Is(err, ErrStartFailed) {
		t.Error("not ErrStartFailed")
	}
	st := r.c.Status(id)
	if st.State != StateStopped {
		t.Errorf("state %s", st.State)
	}
	if se, ok := errors.AsType[*StartError](st.Operation.Err); !ok || se.Reason.Kind != crashreasons.PortInUse {
		t.Errorf("status operation error %#v", st.Operation.Err)
	}
	time.Sleep(150 * time.Millisecond)
	if got := n.events(); len(got) != 0 {
		t.Errorf("a refused start sent %v", got)
	}
	r.start(id) // the next start works
	waitEvents(t, n, notify.Start)
}

// Invariant: a game already running when Foreman starts is not reported as
// a start, even when it reaches the game only afterwards.
func TestEventsNoStartForGameLoadingAtRestart(t *testing.T) {
	r := newRig(t, rigOptions{fake: fakegame.Config{LoadDelay: 400 * time.Millisecond}})
	id := r.newWorld("Loading")
	r.start(id)
	r.stop()
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return r.container(id).Status == "running" }, "Docker to restart it")
	n := &recNotifier{}
	c2 := r.newController(rigOptions{})
	c2.SetNotifier(n)
	r.run(c2)
	eventually(t, func() bool { return c2.Status(id).State == StateStarting }, "the re-attached game to be loading")
	eventually(t, func() bool { return c2.Status(id).State == StateRunning }, "the game to be in game")
	time.Sleep(150 * time.Millisecond)
	if got := n.events(); len(got) != 0 {
		t.Fatalf("sent %v for a game that began before this Foreman", got)
	}
}
