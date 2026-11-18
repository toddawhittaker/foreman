package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The General tab: the settings an admin changes in the UI that have no
// environment form (SPEC 13.1, 6.6, 7.6, 14.6, 12.9).

// generalTabOn shows the General tab.
const generalTabOn = true

// generalDoneSaved is the result the page reports after a save.
const generalDoneSaved = "saved"

// The audit actions of the five settings, with the detail "from A to B".
const (
	AuditSnapshotRetentionSet = "snapshot-retention-set"
	AuditStopCountdownSet     = "stop-countdown-set"
	AuditAuditRetentionSet    = "audit-retention-set"
	AuditSaveUploadLimitSet   = "save-upload-limit-set"
	AuditModUploadLimitSet    = "mod-upload-limit-set"
)

// intSetting describes one whole-number app setting. Key is the name the
// form field and the MCP argument share; Name is the app_settings row, which
// holds the number as decimal text.
type intSetting struct {
	Name, Key string
	Default   int
	Min, Max  int
	Action    string
}

var (
	settingSnapshotRetention = intSetting{Name: "snapshot_retention", Key: "snapshot_retention",
		Default: world.DefaultKeepAutomatic, Min: 1, Max: 100, Action: AuditSnapshotRetentionSet}
	settingStopCountdown = intSetting{Name: "stop_countdown", Key: "stop_countdown_seconds",
		Default: defaultCountdownSeconds, Min: 0, Max: maxCountdownSeconds, Action: AuditStopCountdownSet}
	settingAuditRetention = intSetting{Name: "audit_retention_days", Key: "audit_retention_days",
		Default: 365, Min: 7, Max: 3650, Action: AuditAuditRetentionSet}
	settingSaveUpload = intSetting{Name: "save_upload_mb", Key: "save_upload_mb",
		Default: MaxSaveUpload >> 20, Min: 100, Max: 8192, Action: AuditSaveUploadLimitSet}
	settingModUpload = intSetting{Name: "mod_upload_mb", Key: "mod_upload_mb",
		Default: modUploadMB, Min: 10, Max: 2048, Action: AuditModUploadLimitSet}
)

// generalSettings lists the settings in the order the page shows them.
var generalSettings = []intSetting{
	settingSnapshotRetention, settingStopCountdown, settingAuditRetention, settingSaveUpload, settingModUpload,
}

// valid reports whether n is in the setting's range.
func (d intSetting) valid(n int) bool { return n >= d.Min && n <= d.Max }

