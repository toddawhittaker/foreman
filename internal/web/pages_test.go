package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/i18n"
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

// DESIGN 6: the Worlds tab is the current page only on the world list; on
// every other page about worlds it marks the section, so a screen reader is
// not told it is on the list.
func TestWorldsTabIsCurrentOnWorldPages(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: WorldsPage{}})
	wantIn(t, "worlds", body, `<a href="/worlds" aria-current="page">Worlds</a>`)
	for _, p := range []Page{
		WorldPage{World: world.Summary{ID: "base", Name: "Base"}}, ImportPage{},
		SavesPage{WorldName: "Base"}, snapshotsCase(), RestorePage{WorldID: "base", WorldName: "Base"},
		DeletePage{World: world.Summary{ID: "base", Name: "Base"}}, DeletedPage{},
	} {
		_, body := render(t, r, pageCase{status: 200, user: "todd", page: p})
		wantIn(t, p.templateName(), body, `<a href="/worlds" aria-current="true">Worlds</a>`)
		if nav := between(body, `<nav aria-label="Main">`, `</nav>`); strings.Contains(nav, `aria-current="page"`) {
			t.Errorf("%s: a main nav link claims to be this page", p.templateName())
		}
	}
}

// Pages that have no tab or Cancel to leave by (the import form, a refused
// delete) keep one "Back to" link, with one pattern and one wording.
func TestBackLinks(t *testing.T) {
	r := newTestRenderer(t)
	for _, c := range []struct {
		page Page
		want string
	}{
		{ImportPage{}, `<div class="actions"><a href="/worlds">Back to worlds</a></div>`},
		{DeletePage{World: world.Summary{ID: "base", Name: "Base", Active: true}}, `<div class="actions"><a href="/worlds/base">Back to Base</a></div>`},
	} {
		_, body := render(t, r, pageCase{status: 200, user: "todd", page: c.page})
		wantIn(t, c.page.templateName(), body, c.want)
	}
}

// DESIGN 6: a world's pages are tabs of one another, in place of a "Back to"
// link; only the page shown is marked current.
func TestWorldTabs(t *testing.T) {
	r := newTestRenderer(t)
	for current, p := range map[string]Page{
		"overview":  WorldPage{World: world.Summary{ID: "base", Name: "Base"}},
		"saves":     SavesPage{WorldName: "Base", WorldHref: "/worlds/base"},
		"snapshots": snapshotsCase(),
	} {
		_, body := render(t, r, pageCase{status: 200, user: "todd", page: p})
		tabs := between(body, `<nav class="subnav" aria-label="World pages">`, `</nav>`)
		for tab, href := range map[string]string{"overview": "/worlds/base", "saves": "/worlds/base/saves", "snapshots": "/worlds/base/snapshots", "settings": "/worlds/base/settings"} {
			mark := ""
			if tab == current {
				mark = ` aria-current="page"`
			}
			wantIn(t, current, tabs, `<a href="`+href+`"`+mark+`>`)
		}
		if n := strings.Count(tabs, `aria-current`); n != 1 {
			t.Errorf("%s: %d tabs marked current", current, n)
		}
		if strings.Contains(body, "Back to") {
			t.Errorf("%s: still has a Back to link", current)
		}
	}
	// A world a newer Foreman wrote has no settings to show.
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: WorldPage{World: world.Summary{ID: "base", Name: "Base", Newer: true}}})
	if strings.Contains(body, `href="/worlds/base/settings"`) {
		t.Error("a newer world's page links to its settings")
	}
}

// between returns the text of s from the first start to the next end, or "".
func between(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], end)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}

