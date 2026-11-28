package web

import (
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// toastRegion is the page's toast region, where result messages float over
// the page (DESIGN 6, "Toasts").
func toastRegion(body string) string {
	return between(body, `<div id="toasts" class="toasts">`, "</div>\n</main>")
}

// wantToast fails unless body's toast region holds a toast of the kind with
// text, as escaped HTML. A toast never takes focus.
func wantToast(t *testing.T, what, body string, isError bool, text string) {
	t.Helper()
	region := toastRegion(body)
	kind := `class="toast toast--success"`
	if isError {
		kind = `class="toast toast--error"`
	}
	if !strings.Contains(region, kind) || !strings.Contains(region, "<span data-toast-text>"+text+"</span>") {
		t.Errorf("%s: no %s toast %q in %q", what, kind, text, region)
	}
	if strings.Contains(region, "autofocus") || strings.Contains(region, "tabindex") {
		t.Errorf("%s: a toast takes focus: %q", what, region)
	}
}

// Ruling: a result message takes no room, so it is a toast and nothing the
// admin used moves; the page's own parts no longer carry it (DESIGN 6).
func TestWorldsPagesSumUpInToasts(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sum := world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.1"}
	snap := world.Snapshot{Name: "20261102T120000Z", Manifest: world.Manifest{TakenAt: at}}
	busy := i18n.M("worlds.error.busy")
	for _, c := range []struct {
		name    string
		page    Page
		isError bool
		text    string
	}{
		{"world list after a delete", WorldsPage{Deleted: "Second", DeletedKept: true}, false, "Deleted Second. Its snapshots are under Deleted worlds."},
		{"favorites after a rename", FavoritesPage{Renamed: i18n.M("favorites.renamed", "name", "Club")}, false, "Renamed the favorite to &#34;Club&#34;."},
		{"saves after an upload", SavesPage{WorldName: "Base", Uploaded: "a.zip"}, false, "Uploaded <code>a.zip</code>."},
		{"snapshots after a delete", SnapshotsPage{WorldID: "base", WorldName: "Base", Deleted: []time.Time{at}}, false, `Foreman deleted the snapshot taken <time datetime="2026-11-02T12:00:00Z">2026-11-02 12:00:00 UTC</time>.`},
		{"snapshots refusal", SnapshotsPage{WorldID: "base", WorldName: "Base", Error: busy}, true, "Another operation is running on this world. Wait for it to finish, then try again."},
		{"all snapshots after a delete", AllSnapshotsPage{Removed: 3}, false, "Foreman deleted 3 snapshots."},
		{"restore refusal", RestorePage{WorldID: "base", WorldName: "Base", Snapshot: snap, Error: busy}, true, "Another operation is running on this world. Wait for it to finish, then try again."},
		{"snapshot delete refusal", SnapshotDeletePage{WorldID: "base", WorldName: "Base", Snapshot: snap, Error: busy}, true, "Another operation is running on this world. Wait for it to finish, then try again."},
		{"delete refusal", DeletePage{World: sum, Error: busy}, true, "Another operation is running on this world. Wait for it to finish, then try again."},
		{"clone refusal", ClonePage{World: sum, Error: busy}, true, "Another operation is running on this world. Wait for it to finish, then try again."},
		{"world archive refusal", WorldPage{World: sum, ArchiveError: busy}, true, "Another operation is running on this world. Wait for it to finish, then try again."},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := pageHTML(t, c.page)
			wantToast(t, c.name, body, c.isError, c.text)
			if main := between(body, "<main", `<div id="toasts"`); strings.Contains(main, "message--success") || strings.Contains(main, "message--error") {
				t.Errorf("the page still carries a result message:\n%s", main)
			}
		})
	}
}

