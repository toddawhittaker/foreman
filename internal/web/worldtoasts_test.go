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
		{"saves after an upload", SavesPage{WorldName: "Base", Uploaded: "a.zip"}, false, "Uploaded a.zip."},
		{"snapshots after a delete", SnapshotsPage{WorldID: "base", WorldName: "Base", Deleted: []time.Time{at}}, false, "Foreman deleted the snapshot taken 2026-11-02 12:00:00 UTC."},
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
