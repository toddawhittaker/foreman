package notify

import (
	"strings"
	"time"
)

// Event is the stable id of something Foreman reports (SPEC 8.10). The ids
// are part of the generic webhook body, so they never change.
type Event string

// The events a webhook can be sent.
const (
	Start            Event = "start"
	Stop             Event = "stop"
	Crash            Event = "crash"
	CrashLoop        Event = "crash-loop"
	PlayerJoin       Event = "player-join"
	PlayerLeave      Event = "player-leave"
	ScheduledRestart Event = "scheduled-restart"
	LowDisk          Event = "low-disk"
)

// Events lists every event in the order the page shows them.
var Events = []Event{Start, Stop, Crash, CrashLoop, PlayerJoin, PlayerLeave, ScheduledRestart, LowDisk}

// Known reports whether e is one of Events.
func (e Event) Known() bool {
	for _, k := range Events {
		if e == k {
			return true
		}
	}
	return false
}

// World names the world an event happened in.
type World struct {
	ID   string
	Name string
}

// Message is one event to send. Fields that do not belong to its event are
// left zero.
type Message struct {
	Event Event
	// Time is when it happened; Emit fills in the current time when zero.
	Time  time.Time
	World World
	// Player is who joined or left.
	Player string
	// Reason is the stable id of a crash's cause, when one is known.
	Reason string
	// Skipped is true when a scheduled restart did not run.
	Skipped bool
	// FreeBytes is the free disk space, for LowDisk.
	FreeBytes uint64
}

// eventsOff and parseEventsOff turn the set of events turned off into the
// stored text and back. Storing the ones turned off keeps an event added
// later on by default.
func eventsOff(on map[Event]bool) string {
	var off []string
	for _, e := range Events {
		if !on[e] {
			off = append(off, string(e))
		}
	}
	return strings.Join(off, ",")
}

func parseEventsOff(s string) map[Event]bool {
	off := map[Event]bool{}
	for _, part := range strings.Split(s, ",") {
		if part != "" {
			off[Event(part)] = true
		}
	}
	return off
}
