package web

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/schedule"
)

// noGame is a controller with no world running: the page tests never reach
// the time a restart is due.
type noGame struct{}

func (noGame) Status(string) game.Status { return game.Status{State: game.StateStopped} }
func (noGame) Players(context.Context, string) ([]game.Player, error) {
	return nil, game.ErrNotRunning
}
func (noGame) Restart(context.Context, string, game.Actor, game.StopOptions, game.StartOptions) error {
	return game.ErrNotRunning
}
func (noGame) CancelStop(context.Context, string, game.Actor) error { return game.ErrNoCountdown }

// newScheduleHarness is a harness whose handler has a scheduler over its
// store, at the harness clock's time.
func newScheduleHarness(t *testing.T) (*harness, *schedule.Scheduler) {
	t.Helper()
	hn := newHarness(t, nil)
	sc, err := schedule.New(context.Background(), schedule.Config{
		Game: noGame{}, Store: hn.store, Now: hn.clock.Now, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Active: func(context.Context) (string, string, bool, error) { return "", "", false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Deps{Config: hn.cfg, Store: hn.store, Worlds: hn.worlds, Scheduler: sc, Log: hn.slog, Now: hn.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	hn.h = h
	return hn, sc
}

func scheduleForm(on bool, clock, zone, limit string) url.Values {
	f := url.Values{"time": {clock}, "zone": {zone}, "limit_hours": {limit}}
	if on {
		f.Set("enabled", "on")
	}
	return f
}

func TestScheduleNeedsSignIn(t *testing.T) {
	hn, sc := newScheduleHarness(t)
	hn.wantRedirect(hn.get(PathSchedule), PathSetup)
	hn.wantRedirect(hn.post(PathSchedule, scheduleForm(true, "04:00", "UTC", "1")), PathSetup)
	if _, found := sc.Settings(); found {
		t.Error("a signed-out post saved a schedule")
	}
}

// DESIGN 8.15: no schedule yet; the form offers one at 04:00 UTC with a
// one-hour limit, ticked, so setting it is one save.
func TestScheduleDefaults(t *testing.T) {
	hn, _ := newScheduleHarness(t)
	c := hn.signIn()
	body := hn.get(PathSchedule, withCookie(c)).Body.String()
	for _, want := range []string{
		`id="schedule-state">No restart is scheduled.`,
		`<input type="checkbox" id="schedule-on" name="enabled" value="on" checked>`,
		`value="04:00"`, `<option value="UTC" selected>UTC</option>`, `<optgroup label="Europe">`,
		`<option value="Europe/London">Europe/London</option>`,
		`id="schedule-limit" name="limit_hours" type="text" inputmode="numeric" value="1" required`,
		`Then it restarts the server after warning players in the game for 60 seconds.`,
		`Hours, 0 to 12.`,
		`<nav class="subnav" aria-label="Foreman settings">`,
		`<a href="/settings/restart-schedule" aria-current="page">Restart schedule</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q\n%s", want, body)
		}
	}
}

// SPEC 7.12: saving turns the schedule on, names the next restart, and is
// audited.
func TestScheduleSave(t *testing.T) {
	hn, sc := newScheduleHarness(t)
	c := hn.signIn()
	hn.wantRedirect(hn.post(PathSchedule, scheduleForm(true, "04:30", "Europe/London", "3"), withCookie(c)), PathSchedule+"?done=saved")
	r, found := sc.Settings()
	if !found || !r.Enabled || r.Minute != 4*60+30 || r.Zone != "Europe/London" || r.LimitHours != 3 || r.UpdatedBy != "alice" {
		t.Fatalf("saved %+v", r)
	}
	// The harness clock is 2 November 2026, 12:00 UTC; London is on UTC then.
	next := time.Date(2026, 11, 3, 4, 30, 0, 0, time.UTC)
	if got := sc.State().Next; !got.Equal(next) {
		t.Errorf("next = %v, want %v", got, next)
	}
	if got := hn.audits(AuditScheduleSet); len(got) != 1 || got[0] != "alice  every day at 04:30 Europe/London, waiting up to 3 h" {
		t.Errorf("audit %q", got)
	}
	body := hn.get(PathSchedule+"?done=saved", withCookie(c)).Body.String()
	for _, want := range []string{
		`id="done-message"`, "Saved. The next restart is", `datetime="2026-11-03T04:30:00Z"`,
		"Foreman restarts the active world every day at 04:30 (", "Europe/London", `<option value="Europe/London" selected>`,
		`name="enabled" value="on" checked>`, `value="3"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q\n%s", want, body)
		}
	}
}

// DESIGN 8.15: unticking turns it off and keeps the fields; while off, a
// field that does not parse keeps its saved value.
func TestScheduleTurnOff(t *testing.T) {
	hn, sc := newScheduleHarness(t)
	c := hn.signIn()
	hn.post(PathSchedule, scheduleForm(true, "02:00", "Asia/Tokyo", "0"), withCookie(c))
	hn.wantRedirect(hn.post(PathSchedule, scheduleForm(false, "", "nowhere", "99"), withCookie(c)), PathSchedule+"?done=saved")
	r, _ := sc.Settings()
	if r.Enabled || r.Minute != 2*60 || r.Zone != "Asia/Tokyo" || r.LimitHours != 0 {
		t.Errorf("after turning off: %+v", r)
	}
	if !sc.State().Next.IsZero() {
		t.Error("an off schedule has a next restart")
	}
	body := hn.get(PathSchedule+"?done=saved", withCookie(c)).Body.String()
	if !strings.Contains(body, "Saved. No restart is scheduled.") || !strings.Contains(body, `<option value="Asia/Tokyo" selected>`) {
		t.Errorf("page after turning off\n%s", body)
	}
	if got := hn.audits(AuditScheduleSet); len(got) != 2 || got[0] != "alice  off" {
		t.Errorf("audit %q", got)
	}
}

// DESIGN 8.15, 7.2: each field's error, in field order, with the values
// shown back; nothing is saved.
func TestScheduleRefusesBadFields(t *testing.T) {
	hn, sc := newScheduleHarness(t)
	c := hn.signIn()
	for _, f := range []struct {
		clock, zone, limit string
		errs               []string
	}{
		{"4am", "UTC", "1", []string{"Enter a time such as 04:00."}},
		{"24:00", "UTC", "1", []string{"Enter a time such as 04:00."}},
		{"04:00", "Local", "1", []string{"Choose a time zone from the list."}},
		{"04:00", "US/Eastern", "1", []string{"Choose a time zone from the list."}},
		{"04:00", "UTC", "13", []string{"Enter a whole number of hours from 0 to 12."}},
		{"04:00", "UTC", "1.5", []string{"Enter a whole number of hours from 0 to 12."}},
		{"04:00", "UTC", "-1", []string{"Enter a whole number of hours from 0 to 12."}},
		{"04:00", "UTC", "", []string{"Enter a whole number of hours from 0 to 12."}},
		{"", "", "", []string{"Enter a time such as 04:00.", "Choose a time zone from the list.", "Enter a whole number of hours from 0 to 12."}},
	} {
		rec := hn.post(PathSchedule, scheduleForm(true, f.clock, f.zone, f.limit), withCookie(c))
		body := rec.Body.String()
		if rec.Code != 400 {
			t.Errorf("%v: status %d, want 400", f, rec.Code)
		}
		for _, e := range f.errs {
			if !strings.Contains(body, e) {
				t.Errorf("%v: page lacks %q", f, e)
			}
		}
		if !strings.Contains(body, `id="error-summary"`) || !strings.Contains(body, `aria-invalid="true"`) {
			t.Errorf("%v: no summary or invalid field", f)
		}
		// A refused zone is never shown as UTC chosen (DESIGN 8.15).
		if f.zone != "UTC" && strings.Contains(body, `<option value="UTC" selected>`) {
			t.Errorf("%v: a refused zone shows UTC selected", f)
		}
	}
	if _, found := sc.Settings(); found {
		t.Error("a refused form saved the schedule")
	}
	if got := hn.audits(AuditScheduleSet); len(got) != 0 {
		t.Errorf("refused forms were audited: %q", got)
	}
}

// DESIGN 8.15: the three settings pages share one sub-navigation, and the
// user menu lists them in the same order.
func TestSettingsSubnav(t *testing.T) {
	hn, _ := newScheduleHarness(t)
	c := hn.signIn()
	for _, p := range []struct{ path, current string }{
		{PathAccount, PathAccount},
		{PathSchedule, PathSchedule},
		{PathNotifications, PathNotifications},
	} {
		body := hn.get(p.path, withCookie(c)).Body.String()
		nav := between(body, `<nav class="subnav" aria-label="Foreman settings">`, "</nav>")
		if nav == "" {
			t.Errorf("%s: no Foreman settings sub-navigation", p.path)
			continue
		}
		a, s, n := strings.Index(nav, PathAccount), strings.Index(nav, PathSchedule), strings.Index(nav, PathNotifications)
		if a < 0 || s < a || n < s {
			t.Errorf("%s: sub-navigation order\n%s", p.path, nav)
		}
		if strings.Count(nav, `aria-current="page"`) != 1 || !strings.Contains(nav, `href="`+p.current+`" aria-current="page"`) {
			t.Errorf("%s: current link\n%s", p.path, nav)
		}
		// The main Settings tab is the one way in (DESIGN 6): the page on the
		// restart schedule it opens, the section on the other two.
		main := between(body, `<nav aria-label="Main">`, "</nav>")
		mark := "true"
		if p.path == PathSchedule {
			mark = "page"
		}
		if want := `<a href="` + PathSchedule + `" aria-current="` + mark + `">Settings</a>`; !strings.Contains(main, want) {
			t.Errorf("%s: main nav lacks %s\n%s", p.path, want, main)
		}
		if strings.Count(main, "aria-current") != 1 {
			t.Errorf("%s: %d main tabs marked current", p.path, strings.Count(main, "aria-current"))
		}
		menu := between(body, `<div class="menu__panel">`, PathSignOut)
		for _, link := range []string{PathAccount, PathSchedule, PathNotifications} {
			if strings.Contains(menu, `href="`+link) {
				t.Errorf("%s: the user menu still links to %s", p.path, link)
			}
		}
	}
}

// The Settings tab marks its section on the pages under Notifications too.
func TestSettingsTabOnWebhookPages(t *testing.T) {
	r := newTestRenderer(t)
	for _, p := range []Page{WebhookFormPage{}, WebhookDeletePage{}} {
		_, body := render(t, r, pageCase{status: 200, user: "todd", page: p})
		wantIn(t, p.templateName(), body, `<a href="`+PathSchedule+`" aria-current="true">Settings</a>`)
	}
}
