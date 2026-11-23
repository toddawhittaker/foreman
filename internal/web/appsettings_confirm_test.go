package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// confirmedForm is a General form with the count the confirm page carries.
func confirmedForm(changes map[string]string, snapshots, worlds int) url.Values {
	f := generalForm(changes)
	f.Set(trimSnapshotsField, strconv.Itoa(snapshots))
	f.Set(trimWorldsField, strconv.Itoa(worlds))
	return f
}

// seedAutos gives a new world n automatic snapshots.
func (hn *harness) seedAutos(n int) string {
	hn.t.Helper()
	id := hn.seedSnapshotWorld()
	for range n {
		hn.clock.Advance(time.Minute)
		if _, err := hn.worlds.AutoSnapshot(id, world.ReasonBeforeRestore, world.LabelBeforeRestore, "alice"); err != nil {
			hn.t.Fatal(err)
		}
	}
	return id
}

// SPEC 17.5, 6.6: a save that lowers retention below what snapshots exist
// asks first, saves nothing, and says what will go and what else changes.
func TestGeneralLoweringThatTrimsAsksFirst(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.seedAutos(4)
	rec := hn.post(generalPath(), generalForm(map[string]string{"snapshot_retention": "2", "mod_upload_mb": "300"}), withCookie(c))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST: %d, want the confirm page", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="confirm-heading"`, `aria-current="true">General</a>`, "2 automatic snapshots will be removed", "Worlds affected: 1.",
		"Removed snapshots cannot be restored. To keep them, raise the number again before the next automatic snapshot.",
		"Automatic snapshots to keep per world: 10 to 2", "Largest mod upload (MB): 1,024 to 300", `id="act-general-confirm"`,
		`name="trim_snapshots" value="2"`, `name="trim_worlds" value="1"`, `name="snapshot_retention" value="2"`, `name="mod_upload_mb" value="300"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the confirm page lacks %q", want)
		}
	}
	if strings.Contains(body, "Default stop countdown (seconds):") {
		t.Error("the confirm page lists a value that did not change")
	}
	s, ctx := hn.generalServer(), context.Background()
	if s.snapshotRetention(ctx) != 10 || s.modUploadLimit(ctx) != 1024<<20 || len(hn.audits(AuditSnapshotRetentionSet)) != 0 {
		t.Error("asking changed a setting or wrote an audit row")
	}
	if list, _ := hn.worlds.Snapshots(hn.firstWorld()); len(list) != 4 {
		t.Errorf("%d snapshots after asking, want 4", len(list))
	}
}

func (hn *harness) firstWorld() string {
	hn.t.Helper()
	list, err := hn.worlds.List(context.Background())
	if err != nil || len(list) == 0 {
		hn.t.Fatalf("no world: %v", err)
	}
	return list[0].ID
}

// Confirming saves every submitted value as one save, with the audit rows.
func TestGeneralConfirmSavesAllValues(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.seedAutos(4)
	hn.wantRedirect(hn.post(generalPath(), confirmedForm(map[string]string{"snapshot_retention": "2", "mod_upload_mb": "300"}, 2, 1), withCookie(c)), generalPath()+"?done=saved")
	s, ctx := hn.generalServer(), context.Background()
	if s.snapshotRetention(ctx) != 2 || s.modUploadLimit(ctx) != 300<<20 || hn.worlds.KeepAutomatic() != 2 {
		t.Error("the confirm did not save every value")
	}
	if got := strings.Join(hn.audits(AuditSnapshotRetentionSet), "|"); got != "alice  from 10 to 2" {
		t.Errorf("retention audit %q", got)
	}
	if got := strings.Join(hn.audits(AuditModUploadLimitSet), "|"); got != "alice  from 1024 to 300" {
		t.Errorf("mod limit audit %q", got)
	}
	if list, _ := hn.worlds.Snapshots(hn.firstWorld()); len(list) != 4 {
		t.Errorf("saving removed snapshots: %d left", len(list))
	}
}

// A count that moved since the confirm page was shown asks again, with the
// count that is true now.
func TestGeneralConfirmWithStaleCountAsksAgain(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.seedAutos(4)
	rec := hn.post(generalPath(), confirmedForm(map[string]string{"snapshot_retention": "2"}, 1, 1), withCookie(c))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "2 automatic snapshots will be removed") {
		t.Fatalf("a stale count: %d\n%s", rec.Code, rec.Body.String())
	}
	if hn.generalServer().snapshotRetention(context.Background()) != 10 {
		t.Error("a stale count saved the setting")
	}
}

// Cancel is a link back to the form: nothing is posted, so nothing is saved.
func TestGeneralConfirmCancelLinksBack(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.seedAutos(4)
	body := hn.post(generalPath(), generalForm(map[string]string{"snapshot_retention": "2"}), withCookie(c)).Body.String()
	if !strings.Contains(body, `<a href="`+generalPath()+`">Cancel, keep the current settings</a>`) {
		t.Error("the confirm page has no Cancel link back to the form")
	}
}

// SPEC 17.5: raising retention, or lowering it below no snapshots, saves in
// one click.
func TestGeneralNoTrimSavesAtOnce(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.seedAutos(2)
	hn.wantRedirect(hn.post(generalPath(), generalForm(map[string]string{"snapshot_retention": "2"}), withCookie(c)), generalPath()+"?done=saved")
	hn.wantRedirect(hn.post(generalPath(), generalForm(map[string]string{"snapshot_retention": "50"}), withCookie(c)), generalPath()+"?done=saved")
	if hn.generalServer().snapshotRetention(context.Background()) != 50 {
		t.Error("raising did not save")
	}
	// A refused value never reaches the confirm step.
	if rec := hn.post(generalPath(), generalForm(map[string]string{"snapshot_retention": "0"}), withCookie(c)); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad value: %d", rec.Code)
	}
}
