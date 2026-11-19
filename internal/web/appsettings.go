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
	// Label is the catalogue key of the setting's field label.
	Label string
}

var (
	settingSnapshotRetention = intSetting{Name: "snapshot_retention", Key: "snapshot_retention",
		Default: world.DefaultKeepAutomatic, Min: 1, Max: 100, Action: AuditSnapshotRetentionSet,
		Label: "general.snapshots.label"}
	settingStopCountdown = intSetting{Name: "stop_countdown", Key: "stop_countdown_seconds",
		Default: defaultCountdownSeconds, Min: 0, Max: maxCountdownSeconds, Action: AuditStopCountdownSet,
		Label: "general.stop.label"}
	settingAuditRetention = intSetting{Name: "audit_retention_days", Key: "audit_retention_days",
		Default: 365, Min: 7, Max: 3650, Action: AuditAuditRetentionSet,
		Label: "general.audit.label"}
	settingSaveUpload = intSetting{Name: "save_upload_mb", Key: "save_upload_mb",
		Default: MaxSaveUpload >> 20, Min: 100, Max: 8192, Action: AuditSaveUploadLimitSet,
		Label: "general.uploads.save.label"}
	settingModUpload = intSetting{Name: "mod_upload_mb", Key: "mod_upload_mb",
		Default: modUploadMB, Min: 10, Max: 2048, Action: AuditModUploadLimitSet,
		Label: "general.uploads.mod.label"}
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

// readInt reads a setting. A missing one gives the default. ok is false
// when the read failed or the stored value is not a number in range; the
// value is then the default, which callers that must fail safe do not use.
func (s *server) readInt(ctx context.Context, d intSetting) (n int, ok bool) {
	v, found, err := s.store.AppSetting(ctx, d.Name)
	if err != nil {
		// A browser that left mid-page cancels the request; that is not a fault.
		if ctx.Err() == nil {
			s.log.Error("read an app setting", "setting", d.Name, "error", err)
		}
		return d.Default, false
	}
	if !found {
		return d.Default, true
	}
	n, err = strconv.Atoi(v)
	if err != nil || !d.valid(n) {
		s.log.Error("an app setting holds an unusable value, so its default is used", "setting", d.Name)
		return d.Default, false
	}
	return n, true
}

// intSetting reads a setting, giving the default when it cannot be read, so
// a database fault never changes a limit to something odd. Snapshot
// retention does not use it: see applyRetention.
func (s *server) intSetting(ctx context.Context, d intSetting) int {
	n, _ := s.readInt(ctx, d)
	return n
}

// setIntSetting stores n and audits the change as from old to n under by.
// The caller has checked the range and holds generalMu. Nothing is stored or
// audited when the value does not change.
func (s *server) setIntSetting(ctx context.Context, by game.Actor, d intSetting, n int) error {
	old, ok := s.readInt(ctx, d)
	if ok && old == n {
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

// auditRetentionDays is how long audit entries are kept (SPEC 12.9). The
// hourly sweep reads it; it gives the default when the setting cannot be read.
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

// applyRetention gives the world manager the saved retention at start. If
// the setting cannot be read it sets the largest figure, so a fault never
// makes a world drop snapshots it should keep. A lower figure takes effect
// at each world's next automatic snapshot; nothing is removed now.
func (s *server) applyRetention(ctx context.Context) {
	if s.worlds == nil {
		return
	}
	n, ok := s.readInt(ctx, settingSnapshotRetention)
	if !ok {
		s.log.Error("snapshot retention could not be read, so every world keeps up to the largest number of automatic snapshots", "keep", settingSnapshotRetention.Max)
		n = settingSnapshotRetention.Max
	}
	s.worlds.SetKeepAutomatic(n)
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
	// One save at a time, so each audit entry's old value is the real one.
	s.generalMu.Lock()
	defer s.generalMu.Unlock()
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
		// Applied with the value just written, so a failure on a later
		// setting still leaves the manager on what was saved.
		if d.Key == settingSnapshotRetention.Key && s.worlds != nil {
			s.worlds.SetKeepAutomatic(n)
		}
	}
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
	if t.Snapshots, t.Worlds, err = s.snapshotTrim(ctx, retention); err != nil {
		return trimCount{}, err
	}
	cutoff := s.now().AddDate(0, 0, -auditDays)
	if t.AuditEntries, err = s.store.CountAuditBefore(ctx, cutoff); err != nil {
		return trimCount{}, fmt.Errorf("count the audit entries a sweep would remove: %w", err)
	}
	return t, nil
}

// snapshotTrim counts the automatic snapshots, and the worlds holding them,
// that retention n would remove.
func (s *server) snapshotTrim(ctx context.Context, n int) (snapshots, worlds int, err error) {
	if s.worlds == nil {
		return 0, 0, nil
	}
	if snapshots, worlds, err = s.worlds.TrimCount(ctx, n); err != nil {
		return 0, 0, fmt.Errorf("count the snapshots retention would remove: %w", err)
	}
	return snapshots, worlds, nil
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
	case settingModUpload.Key:
		return &p.ModUpload
	}
	panic("no General field for " + key)
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
	confirm, needed, err := s.generalConfirmPage(ctx, r, want)
	if err != nil {
		s.fail(w, r, "check what the new retention removes", err)
		return
	}
	if needed {
		s.page(w, r, http.StatusOK, u.Username, confirm)
		return
	}
	if err := s.saveGeneral(ctx, actor(u), want); err != nil {
		s.fail(w, r, "save the general settings", err)
		return
	}
	http.Redirect(w, r, generalPath()+"?done="+generalDoneSaved, http.StatusSeeOther)
}
