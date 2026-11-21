package game

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// Retry waits for a dropped Docker event stream, doubling from the first.
const (
	eventRetryFirst = time.Second
	eventRetryMost  = 30 * time.Second
)

// watchOOM follows Docker's oom and die events for this install's games
// until ctx ends (SPEC 8.7, 14.3). Docker clears a container's
// out-of-memory flag when it restarts the game, often before any poll or
// look at the exit sees it; the oom event is never missed that way. When
// the socket proxy refuses events, polling alone goes on, as before, and
// this says so in the log once.
func (c *Controller) watchOOM(ctx context.Context) {
	retry := eventRetryFirst
	var since time.Time          // the newest event's time; zero asks for new events only
	oomLast := map[string]bool{} // by container: its newest event was oom
	var failed error             // the last failure to open the stream; nil once one opens
	var broke error              // the last error an open stream ended with; nil once one works
	for {
		s, err := c.docker.Events(ctx, docker.EventOptions{
			Actions: []string{"oom", "die"},
			Labels:  []string{LabelInstall + "=" + c.hostData},
			Since:   since,
		})
		if docker.EventsRefused(err) {
			c.log.Info("Docker events are not allowed by the socket proxy, so Foreman finds out-of-memory kills by polling and can miss one; set EVENTS=1 on the proxy to name every one", "error", err)
			return
		}
		if err != nil && ctx.Err() == nil {
			c.logFault("cannot follow Docker events", failed, err)
			failed = err
		}
		if err == nil {
			retry, failed = eventRetryFirst, nil
			c.eventsLive.Store(true)
			for {
				ev, err := s.Next()
				if errors.Is(err, io.EOF) {
					broke = nil
					break
				}
				if err != nil {
					// Shutdown cuts the stream too; that is no fault.
					if ctx.Err() == nil {
						c.logFault("cannot follow Docker events", broke, err)
						broke = err
					}
					break
				}
				broke = nil
				if ev.Time.After(since) {
					since = ev.Time
				}
				c.dockerEvent(ev, oomLast)
			}
			c.eventsLive.Store(false)
			_ = s.Close()
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retry):
		}
		retry = min(retry*2, eventRetryMost)
	}
}

// dockerEvent takes in one event for a game container. An oom event names
// the exit that follows it, and that exit's die event moves the exit's
// time to Docker's, so a later look at the same exit changes nothing. Every
// die is recorded, so a start can wait for it. An event no newer than what
// is known, replayed after a reconnect say, is passed over.
func (c *Controller) dockerEvent(ev docker.Event, oomLast map[string]bool) {
	wasOOM := oomLast[ev.ID]
	if ev.Action == "oom" {
		oomLast[ev.ID] = true
	} else {
		delete(oomLast, ev.ID)
	}
	for _, w := range c.allRuns() {
		w.mu.Lock()
		if w.containerID == ev.ID {
			if ev.Action == "die" && ev.Time.After(w.lastDie) {
				w.lastDie = ev.Time
				c.changedLocked(w, false)
			}
			if ev.Time.After(w.exitAt) {
				switch {
				case ev.Action == "oom":
					w.oomKilled, w.exitAt = true, ev.Time
					c.changedLocked(w, true)
				case ev.Action == "die" && wasOOM && w.oomKilled:
					w.exitAt = ev.Time
				}
			}
		}
		w.mu.Unlock()
	}
}

// dieWaitLimit bounds how long a start waits for the die event of an exit
// it has seen, since Docker sends oom before die.
const dieWaitLimit = 2 * time.Second

// awaitDie waits, while the event stream is open, until the die event for
// the container's exit at finished has arrived, for at most dieWaitLimit
// or until ctx ends. Docker stamps the die no earlier than FinishedAt.
func (c *Controller) awaitDie(ctx context.Context, w *worldRun, cid string, finished time.Time) {
	if !c.eventsLive.Load() || finished.IsZero() {
		return
	}
	limit := time.NewTimer(dieWaitLimit)
	defer limit.Stop()
	for {
		w.mu.Lock()
		got := w.containerID != cid || !w.lastDie.Before(finished)
		changed := w.changed
		w.mu.Unlock()
		if got {
			return
		}
		select {
		case <-changed:
		case <-limit.C:
			return
		case <-ctx.Done():
			return
		}
	}
}
