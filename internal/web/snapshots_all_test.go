package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func allSnapshotsCase() AllSnapshotsPage {
	s := snapshotsCase().Snapshots
	return AllSnapshotsPage{
		UsedBytes: 2_100_000, FreeBytes: 40_000_000_000, Done: "Foreman deleted the snapshot taken 2026-11-03 14:25:00 UTC.",
		Rows: []SnapshotRow{
			{WorldName: longName, SnapshotUse: world.SnapshotUse{WorldID: "base", Snapshot: s[0], Frees: 1000}},
			{WorldName: "Other", SnapshotUse: world.SnapshotUse{WorldID: "other", Snapshot: s[1]}},
		},
	}
}

// seedTwoSnapshots makes a world with two manual snapshots a minute apart.
func (hn *harness) seedTwoSnapshots() (id string, first, second world.Snapshot) {
	hn.t.Helper()
	id = hn.seedSnapshotWorld()
	first, err := hn.worlds.TakeSnapshot(id, "First", "alice")
	if err != nil {
		hn.t.Fatal(err)
	}
	hn.clock.Advance(time.Minute)
	second, err = hn.worlds.TakeSnapshot(id, "Second", "alice")
	if err != nil {
		hn.t.Fatal(err)
	}
	return id, first, second
}

func (hn *harness) audits(action string) []string {
	hn.t.Helper()
	list, err := hn.store.ListAudit(context.Background(), 100)
	if err != nil {
		hn.t.Fatal(err)
	}
	var out []string
	for _, e := range list {
		if e.Action == action {
			out = append(out, e.Username+" "+e.WorldID+" "+e.Detail)
		}
	}
	return out
}

// SPEC 6.12: the Snapshots tab lists every world's snapshots with a total
// that counts shared zips once, and links each to a delete confirmation.
func TestSnapshotsTabListsEveryWorld(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, first, _ := hn.seedTwoSnapshots()

	rec := hn.get(PathSnapshots, withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{
		`aria-current="page">Snapshots</a>`, "<strong>Base</strong>", "First", "Second", "by alice",
		`href="` + snapshotDeletePath(id, first.Name) + `?from=all"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	_, total, err := hn.worlds.AllSnapshots()
	if err != nil || !strings.Contains(body, "<dd>"+world.FormatBytes(total)+"</dd>") {
		t.Errorf("total %d not shown: %v", total, err)
	}
	if world := hn.get(snapshotsPath(id), withCookie(c)).Body.String(); !strings.Contains(world, `href="`+snapshotDeletePath(id, first.Name)+`"`) {
		t.Error("the world's Snapshots page has no delete link")
	}
	hn.wantRedirect(hn.get(PathSnapshots), signInAfter(PathSnapshots))
}

func TestSnapshotDeleteConfirmsThenDeletesAndAudits(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, first, second := hn.seedTwoSnapshots()
	saves := filepath.Join(hn.worlds.Dirs().Worlds, id, "saves", "base.zip")

	form := hn.get(snapshotDeletePath(id, first.Name), withCookie(c)).Body.String()
	for _, want := range []string{`Delete the snapshot "First" of Base from`, "2026-11-02 12:00:00 UTC", "Free ", "Delete snapshot"} {
		if !strings.Contains(form, want) {
			t.Errorf("confirmation missing %q:\n%s", want, form)
		}
	}
	if strings.Contains(form, "Rollback uses this snapshot") {
		t.Error("a manual snapshot shows the rollback warning")
	}
	if len(hn.audits(AuditSnapshotDelete)) != 0 {
		t.Error("the confirmation page wrote an audit entry")
	}

	rec := hn.post(snapshotDeletePath(id, first.Name), nil, withCookie(c))
	hn.wantRedirect(rec, snapshotsPath(id)+"?deleted="+first.Name)
	if list, _ := hn.worlds.Snapshots(id); len(list) != 1 || list[0].Name != second.Name {
		t.Errorf("left %+v", list)
	}
	if b, err := os.ReadFile(saves); err != nil || string(b) != "save one" { //nolint:gosec // a test temp file
		t.Errorf("world save after delete: %q %v", b, err)
	}
	if got := hn.audits(AuditSnapshotDelete); len(got) != 1 || !strings.HasPrefix(got[0], "alice "+id+" "+first.Name) {
		t.Errorf("audit %q", got)
	}
	done := hn.get(rec.Header().Get("Location"), withCookie(c)).Body.String()
	if !strings.Contains(done, "Foreman deleted the snapshot taken 2026-11-02 12:00:00 UTC.") {
		t.Error("no done message")
	}

	// From the tab, the delete returns to the tab.
	rec = hn.post(snapshotDeletePath(id, second.Name), map[string][]string{"from": {"all"}}, withCookie(c))
	hn.wantRedirect(rec, PathSnapshots+"?deleted="+second.Name)
}

func TestSnapshotDeleteRefusesBadRequests(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, first, _ := hn.seedTwoSnapshots()
	path := snapshotDeletePath(id, first.Name)

	if rec := hn.post(path, nil, withCookie(c), withHeader("Sec-Fetch-Site", "cross-site")); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
	if rec := hn.post(path, nil, withCookie(c), withHeader("Origin", "https://evil.example")); rec.Code != http.StatusForbidden {
		t.Errorf("foreign origin: %d", rec.Code)
	}
	hn.wantRedirect(hn.post(path, nil), PathSignIn)
	for _, p := range []string{
		snapshotDeletePath(id, "20991103T142500Z"),
		snapshotDeletePath(id, "..%2f..%2fworlds"),
		snapshotDeletePath(id, "x"),
		snapshotDeletePath("nope", first.Name),
	} {
		if rec := hn.post(p, nil, withCookie(c)); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: %d", p, rec.Code)
		}
	}

	unlock, err := hn.worlds.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	rec := hn.post(path, nil, withCookie(c))
	unlock()
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Nothing was deleted") {
		t.Errorf("busy: %d", rec.Code)
	}
	if list, _ := hn.worlds.Snapshots(id); len(list) != 2 {
		t.Errorf("a refused delete removed a snapshot: %+v", list)
	}
	if len(hn.audits(AuditSnapshotDelete)) != 0 {
		t.Error("a refused delete was audited")
	}
}

// SPEC 6.12, 10.4: the snapshot rollback depends on can be deleted, but the
// confirmation says rollback goes with it.
func TestSnapshotDeleteWarnsAboutRollback(t *testing.T) {
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
	form := hn.get(snapshotDeletePath(id, snap.Name), withCookie(c)).Body.String()
	if !strings.Contains(form, "you can no longer roll Base back to game version 2.0.72") {
		t.Errorf("no rollback warning:\n%s", form)
	}
	hn.wantRedirect(hn.post(snapshotDeletePath(id, snap.Name), nil, withCookie(c)), snapshotsPath(id)+"?deleted="+snap.Name)
	if _, _, err := hn.worlds.RollbackTarget(id); err == nil {
		t.Error("rollback still has a target")
	}
}
