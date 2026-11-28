package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

func (hn *harness) generalServer() *server {
	return &server{store: hn.store, worlds: hn.worlds, log: hn.slog, now: hn.clock.Now}
}

// generalForm is a valid General form with the defaults, with changes on top.
func generalForm(changes map[string]string) url.Values {
	f := url.Values{
		"snapshot_retention":     {"10"},
		"stop_countdown_seconds": {"60"},
		"audit_retention_days":   {"365"},
		"save_upload_mb":         {"1024"},
		"mod_upload_mb":          {"200"},
	}
	for k, v := range changes {
		f.Set(k, v)
	}
	return f
}

// SPEC 13.1, 6.6, 7.6, 14.6, 12.9: with nothing stored each setting has its
// default, and a stored value outside its range counts as the default.
func TestGeneralSettingDefaults(t *testing.T) {
	hn := newHarness(t, nil)
	s, ctx := hn.generalServer(), context.Background()
	if s.snapshotRetention(ctx) != 10 || s.stopCountdown(ctx) != 60*time.Second || s.auditRetentionDays(ctx) != 365 ||
		s.saveUploadLimit(ctx) != 1<<30 || s.modUploadLimit(ctx) != 1024<<20 {
		t.Fatalf("defaults %d %v %d %d %d", s.snapshotRetention(ctx), s.stopCountdown(ctx), s.auditRetentionDays(ctx), s.saveUploadLimit(ctx), s.modUploadLimit(ctx))
	}
	for _, c := range []struct{ name, value string }{
		{"snapshot_retention", "0"}, {"snapshot_retention", "101"}, {"snapshot_retention", "ten"},
		{"audit_retention_days", "6"}, {"mod_upload_mb", "9"}, {"save_upload_mb", "99"},
	} {
		if err := hn.store.SetAppSetting(ctx, c.name, c.value, "alice", hn.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if s.snapshotRetention(ctx) != 10 || s.auditRetentionDays(ctx) != 365 || s.modUploadLimit(ctx) != 1024<<20 || s.saveUploadLimit(ctx) != 1<<30 {
		t.Error("an out-of-range stored value was used")
	}
	for name, want := range map[string]int{"snapshot_retention": 100, "stop_countdown": 0, "audit_retention_days": 3650, "mod_upload_mb": 2048, "save_upload_mb": 8192} {
		if err := hn.store.SetAppSetting(ctx, name, strconv.Itoa(want), "alice", hn.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if s.snapshotRetention(ctx) != 100 || s.stopCountdown(ctx) != 0 || s.auditRetentionDays(ctx) != 3650 || s.modUploadLimit(ctx) != 2048<<20 || s.saveUploadLimit(ctx) != 8192<<20 {
		t.Error("a value at the edge of its range was refused")
	}
}

// SPEC 12.9: a change from a stored value that could not be read is audited
// as from unknown, not from the default the page fell back to.
func TestSettingChangeFromUnreadableValue(t *testing.T) {
	hn := newHarness(t, nil)
	s, ctx := hn.generalServer(), context.Background()
	by := game.Actor{Username: "alice"}
	for name, v := range map[string]string{settingUpdateCheck.Name: "maybe", settingSnapshotRetention.Name: "ten"} {
		if err := hn.store.SetAppSetting(ctx, name, v, "alice", hn.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.saveGeneral(ctx, by, map[string]int{settingSnapshotRetention.Key: 5}, map[string]bool{settingUpdateCheck.Key: true}); err != nil {
		t.Fatal(err)
	}
	for action, want := range map[string]string{AuditUpdateCheckSet: "alice  from unknown to on", AuditSnapshotRetentionSet: "alice  from unknown to 5"} {
		if got := hn.audits(action); len(got) != 1 || got[0] != want {
			t.Errorf("%s audit %q, want %q", action, got, want)
		}
	}
}

// SPEC 13.1: the page shows every setting and says where it is set.
func TestGeneralPageShowsSettings(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	rec := hn.get(generalPath(), withCookie(c))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="general-snapshot-retention"`, `value="10"`, `id="general-stop-countdown-seconds"`, `value="60"`,
		`id="general-audit-retention-days"`, `value="365"`, `id="general-save-upload-mb"`, `value="1024"`,
		`id="general-mod-upload-mb"`, `value="1024"`, `aria-current="page">General</a>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(body, `id="general-trim"`) {
		t.Error("the page announced a trim with nothing to trim")
	}
	if n := strings.Count(body, "Set here."); n != 1 {
		t.Errorf(`"Set here." appears %d times, want once`, n)
	}
}

// SPEC 13.1, rulings 8 and 12: a save stores every changed value, audits
// each with its old and new value, leaves the unchanged ones alone, and
// gives the new retention to the world manager.
func TestGeneralSaveStoresAuditsAndApplies(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	rec := hn.post(generalPath(), generalForm(map[string]string{"snapshot_retention": "3", "stop_countdown_seconds": "0", "mod_upload_mb": " 300 "}), withCookie(c))
	hn.wantRedirect(rec, generalPath()+"?done=saved")
	s, ctx := hn.generalServer(), context.Background()
	if s.snapshotRetention(ctx) != 3 || s.stopCountdown(ctx) != 0 || s.modUploadLimit(ctx) != 300<<20 || s.auditRetentionDays(ctx) != 365 {
		t.Errorf("stored %d %v %d", s.snapshotRetention(ctx), s.stopCountdown(ctx), s.modUploadLimit(ctx))
	}
	if hn.worlds.KeepAutomatic() != 3 {
		t.Errorf("the world manager keeps %d, want 3", hn.worlds.KeepAutomatic())
	}
	for action, want := range map[string]string{
		AuditSnapshotRetentionSet: "alice  from 10 to 3", AuditStopCountdownSet: "alice  from 60 to 0", AuditModUploadLimitSet: "alice  from 1024 to 300",
	} {
		if got := strings.Join(hn.audits(action), "|"); got != want {
			t.Errorf("%s audit %q, want %q", action, got, want)
		}
	}
	for _, action := range []string{AuditAuditRetentionSet, AuditSaveUploadLimitSet} {
		if got := hn.audits(action); len(got) != 0 {
			t.Errorf("%s audited without a change: %v", action, got)
		}
	}
	if body := hn.get(generalPath()+"?done=saved", withCookie(c)).Body.String(); !strings.Contains(body, `class="toast toast--success" data-toast`) || !strings.Contains(body, `value="3"`) {
		t.Error("the done page does not show the saved values")
	}
}

// SPEC 13.1: a value outside its range, or not a whole number, refuses the
// whole form and stores nothing.
func TestGeneralSaveRefusesBadValues(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	for field, bad := range map[string]string{
		"snapshot_retention": "0", "stop_countdown_seconds": "601", "audit_retention_days": "6", "save_upload_mb": "99",
		"mod_upload_mb": "2049",
	} {
		for _, v := range []string{bad, "", "ten", "-1", "1e3", "5.5"} {
			rec := hn.post(generalPath(), generalForm(map[string]string{"snapshot_retention": "4", field: v}), withCookie(c))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s=%q: %d, want 400", field, v, rec.Code)
				continue
			}
			body := rec.Body.String()
			if !strings.Contains(body, `id="error-summary"`) || !strings.Contains(body, `href="#general-`) || !strings.Contains(body, `aria-invalid="true"`) {
				t.Errorf("%s=%q: no error shown", field, v)
			}
		}
	}
	if got := hn.generalServer().snapshotRetention(context.Background()); got != 10 {
		t.Errorf("a refused form stored retention %d", got)
	}
	if got := hn.audits(AuditSnapshotRetentionSet); len(got) != 0 {
		t.Errorf("a refused form was audited: %v", got)
	}
}

func TestGeneralNeedsSignIn(t *testing.T) {
	hn := newHarness(t, nil)
	hn.signIn()
	if rec := hn.get(generalPath()); rec.Code == http.StatusOK {
		t.Error("the page answered a signed-out visitor")
	}
	rec := hn.post(generalPath(), generalForm(map[string]string{"snapshot_retention": "2"}))
	if rec.Code == http.StatusOK || hn.generalServer().snapshotRetention(context.Background()) != 10 {
		t.Errorf("a signed-out post: %d", rec.Code)
	}
}

// SPEC 6.6, ruling 8: lowering retention deletes nothing when saved; the
// page says how many automatic snapshots across how many worlds will go.
func TestGeneralLoweringRetentionDeletesNothingAndCounts(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id := hn.seedSnapshotWorld()
	for range 4 {
		hn.clock.Advance(time.Minute)
		if _, err := hn.worlds.AutoSnapshot(id, world.ReasonBeforeRestore, world.LabelBeforeRestore, "alice"); err != nil {
			t.Fatal(err)
		}
	}
	hn.wantRedirect(hn.post(generalPath(), confirmedForm(map[string]string{"snapshot_retention": "1"}, 3, 1), withCookie(c)), generalPath()+"?done=saved")
	list, err := hn.worlds.Snapshots(id)
	if err != nil || len(list) != 4 {
		t.Fatalf("%d snapshots after saving, %v; want 4", len(list), err)
	}
	body := hn.get(generalPath()+"?done=saved", withCookie(c)).Body.String()
	// The count sums up the save, so it is in the toast (DESIGN 6, "Toasts").
	toast := between(body, `<div id="toasts" class="toasts">`, "</div></div>")
	if !strings.Contains(toast, "Saved. The new settings apply from now on.") || !strings.Contains(toast, "3 automatic snapshots will be removed") || !strings.Contains(toast, "Worlds affected: 1.") {
		t.Errorf("the page does not count what will go:\n%s", toast)
	}
	// The next automatic snapshot trims the world to the new figure.
	hn.clock.Advance(time.Minute)
	if _, err := hn.worlds.AutoSnapshot(id, world.ReasonBeforeRestore, world.LabelBeforeRestore, "alice"); err != nil {
		t.Fatal(err)
	}
	if list, _ := hn.worlds.Snapshots(id); len(list) != 1 {
		t.Errorf("%d snapshots after the next one, want 1", len(list))
	}
}

// SPEC 13.1: NewHandler gives the world manager the saved retention.
func TestNewHandlerAppliesSavedRetention(t *testing.T) {
	hn := newHarness(t, nil)
	if err := hn.store.SetAppSetting(context.Background(), settingSnapshotRetention.Name, "7", "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHandler(Deps{Config: hn.cfg, Store: hn.store, Worlds: hn.worlds, Log: hn.slog, Now: hn.clock.Now}); err != nil {
		t.Fatal(err)
	}
	if hn.worlds.KeepAutomatic() != 7 {
		t.Errorf("the manager keeps %d, want 7", hn.worlds.KeepAutomatic())
	}
}

// SPEC 21.7: foreman_settings reports every General-tab setting.
func TestMCPForemanSettingsReportsGeneral(t *testing.T) {
	r := newForemanRig(t, keyed)
	_, out := r.run(t, "foreman_settings", map[string]any{})
	got, _ := out["general"].(map[string]any)
	want := map[string]float64{"snapshot_retention": 10, "stop_countdown_seconds": 60, "audit_retention_days": 365, "save_upload_mb": 1024, "mod_upload_mb": 1024}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("general.%s = %v, want %v", k, got[k], v)
		}
	}
}

// SPEC 21.8: the first call changes and audits nothing, and says each
// change, the retention trim and the audit entries a sweep would delete.
func TestMCPSetForemanSettingsPreviewThenConfirm(t *testing.T) {
	r := newForemanRig(t, keyed)
	ctx := context.Background()
	id := r.worldWithAutos(t, 5)
	old := r.clock.Now().AddDate(0, 0, -30)
	for range 2 {
		if err := r.store.AddAudit(ctx, store.AuditEntry{Time: old, Username: "system", Action: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	args := map[string]any{"snapshot_retention": 2, "audit_retention_days": 7}
	before := r.auditCount()
	res, out := r.run(t, "set_foreman_settings", args)
	if res.IsError || out["done"] != false || out["confirm_code"] == "" {
		t.Fatalf("preview: %s %s", resultText(res), asJSON(out))
	}
	if asJSON(out["changes"]) != `[{"from":10,"setting":"snapshot_retention","to":2},{"from":365,"setting":"audit_retention_days","to":7}]` {
		t.Errorf("changes %s", asJSON(out["changes"]))
	}
	if out["snapshots_to_remove"] != 3.0 || out["worlds_affected"] != 1.0 || out["audit_entries_to_delete"] != 2.0 {
		t.Errorf("counts %s", asJSON(out))
	}
	if !strings.Contains(asJSON(out["will"]), "Removed snapshots cannot be restored.") {
		t.Errorf("the preview does not say removed snapshots stay gone: %s", asJSON(out["will"]))
	}
	if got := r.generalServer().snapshotRetention(ctx); got != 10 || r.auditCount() != before || r.worlds.KeepAutomatic() != 10 {
		t.Fatalf("the preview changed something: retention %d, audit %d to %d", got, before, r.auditCount())
	}

	_, res2, done := confirmCall(t, r.cs, "set_foreman_settings", args)
	if res2.IsError || done["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res2), asJSON(done))
	}
	if s := r.generalServer(); s.snapshotRetention(ctx) != 2 || s.auditRetentionDays(ctx) != 7 || s.stopCountdown(ctx) != 60*time.Second {
		t.Error("the confirm did not save the changes only")
	}
	if r.worlds.KeepAutomatic() != 2 {
		t.Errorf("the world manager keeps %d, want 2", r.worlds.KeepAutomatic())
	}
	if list, _ := r.worlds.Snapshots(id); len(list) != 5 {
		t.Errorf("saving deleted snapshots: %d left", len(list))
	}
	for action, want := range map[string]string{AuditSnapshotRetentionSet: "from 10 to 2", AuditAuditRetentionSet: "from 365 to 7"} {
		rows := r.auditRows(action)
		if len(rows) != 1 || rows[0].TokenName != "Claude" || rows[0].Username != "alice" || rows[0].Detail != want {
			t.Errorf("%s audit %+v", action, rows)
		}
	}
}

// worldWithAutos makes a world with n automatic snapshots.
func (r *foremanRig) worldWithAutos(t *testing.T, n int) string {
	t.Helper()
	id := r.seedSnapshotWorld()
	for range n {
		r.clock.Advance(time.Minute)
		if _, err := r.worlds.AutoSnapshot(id, world.ReasonBeforeRestore, world.LabelBeforeRestore, "alice"); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// SPEC 21.8, ruling 10: an argument left out keeps its value; one outside
// its range is refused, and a refused call still spends its code.
func TestMCPSetForemanSettingsRefusesBadValues(t *testing.T) {
	r := newForemanRig(t, keyed)
	for name, bad := range map[string]int{"snapshot_retention": 0, "stop_countdown_seconds": 601, "audit_retention_days": 6, "save_upload_mb": 99, "mod_upload_mb": 2049} {
		res, _ := r.run(t, "set_foreman_settings", map[string]any{name: bad})
		if !res.IsError || !strings.Contains(resultText(res), name+" must be a whole number from") {
			t.Errorf("%s=%d: %s", name, bad, resultText(res))
		}
	}
	code := askCode(t, r.cs, "set_foreman_settings", map[string]any{"mod_upload_mb": 300})
	if res, _ := r.run(t, "set_foreman_settings", withCode(map[string]any{"mod_upload_mb": 2049}, code)); !res.IsError {
		t.Error("a bad value was accepted with a code")
	}
	if res, _ := r.run(t, "set_foreman_settings", withCode(map[string]any{"mod_upload_mb": 300}, code)); !res.IsError {
		t.Error("a code was used twice")
	}
	if got := r.generalServer().modUploadLimit(context.Background()); got != 1024<<20 {
		t.Errorf("mod limit %d after refusals", got)
	}
}

// SPEC 21.8, ruling 10: a setting that changed since the preview saves
// nothing and returns a fresh preview and code.
func TestMCPSetForemanSettingsMovedState(t *testing.T) {
	r := newForemanRig(t, keyed)
	ctx := context.Background()
	args := map[string]any{"stop_countdown_seconds": 30}
	code := askCode(t, r.cs, "set_foreman_settings", args)
	if err := r.store.SetAppSetting(ctx, settingModUpload.Name, "250", "bob", r.clock.Now()); err != nil {
		t.Fatal(err)
	}
	res, out := r.run(t, "set_foreman_settings", withCode(args, code))
	fresh, _ := out["confirm_code"].(string)
	if !res.IsError || out["done"] != false || fresh == "" || fresh == code {
		t.Fatalf("moved: %s %s", resultText(res), asJSON(out))
	}
	if r.generalServer().stopCountdown(ctx) != 60*time.Second {
		t.Fatal("a moved preview saved the setting")
	}
	res, out = r.run(t, "set_foreman_settings", withCode(args, fresh))
	if res.IsError || out["done"] != true || r.generalServer().stopCountdown(ctx) != 30*time.Second {
		t.Errorf("the fresh code: %s %s", resultText(res), asJSON(out))
	}
}

// SPEC 21.8: a call that changes nothing returns no code.
func TestMCPSetForemanSettingsNothingToChange(t *testing.T) {
	r := newForemanRig(t, keyed)
	for _, args := range []map[string]any{{}, {"snapshot_retention": 10}} {
		res, out := r.run(t, "set_foreman_settings", args)
		if res.IsError || out["done"] != false || out["confirm_code"] != nil || !strings.Contains(resultText(res), "Nothing to change") {
			t.Errorf("%s: %s %s", asJSON(args), resultText(res), asJSON(out))
		}
	}
}
