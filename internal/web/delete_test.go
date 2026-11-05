package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedTwo makes the active world "Base" and a second world "Second", and
// returns the second's id.
func (hn *harness) seedTwo() (activeID, otherID string) {
	hn.t.Helper()
	activeID = hn.seedSnapshotWorld()
	otherID, err := hn.worlds.Create(hn.t.Context(), "Second", "2.0.72", false)
	if err != nil {
		hn.t.Fatal(err)
	}
	p := filepath.Join(hn.worlds.Dirs().Worlds, otherID, "saves", "second.zip")
	if err := os.WriteFile(p, []byte("second"), 0o600); err != nil {
		hn.t.Fatal(err)
	}
	return activeID, otherID
}

// SPEC 5.10: the confirmation page, the final snapshot, then the list of
// deleted worlds with a way back.
func TestDeleteWithFinalSnapshotThenRestore(t *testing.T) {
	ls := newLiveServer(t, nil)
	hn := ls.harness
	c := hn.signIn()
	_, id := hn.seedTwo()

	form := hn.get(deletePath(id), withCookie(c)).Body.String()
	if !strings.Contains(form, `Delete the world "Second"?`) || !strings.Contains(form, `name="final_snapshot" checked`) {
		t.Errorf("confirmation:\n%s", form)
	}

	rec := hn.post(deletePath(id), url.Values{"final_snapshot": {"on"}, "confirm_name": {"Second"}}, withCookie(c))
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != PathWorlds+"?deleted="+id {
		t.Fatalf("delete: %d %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if _, err := os.Stat(filepath.Join(hn.worlds.Dirs().Worlds, id)); !os.IsNotExist(err) {
		t.Errorf("world folder remains: %v", err)
	}
	body := hn.get(rec.Header().Get("Location"), withCookie(c)).Body.String()
	for _, want := range []string{"Deleted Second. Its snapshots are under", `href="/deleted"`} {
		if !strings.Contains(body, want) {
			t.Errorf("world list lacks %q:\n%s", want, body)
		}
	}
	body = hn.get(PathDeleted, withCookie(c)).Body.String()
	for _, want := range []string{"Before delete", "Restore"} {
		if !strings.Contains(body, want) {
			t.Errorf("deleted page lacks %q:\n%s", want, body)
		}
	}
	list, _ := hn.worlds.Snapshots(id)
	if len(list) != 1 {
		t.Fatalf("snapshots %+v", list)
	}

	// Restoring a deleted world brings it back, and the confirmation
	// renders for a world with no folder.
	if rec := hn.get(restorePath(id, list[0].Name), withCookie(c)); rec.Code != http.StatusOK {
		t.Fatalf("restore form: %d", rec.Code)
	}
	hn.clock.Advance(time.Minute)
	resp := ls.do(http.MethodPost, restorePath(id, list[0].Name), url.Values{}, http.Header{"Cookie": {c.String()}})
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "restored=") {
		t.Fatalf("restore: %d %q\n%s", resp.StatusCode, resp.Header.Get("Location"), resp.body)
	}
	if b, _ := os.ReadFile(filepath.Join(hn.worlds.Dirs().Worlds, id, "saves", "second.zip")); string(b) != "second" { //nolint:gosec // a test temp file
		t.Errorf("restored save %q", b)
	}
	body = hn.get(resp.Header.Get("Location"), withCookie(c)).Body.String()
	if !strings.Contains(body, "The world is restored from the snapshot") {
		t.Errorf("done message missing:\n%s", body)
	}
	if body := hn.get(PathDeleted, withCookie(c)).Body.String(); !strings.Contains(body, "No deleted worlds") {
		t.Errorf("restored world still listed:\n%s", body)
	}
}

func TestDeleteWithoutSnapshot(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	_, id := hn.seedTwo()
	rec := hn.post(deletePath(id), url.Values{"confirm_name": {"Second"}}, withCookie(c))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d\n%s", rec.Code, rec.Body)
	}
	if list, _ := hn.worlds.Snapshots(id); len(list) != 0 {
		t.Errorf("a snapshot was taken with the box unticked: %v", list)
	}
	body := hn.get(rec.Header().Get("Location"), withCookie(c)).Body.String()
	if !strings.Contains(body, "It had no snapshots") {
		t.Errorf("deleted page:\n%s", body)
	}
}

func TestDeleteRefusals(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	activeID, id := hn.seedTwo()

	rec := hn.post(deletePath(id), url.Values{"final_snapshot": {"on"}, "confirm_name": {"second"}}, withCookie(c))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Type Second exactly, or cancel") {
		t.Errorf("wrong name: %d\n%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `value="second"`) {
		t.Errorf("the typed name was not kept")
	}
	rec = hn.post(deletePath(activeID), url.Values{"final_snapshot": {"on"}, "confirm_name": {"Base"}}, withCookie(c))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "active world cannot be deleted") {
		t.Errorf("active: %d\n%s", rec.Code, rec.Body)
	}
	unlock, err := hn.worlds.Lock(id)
	if err != nil {
		t.Fatal(err)
	}
	rec = hn.post(deletePath(id), url.Values{"confirm_name": {"Second"}}, withCookie(c))
	unlock()
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "nother operation is running") {
		t.Errorf("busy: %d", rec.Code)
	}
	for _, p := range []string{deletePath("nope"), restorePath("nope", "20261103T142500Z"), restorePath(id, "20261103T142500Z")} {
		if rec := hn.get(p, withCookie(c)); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d", p, rec.Code)
		}
	}
	for _, d := range []string{id, activeID} {
		if _, err := os.Stat(filepath.Join(hn.worlds.Dirs().Worlds, d)); err != nil {
			t.Errorf("a refused delete changed %s: %v", d, err)
		}
	}
	hn.wantRedirect(hn.get(deletePath(id)), signInAfter(deletePath(id)))
	hn.wantRedirect(hn.get(PathDeleted), signInAfter(PathDeleted))
}

// A crafted link must not claim a world that still exists is deleted.
func TestWorldListDoesNotClaimALiveWorldIsGone(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	_, id := hn.seedTwo()
	body := hn.get(PathWorlds+"?deleted="+id, withCookie(c)).Body.String()
	if strings.Contains(body, "Deleted Second") {
		t.Errorf("deleted page:\n%s", body)
	}
}
