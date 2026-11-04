package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
)

// liveRegions are the parts of the server page a live update replaces.
// Each is sent only when it changed, so an update to one never moves focus
// out of another.
var liveRegions = []string{"facts", "players", "control"}

// handleEvents streams a world's server page as server-sent events (SPEC
// 16.3): each region's HTML whenever it changes, starting with all of them,
// so a page that reconnects misses nothing. The stream ends when its user
// is no longer signed in, when the page falls too far behind, and first of
// all at shutdown.
func (s *server) handleEvents(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	if !s.runs.track() {
		http.Error(w, "Foreman is shutting down.", http.StatusServiceUnavailable)
		return
	}
	defer s.runs.wg.Done()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // proxies must pass each event on at once
	w.WriteHeader(http.StatusOK)
	out := &liveStream{w: w, rc: http.NewResponseController(w), render: s.render, sent: map[string]string{}}

	view, updates, stop := s.game.Watch(sum.ID)
	defer stop()
	results := s.runs.watch()
	beat := time.NewTicker(heartbeatEvery)
	defer beat.Stop()
	st, changed := view.Status, true
	for {
		if changed {
			if err := out.send(s.serverPage(r.Context(), sum, st)); err != nil {
				return
			}
		}
		changed = true
		select {
		case <-r.Context().Done():
			return
		case <-s.runs.live.Done():
			return
		case up, open := <-updates:
			if !open {
				return
			}
			changed = up.Kind == game.UpdateStatus
			if changed {
				st = up.Status
			}
			// Only the newest status waiting is sent, so a slow send (the
			// player list asks the game over RCON) never falls behind.
			for drained := false; !drained; {
				select {
				case up, open := <-updates:
					if !open {
						return
					}
					if up.Kind == game.UpdateStatus {
						st, changed = up.Status, true
					}
				default:
					drained = true
				}
			}
		case <-results:
			results = s.runs.watch()
			st = s.game.Status(sum.ID)
		case <-beat.C:
			if _, state, err := s.identify(r); err != nil || state != isSignedIn {
				return
			}
			if err := out.comment(); err != nil {
				return
			}
			changed = false
		}
	}
}

// liveStream writes server-sent events.
type liveStream struct {
	w      http.ResponseWriter
	rc     *http.ResponseController
	render *Renderer
	sent   map[string]string // the last HTML sent for each region
	state  game.State        // the last state sent; "" before the first
}

// send writes each region of p that changed since the last send. When the
// state changed, the facts region carries a sentence for #announcer.
func (l *liveStream) send(p ServerPage) error {
	wrote := false
	for _, region := range liveRegions {
		var b strings.Builder
		if err := l.render.Fragment(&b, p, "server-"+region); err != nil {
			return err
		}
		html := b.String()
		if l.sent[region] == html {
			continue
		}
		l.sent[region] = html
		if region == "facts" && l.state != "" && l.state != p.Status.State {
			html += `<div hx-swap-oob="innerHTML:#announcer">` + announcement(p.Status.State) + `</div>`
		}
		if err := l.event(region, html); err != nil {
			return err
		}
		wrote = true
	}
	l.state = p.Status.State
	if !wrote {
		return nil
	}
	return l.rc.Flush()
}

// event writes one event; each line of data gets its own field.
func (l *liveStream) event(name, data string) error {
	var b strings.Builder
	b.WriteString("event: " + name + "\n")
	for _, line := range strings.Split(strings.ReplaceAll(data, "\r", ""), "\n") {
		b.WriteString("data: " + line + "\n")
	}
	b.WriteString("\n")
	_, err := l.w.Write([]byte(b.String()))
	return err
}

// comment keeps a quiet stream alive; browsers ignore it.
func (l *liveStream) comment() error {
	if _, err := l.w.Write([]byte(": still here\n\n")); err != nil {
		return err
	}
	return l.rc.Flush()
}
