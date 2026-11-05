package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// wantIn fails for each of wants missing from body.
func wantIn(t *testing.T, what, body string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("%s lacks %q", what, w)
		}
	}
}

// DESIGN 7.3: the Worlds tab is marked current on every page about worlds.
func TestWorldsTabIsCurrentOnWorldPages(t *testing.T) {
	r := newTestRenderer(t)
	for _, p := range []Page{
		WorldsPage{}, WorldPage{World: world.Summary{ID: "base", Name: "Base"}}, ImportPage{},
		SavesPage{WorldName: "Base"}, snapshotsCase(), RestorePage{WorldID: "base", WorldName: "Base"},
		DeletePage{World: world.Summary{ID: "base", Name: "Base"}}, DeletedPage{},
	} {
		_, body := render(t, r, pageCase{status: 200, user: "todd", page: p})
		wantIn(t, p.templateName(), body, `<a href="/worlds" aria-current="page">Worlds</a>`)
	}
}

// Every page under a world goes back to it, and the others back to the
// list, with one pattern and one wording.
func TestBackLinks(t *testing.T) {
	r := newTestRenderer(t)
	sum := world.Summary{ID: "base", Name: "Base"}
	for _, c := range []struct {
		page Page
		want string
	}{
		{WorldPage{World: sum}, `<div class="actions"><a href="/worlds">Back to worlds</a></div>`},
		{ImportPage{}, `<div class="actions"><a href="/worlds">Back to worlds</a></div>`},
		{DeletedPage{}, `<div class="actions"><a href="/worlds">Back to worlds</a></div>`},
		{SavesPage{WorldName: "Base", WorldHref: "/worlds/base"}, `<div class="actions"><a href="/worlds/base">Back to Base</a></div>`},
		{snapshotsCase(), `<div class="actions"><a href="/worlds/base">Back to Base</a></div>`},
		{RestorePage{WorldID: "base", WorldName: "Base"}, `<div class="actions"><a href="/worlds/base">Back to Base</a></div>`},
		{RestorePage{WorldID: "base", WorldName: "Base", Deleted: true}, `<div class="actions"><a href="/worlds">Back to worlds</a></div>`},
		{DeletePage{World: sum}, `<div class="actions"><a href="/worlds/base">Back to Base</a></div>`},
	} {
		_, body := render(t, r, pageCase{status: 200, user: "todd", page: c.page})
		wantIn(t, c.page.templateName(), body, c.want)
	}
}

// SPEC 5.10, 6.6: the world page links to its delete confirmation, the
// list to the deleted worlds, and an unmeasured size says so.
func TestWorldLinksAndUnknownSize(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: WorldPage{World: world.Summary{ID: "base", Name: "Base", SizeUnknown: true}}})
	wantIn(t, "world page", body, `href="/worlds/base/delete"`, ">Delete world<", "<dd>unknown</dd>")
	_, body = render(t, r, pageCase{status: 200, user: "todd", page: WorldsPage{Worlds: []world.Summary{{ID: "a", Name: "A", SizeUnknown: true}}}})
	wantIn(t, "world list", body, `href="/deleted"`, "<dd>unknown</dd>")
}

// A saves folder that cannot be read says "unknown", not "Never".
func TestUnknownLastPlayed(t *testing.T) {
	r := newTestRenderer(t)
	sums := []world.Summary{{ID: "a", Name: "A", LastPlayedUnknown: true}}
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: WorldPage{World: sums[0]}})
	wantIn(t, "world page", body, "<dt>Last played</dt><dd>unknown</dd>")
	_, body = render(t, r, pageCase{status: 200, user: "todd", page: WorldsPage{Worlds: sums}})
	wantIn(t, "world list", body, "<dt>Last played</dt><dd>unknown</dd>")
	// A world with no saves, and nothing wrong, still says Never.
	_, body = render(t, r, pageCase{status: 200, user: "todd", page: WorldPage{World: world.Summary{ID: "b", Name: "B"}}})
	wantIn(t, "world page", body, "<dt>Last played</dt><dd>Never</dd>")
}