// DESIGN 6, "The page header": every page of the Worlds area draws its
// header with the one partial, so sibling tabs keep identical header boxes.
func TestWorldsPagesUseThePageHeader(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sum := world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.1"}
	snap := world.Snapshot{Name: "20261102T120000Z", Manifest: world.Manifest{TakenAt: at}}
	for name, p := range map[string]Page{
		"worlds":          WorldsPage{},
		"favorites":       FavoritesPage{},
		"deleted":         DeletedPage{},
		"world":           WorldPage{World: sum},
		"saves":           SavesPage{WorldName: "Base"},
		"snapshots":       SnapshotsPage{WorldID: "base", WorldName: "Base"},
		"all snapshots":   AllSnapshotsPage{},
		"restore":         RestorePage{WorldID: "base", WorldName: "Base", Snapshot: snap},
		"snapshot delete": SnapshotDeletePage{WorldID: "base", WorldName: "Base", Snapshot: snap},
		"delete":          DeletePage{World: sum},
		"clone":           ClonePage{World: sum},
		"import":          ImportPage{},
		"factory map":     FactoryMapPage{World: sum},
		"update":          UpdatePage{World: sum},
		"rollback":        RollbackPage{World: sum},
	} {
		body := pageHTML(t, p)
		if strings.Count(body, `<header class="pagehead">`) != 1 || strings.Count(body, "<h1") != 1 {
			t.Errorf("%s: want one page header with the one h1:\n%s", name, body)
		}
		if strings.Contains(body, `class="page-head`) || strings.Contains(body, `class="title-row"`) {
			t.Errorf("%s: an old page head is left", name)
		}
	}
}

// A page opened from an area keeps that area highlighted: Restore from the
// Snapshots tab highlights Snapshots, and Cancel goes back there (DESIGN 6).
func TestRestoreFromTheSnapshotsTabKeepsItsArea(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, snap, _ := hn.seedTwoSnapshots()
	all := hn.get(PathSnapshots, withCookie(c)).Body.String()
	from := restorePath(id, snap.Name) + "?from=" + fromAll
	wantIn(t, "snapshots tab", all, `href="`+from+`"`)

	body := hn.get(from, withCookie(c)).Body.String()
	wantIn(t, "restore from the tab", body,
		`<a href="`+PathSnapshots+`" aria-current="true">`,
		`action="`+restorePath(id, snap.Name)+`?from=all"`,
		`<a href="`+PathSnapshots+`">`)
	body = hn.get(restorePath(id, snap.Name), withCookie(c)).Body.String()
	wantIn(t, "restore from the world", body, `<a href="`+PathWorlds+`" aria-current="true">`, `<a href="`+snapshotsPath(id)+`">`)
}

// DESIGN 6, "Toasts", "Shown once": a toast built from a result parameter
// in the address names it, so toast.js drops it and a reload shows nothing.
func TestWorldsToastsNameTheParameterTheyCameFrom(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sum := world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.1"}
	for _, c := range []struct {
		name string
		page Page
		drop string
	}{
		{"world list after a delete", WorldsPage{Deleted: "Second"}, "deleted removed"},
		{"favorites after a rename", FavoritesPage{Renamed: i18n.M("favorites.renamed", "name", "Club")}, "renamed"},
		{"saves after an upload", SavesPage{WorldName: "Base", Uploaded: "a.zip"}, "uploaded"},
		{"saves after a choice", SavesPage{WorldName: "Base", Chosen: true}, "chosen"},
		{"snapshots after a take", SnapshotsPage{WorldID: "base", WorldName: "Base", Done: i18n.M("worlds.snapshots.done.taken", "label", "x"), DoneParam: "taken"}, "taken"},
		{"snapshots after a delete", SnapshotsPage{WorldID: "base", WorldName: "Base", Deleted: []time.Time{at}}, "deleted removed"},
		{"all snapshots after a delete", AllSnapshotsPage{Removed: 3}, "deleted removed"},
		{"world after a rename", WorldPage{World: sum, Done: i18n.M("worlds.world.renamed", "name", "Base")}, "renamed archived imported unchecked cloned"},
		{"world after an update", WorldPage{World: sum, UpdateDone: i18n.M("worlds.world.renamed", "name", "Base")}, "result"},
		{"world after a favorite", WorldPage{World: sum, FavMade: &FavoriteMade{Name: "Club"}}, "favorite"},
	} {
		t.Run(c.name, func(t *testing.T) {
			region := toastRegion(pageHTML(t, c.page))
			if !strings.Contains(region, `data-toast data-drop="`+c.drop+`"`) {
				t.Errorf("toast does not drop %q: %q", c.drop, region)
			}
		})
	}
	// An error comes from the answer to a post, not the address.
	region := toastRegion(pageHTML(t, SnapshotsPage{WorldID: "base", WorldName: "Base", Error: i18n.M("worlds.error.busy")}))
	if strings.Contains(region, "data-drop") {
		t.Errorf("an error toast drops a parameter: %q", region)
	}
}

// SPEC 17.11: a deleted snapshot's time in a toast is a <time> element, so
// time.js shows it in the browser's zone.
func TestDeletedSnapshotToastHoldsATimeElement(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	region := toastRegion(pageHTML(t, AllSnapshotsPage{Deleted: []time.Time{at}}))
	if !strings.Contains(region, `<time datetime="2026-11-02T12:00:00Z">2026-11-02 12:00:00 UTC</time>`) {
		t.Errorf("no time element in %q", region)
	}
}

