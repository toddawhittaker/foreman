package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
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
	if !strings.Contains(form, `Delete the world &#34;Second&#34;?`) || !strings.Contains(form, `name="final_snapshot" checked`) {
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

const (
	reasonActive     = "This is the active world. Switch to another world before deleting it."
	reasonNewer      = "This world is from a newer Foreman, so this version will not delete it."
	reasonUnreadable = "Because its world.json cannot be read, Foreman will not delete this world."
)

// DESIGN 7.1: say why, instead of offering an action that will be refused.
func TestWorldPageDeletePanel(t *testing.T) {
	r := newTestRenderer(t)
	cases := []struct {
		name   string
		sum    world.Summary
		reason string
	}{
		{"active", world.Summary{ID: "base", Name: "Base", Active: true}, reasonActive},
		{"newer", world.Summary{ID: "base", Name: "Base", Newer: true}, reasonNewer},
		{"unreadable", world.Summary{ID: "base", Name: "base", Unreadable: true}, reasonUnreadable},
		{"deletable", world.Summary{ID: "base", Name: "Base"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, body := render(t, r, pageCase{status: 200, page: WorldPage{World: c.sum, Name: c.sum.Name}})
			hasLink := strings.Contains(body, `href="/worlds/base/delete"`)
			if c.reason == "" {
				if !hasLink {
					t.Fatalf("no Delete world button:\n%s", body)
				}
				// The button only leads to the confirmation: a grey one.
				if strings.Contains(body, "button--danger") {
					t.Errorf("the world page's Delete button is red:\n%s", body)
				}
				return
			}
			if hasLink || strings.Contains(body, "Delete world") {
				t.Errorf("a Delete button is offered:\n%s", body)
			}
			if !strings.Contains(strings.ReplaceAll(body, "&#39;", "'"), c.reason) {
				t.Errorf("reason %q missing:\n%s", c.reason, body)
			}
		})
	}
}

// The confirmation page shows the reason instead of the form for a world
// that cannot be deleted; the confirmation keeps its red button otherwise.
func TestDeleteConfirmationShowsTheReasonInsteadOfTheForm(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	activeID, otherID := hn.seedTwo()
	future, err := hn.worlds.Create(t.Context(), "Future", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	broken, err := hn.worlds.Create(t.Context(), "Broken", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := hn.worlds.Dirs().Worlds
	if err := os.WriteFile(filepath.Join(dir, future, "world.json"), []byte(`{"format_version": 9, "name": "Future"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, broken, "world.json"), []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	for id, reason := range map[string]string{activeID: reasonActive, future: reasonNewer, broken: reasonUnreadable} {
		rec := hn.get(deletePath(id), withCookie(c))
		body := strings.ReplaceAll(rec.Body.String(), "&#39;", "'")
		if rec.Code != http.StatusOK || !strings.Contains(body, reason) {
			t.Errorf("%s: %d, reason missing:\n%s", id, rec.Code, body)
		}
		if strings.Contains(body, `name="confirm_name"`) || strings.Contains(body, "button--danger") {
			t.Errorf("%s: the form is offered:\n%s", id, body)
		}
	}
	body := hn.get(deletePath(otherID), withCookie(c)).Body.String()
	if !strings.Contains(body, `class="button button--danger"`) || !strings.Contains(body, `name="confirm_name"`) {
		t.Errorf("a deletable world lost its confirmation form:\n%s", body)
	}
}

// A snapshot's reason is shown above its label only when it adds words the
// label lacks.
func TestSnapshotReasonShownOnlyWhenItAddsWords(t *testing.T) {
	cases := []struct{ reason, label, want string }{
		{world.ReasonManual, "Manual snapshot", ""},
		{world.ReasonManual, "manual", ""},
		{world.ReasonBeforeDelete, world.LabelBeforeDelete, ""},
		{world.ReasonBeforeRestore, world.LabelBeforeRestore, ""},
		{world.ReasonManual, "Before the rocket launch", "Manual"},
		{world.ReasonBeforeDelete, "Final copy", "Before delete"},
		{world.ReasonManual, "Manually tuned", "Manual"},
	}
	for _, c := range cases {
		s := world.Snapshot{Manifest: world.Manifest{Reason: c.reason, Label: c.label}}
		if got := snapshotReason(i18n.English, s); got != c.want {
			t.Errorf("snapshotReason(%q, %q) = %q, want %q", c.reason, c.label, got, c.want)
		}
	}

	r := newTestRenderer(t)
	for _, c := range []struct {
		page Page
		// open starts the text of a snapshot's first cell; label is what
		// follows a label that is not repeated by a reason.
		open, label string
	}{
		{snapshotsCase(), "<span>", `<span class="visually-hidden">`},
		{DeletedPage{Worlds: []world.DeletedWorld{{ID: "base", Name: "Base", Snapshots: snapshotsCase().Snapshots}}}, `<td role="cell">`, "<br><a"},
	} {
		_, body := render(t, r, pageCase{status: 200, page: c.page})
		// The case has a "Manual" snapshot labelled with a long name, and
		// a "Before restore" snapshot labelled "Before restore".
		if !strings.Contains(body, c.open+"Manual<br>") {
			t.Errorf("a reason that adds words is hidden:\n%s", body)
		}
		if strings.Contains(body, c.open+"Before restore<br>Before restore") || !strings.Contains(body, c.open+"Before restore"+c.label) {
			t.Errorf("a repeated reason is shown:\n%s", body)
		}
	}
}

// The page offers no delete for a world whose world.json cannot be read;
// a request sent anyway is refused in words and removes nothing.
func TestDeletePostRefusedForUnreadableWorld(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.seedTwo()
	broken, err := hn.worlds.Create(t.Context(), "Broken", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(hn.worlds.Dirs().Worlds, broken)
	if err := os.WriteFile(filepath.Join(dir, "world.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := hn.post(deletePath(broken), url.Values{"confirm_name": {broken}}, withCookie(c))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "world.json cannot be read") {
		t.Errorf("delete: %d\n%s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the refused delete changed the world: %v", err)
	}
}
