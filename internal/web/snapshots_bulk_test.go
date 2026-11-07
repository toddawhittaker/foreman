package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func ref(id, name string) string { return id + "/" + name }

// playOn replaces the world's save, so snapshots taken before it hold the
// old save alone (or share it only with each other).
func (hn *harness) playOn(id string) {
	hn.t.Helper()
	p := filepath.Join(hn.worlds.Dirs().Worlds, id, "saves", "base.zip")
	if err := os.Remove(p); err != nil {
		hn.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("the world moved on"), 0o600); err != nil {
		hn.t.Fatal(err)
	}
}

// ticked lists the checkbox values that start ticked on a page.
func ticked(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`<input type="checkbox" name="snap" value="([^"]+)"( checked)?>`).FindAllStringSubmatch(body, -1) {
		if m[2] != "" {
			out = append(out, m[1])
		}
	}
	return out
}

// SPEC 6.12: the shortcut ticks automatic snapshots only, in one world or
// all of them, and keeps what was ticked by hand; it deletes nothing.
func TestSelectAllAutomaticTicksOnlyAutomaticSnapshots(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, other := hn.seedTwo()
	manual, err := hn.worlds.TakeSnapshot(id, "Mine", "alice")
	if err != nil {
		t.Fatal(err)
	}
	hn.clock.Advance(time.Minute)
	auto, err := hn.worlds.AutoSnapshot(id, world.ReasonSwitch, world.LabelBeforeSwitch, "alice")
	if err != nil {
		t.Fatal(err)
	}
	hn.clock.Advance(time.Minute)
	otherAuto, err := hn.worlds.AutoSnapshot(other, world.ReasonSwitch, world.LabelBeforeSwitch, "alice")
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) []string {
		rec := hn.get(path, withCookie(c))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		return ticked(rec.Body.String())
	}
	if got := get(PathSnapshots); len(got) != 0 {
		t.Errorf("a fresh page ticks %v", got)
	}
	if got := get(PathSnapshots + "?select=automatic"); len(got) != 2 || got[0] != ref(id, auto.Name) || got[1] != ref(other, otherAuto.Name) {
		t.Errorf("all worlds ticked %v", got)
	}
	if got := get(PathSnapshots + "?select=automatic:" + other); len(got) != 1 || got[0] != ref(other, otherAuto.Name) {
		t.Errorf("one world ticked %v", got)
	}
	// A box ticked by hand stays ticked when the shortcut is pressed.
	if got := get(PathSnapshots + "?select=automatic&snap=" + url.QueryEscape(ref(id, manual.Name))); len(got) != 3 {
		t.Errorf("hand tick lost: %v", got)
	}
	if got := get(snapshotsPath(id) + "?select=automatic"); len(got) != 1 || got[0] != ref(id, auto.Name) {
		t.Errorf("the world's page ticked %v", got)
	}
	// A value that names no snapshot is ignored.
	if got := get(PathSnapshots + "?snap=../etc&snap=nope&snap=" + url.QueryEscape(ref("..", manual.Name))); len(got) != 0 {
		t.Errorf("bad values ticked %v", got)
	}
	if list, _ := hn.worlds.Snapshots(id); len(list) != 2 {
		t.Errorf("a shortcut deleted something: %+v", list)
	}
	if len(hn.audits(AuditSnapshotDelete)) != 0 {
		t.Error("a shortcut was audited as a delete")
	}
}

