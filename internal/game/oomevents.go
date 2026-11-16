package game

import (
	"context"
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
	var since time.Time
	oomLast := map[string]bool{} // by container: its newest event was oom
	for {
		opened := time.Now()
		s, err := c.docker.Events(ctx, docker.EventOptions{
			Actions: []string{"oom", "die"},
			Labels:  []string{LabelInstall + "=" + c.hostData},
			Since:   since,
		})
		if docker.EventsRefused(err) {
			c.log.Info("Docker events are not allowed by the socket proxy, so Foreman finds out-of-memory kills by polling and can miss one; set EVENTS=1 on the proxy to name every one", "error", err)
			return
		}
		if err == nil {
			retry = eventRetryFirst
			if opened.After(since) {
				since = opened
			}
			for {
				ev, err := s.Next()
				if err != nil {
					break
				}
				if ev.Time.After(since) {
					since = ev.Time
				}
				c.dockerEvent(ev, oomLast)
			}
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
// time to Docker's, so a later look at the same exit changes nothing. An
// event no newer than what is known, replayed after a reconnect say, is
// passed over.
func (c *Controller) dockerEvent(ev docker.Event, oomLast map[string]bool) {
	wasOOM := oomLast[ev.ID]
	if ev.Action == "oom" {
		oomLast[ev.ID] = true
	} else {
		delete(oomLast, ev.ID)
	}
	for _, w := range c.allRuns() {
		w.mu.Lock()
		if w.containerID == ev.ID && ev.Time.After(w.exitAt) {
			switch {
			case ev.Action == "oom":
				w.oomKilled, w.exitAt = true, ev.Time
				c.changedLocked(w, true)
			case ev.Action == "die" && wasOOM && w.oomKilled:
				w.exitAt = ev.Time
			}
		}
		w.mu.Unlock()
	}
}
