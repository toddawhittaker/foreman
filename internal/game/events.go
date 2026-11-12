package game

import (
	"time"

	"github.com/toddawhittaker/foreman/internal/game/crashreasons"
	"github.com/toddawhittaker/foreman/internal/notify"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Low disk space (SPEC 8.13): a warning below twice the write reserve of
// SPEC 6.11, cleared only above a higher mark so it does not flap.
const (
	LowDiskBelow = 2 * world.SpaceReserve
	LowDiskClear = LowDiskBelow + 500_000_000
)

// eventBuffer is how many events may wait for the notifier before more are
// dropped.
const eventBuffer = 256

// Notifier takes the events Foreman reports (SPEC 8.10). *notify.Notifier
// satisfies it.
type Notifier interface {
	Emit(m notify.Message)
}

// SetNotifier sets where events go. Without one they are dropped.
func (c *Controller) SetNotifier(n Notifier) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.notifier = n
}

func (c *Controller) notifierNow() Notifier {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.notifier
}

// Emit reports an event about the world m.World.ID names; the world's name
// is filled in. It never blocks: when too many events wait, m is dropped
// and logged (SPEC 8.12).
func (c *Controller) Emit(m notify.Message) {
	if m.Time.IsZero() {
		m.Time = c.now()
	}
	select {
	case c.events <- m:
	default:
		if c.eventsFull.CompareAndSwap(false, true) {
			c.log.Warn("notifications dropped: too many are waiting; more are dropped without a log line until they drain",
				"event", string(m.Event), "world", m.World.ID)
		}
	}
}

// emitLocked reports an event about w. w.mu is held.
func (c *Controller) emitLocked(w *worldRun, m notify.Message) {
	m.World.ID = w.id
	c.Emit(m)
}

// deliverEvents hands queued events to the notifier until the controller
// shuts down. The notifier may be slow; nothing else waits for it.
func (c *Controller) deliverEvents() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case m := <-c.events:
			c.eventsFull.Store(false)
			n := c.notifierNow()
			if n == nil {
				continue
			}
			if m.World.Name == "" {
				m.World.Name = c.worldName(m.World.ID)
			}
			n.Emit(m)
		}
	}
}

// noteEventsLocked reports a start, stop, crash or crash loop when the
// game's state moved to one. The first state known is only noted, so a
// game taken on after a Foreman restart, or a log line from before it,
// never reports again (SPEC 3.8, 8.10). w.mu is held.
func (c *Controller) noteEventsLocked(w *worldRun) {
	st, _ := c.gameStateLocked(w, true)
	if st == StateUnknown {
		return
	}
	prev := w.evState
	w.evState = st
	if prev == "" || prev == st || w.evQuiet {
		return
	}
	switch st {
	case StateRunning:
		// A game that began before this Foreman did was reported by the
		// one before it.
		if w.startedAt.After(c.eventsFrom) {
			c.emitLocked(w, notify.Message{Event: notify.Start})
		}
	case StateStopped:
		if prev == StateRunning || prev == StateStopping {
			c.emitLocked(w, notify.Message{Event: notify.Stop})
		}
	case StateCrashed:
		c.emitLocked(w, notify.Message{Event: notify.Crash, Reason: c.crashLocked(w).Kind})
	case StateCrashLooping:
		c.emitLocked(w, notify.Message{Event: notify.CrashLoop, Reason: c.crashLocked(w).Kind})
	}
}

// liveLineLocked reports whether a log line Docker stamped at is newer than
// any line seen before it and than this Foreman, so what it says may be
// reported. A re-attach replays the run's log, which must not report
// again. w.mu is held.
func (w *worldRun) liveLineLocked(at time.Time) bool {
	if at.IsZero() || !at.After(w.eventsAt) {
		return false
	}
	w.eventsAt = at
	return true
}

// crashLocked is the reason for the current crash, from the log of the
// current container and Docker's out-of-memory flag (SPEC 8.7). w.mu is
// held.
func (c *Controller) crashLocked(w *worldRun) LoadReason {
	var f loadFailure
	for _, l := range w.logs.LinesSince(w.logMark) {
		f.add(l)
	}
	r := f.reason(w.redact)
	if w.oomKilled {
		r.Kind = crashreasons.OutOfMemory
	}
	return r
}

// checkDisk reads the free space where worlds are kept while the game runs,
// and warns once each time it falls below LowDiskBelow (SPEC 8.13).
func (c *Controller) checkDisk(w *worldRun, cid string) {
	free, err := c.freeBytes(c.worlds.Dirs().Worlds)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.containerID != cid {
		return
	}
	if err != nil {
		if !w.diskErr {
			c.log.Warn("cannot read the free disk space", "world", w.id, "error", err)
		}
		w.diskErr = true
		return
	}
	w.diskErr = false
	w.diskFree = free
	switch {
	case !w.lowDisk && free < LowDiskBelow:
		w.lowDisk = true
		c.emitLocked(w, notify.Message{Event: notify.LowDisk, FreeBytes: free})
		c.changedLocked(w, true)
	case w.lowDisk && free > LowDiskClear:
		w.lowDisk = false
		c.changedLocked(w, true)
	}
}
