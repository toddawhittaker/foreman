package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
)

// A blocked start is said with the state that blocks it, so a screen reader
// user learns why Start server is gone.
func TestBlockedStartIsAnnounced(t *testing.T) {
	l := &liveStream{render: newTestRenderer(t), sent: map[string]string{}}
	rec := httptest.NewRecorder()
	l.w, l.rc = rec, http.NewResponseController(rec)
	if err := l.send(serverCase(game.StateRunning, func(*ServerPage) {})); err != nil {
		t.Fatal(err)
	}
	rec.Body.Reset()
	if err := l.send(serverCase(game.StateCrashLooping, func(*ServerPage) {})); err != nil {
		t.Fatal(err)
	}
	if body := rec.Body.String(); !strings.Contains(body, "Server is now crash-looping "+english(blockedAnnouncement)) {
		t.Errorf("the blocked start is not read out:\n%s", body)
	}
}

// DESIGN 7.5: a new result is announced even when no region's HTML changed,
// and on an event of its own, so a missing region cannot lose it.
func TestAnnouncementIsItsOwnEvent(t *testing.T) {
	r := newTestRenderer(t)
	failed := serverCase(game.StateStopped, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: game.AuditStart, Err: game.ErrStartFailed}, true
	})
	l := &liveStream{render: r, sent: map[string]string{}}
	rec := httptest.NewRecorder()
	l.w, l.rc = rec, http.NewResponseController(rec)
	if err := l.send(failed); err != nil { // the first send fills sent and is silent
		t.Fatal(err)
	}
	l.result = "" // the result sentence is new, the regions' HTML is not
	rec.Body.Reset()
	if err := l.send(failed); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "event: announce\n") || !strings.Contains(body, "Error: The game did not start.") {
		t.Errorf("no announcement when no region changed:\n%s", body)
	}

	// With regions changing too, the sentence is still sent once, on its own.
	l = &liveStream{render: r, sent: map[string]string{}}
	rec = httptest.NewRecorder()
	l.w, l.rc = rec, http.NewResponseController(rec)
	if err := l.send(serverCase(game.StateRunning, func(*ServerPage) {})); err != nil {
		t.Fatal(err)
	}
	rec.Body.Reset()
	if err := l.send(failed); err != nil {
		t.Fatal(err)
	}
	body = rec.Body.String()
	if n := strings.Count(body, "innerHTML:#announcer"); n != 1 {
		t.Errorf("the announcement was sent %d times:\n%s", n, body)
	}
	for _, part := range strings.Split(body, "\n\n") {
		if strings.Contains(part, "#announcer") && !strings.HasPrefix(part, "event: announce\n") {
			t.Errorf("an announcement rides on a region event:\n%s", part)
		}
	}
}