// SPEC 5.10, 6.6: the world page links to its delete confirmation, the
// list to the deleted worlds, and an unmeasured size says so.
func TestWorldLinksAndUnknownSize(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: WorldPage{World: world.Summary{ID: "base", Name: "Base", SizeUnknown: true}}})
	wantIn(t, "world page", body, `href="/worlds/base/delete"`, ">Delete world<", "<dd>unknown</dd>")
	_, body = render(t, r, pageCase{status: 200, user: "todd", page: WorldsPage{Worlds: []world.Summary{{ID: "a", Name: "A", SizeUnknown: true}}}})
	wantIn(t, "world list", body, `href="/deleted"`, `<dd>unknown<span class="visually-hidden">,</span> <span class="fact-note">snapshots add <span class="nowrap">0 B</span></span></dd>`)
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
		Snapshot: snapshotsCase().Snapshots[0], Error: i18n.M("worlds.restore.failed"), Undo: "20261103T150000Z"}})
	wantIn(t, "restore page", body, `Foreman took the snapshot &#34;Before restore&#34; first`,
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
		`<th scope="col" role="columnheader">Snapshot</th><th scope="col" role="columnheader">Taken</th><th scope="col" class="num" role="columnheader">Size</th>`,
		`href="#restore-20261103T142500Z"`, `id="restore-20261103T142500Z"`,
		`<input type="checkbox" name="snap" value="base/20261103T142500Z"><span class="snap-name"><span class="snap-kind">Manual</span><br>`,
		`<span class="nowrap">2026-11-03</span> <span class="nowrap">14:25 UTC</span>`,
		`taken <time datetime="2026-11-03T14:25:00Z">2026-11-03 14:25:00 UTC</time>`, `taken <time datetime="2026-11-03T14:25:30Z">2026-11-03 14:25:30 UTC</time>`)
	_, body = render(t, r, pageCase{status: 200, user: "todd", page: DeletedPage{Worlds: []world.DeletedWorld{
		{ID: "base", Name: "Base", Snapshots: p.Snapshots}}}})
	wantIn(t, "deleted page", body,
		`<th scope="col" role="columnheader">Snapshot</th><th scope="col" role="columnheader">Taken</th><th scope="col" class="num" role="columnheader">Size</th>`,
		`<span class="nowrap">14:25 UTC</span>`, `taken <time datetime="2026-11-03T14:25:30Z">2026-11-03 14:25:30 UTC</time>`)
}

// The disk bar is as wide as the snapshots' share of the space they add
// to, and shows a sliver for a share under one percent; Delete selected
// sits in the bar the script shows once a box is ticked, with the place for
// the number ticked.
func TestSnapshotDiskBarAndSelectionBar(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 200, user: "todd", page: snapshotsCase()})
	wantIn(t, "snapshots page", body,
		`<span class="meter disk-bar" aria-hidden="true"><span style="inline-size: 1%"></span></span>`,
		"<dt>Snapshots add</dt><dd>", "<dt>Free space</dt><dd>",
		`<div class="actions select-bar" data-select-bar>`,
		`<span class="select-bar__count" data-tick-count aria-hidden="true"></span>`,
		`<button type="submit" class="button button--risky" data-needs-tick>Delete selected</button>`)
}

// DESIGN 7.2: an error on the load choice says what to do in a full
// sentence, above the saves, and the summary sends focus to it.
func TestLoadChoiceError(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _ := savesWorld(t, hn)
	rec := hn.chooseSave(c, id, url.Values{"load": {"gone.zip"}})
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d", rec.Code)
	}
	wantIn(t, "saves page", body, i18n.English.T("worlds.saves.gone"), `href="#load-save-error"`, `id="load-save-error" tabindex="-1"`)
	if e, tbl := strings.Index(body, `id="load-save-error"`), strings.Index(body, "<table"); e > tbl {
		t.Error("the error should come before the saves table")
	}
}

// DESIGN 7.2: an error on the folder choice marks its checkboxes invalid
// and points each at the error.
func TestImportFolderErrorMarksCheckboxes(t *testing.T) {
	r := newTestRenderer(t)
	_, body := render(t, r, pageCase{status: 400, user: "todd", page: ImportPage{
		Inbox:  []world.InboxFolder{{Name: "a", Saves: 1}, {Name: "b", Saves: 1}},
		Folder: ImportForm{IDPrefix: "folder", SourceError: msgNoFolder()}, Upload: ImportForm{IDPrefix: "upload"}}})
	for _, n := range []string{"a", "b"} {
		if !strings.Contains(body, `type="checkbox" id="folder-source-`) ||
			!strings.Contains(body, `name="folder" value="`+n+`" data-name="`+n+`" aria-invalid="true" aria-describedby="folder-source-error"`) {
			t.Errorf("folder %s not marked invalid:\n%s", n, body)
		}
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