// SPEC 6.6: restoring a deleted world recreates it and takes no "Before
// restore" snapshot, so the page must not promise one.
func TestRestorePageForDeletedWorld(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: RestorePage{WorldID: "base", WorldName: "Base",
		Snapshot: snapshotsCase().Snapshots[0], Deleted: true}})
	wantIn(t, "restore page", body, "Foreman will recreate this world from the snapshot")
	if strings.Contains(body, "Before restore") {
		t.Error(`restore page for a deleted world mentions "Before restore"`)
	}
}

// SPEC 6.5: a restore that failed after its undo snapshot was taken says
// so and links to restoring it.
func TestFailedRestoreLinksToItsUndo(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 500, user: "todd", page: RestorePage{WorldID: "base", WorldName: "Base",
		Snapshot: snapshotsCase().Snapshots[0], Error: "Foreman could not finish the restore.", Undo: "20261103T150000Z"}})
	wantIn(t, "restore page", body, `Foreman took the snapshot "Before restore" first`,
		`href="/worlds/base/snapshots/20261103T150000Z/restore"`)
}

// DESIGN 8.3: three columns, the reason above the label, timestamps that
// break only between date and time, "Go to the snapshot" landing on the
// Restore link, and Restore names that tell apart snapshots taken in the
// same minute.
func TestSnapshotTables(t *testing.T) {
	r := newTestRenderer(t)
	p := snapshotsCase()
	p.Snapshots[1].TakenAt = p.Snapshots[0].TakenAt.Add(30 * time.Second)
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: p})
	wantIn(t, "snapshots page", body,
		`<th scope="col">Snapshot</th><th scope="col">Taken</th><th scope="col" class="num">Size</th>`,
		`href="#restore-20261103T142500Z"`, `id="restore-20261103T142500Z"`,
		`<input type="checkbox" name="snap" value="base/20261103T142500Z"><span>Manual<br>`,
		`<span class="nowrap">2026-11-03</span> <span class="nowrap">14:25 UTC</span>`,
		"taken 2026-11-03 14:25:00 UTC", "taken 2026-11-03 14:25:30 UTC")
	_, body = render(t, r, pageCase{status: 200, user: "todd", page: DeletedPage{Worlds: []world.DeletedWorld{
		{ID: "base", Name: "Base", Snapshots: p.Snapshots}}}})
	wantIn(t, "deleted page", body,
		`<th scope="col">Snapshot</th><th scope="col">Taken</th><th scope="col" class="num">Size</th>`,
		`<span class="nowrap">14:25 UTC</span>`, "taken 2026-11-03 14:25:30 UTC")
}

// DESIGN 7.2: an error on the load choice sends focus to its first radio,
// marks every radio invalid, and says what to do in a full sentence.
func TestLoadChoiceError(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	rec := hn.chooseSave(c, id, url.Values{"load": {"gone.zip"}})
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d", rec.Code)
	}
	wantIn(t, "saves page", body, msgSaveGone, `href="#load-newest"`, `id="load-newest"`)
	if n := strings.Count(body, `name="load"`); n == 0 || strings.Count(body, `aria-invalid="true" aria-describedby="load-save-error">`) < n {
		t.Errorf("not every radio of %d is marked invalid", n)
	}
}

// DESIGN 7.2: an error on the folder choice marks its radios invalid.
func TestImportFolderErrorMarksRadios(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 400, user: "todd", page: ImportPage{
		Inbox:  []world.InboxFolder{{Name: "a", Saves: 1}, {Name: "b", Saves: 1}},
		Folder: ImportForm{IDPrefix: "folder", SourceError: "Choose a folder."}, Upload: ImportForm{IDPrefix: "upload"}}})
	if strings.Count(body, `name="folder" value="a" aria-invalid="true"`)+strings.Count(body, `name="folder" value="b" aria-invalid="true"`) != 2 {
		t.Error("folder radios not marked invalid")
	}
}

// SPEC 5.10: after a delete the world list says what happened and where
// the snapshots went.
func TestWorldListAfterDelete(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	_, id := hn.seedTwo()
	rec := hn.post(deletePath(id), url.Values{"final_snapshot": {"on"}, "confirm_name": {"Second"}}, withCookie(c))
	hn.wantRedirect(rec, PathWorlds+"?deleted="+id)
	body := hn.get(rec.Header().Get("Location"), withCookie(c)).Body.String()
	wantIn(t, "world list", body, `class="message message--success"`,
		`Deleted Second. Its snapshots are under Deleted worlds.`, `<a href="/deleted">Go to Deleted worlds</a>`)
}
