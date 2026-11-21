package game

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
)

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
	var logs lockedBuffer
	r := newRig(t, rigOptions{noRun: true, logs: &logs})
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

// SPEC 8.7: the start can see the log end before Docker's events reach
// Foreman; Docker sends oom before die. With the stream live, the start
// waits for the die of the exit it saw, and so still names the kill.
func TestStartWaitsForTheDieEventOfTheExitItSaw(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	d := r.game.Daemon()
	d.AllowEvents(true)
	id := r.newWorld("Late events")
	r.start(id)
	cid := r.container(id).ID
	w := r.c.world(id)
	watch(t, r.c)
	eventually(t, func() bool { return r.c.eventsLive.Load() }, "the event stream to open")

	d.HoldEvents(true)
	d.Exit(cid, 137, true)
	d.Restart(cid) // Docker clears the flag
	got := make(chan *StartError, 1)
	go func() { got <- r.c.stoppedWhileLoading(context.Background(), w, cid) }()
	time.Sleep(300 * time.Millisecond)
	d.HoldEvents(false)
	if serr := <-got; serr.Reason.Kind != crashreasons.OutOfMemory {
		t.Errorf("start error reason %q, want %q", serr.Reason.Kind, crashreasons.OutOfMemory)
	}
}

// Without a live stream the start does not wait.
func TestStartDoesNotWaitForDieWithoutEvents(t *testing.T) {
	r := newRig(t, rigOptions{noRun: true})
	id := r.newWorld("No events")
	r.start(id)
	cid := r.container(id).ID
	began := time.Now()
	r.c.awaitDie(context.Background(), r.c.world(id), cid, time.Now())
	if time.Since(began) > 100*time.Millisecond {
		t.Errorf("waited %v with no event stream", time.Since(began))
	}
}

// eventsLogFault runs a controller whose every event stream gets status and
// body, until the watcher has asked twice, and returns the controller's log.
func eventsLogFault(t *testing.T, status int, body string) string {
	t.Helper()
	var logs lockedBuffer
	r := newRig(t, rigOptions{noRun: true})
	var asked atomic.Int32
	r.proxyDocker(rigOptions{logs: &logs}, func(w http.ResponseWriter, req *http.Request, next http.Handler) {
		if !strings.HasSuffix(req.URL.Path, "/events") {
			next.ServeHTTP(w, req)
			return
		}
		asked.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	eventually(t, func() bool { return asked.Load() >= 2 }, "the watcher to ask again")
	r.stop()
	return logs.String()
}

// A stream that cannot open is logged once while the same fault lasts,
// not at every retry.
func TestEventStreamThatCannotOpenIsLoggedOnce(t *testing.T) {
	got := eventsLogFault(t, http.StatusInternalServerError, `{"message":"daemon busy"}`)
	if n := strings.Count(got, "cannot follow Docker events"); n != 1 {
		t.Errorf("logged %d times, want once:\n%s", n, got)
	}
	if !strings.Contains(got, "daemon busy") {
		t.Errorf("log %q, want Docker's words", got)
	}
}

// A stream that opens and then breaks off is logged once while the same
// fault lasts, like one that cannot open.
func TestEventStreamThatBreaksIsLoggedOnce(t *testing.T) {
	got := eventsLogFault(t, http.StatusOK, `{"Type":"container","Action":`)
	if n := strings.Count(got, "cannot follow Docker events"); n != 1 {
		t.Errorf("logged %d times, want once:\n%s", n, got)
	}
	if !strings.Contains(got, "broke off") {
		t.Errorf("log %q, want the break", got)
	}
}

// A stream that simply ends, as when a proxy drops an idle connection, or
// that shutdown cuts, is no fault to log.
func TestEventStreamThatEndsIsNotLogged(t *testing.T) {
	var logs lockedBuffer
	r := newRig(t, rigOptions{noRun: true, logs: &logs})
	d := r.game.Daemon()
	d.AllowEvents(true)
	stop := watch(t, r.c)
	eventually(t, func() bool { return d.OpenEventStreams() == 1 }, "the event stream to open")
	d.CutEvents()
	eventually(t, func() bool { return d.EventStreams() == 2 && d.OpenEventStreams() == 1 }, "the stream to reopen")
	stop()
	if got := logs.String(); strings.Contains(got, "cannot follow Docker events") {
		t.Errorf("an ordinary end was logged:\n%s", got)
	}
}
