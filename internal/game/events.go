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
		c.flushCrashLocked(w, false)
		return
	}
	// Whatever happens next follows the crash, so the crash goes first.
	c.flushCrashLocked(w, true)
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
		w.crashEvent = &pendingCrash{event: notify.Crash, at: c.now()}
		c.flushCrashLocked(w, false)
	case StateCrashLooping:
		w.crashEvent = &pendingCrash{event: notify.CrashLoop, at: c.now()}
		c.flushCrashLocked(w, false)
	}
}

// pendingCrash is a crash event seen at at, waiting for its reason.
type pendingCrash struct {
	event notify.Event
	at    time.Time
}

// flushCrashLocked sends the waiting crash event once the log names its
// reason, once it has waited crashSettle, or at once when force is set.
// The poll looks again within crashSettle, so the event is never lost
// while Foreman runs. w.mu is held.
func (c *Controller) flushCrashLocked(w *worldRun, force bool) {
	p := w.crashEvent
	if p == nil {
		return
	}
	reason := c.crashLocked(w).Kind
	settle := c.crashSettle
	if settle == 0 {
		settle = 2 * c.pollInterval
	}
	if reason == "" && !force && c.now().Before(p.at.Add(settle)) {
		return
	}
	w.crashEvent = nil
	c.emitLocked(w, notify.Message{Event: p.event, Time: p.at, Reason: reason})
}

// liveLineLocked reports whether a log line Docker stamped at is newer than
// any line seen before it and than this Foreman, so what it says may be
// reported. A re-attach replays the run's log, which must not report
// again. Docker's time is compared with Foreman's own clock, so this
// assumes both run on one host's clock: a remote Docker ahead in time
// could repeat a join or leave. w.mu is held.
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

// checkDisk reads the free space where worlds are kept while a game runs,
// and warns once each time it falls below LowDiskBelow (SPEC 8.13). The
// disk is shared, so the warning is Foreman's, not the world's; the first
// reading after Foreman starts is only noted, like the first look at a game.
func (c *Controller) checkDisk(w *worldRun, cid string) {
	free, err := c.freeBytes(c.worlds.Dirs().Worlds)
	c.mu.Lock()
	if err != nil {
		logIt := !c.diskErr
		c.diskErr = true
		c.mu.Unlock()
		if logIt {
			c.log.Warn("cannot read the free disk space", "error", err)
		}
		return
	}
	first := !c.diskRead
	c.diskRead, c.diskErr, c.diskFree = true, false, free
	var changed, send bool
	switch {
	case !c.lowDisk && free < LowDiskBelow:
		c.lowDisk, changed, send = true, true, !first
	case c.lowDisk && free > LowDiskClear:
		c.lowDisk, changed = false, true
	}
	c.mu.Unlock()
	if send {
		c.Emit(notify.Message{Event: notify.LowDisk, World: notify.World{ID: w.id}, FreeBytes: free})
	}
	if changed {
		w.mu.Lock()
		if w.containerID == cid {
			c.changedLocked(w, true)
		}
		w.mu.Unlock()
	}
}

// diskLow reports whether free space is low, and the last reading then.
func (c *Controller) diskLow() (bool, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.lowDisk {
		return false, 0
	}
	return true, c.diskFree
}
