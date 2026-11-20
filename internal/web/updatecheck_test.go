package web

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeUpdates stands in for the background check: it always knows of
// release 1.3.0 and counts the checks asked for.
type fakeUpdates struct {
	enabled func(context.Context) bool
	asked   atomic.Int32
}

func (f *fakeUpdates) Newer(ctx context.Context) string {
	if !f.enabled(ctx) {
		return ""
	}
	return "1.3.0"
}

func (f *fakeUpdates) CheckSoon() { f.asked.Add(1) }

func newUpdatesHarness(t *testing.T) (*harness, *fakeUpdates) {
	t.Helper()
	var f *fakeUpdates
	hn := newHarnessDeps(t, nil, nil, func(d *Deps) {
		f = &fakeUpdates{enabled: UpdateCheckEnabled(d.Store)}
		d.Updates = f
	})
	return hn, f
}

// SPEC 17, 14.10: About names a newer release with a link to its page,
// both on the About page and in the dialog on every other page.
func TestAboutShowsANewerRelease(t *testing.T) {
	hn, _ := newUpdatesHarness(t)
	c := hn.signIn()
	for _, path := range []string{PathAbout, generalPath()} {
		body := hn.get(path, withCookie(c)).Body.String()
		for _, want := range []string{`id="about-newer"`, "Foreman 1.3.0 is available.",
			`<a href="https://github.com/toddawhittaker/foreman/releases/tag/v1.3.0" target="_blank" rel="noopener noreferrer">See what&#39;s new`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s lacks %q", path, want)
			}
		}
	}
}

// SPEC 13.1: the switch is on by default; turning it off is stored, audited
// "from on to off" and hides the notice; turning it on asks GitHub at once.
func TestUpdateCheckSwitch(t *testing.T) {
	hn, f := newUpdatesHarness(t)
	c := hn.signIn()
	if body := hn.get(generalPath(), withCookie(c)).Body.String(); !strings.Contains(body, `name="update_check" checked`) {
		t.Fatal("the switch is not on by default")
	}
	// generalForm has no update_check, as a browser posts an unticked box.
	if rec := hn.post(generalPath(), generalForm(nil), withCookie(c)); rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d", rec.Code)
	}
	if got := strings.Join(hn.audits(AuditUpdateCheckSet), "|"); got != "alice  from on to off" {
		t.Errorf("audit %q", got)
	}
	if body := hn.get(PathAbout, withCookie(c)).Body.String(); strings.Contains(body, "about-newer") {
		t.Error("the notice shows with the switch off")
	}
	if f.asked.Load() != 0 {
		t.Error("turning the switch off asked GitHub")
	}
	if rec := hn.post(generalPath(), generalForm(map[string]string{"update_check": "on"}), withCookie(c)); rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d", rec.Code)
	}
	if got := strings.Join(hn.audits(AuditUpdateCheckSet), "|"); !strings.Contains(got, "from off to on") {
		t.Errorf("audit %q", got)
	}
	if f.asked.Load() != 1 {
		t.Errorf("turning the switch on asked GitHub %d times, want 1", f.asked.Load())
	}
	// Saving again with no change asks nothing and audits nothing.
	hn.post(generalPath(), generalForm(map[string]string{"update_check": "on"}), withCookie(c))
	if f.asked.Load() != 1 || len(hn.audits(AuditUpdateCheckSet)) != 2 {
		t.Error("an unchanged switch was saved again")
	}
}

// SPEC 21.8: foreman_settings shows the switch and set_foreman_settings
// changes it like any other General setting.
func TestMCPUpdateCheck(t *testing.T) {
	r := newForemanRig(t, nil)
	_, out := r.run(t, "foreman_settings", map[string]any{})
	if !strings.Contains(asJSON(out), `"update_check":true`) {
		t.Fatalf("settings: %s", asJSON(out))
	}
	args := map[string]any{"update_check": false}
	code := askCode(t, r.cs, "set_foreman_settings", args)
	res, out := r.run(t, "set_foreman_settings", withCode(args, code))
	if res.IsError || out["done"] != true {
		t.Fatalf("confirm: %s %s", resultText(res), asJSON(out))
	}
	if text := asJSON(out); !strings.Contains(text, `{"from":true,"setting":"update_check","to":false}`) {
		t.Errorf("changes: %s", text)
	}
	if UpdateCheckEnabled(r.store)(context.Background()) {
		t.Error("the switch is still on")
	}
	if rows := r.auditRows(AuditUpdateCheckSet); len(rows) != 1 || rows[0].Detail != "from on to off" {
		t.Errorf("audit rows %+v", rows)
	}
}

// SPEC 14.10: a switch that cannot be read, or holds an unknown value,
// counts as off for the background check, so a fault sends no request.
func TestUpdateCheckEnabledFailsClosed(t *testing.T) {
	hn := newHarness(t, nil)
	ctx := context.Background()
	enabled := UpdateCheckEnabled(hn.store)
	if !enabled(ctx) {
		t.Fatal("the switch is off with nothing stored")
	}
	if err := hn.store.SetAppSetting(ctx, settingUpdateCheck.Name, "maybe", "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if enabled(ctx) {
		t.Error("an unknown stored value counts as on")
	}
	if err := hn.store.SetAppSetting(ctx, settingUpdateCheck.Name, "on", "alice", hn.clock.Now()); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if enabled(cancelled) {
		t.Error("a failed read counts as on")
	}
}

// SPEC 13.1, 17.5: the confirm page lists the switch when it changes and
// carries it through, so confirming keeps a ticked box on and turns it off
// only when the admin unticked it.
func TestGeneralConfirmCarriesTheSwitch(t *testing.T) {
	hn, _ := newUpdatesHarness(t)
	c := hn.signIn()
	hn.seedAutos(4)
	ticked := map[string]string{"snapshot_retention": "2", "update_check": "on"}
	body := hn.post(generalPath(), generalForm(ticked), withCookie(c)).Body.String()
	if !strings.Contains(body, `name="update_check" value="on"`) || strings.Contains(body, "Check GitHub for new Foreman versions:") {
		t.Error("a ticked, unchanged switch is not carried, or is listed as a change")
	}
	hn.wantRedirect(hn.post(generalPath(), confirmedForm(ticked, 2, 1), withCookie(c)), generalPath()+"?done=saved")
	if !UpdateCheckEnabled(hn.store)(context.Background()) || len(hn.audits(AuditUpdateCheckSet)) != 0 {
		t.Fatal("confirming with the box ticked changed the switch")
	}

	unticked := map[string]string{"snapshot_retention": "1"}
	body = hn.post(generalPath(), generalForm(unticked), withCookie(c)).Body.String()
	if strings.Contains(body, `name="update_check"`) || !strings.Contains(body, "Check GitHub for new Foreman versions: on to off") {
		t.Error("an unticked switch is carried, or its change is not listed")
	}
	hn.wantRedirect(hn.post(generalPath(), confirmedForm(unticked, 3, 1), withCookie(c)), generalPath()+"?done=saved")
	if UpdateCheckEnabled(hn.store)(context.Background()) {
		t.Error("confirming with the box unticked left the switch on")
	}
}
