package web

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/schedule"
	"github.com/toddawhittaker/foreman/internal/store"
)

// PathSchedule is the restart schedule page (SPEC 7.12, DESIGN 8.15).
const PathSchedule = "/settings/restart-schedule"

// AuditScheduleSet records a saved restart schedule (SPEC 12.9).
const AuditScheduleSet = "restart-schedule-set"

// scheduleDoneSaved is the result the page reports after a save.
const scheduleDoneSaved = "saved"

// clockField matches the time input's value, HH:MM.
var clockField = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

// ZoneGroup is one region's time zones in the select.
type ZoneGroup struct {
	Region string
	Zones  []ZoneOption
}

// ZoneOption is one time zone in the select.
type ZoneOption struct {
	Name     string
	Selected bool
}

// SchedulePage is the restart schedule page. The field values are what the
// form shows: the saved schedule, or what was posted when it was refused.
type SchedulePage struct {
	// On is whether a saved schedule restarts the world; Next is when.
	On   bool
	Next time.Time
	// SavedClock and SavedZone are the saved schedule's time and zone, for
	// the state sentence.
	SavedClock string
	SavedZone  string
	// The form's fields.
	Enabled bool
	Clock   string
	Zone    string
	// UTCSelected and Regions are the zone select's options.
	UTCSelected bool
	Regions     []ZoneGroup
	Limit       string
	Saved       bool

	ClockError i18n.Msg
	ZoneError  i18n.Msg
	LimitError i18n.Msg
}

func (SchedulePage) templateName() string { return "schedule" }
func (SchedulePage) navItem() string      { return PathSettingsTab + navPage }
func (p SchedulePage) hasErrors() bool    { return len(p.Problems()) > 0 }

// Path is where the form posts.
func (SchedulePage) Path() string { return PathSchedule }

// MaxLimit is the longest wait, in hours.
func (SchedulePage) MaxLimit() int { return schedule.MaxLimitHours }

// WarningSeconds is the fixed warning before a scheduled restart.
func (SchedulePage) WarningSeconds() int { return int(schedule.Warning / time.Second) }

// UTC is the zone offered first.
func (SchedulePage) UTC() string { return schedule.UTC }

// Problems lists the form's errors in the order the fields appear.
func (p SchedulePage) Problems() []Problem {
	return problems(Problem{"schedule-time", p.ClockError}, Problem{"schedule-zone", p.ZoneError}, Problem{"schedule-limit", p.LimitError})
}

func (s *server) scheduleRoutes() {
	s.mux.HandleFunc("GET "+PathSchedule, s.requireUser(s.handleSchedule))
	s.mux.HandleFunc("POST "+PathSchedule, s.requireUser(s.handleScheduleSave))
}

// clockText writes minutes after midnight as HH:MM.
func clockText(minute int) string { return fmt.Sprintf("%02d:%02d", minute/60, minute%60) }

// schedulePage is the page for the saved schedule.
func (s *server) schedulePage() SchedulePage {
	r, found := s.scheduler.Settings()
	p := SchedulePage{
		On: r.Enabled, Next: s.scheduler.State().Next,
		SavedClock: clockText(r.Minute), SavedZone: r.Zone,
		// A new schedule's box starts ticked, so setting one is one save (DESIGN 8.15).
		Enabled: r.Enabled || !found, Clock: clockText(r.Minute), Zone: r.Zone, Limit: strconv.Itoa(r.LimitHours),
	}
	p.On = p.On && !p.Next.IsZero()
	return p
}

// withZones fills the zone select, grouped by region, with the page's zone
// selected. A refused zone selects none, so the page never shows UTC as
// chosen when it was not.
func (p SchedulePage) withZones() SchedulePage {
	p.UTCSelected = p.Zone == schedule.UTC
	var groups []ZoneGroup
	for _, z := range schedule.Zones()[1:] {
		region, _, _ := strings.Cut(z, "/")
		if len(groups) == 0 || groups[len(groups)-1].Region != region {
			groups = append(groups, ZoneGroup{Region: region})
		}
		g := &groups[len(groups)-1]
		g.Zones = append(g.Zones, ZoneOption{Name: z, Selected: z == p.Zone})
	}
	p.Regions = groups
	return p
}

func (s *server) handleSchedule(w http.ResponseWriter, r *http.Request, u store.User) {
	p := s.schedulePage()
	p.Saved = r.URL.Query().Get("done") == scheduleDoneSaved
	s.page(w, r, http.StatusOK, u.Username, p.withZones())
}

// parseClock reads HH:MM as minutes after midnight.
func parseClock(v string) (int, bool) {
	m := clockField.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, false
	}
	h, _ := strconv.Atoi(m[1])
	mm, _ := strconv.Atoi(m[2])
	return h*60 + mm, true
}

// parseLimit reads a whole number of hours from 0 to the limit.
func parseLimit(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 2 || strings.Trim(v, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil && n <= schedule.MaxLimitHours
}

func (s *server) handleScheduleSave(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	saved, _ := s.scheduler.Settings()
	next := store.RestartSchedule{Enabled: r.PostFormValue("enabled") != "", UpdatedBy: u.Username}
	p := s.schedulePage()
	p.Enabled = next.Enabled
	p.Clock, p.Zone, p.Limit = r.PostFormValue("time"), r.PostFormValue("zone"), r.PostFormValue("limit_hours")

	minute, clockOK := parseClock(p.Clock)
	zoneOK := schedule.ValidZone(p.Zone)
	limit, limitOK := parseLimit(p.Limit)
	if next.Enabled {
		if !clockOK {
			p.ClockError = i18n.M("schedule.time.error")
		}
		if !zoneOK {
			p.ZoneError = i18n.M("schedule.zone.error")
		}
		if !limitOK {
			p.LimitError = i18n.M("schedule.limit.error", "max", schedule.MaxLimitHours)
		}
		if p.hasErrors() {
			s.page(w, r, http.StatusBadRequest, u.Username, p.withZones())
			return
		}
	}
	// While off the fields are ignored: one that does not parse keeps its
	// saved value, so turning the schedule on again is one tick.
	next.Minute, next.Zone, next.LimitHours = saved.Minute, saved.Zone, saved.LimitHours
	if clockOK {
		next.Minute = minute
	}
	if zoneOK {
		next.Zone = p.Zone
	}
	if limitOK {
		next.LimitHours = limit
	}
	if err := s.scheduler.Set(r.Context(), next); err != nil {
		s.fail(w, r, "save the restart schedule", err)
		return
	}
	detail := "off"
	if next.Enabled {
		detail = fmt.Sprintf("every day at %s %s, waiting up to %d h", clockText(next.Minute), next.Zone, next.LimitHours)
	}
	s.audit(r.Context(), u, AuditScheduleSet, "", detail)
	http.Redirect(w, r, PathSchedule+"?done="+scheduleDoneSaved, http.StatusSeeOther)
}
