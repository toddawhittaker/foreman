package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func snapshotsCase() SnapshotsPage {
	at := time.Date(2026, 11, 3, 14, 25, 0, 0, time.UTC)
	return SnapshotsPage{
		WorldID: "base", WorldName: "Base", AddBytes: 2_100_000, FreeBytes: 40_000_000_000, Copied: true, More: true,
		Done: `Foreman took the snapshot "x".`, DoneSnap: "20261103T142500Z",
		Snapshots: []world.Snapshot{
			{Name: "20261103T142500Z", Manifest: world.Manifest{Label: longName, Reason: world.ReasonManual, TakenAt: at,
				Files: []world.ManifestEntry{{Path: "saves/a.zip", Size: 2_100_000}}}},
			{Name: "20261103T142400Z", Manifest: world.Manifest{Label: "Before restore", Reason: world.ReasonBeforeRestore, TakenAt: at}},
		},
	}
}

func (hn *harness) seedSnapshotWorld() string {
	hn.t.Helper()
	id, err := hn.worlds.Create(hn.t.Context(), "Base", "2.0.72", true)
	if err != nil {
		hn.t.Fatal(err)
	}
	p := filepath.Join(hn.worlds.Dirs().Worlds, id, "saves", "base.zip")
	if err := os.WriteFile(p, []byte("save one"), 0o600); err != nil {
		hn.t.Fatal(err)
	}
	return id
}

func TestSnapshotTakeListAndRestore(t *testing.T) {
	// Taking and restoring lift the server's deadlines, which needs a real
	// connection.
	ls := newLiveServer(t, nil)
	hn := ls.harness
	c := hn.signIn()
	id := hn.seedSnapshotWorld()

	rec := ls.do(http.MethodPost, snapshotsPath(id), url.Values{"label": {"  First  "}}, http.Header{"Cookie": {c.String()}})
	if rec.StatusCode != http.StatusSeeOther {
		t.Fatalf("take: status %d\n%s", rec.StatusCode, rec.body)
	}
	loc := rec.Header.Get("Location")
	body := hn.get(loc, withCookie(c)).Body.String()
	if !strings.Contains(body, `Foreman took the snapshot &#34;First&#34;.`) || !strings.Contains(body, "<span>Manual<br>First<span class=") {
		t.Errorf("list after take:\n%s", body)
	}
	list, _ := hn.worlds.Snapshots(id)
	if len(list) != 1 || list[0].TakenBy != "alice" {
		t.Fatalf("snapshots %+v", list)
	}
	snap := list[0].Name

	save := filepath.Join(hn.worlds.Dirs().Worlds, id, "saves", "base.zip")
	if err := os.Remove(save); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(save, []byte("played on"), 0o600); err != nil {
		t.Fatal(err)
	}

	form := hn.get(restorePath(id, snap), withCookie(c)).Body.String()
	if !strings.Contains(form, `Restore the snapshot "First"`) || !strings.Contains(form, "Before restore") {
		t.Errorf("confirmation:\n%s", form)
	}
	hn.clock.Advance(time.Minute)
	resp := ls.do(http.MethodPost, restorePath(id, snap), url.Values{}, http.Header{"Cookie": {c.String()}})
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "undo=") {
		t.Fatalf("restore: status %d %q\n%s", resp.StatusCode, resp.Header.Get("Location"), resp.body)
	}
	if b, _ := os.ReadFile(save); string(b) != "save one" { //nolint:gosec // a test temp file
		t.Errorf("save after restore %q", b)
	}
	body = hn.get(resp.Header.Get("Location"), withCookie(c)).Body.String()
	if !strings.Contains(body, "to undo the restore, restore that snapshot") {
		t.Errorf("done message missing:\n%s", body)
	}
}

func TestSnapshotRoutesRefuseBadInput(t *testing.T) {
	ls := newLiveServer(t, nil)
	hn := ls.harness
	c := hn.signIn()
	id := hn.seedSnapshotWorld()

	for _, p := range []string{
		snapshotsPath("nope"),
		restorePath(id, "20261103T142500Z"),
		restorePath(id, "..%2f..%2fetc"),
		restorePath(id, "x"),
	} {
		if rec := hn.get(p, withCookie(c)); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d", p, rec.Code)
		}
	}
	cookie := http.Header{"Cookie": {c.String()}}
	rec := ls.do(http.MethodPost, snapshotsPath(id), url.Values{"label": {strings.Repeat("x", 101)}}, cookie)
	if rec.StatusCode != http.StatusBadRequest || !strings.Contains(rec.body, "Use at most 100 characters") {
		t.Errorf("long label: %d", rec.StatusCode)
	}
	if list, _ := hn.worlds.Snapshots(id); len(list) != 0 {
		t.Errorf("a refused label took a snapshot")
	}

	unlock, err := hn.worlds.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	rec = ls.do(http.MethodPost, snapshotsPath(id), url.Values{}, cookie)
	unlock()
	if rec.StatusCode != http.StatusConflict || !strings.Contains(rec.body, "nother operation is running") {
		t.Errorf("busy: %d", rec.StatusCode)
	}
	hn.wantRedirect(hn.get(snapshotsPath(id)), signInAfter(snapshotsPath(id)))
}
