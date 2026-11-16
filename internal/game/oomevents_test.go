package game

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
)

// lockedBuffer is a log destination safe for the watcher's goroutine.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// watch runs the event watcher until the test ends, and fails the test if
// it is still running a moment after its context ends.
func watch(t *testing.T, c *Controller) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.watchOOM(ctx)
		close(done)
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("the event watcher kept running after its context ended")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

// SPEC 8.7: Docker clears the out-of-memory flag when it restarts the game,
// here before any look at the exit. The oom event still names the kill, so
// a start that stopped while loading says out of memory.
func TestOOMEventNamesAKillDockerAlreadyRestarted(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	d := r.game.Daemon()
	d.AllowEvents(true)
	id := r.newWorld("OOM event")
	r.start(id)
	cid := r.container(id).ID
	w := r.c.world(id)
	watch(t, r.c)
	eventually(t, func() bool { return d.OpenEventStreams() == 1 }, "the event stream to open")

	d.Exit(cid, 137, true)
	d.Restart(cid) // Docker clears the flag
	if cs, _ := d.Container(cid); cs.OOMKilled {
		t.Fatal("setup: the flag should be cleared")
	}
	eventually(t, func() bool { return oomOf(w) }, "the oom event to be taken in")
	serr := r.c.stoppedWhileLoading(context.Background(), w, cid)
	if serr.Reason.Kind != crashreasons.OutOfMemory {
		t.Errorf("start error reason %q, want %q", serr.Reason.Kind, crashreasons.OutOfMemory)
	}
}

// SPEC 14.3: events are optional. With the proxy refusing them the watcher
// says so in the log once, ends, and the monitor polls as before.
func TestRefusedEventsFallBackToPollingQuietly(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	var logs lockedBuffer
	r.c.log = slog.New(slog.NewTextHandler(&logs, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.c.watchOOM(ctx) // returns on the refusal
	if ctx.Err() != nil {
		t.Fatal("the watcher did not end when events were refused")
	}
	if n := strings.Count(logs.String(), "EVENTS=1"); n != 1 {
		t.Errorf("fallback logged %d times, want once:\n%s", n, logs.String())
	}

	// The full monitor still runs, and a start still works.
	r.stop = r.run(r.c)
	id := r.newWorld("Polling")
	r.start(id)
	if st := r.c.Status(id); st.State != StateRunning {
		t.Errorf("state %s, want %s", st.State, StateRunning)
	}
}

// A stream the proxy drops is opened again, and events on the new one
// still count. Ending the context ends the watcher and its stream.
func TestEventStreamReconnectsAndEndsWithItsContext(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	d := r.game.Daemon()
	d.AllowEvents(true)
	id := r.newWorld("Reconnect")
	r.start(id)
	cid := r.container(id).ID
	w := r.c.world(id)
	stop := watch(t, r.c)
	eventually(t, func() bool { return d.OpenEventStreams() == 1 }, "the event stream to open")

	d.CutEvents()
	eventually(t, func() bool { return d.EventStreams() == 2 && d.OpenEventStreams() == 1 }, "the stream to reopen")
	d.Exit(cid, 137, true)
	eventually(t, func() bool { return oomOf(w) }, "the oom event on the new stream")

	stop()
	eventually(t, func() bool { return d.OpenEventStreams() == 0 }, "the stream to close")
}

// Ending the context while the watcher waits to reconnect ends it too.
func TestEventWatcherEndsWhileWaitingToReconnect(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	d := r.game.Daemon()
	d.AllowEvents(true)
	stop := watch(t, r.c)
	eventually(t, func() bool { return d.OpenEventStreams() == 1 }, "the event stream to open")
	d.CutEvents()
	eventually(t, func() bool { return d.OpenEventStreams() == 0 }, "the stream to drop")
	stop()
}

// An event no newer than what is known, as a reconnect replays, changes
// nothing; the die after an oom keeps the kill named and takes Docker's
// time for the exit, so a later look at that exit adds nothing.
func TestDockerEventOrdering(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("Ordering")
	r.start(id)
	cid := r.container(id).ID
	w := r.c.world(id)
	t0 := time.Now().UTC()
	oomLast := map[string]bool{}

	w.mu.Lock()
	w.oomKilled, w.exitAt = false, t0
	w.mu.Unlock()
	r.c.dockerEvent(docker.Event{Action: "oom", ID: cid, Time: t0.Add(-time.Second)}, oomLast)
	if oomOf(w) {
		t.Fatal("a replayed oom older than the newest known exit was taken in")
	}
	r.c.dockerEvent(docker.Event{Action: "oom", ID: "someone-else", Time: t0.Add(time.Second)}, oomLast)
	if oomOf(w) {
		t.Fatal("another container's oom was taken in")
	}
	r.c.dockerEvent(docker.Event{Action: "oom", ID: cid, Time: t0.Add(time.Second)}, oomLast)
	r.c.dockerEvent(docker.Event{Action: "die", ID: cid, Time: t0.Add(2 * time.Second)}, oomLast)
	w.mu.Lock()
	oom, at := w.oomKilled, w.exitAt
	w.mu.Unlock()
	if !oom || !at.Equal(t0.Add(2*time.Second)) {
		t.Errorf("after oom and die: oom %v, exit at %v; want true, the die's time", oom, at)
	}
	if exitSeenLocked(w, docker.State{OOMKilled: false, FinishedAt: t0.Add(2 * time.Second)}) {
		t.Error("a look at the same exit overruled the event")
	}
}