// A toast's argument, such as a world's name, is escaped, so a name cannot
// put markup on the page.
func TestToastEscapesItsArguments(t *testing.T) {
	region := toastRegion(pageHTML(t, WorldsPage{Deleted: `<script>alert(1)</script>`}))
	if strings.Contains(region, "<script>") {
		t.Fatalf("the name went in as markup: %q", region)
	}
	if !strings.Contains(region, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("the name is not shown escaped: %q", region)
	}
}

// A failed restore opened from the Snapshots tab offers its undo from the
// same tab, and the undo warning starts with the failure (DESIGN 6).
func TestRestoreUndoKeepsItsArea(t *testing.T) {
	p := RestorePage{WorldID: "base", Undo: "20261102T120000Z"}
	if got, want := p.UndoPath(), restorePath("base", "20261102T120000Z"); got != want {
		t.Errorf("undo from a world = %q, want %q", got, want)
	}
	p.FromAll = true
	if got, want := p.UndoPath(), restorePath("base", "20261102T120000Z")+"?from=all"; got != want {
		t.Errorf("undo from the tab = %q, want %q", got, want)
	}
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	body := pageHTML(t, RestorePage{WorldID: "base", WorldName: "Base", Snapshot: world.Snapshot{Name: "20261102T120000Z", Manifest: world.Manifest{TakenAt: at}},
		Error: i18n.M("worlds.error.busy"), Undo: "20261102T130000Z"})
	undo := between(body, `id="restore-undo">`, "</div>")
	if !strings.Contains(undo, "<strong>Warning:</strong> Foreman could not finish the restore.") || strings.Contains(undo, "autofocus") {
		t.Errorf("the undo warning does not start with the failure: %q", undo)
	}
}

// The way to the snapshot a result named stays in the page after its toast
// leaves; a delete names none.
func TestSnapshotsResultLinksToItsSnapshot(t *testing.T) {
	body := pageHTML(t, SnapshotsPage{WorldID: "base", WorldName: "Base", Done: i18n.M("worlds.snapshots.done.taken", "label", "x"), DoneSnap: "20261102T120000Z", DoneParam: "taken"})
	wantIn(t, "snapshots after a take", body, `<a href="#restore-20261102T120000Z">Go to the snapshot</a>`)
	if strings.Contains(pageHTML(t, SnapshotsPage{WorldID: "base", WorldName: "Base"}), "Go to the snapshot") {
		t.Error("a page with no result links to a snapshot")
	}
}

// The confirmation pages name their section by the page's heading.
func TestWorldsConfirmationsAreNamed(t *testing.T) {
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	sum := world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.1"}
	snap := world.Snapshot{Name: "20261102T120000Z", Manifest: world.Manifest{TakenAt: at}}
	for name, p := range map[string]Page{
		"restore":         RestorePage{WorldID: "base", WorldName: "Base", Snapshot: snap},
		"rollback":        RollbackPage{World: sum, Version: "2.0.0"},
		"update":          UpdatePage{World: sum},
		"delete":          DeletePage{World: sum},
		"snapshot delete": SnapshotDeletePage{WorldID: "base", WorldName: "Base", Snapshot: snap},
		"bulk delete":     SnapshotBulkDeletePage{Items: []DeleteItem{{WorldID: "base", WorldName: "Base", Snapshot: snap}}},
		"favorite delete": FavoriteDeletePage{Favorite: FavoriteRow{world.Favorite{ID: "f", Name: "Club"}}},
	} {
		body := pageHTML(t, p)
		if !strings.Contains(body, `<h1><span id="confirm-heading">`) {
			t.Errorf("%s: the heading has no confirm-heading", name)
		}
		if !strings.Contains(body, `<section class="confirm" aria-labelledby="confirm-heading">`) {
			t.Errorf("%s: the confirmation is not named", name)
		}
	}
}

// The list of imported worlds has a visible lead-in that names it.
func TestImportResultListHasALeadIn(t *testing.T) {
	body := pageHTML(t, ImportPage{Result: &BulkResult{Imported: []BulkImported{{Folder: "a", WorldID: "a", WorldName: "A"}}}})
	wantIn(t, "import result", body, `<p id="import-result-lead">Imported 1 folder.</p>`, `<ul aria-labelledby="import-result-lead">`)
}