// SPEC 6.12: the confirmation states what the whole selection frees, which
// is more than the sum of its rows when they share a file only with each
// other, and says when rollback goes with a snapshot.
func TestBulkDeleteConfirmationCountsTheSelectionTogether(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, first, second := hn.seedTwoSnapshots()
	hn.playOn(id)
	count, err := hn.worlds.CountSnapshots()
	if err != nil {
		t.Fatal(err)
	}
	both := []world.SnapshotRef{{WorldID: id, Name: first.Name}, {WorldID: id, Name: second.Name}}
	alone := count.Frees(both[:1]) + count.Frees(both[1:])
	together := count.Frees(both)
	if together <= alone {
		t.Fatalf("the test needs a shared file: alone %d, together %d", alone, together)
	}

	q := url.Values{"snap": {ref(id, first.Name), ref(id, second.Name), ref(id, "20991103T142500Z"), "junk"}}
	rec := hn.get(PathSnapshotsDelete+"?"+q.Encode(), withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		`<h1 id="confirm-heading">Delete 2 snapshots?</h1>`, `Base: &#34;First&#34;, taken`, `Base: &#34;Second&#34;, taken`,
		"Free " + world.FormatBytes(together) + " of disk space",
		"Already deleted", "is deleted already",
		`<input type="hidden" name="snap" value="` + ref(id, first.Name) + `">`,
		`<input type="hidden" name="snap" value="` + ref(id, second.Name) + `">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("confirmation missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `value="`+ref(id, "20991103T142500Z")+`"`) {
		t.Error("a snapshot that is gone would be posted")
	}
	if len(hn.audits(AuditSnapshotDelete)) != 0 {
		t.Error("the confirmation page wrote an audit entry")
	}

	rec = hn.get(PathSnapshotsDelete, withCookie(c))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Nothing is ticked.") {
		t.Errorf("nothing ticked: %d", rec.Code)
	}
	hn.wantRedirect(hn.get(PathSnapshotsDelete), signInAfter(PathSnapshotsDelete))
}

// A gone snapshot whose name has the right shape but no real date is listed
// by its name, never with the zero date.
func TestBulkDeleteGoneSnapshotWithoutADateShowsItsName(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id := hn.seedSnapshotWorld()
	const bad = "99999999T999999Z"
	if _, ok := world.SnapshotNameTime(bad); ok || !world.ValidSnapshotName(bad) {
		t.Fatal("the test needs a name of the right shape that holds no date")
	}
	rec := hn.get(PathSnapshotsDelete+"?snap="+url.QueryEscape(ref(id, bad)), withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Already deleted") {
		t.Fatalf("status %d:\n%s", rec.Code, body)
	}
	if !strings.Contains(body, "the snapshot <code>"+bad+"</code> is deleted already") {
		t.Errorf("the name is not shown:\n%s", body)
	}
	if strings.Contains(body, "0001-01-01") {
		t.Errorf("the zero date is shown:\n%s", body)
	}
}

func TestBulkDeleteWarnsAboutRollback(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id := hn.seedSnapshotWorld()
	snap, err := hn.worlds.AutoSnapshot(id, world.ReasonGameUpdate, world.LabelBeforeUpdate, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := hn.worlds.SetGameVersionHeld(id, "2.0.73"); err != nil {
		t.Fatal(err)
	}
	body := hn.get(PathSnapshotsDelete+"?snap="+url.QueryEscape(ref(id, snap.Name)), withCookie(c)).Body.String()
	if !strings.Contains(body, "you can no longer roll Base back to game version 2.0.72") {
		t.Errorf("no rollback warning:\n%s", body)
	}
}

// SPEC 6.12: each snapshot is audited before it goes; only the confirmed
// ones are deleted; the delete returns to the page it started on.
func TestBulkDeleteDeletesAuditsAndReturns(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, first, second := hn.seedTwoSnapshots()
	hn.clock.Advance(time.Minute)
	keep, err := hn.worlds.TakeSnapshot(id, "Keep", "alice")
	if err != nil {
		t.Fatal(err)
	}
	save := filepath.Join(hn.worlds.Dirs().Worlds, id, "saves", "base.zip")

	form := url.Values{"snap": {ref(id, first.Name), ref(id, second.Name)}}
	rec := hn.post(PathSnapshotsDelete, form, withCookie(c))
	hn.wantRedirect(rec, PathSnapshots+"?"+deletedQuery(first.Name, second.Name))
	if list, _ := hn.worlds.Snapshots(id); len(list) != 1 || list[0].Name != keep.Name {
		t.Errorf("left %+v", list)
	}
	if b, err := os.ReadFile(save); err != nil || string(b) != "save one" { //nolint:gosec // a test temp file
		t.Errorf("world save after delete: %q %v", b, err)
	}
	got := hn.audits(AuditSnapshotDelete)
	if len(got) != 2 || !strings.Contains(strings.Join(got, "\n"), "alice "+id+" "+first.Name) || !strings.Contains(strings.Join(got, "\n"), "alice "+id+" "+second.Name) {
		t.Errorf("audit %q", got)
	}
	if done := hn.get(rec.Header().Get("Location"), withCookie(c)).Body.String(); !strings.Contains(done, "Foreman deleted 2 snapshots.") {
		t.Error("no done message")
	}

	// From a world's page, the delete returns there.
	rec = hn.post(PathSnapshotsDelete, url.Values{"snap": {ref(id, keep.Name)}, "world": {id}}, withCookie(c))
	hn.wantRedirect(rec, snapshotsPath(id)+"?"+deletedQuery(keep.Name))
}

// SPEC 6.12: a delete that fails is reported, and the ones already done
// stay done; a world an operation holds is refused.
func TestBulkDeleteReportsWhatStayed(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, other := hn.seedTwo()
	a, err := hn.worlds.TakeSnapshot(id, "One", "alice")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hn.worlds.TakeSnapshot(other, "Two", "alice")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := hn.worlds.Lock(other)
	if err != nil {
		t.Fatal(err)
	}
	rec := hn.post(PathSnapshotsDelete, url.Values{"snap": {ref(id, a.Name), ref(other, b.Name)}}, withCookie(c))
	unlock()
	body := rec.Body.String()
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		"Foreman deleted the snapshot taken", "1 of 2 snapshots were not deleted.",
		"The last try failed: Another operation is running on this world.", `<h1 id="confirm-heading">Delete 1 snapshot?</h1>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
	if list, _ := hn.worlds.Snapshots(id); len(list) != 0 {
		t.Errorf("the free world's snapshot stayed: %+v", list)
	}
	if list, _ := hn.worlds.Snapshots(other); len(list) != 1 {
		t.Errorf("the busy world's snapshot went: %+v", list)
	}
	if got := hn.audits(AuditSnapshotDelete); len(got) != 1 || !strings.Contains(got[0], id) {
		t.Errorf("audit %q", got)
	}

	// Everything busy: nothing goes, nothing is audited, and the page says so.
	unlock, err = hn.worlds.Lock(other)
	if err != nil {
		t.Fatal(err)
	}
	rec = hn.post(PathSnapshotsDelete, url.Values{"snap": {ref(other, b.Name)}}, withCookie(c))
	unlock()
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Nothing was deleted.") {
		t.Errorf("all busy: %d", rec.Code)
	}
	if len(hn.audits(AuditSnapshotDelete)) != 1 {
		t.Error("a refused delete was audited")
	}
}

