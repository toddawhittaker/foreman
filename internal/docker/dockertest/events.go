package dockertest

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"
)

// event is one container event the fake recorded.
type event struct {
	action string
	id     string
	labels map[string]string
	at     time.Time
}

// AllowEvents makes GET /events stream events, as a socket proxy with
// EVENTS=1 does. Off, the default, it answers 403, as one with EVENTS=0.
func (d *Daemon) AllowEvents(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.eventsOn = on
}

// HoldEvents keeps open event streams from sending new events until it is
// turned off again, as a busy daemon delivers them late.
func (d *Daemon) HoldEvents(on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.eventsHeld = on
	d.broadcast()
}

// CutEvents ends every open event stream, as a proxy that drops an idle
// connection does.
func (d *Daemon) CutEvents() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.eventsCut++
	d.broadcast()
}

// EventStreams is how many event streams the fake has opened so far.
func (d *Daemon) EventStreams() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.eventStreams
}

// OpenEventStreams is how many event streams are open now.
func (d *Daemon) OpenEventStreams() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.eventsOpen
}

// recordEvent notes an event for the event stream. d.mu is held.
func (d *Daemon) recordEvent(c *Container, action string) {
	d.events = append(d.events, event{action: action, id: c.ID, labels: c.Spec.Labels, at: time.Now().UTC()})
	d.broadcast()
}

// eventStream answers GET /events: new events that match the filters, one
// JSON object each, until the client goes or CutEvents ends the stream.
// Only the type, event and label filters Foreman uses are honored.
func (d *Daemon) eventStream(w http.ResponseWriter, r *http.Request) {
	var filters map[string][]string
	if f := r.URL.Query().Get("filters"); f != "" {
		if err := json.Unmarshal([]byte(f), &filters); err != nil {
			reply(w, http.StatusBadRequest, "invalid filters")
			return
		}
	}
	d.mu.Lock()
	if !d.eventsOn {
		d.mu.Unlock()
		reply(w, http.StatusForbidden, "events are not allowed")
		return
	}
	d.eventStreams++
	d.eventsOpen++
	next, cut := len(d.events), d.eventsCut
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.eventsOpen--
		d.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	flush(w)
	enc := json.NewEncoder(w)
	for {
		d.mu.Lock()
		var evs []event
		if !d.eventsHeld {
			evs = append(evs, d.events[next:]...)
			next = len(d.events)
		}
		ended := d.eventsCut != cut
		changed := d.changed
		d.mu.Unlock()
		for _, e := range evs {
			if !matches(e, filters) {
				continue
			}
			_ = enc.Encode(map[string]any{
				"Type": "container", "Action": e.action,
				"Actor":    map[string]any{"ID": e.id, "Attributes": e.labels},
				"time":     e.at.Unix(),
				"timeNano": e.at.UnixNano(),
			})
		}
		flush(w)
		if ended {
			return
		}
		select {
		case <-changed:
		case <-r.Context().Done():
			return
		}
	}
}

func matches(e event, filters map[string][]string) bool {
	if t := filters["type"]; len(t) > 0 && !slices.Contains(t, "container") {
		return false
	}
	if a := filters["event"]; len(a) > 0 && !slices.Contains(a, e.action) {
		return false
	}
	for _, l := range filters["label"] {
		k, v, hasValue := strings.Cut(l, "=")
		got, ok := e.labels[k]
		if !ok || (hasValue && got != v) {
			return false
		}
	}
	return true
}
