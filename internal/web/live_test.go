package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

// sentToasts sends each page in turn on one stream and returns, per send, the
// texts of the toasts that send carried (DESIGN 6, "Toasts").
func sentToasts(t *testing.T, l *liveStream, pages ...ServerPage) [][]string {
	t.Helper()
	rec := httptest.NewRecorder()
	l.w, l.rc = rec, http.NewResponseController(rec)
	var out [][]string
	for _, p := range pages {
		rec.Body.Reset()
		if err := l.send(p); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, m := range reToast.FindAllStringSubmatch(rec.Body.String(), -1) {
			got = append(got, m[2])
		}
		out = append(out, got)
	}
	return out
}

func newStream(t *testing.T) *liveStream {
	return &liveStream{render: newTestRenderer(t), sent: map[string]string{}}
}

// DESIGN 6, "Toasts": when a second result's toast joins the first, only the
// new one is sent; the first is on the page already.
func TestLiveSendsOnlyTheNewToast(t *testing.T) {
	done := actionResult{Action: actionSnapshot, Taken: "Manual", At: scheduleNow}
	one := serverCase(game.StateRunning, func(p *ServerPage) { p.Result, p.HaveResult = done, true })
	two := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Result, p.HaveResult = done, true
		p.Others = []actionResult{{Action: game.AuditStop, Err: game.ErrNotRunning, At: scheduleNow.Add(time.Second)}}
	})
	quiet := serverCase(game.StateRunning, func(*ServerPage) {})
	got := sentToasts(t, newStream(t), quiet, one, two, two)
	if len(got[1]) != 1 || !strings.Contains(got[1][0], "Manual") {
		t.Errorf("the first toast: %q", got[1])
	}
	if len(got[2]) != 1 || !strings.Contains(got[2][0], "did not finish") {
		t.Errorf("the second toast's send carried %q, want only the new error", got[2])
	}
	if len(got[3]) != 0 {
		t.Errorf("an unchanged page sent %q again", got[3])
	}
}

// SPEC 7.6: a cancelled stop's result is read from the operation until it is
// stored; each update makes it again, with a new time, but its toast goes
// once, and the stored result keeps the same toast.
func TestCancelledStopToastsOnce(t *testing.T) {
	st := game.Status{World: "base", State: game.StateRunning,
		Operation: &game.Operation{Action: game.AuditStop, Done: true, Error: game.ErrStopCancelled.Error(), ID: 7}}
	pending := func(now time.Time) ServerPage {
		return serverCase(game.StateRunning, func(p *ServerPage) { p.Result, p.HaveResult = cancelledByOperation(st, now) })
	}
	stored := serverCase(game.StateRunning, func(p *ServerPage) {
		p.Result, p.HaveResult = actionResult{Action: game.AuditStop, Err: game.ErrStopCancelled, At: scheduleNow.Add(time.Minute), Op: 7}, true
	})
	got := sentToasts(t, newStream(t), serverCase(game.StateRunning, func(*ServerPage) {}),
		pending(scheduleNow), pending(scheduleNow.Add(time.Second)), stored)
	if len(got[1]) != 1 {
		t.Fatalf("the cancel's toast: %q", got[1])
	}
	if len(got[2])+len(got[3]) != 0 {
		t.Errorf("the cancel's toast came again: %q then %q", got[2], got[3])
	}
}

// DESIGN 6: the Downloaded toast is keyed on the download, not on the state
// that shows it, so starting and stopping the server never sends it again.
func TestDownloadedToastSurvivesStartAndStop(t *testing.T) {
	download := actionResult{Action: game.AuditImageDownload, At: scheduleNow}
	at := func(s game.State) ServerPage {
		return serverCase(s, func(p *ServerPage) {
			p.Result, p.HaveResult = download, true
			p.Image = "factoriotools/factorio:2.1.24"
		})
	}
	got := sentToasts(t, newStream(t), serverCase(game.StateStopped, func(*ServerPage) {}),
		at(game.StateStopped), at(game.StateRunning), at(game.StateStopped))
	if len(got[1]) != 1 || !strings.Contains(got[1][0], "Downloaded") {
		t.Fatalf("the download's toast: %q", got[1])
	}
	if len(got[2])+len(got[3]) != 0 {
		t.Errorf("start and stop sent it again: %q then %q", got[2], got[3])
	}
}

// DESIGN 6: a stream that opens again (a reconnect, or the stream after a
// load) sends no toast its page showed or its own stream sent before; one no
// page has shown, from a result that landed before the stream opened, still
// comes and is noted, and so does one another page showed after this one was
// drawn.
func TestReconnectReplaysNoToast(t *testing.T) {
	done := actionResult{Action: actionSnapshot, Taken: "Manual", At: scheduleNow}
	page := func(toasted map[string]toastMark) ServerPage {
		return serverCase(game.StateRunning, func(p *ServerPage) { p.Result, p.HaveResult, p.Toasted = done, true, toasted })
	}
	var noted []string
	l := newStream(t)
	l.toastSeq = 4
	l.toasted = func(keys []string) { noted = append(noted, keys...) }
	if got := sentToasts(t, l, page(nil)); len(got[0]) != 1 || len(noted) != 1 {
		t.Fatalf("a result no page showed: sent %q, noted %q", got[0], noted)
	}
	key := noted[0]
	for name, c := range map[string]struct {
		mark toastMark
		want int
	}{
		"its own stream sent it, then reconnected": {toastMark{At: 5, Page: 4}, 0},
		"the page itself showed it":                {toastMark{At: 4, Page: 4}, 0},
		"an earlier page showed it":                {toastMark{At: 2, Page: 2}, 0},
		"another page showed it after this load":   {toastMark{At: 6, Page: 6}, 1},
		"another page's stream sent it after":      {toastMark{At: 7, Page: 6}, 1},
	} {
		again := newStream(t)
		again.toastSeq = 4
		if got := sentToasts(t, again, page(map[string]toastMark{key: c.mark})); len(got[0]) != c.want {
			t.Errorf("%s: sent %q, want %d", name, got[0], c.want)
		}
	}
}

// DESIGN 6, "Toasts": each open page shows a result's toast once. A load
// drawn just before the result lands, whose marks are noted only after
// another page's stream sent and marked the toast, still gets it from its
// own stream; the load never showed it.
func TestALoadDrawnBeforeTheResultGetsItsToast(t *testing.T) {
	done := actionResult{Action: actionSnapshot, Taken: "Manual", At: scheduleNow}
	key := toastKeys(serverCase(game.StateStopped, func(p *ServerPage) { p.Result, p.HaveResult = done, true }).resultToasts())[0]
	runs := newGameRuns()
	watching := runs.loadToasts()
	runs.pageToasts("w", watching, nil)
	acting := runs.loadToasts()                     // the acting page's load is drawn, with no result yet
	runs.streamToasts("w", []string{key}, watching) // the result lands; the watching page's stream sends its toast
	runs.pageToasts("w", acting, nil)               // the load's marks: it showed no toast
	l := newStream(t)
	l.toastSeq = acting
	p := serverCase(game.StateStopped, func(p *ServerPage) { p.Result, p.HaveResult, p.Toasted = done, true, runs.toastedKeys("w") })
	if got := sentToasts(t, l, p); len(got[0]) != 1 {
		t.Fatalf("the acting page's stream sent %q, want the Done toast", got[0])
	}
}
