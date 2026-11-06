package web

import (
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
)

// liveRegions are the parts of the server page a live update replaces.
// Each is sent only when it changed, so an update to one never moves focus
// out of another.
var liveRegions = []string{"active", "facts", "players", "control"}

// reconnectAfter is how soon a browser reconnects after a stream ends, so
// a stream that ends by design (proxyStreamLimit) is back before the page
// says live updates stopped.
const reconnectAfter = 2 * time.Second

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
		http.Error(w, localeOf(r).T("server.stream.shutting_down"), http.StatusServiceUnavailable)
		return
	}
	defer s.runs.wg.Done()
	opened := s.now()
	out, err := s.openStream(w, localeOf(r))
	if err != nil {
		return
	}

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
		case <-s.runs.ctx.Done():
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
			if !s.stillSignedIn(r, opened) {
				return
			}
			if err := out.comment(); err != nil {
				return
			}
			changed = false
		}
	}
}

// openStream starts a server-sent event response.
func (s *server) openStream(w http.ResponseWriter, loc i18n.Locale) (*liveStream, error) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // proxies must pass each event on at once
	w.WriteHeader(http.StatusOK)
	l := &liveStream{w: w, rc: http.NewResponseController(w), render: s.render, loc: loc, sent: map[string]string{}}
	if _, err := w.Write([]byte("retry: " + strconv.Itoa(int(reconnectAfter/time.Millisecond)) + "\n\n")); err != nil {
		return nil, err
	}
	return l, nil
}

// liveStream writes server-sent events.
type liveStream struct {
	w      http.ResponseWriter
	rc     *http.ResponseController
	render *Renderer
	loc    i18n.Locale       // the language the page was opened in
	sent   map[string]string // the last HTML sent for each region
	// started is set after the first send, which is never read out: it is
	// what the page already shows.
	started bool
	state   game.State // the last state sent
	result  string     // the last results sent, as read out
	moved   bool       // the page was told to follow a switch
}

// send writes each region of p that changed since the last send. A new
// result, or else a new state, also goes to #announcer (DESIGN 7.5). A
// failed result stays on the page, so it must not silence the state
// changes after it.
func (l *liveStream) send(p ServerPage) error {
	p.Live = true
	result := l.loc.Format(resultAnnouncement(p))
	var say i18n.Msg
	if l.started {
		switch {
		case result != "" && result != l.result:
			say = resultAnnouncement(p)
		case l.state != p.Status.State:
			say = announcement(p.Status.State)
		}
	}
	if !say.IsZero() && !p.StartBlocked().IsZero() {
		say = join(say, blockedAnnouncement)
	}
	l.started, l.state, l.result = true, p.Status.State, result

	wrote := false
	for _, region := range liveRegions {
		var b strings.Builder
		if err := l.render.Fragment(&b, l.loc, p, "server-"+region); err != nil {
			return err
		}
		data := b.String()
		if l.sent[region] == data {
			continue
		}
		l.sent[region] = data
		if err := l.event(region, data); err != nil {
			return err
		}
		wrote = true
	}
	// The sentence has an event of its own, so it is never lost when no
	// region changed with it.
	if !say.IsZero() {
		if err := l.event("announce", `<div hx-swap-oob="innerHTML:#announcer">`+html.EscapeString(l.loc.Format(say))+`</div>`); err != nil {
			return err
		}
		wrote = true
	}
	// The world switched away from is no longer the one the Server page
	// shows, so the page loads again to show the new one (SPEC 7.9). The
	// sentence goes along, because the reload would drop the announce event.
	if p.Moved() && !l.moved {
		if err := l.event("moved", html.EscapeString(l.loc.Format(p.movedSentence()))); err != nil {
			return err
		}
		l.moved, wrote = true, true
	}
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