// A form submitted twice, or with some snapshots already gone, counts what
// is gone as deleted and counts as kept only what really stayed.
func TestBulkDeleteCountsGoneSnapshotsAsDeleted(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, other := hn.seedTwo()
	a, err := hn.worlds.TakeSnapshot(id, "One", "alice")
	if err != nil {
		t.Fatal(err)
	}
	b, err := hn.worlds.TakeSnapshot(other, "Two", "alice")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"snap": {ref(id, a.Name), ref(other, b.Name)}}
	hn.wantRedirect(hn.post(PathSnapshotsDelete, form, withCookie(c)), PathSnapshots+"?"+deletedQuery(a.Name, b.Name))
	if n := len(hn.audits(AuditSnapshotDelete)); n != 2 {
		t.Fatalf("%d audit entries after the first post", n)
	}
	// The same form again.
	hn.wantRedirect(hn.post(PathSnapshotsDelete, form, withCookie(c)), PathSnapshots+"?"+deletedQuery(a.Name, b.Name))
	if n := len(hn.audits(AuditSnapshotDelete)); n != 2 {
		t.Errorf("a resubmit was audited: %d entries", n)
	}

	// One gone, one kept by a running operation: the count says 1 of 2.
	c2, err := hn.worlds.TakeSnapshot(other, "Three", "alice")
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := hn.worlds.Lock(other)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	rec := hn.post(PathSnapshotsDelete, url.Values{"snap": {ref(id, a.Name), ref(other, c2.Name)}}, withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusConflict || !strings.Contains(body, "1 of 2 snapshots were not deleted.") {
		t.Errorf("partial: %d\n%s", rec.Code, body)
	}
	if !strings.Contains(body, "Foreman deleted the snapshot taken") {
		t.Errorf("the gone snapshot is not counted as deleted:\n%s", body)
	}
	if strings.Contains(body, "not there any more") {
		t.Error("a gone snapshot is listed as staying")
	}
}

func TestBulkDeleteRefusesBadRequests(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, first, _ := hn.seedTwoSnapshots()
	form := url.Values{"snap": {ref(id, first.Name)}}

	if rec := hn.post(PathSnapshotsDelete, form, withCookie(c), withHeader("Sec-Fetch-Site", "cross-site")); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
	hn.wantRedirect(hn.post(PathSnapshotsDelete, form), PathSignIn)
	for name, bad := range map[string]url.Values{
		"nothing ticked": {},
		"bad values":     {"snap": {"../" + first.Name, id + "/../" + first.Name, id + "/x", ref("..", first.Name), "", "/"}},
	} {
		if rec := hn.post(PathSnapshotsDelete, bad, withCookie(c)); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	// A snapshot that is not there counts as deleted: the Done page, with
	// nothing removed and nothing audited.
	rec := hn.post(PathSnapshotsDelete, url.Values{"snap": {ref(id, "20991103T142500Z")}}, withCookie(c))
	hn.wantRedirect(rec, PathSnapshots+"?"+deletedQuery("20991103T142500Z"))
	if list, _ := hn.worlds.Snapshots(id); len(list) != 2 {
		t.Errorf("a refused delete removed a snapshot: %+v", list)
	}
	if len(hn.audits(AuditSnapshotDelete)) != 0 {
		t.Error("a refused delete was audited")
	}
}