// parse reads a whole number in the range from form text.
func (d intSetting) parse(v string) (int, bool) {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 5 || strings.Trim(v, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil && d.valid(n)
}

// rangeMsg is the refusal for a value outside the range, shared by the
// page and the MCP tool.
func (d intSetting) rangeMsg() i18n.Msg {
	return i18n.M("general.error.range", "min", d.Min, "max", d.Max)
}

// intSetting reads a setting. A missing, unreadable or out-of-range value
// gives the default, so a database fault never changes a limit to
// something odd.
func (s *server) intSetting(ctx context.Context, d intSetting) int {
	v, found, err := s.store.AppSetting(ctx, d.Name)
	if err != nil {
		s.log.Error("read an app setting", "setting", d.Name, "error", err)
		return d.Default
	}
	n, err := strconv.Atoi(v)
	if !found || err != nil || !d.valid(n) {
		return d.Default
	}
	return n
}

// setIntSetting stores n and audits the change as from old to n under by.
// The caller has checked the range. Nothing is stored or audited when the
// value does not change.
func (s *server) setIntSetting(ctx context.Context, by game.Actor, d intSetting, n int) error {
	old := s.intSetting(ctx, d)
	if old == n {
		return nil
	}
	if err := s.store.SetAppSetting(ctx, d.Name, strconv.Itoa(n), by.Username, s.now()); err != nil {
		return err
	}
	s.audit(ctx, by, d.Action, "", fmt.Sprintf("from %d to %d", old, n))
	return nil
}

// snapshotRetention is how many automatic snapshots each world keeps.
func (s *server) snapshotRetention(ctx context.Context) int {
	return s.intSetting(ctx, settingSnapshotRetention)
}

// stopCountdown is the countdown a stop or restart starts with when it
// asks for none (SPEC 7.6).
func (s *server) stopCountdown(ctx context.Context) time.Duration {
	return time.Duration(s.intSetting(ctx, settingStopCountdown)) * time.Second
}

// auditRetentionDays is how long audit entries are kept (SPEC 12.9).
func (s *server) auditRetentionDays(ctx context.Context) int {
	return s.intSetting(ctx, settingAuditRetention)
}

// saveUploadLimit and modUploadLimit are the upload limits in bytes (SPEC 14.6).
func (s *server) saveUploadLimit(ctx context.Context) int64 {
	return int64(s.intSetting(ctx, settingSaveUpload)) << 20
}

func (s *server) modUploadLimit(ctx context.Context) int64 {
	return int64(s.intSetting(ctx, settingModUpload)) << 20
}

// applyRetention gives the world manager the saved retention. It runs at
// start and after each save of the setting, so a lower figure takes effect
// at each world's next automatic snapshot and nothing is removed now.
func (s *server) applyRetention(ctx context.Context) {
	if s.worlds != nil {
		s.worlds.SetKeepAutomatic(s.snapshotRetention(ctx))
	}
}

// generalValues reads every General setting by Key.
func (s *server) generalValues(ctx context.Context) map[string]int {
	out := make(map[string]int, len(generalSettings))
	for _, d := range generalSettings {
		out[d.Key] = s.intSetting(ctx, d)
	}
	return out
}

// saveGeneral stores the settings in want, by Key. Every value is checked
// first, and nothing is stored when one is out of range.
func (s *server) saveGeneral(ctx context.Context, by game.Actor, want map[string]int) error {
	for _, d := range generalSettings {
		if n, ok := want[d.Key]; ok && !d.valid(n) {
			return fmt.Errorf("%s is outside %d to %d", d.Key, d.Min, d.Max)
		}
	}
	for _, d := range generalSettings {
		n, ok := want[d.Key]
		if !ok {
			continue
		}
		if err := s.setIntSetting(ctx, by, d, n); err != nil {
			return err
		}
	}
	s.applyRetention(ctx)
	return nil
}

// trimCount is the automatic snapshots, and the worlds that hold them, that
// would go at retention n, and the audit entries a sweep with a retention
// of days would delete (SPEC 6.6, 12.9).
type trimCount struct {
	Snapshots, Worlds, AuditEntries int
}

func (s *server) trimCount(ctx context.Context, retention, auditDays int) (trimCount, error) {
	var t trimCount
	var err error
	if s.worlds != nil {
		if t.Snapshots, t.Worlds, err = s.worlds.TrimCount(ctx, retention); err != nil {
			return trimCount{}, fmt.Errorf("count the snapshots retention would remove: %w", err)
		}
	}
	cutoff := s.now().AddDate(0, 0, -auditDays)
	if t.AuditEntries, err = s.store.CountAuditBefore(ctx, cutoff); err != nil {
		return trimCount{}, fmt.Errorf("count the audit entries a sweep would remove: %w", err)
	}
	return t, nil
}

// GeneralField is one setting's input on the page.
type GeneralField struct {
	// ID is the input's id and Name its form name, which is also the MCP argument.
	ID, Name string
	Value    string
	Error    i18n.Msg
	Min, Max int
}

// GeneralPage is the General tab. The values are the saved ones, or what
// was posted when a field was refused.
type GeneralPage struct {
	SnapshotRetention, StopCountdown, AuditRetention, SaveUpload, ModUpload GeneralField
	Saved                                                                   bool
	// Trim is what the saved retention will remove at the next automatic
	// snapshots; Trims is false when nothing will go or it could not be read.
	Trim  trimCount
	Trims bool
}

func (GeneralPage) templateName() string { return "generalsettings" }
func (GeneralPage) navItem() string      { return PathSchedule }
func (p GeneralPage) hasErrors() bool    { return len(p.Problems()) > 0 }

// Path is where the form posts.
func (GeneralPage) Path() string { return generalPath() }

// Problems lists the form's errors in the order the fields appear.
func (p GeneralPage) Problems() []Problem {
	return problems(
		Problem{p.SnapshotRetention.ID, p.SnapshotRetention.Error},
		Problem{p.StopCountdown.ID, p.StopCountdown.Error},
		Problem{p.AuditRetention.ID, p.AuditRetention.Error},
		Problem{p.SaveUpload.ID, p.SaveUpload.Error},
		Problem{p.ModUpload.ID, p.ModUpload.Error})
}

// field returns the page's input for the setting with the given Key.
func (p *GeneralPage) field(key string) *GeneralField {
	switch key {
	case settingSnapshotRetention.Key:
		return &p.SnapshotRetention
	case settingStopCountdown.Key:
		return &p.StopCountdown
	case settingAuditRetention.Key:
		return &p.AuditRetention
	case settingSaveUpload.Key:
		return &p.SaveUpload
	}
	return &p.ModUpload
}

// withFields gives each input its id, form name and range.
func (p GeneralPage) withFields() GeneralPage {
	for _, d := range generalSettings {
		f := p.field(d.Key)
		f.ID, f.Name, f.Min, f.Max = "general-"+strings.ReplaceAll(d.Key, "_", "-"), d.Key, d.Min, d.Max
	}
	return p
}

// generalPage is the page showing the saved values.
func (s *server) generalPage(ctx context.Context) GeneralPage {
	p := GeneralPage{}.withFields()
	for k, n := range s.generalValues(ctx) {
		p.field(k).Value = strconv.Itoa(n)
	}
	t, err := s.trimCount(ctx, s.snapshotRetention(ctx), s.auditRetentionDays(ctx))
	if err != nil {
		s.log.Error("count what retention will remove", "error", err)
	}
	p.Trim, p.Trims = t, err == nil && t.Snapshots > 0
	return p
}

func (s *server) generalRoutes() {
	s.mux.HandleFunc("GET "+generalPath(), s.requireUser(s.handleGeneral))
	s.mux.HandleFunc("POST "+generalPath(), s.requireUser(s.handleGeneralSave))
}

func (s *server) handleGeneral(w http.ResponseWriter, r *http.Request, u store.User) {
	p := s.generalPage(r.Context())
	p.Saved = r.URL.Query().Get("done") == generalDoneSaved
	s.page(w, r, http.StatusOK, u.Username, p)
}

func (s *server) handleGeneralSave(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	ctx := r.Context()
	p := s.generalPage(ctx)
	want := map[string]int{}
	for _, d := range generalSettings {
		f := p.field(d.Key)
		f.Value = r.PostFormValue(d.Key)
		n, ok := d.parse(f.Value)
		if !ok {
			f.Error = d.rangeMsg()
			continue
		}
		want[d.Key] = n
	}
	if p.hasErrors() {
		s.page(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	if err := s.saveGeneral(ctx, actor(u), want); err != nil {
		s.fail(w, r, "save the general settings", err)
		return
	}
	http.Redirect(w, r, generalPath()+"?done="+generalDoneSaved, http.StatusSeeOther)
}
