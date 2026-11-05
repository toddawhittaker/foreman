package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

// newImportRig is a game rig whose Docker holds only the 2.1.24 image, with
// rel as Docker Hub and the data directory at /srv/foreman on the host.
func newImportRig(t *testing.T, rel *fakeReleases) *gameRig {
	t.Helper()
	fake := fakegame.New(t, fakegame.Config{Images: []string{image21}})
	g := newGameRigOn(t, fake.Host(), fake)
	d := Deps{Config: g.cfg, Store: g.store, Worlds: g.worlds, Game: g.ctrl, Log: g.slog, Now: g.clock.Now,
		HostDataDir: "/srv/foreman"}
	if rel != nil {
		d.Releases = rel
	}
	h, err := NewHandler(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	g.h = h
	g.liveServer = newLiveServerFor(t, g.harness)
	return g
}

// inboxSave puts one save fixture, or the bytes given, in an inbox folder.
func (g *gameRig) inboxSave(folder, fixture string) {
	g.t.Helper()
	putInboxSave(g.t, g.harness, folder, fixtureBytes(g.t, fixture))
}

func (g *gameRig) worldByName(name string) world.Summary {
	g.t.Helper()
	list, err := g.worlds.List(context.Background())
	if err != nil {
		g.t.Fatal(err)
	}
	for _, w := range list {
		if w.Name == name {
			return w
		}
	}
	g.t.Fatalf("no world %s in %+v", name, list)
	return world.Summary{}
}

// SPEC 3.5: the page names the import folder on the Docker host, where the
// admin copies folders, not the path inside Foreman's container.
func TestImportPageShowsTheHostPath(t *testing.T) {
	g := newImportRig(t, nil)
	body := g.get(PathImport)
	g.wantContains("import page", body, "<code>/srv/foreman/import</code>")
	g.wantMissing("import page", body, g.worlds.Dirs().Imports)
}

// Ruling 1 (#124): several ticked folders each become a world named after
// the folder, with the save's version and Space Age. An unreadable save is
// skipped with its reason. One download runs per missing version.
func TestBulkImport(t *testing.T) {
	g := newImportRig(t, nil)
	g.inboxSave("sa", "2.1.24-space-age.zip")
	g.inboxSave("plain", "2.0.77-no-space-age.zip")
	g.inboxSave("plain-too", "2.0.77-no-space-age.zip")
	putInboxSave(t, g.harness, "broken", saveZip(t))

	body := g.get(PathImport)
	g.wantContains("import page", body, `type="checkbox" id="folder-source-0" name="folder"`, "game image not downloaded")

	rep := g.post(PathImportFolder, url.Values{"folder": {"sa", "plain", "plain-too", "broken"}, "name": {"Ignored"},
		"game_version_typed": {"1.1.110"}, "download": {"on"}})
	if rep.StatusCode != http.StatusOK {
		t.Fatalf("status %d\n%s", rep.StatusCode, rep.body)
	}
	g.wantContains("result", rep.body, "<strong>Done:</strong> Imported 3 folders.",
		"<code>broken</code>: Foreman could not read its save", "Downloading the game image for Factorio <code>2.0.77</code>.")
	g.wantMissing("result", rep.body, "Ignored")
	if w := g.worldByName("sa"); w.GameVersion != "2.1.24" || !w.SpaceAge {
		t.Errorf("sa = %+v", w)
	}
	plain := g.worldByName("plain")
	if plain.GameVersion != "2.0.77" || plain.SpaceAge {
		t.Errorf("plain = %+v", plain)
	}
	too := g.worldByName("plain-too")
	// The download runs through one of the two 2.0.77 worlds.
	runs := g.h.(*Handler).runs
	deadline := time.Now().Add(10 * time.Second)
	var ran []string
	for len(ran) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		for _, id := range []string{plain.ID, too.ID} {
			if _, ok := runs.result(id); ok {
				ran = append(ran, id)
			}
		}
	}
	if len(ran) != 1 {
		t.Fatalf("downloads ran on %v, want one world", ran)
	}
	if err := g.waitResult(ran[0], game.AuditImageDownload); err != nil {
		t.Fatalf("download: %v", err)
	}
	if pulls := g.fake.Daemon().Pulls(); !slices.Equal(pulls, []string{image20}) {
		t.Errorf("pulls %v, want only %s", pulls, image20)
	}
}

// Ruling 2 (#127): a folder whose newest save is already in a world is
// listed apart, names that world, and can still be imported.
func TestImportListsAlreadyImportedFolders(t *testing.T) {
	g := newImportRig(t, nil)
	g.inboxSave("done", "2.1.24-no-space-age.zip")
	g.inboxSave("new", "2.1.24-no-space-age.zip")
	g.wantRedirectTo("/worlds/first?imported=1", g.post(PathImportFolder, url.Values{"folder": {"done"}, "name": {"First"}}))

	body := g.get(PathImport)
	g.wantContains("import page", body, "<strong>Already imported.</strong>", "Imported as First on", `<a href="/worlds/first">Open First</a>`)
	g.wantContains("world page", g.get("/worlds/first?imported=1"), "<strong>Done:</strong> Imported First.",
		"<dt>Imported from</dt><dd>The folder <code>done</code>, save <code>a.zip</code>")
	g.wantRedirectTo("/worlds/second?imported=1", g.post(PathImportFolder, url.Values{"folder": {"done"}, "name": {"Second"}}))
}

// #139: Space Age is refused for a world before 2.0.
func TestImportRefusesSpaceAgeFor1_1(t *testing.T) {
	g := newImportRig(t, nil)
	g.inboxSave("old", "1.1.110-vanilla.zip")
	rep := g.post(PathImportFolder, url.Values{"folder": {"old"}, "name": {"Old"}, "space_age": {"on"}})
	if rep.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", rep.StatusCode)
	}
	g.wantContains("refusal", rep.body, "Space Age needs Factorio 2.0 or later", `href="#folder-space-age"`)
	if list, _ := g.worlds.List(context.Background()); len(list) != 0 {
		t.Errorf("made %+v", list)
	}
}

// The Space Age box that differs from the save gets a warning beside it.
func TestImportWarnsWhenSpaceAgeDiffersFromTheSave(t *testing.T) {
	g := newImportRig(t, nil)
	g.inboxSave("plain", "2.1.24-no-space-age.zip")
	rep := g.post(PathImportFolder, url.Values{"folder": {"plain"}, "name": {""}, "space_age": {"on"}})
	g.wantContains("re-shown form", rep.body, "Same as the save (2.1.24)", spaceAgeOnWarning)
}

// Ruling 4: a typed version must be a downloaded image or on Docker Hub's
// list. When Docker Hub cannot be asked the import goes ahead and says so.
func TestImportChecksATypedVersion(t *testing.T) {
	rel := &fakeReleases{versions: []string{"2.1.25", "2.0.77"}}
	g := newImportRig(t, rel)
	for _, f := range []string{"a", "b", "c", "d"} {
		g.inboxSave(f, "2.0.77-no-space-age.zip")
	}
	rep := g.post(PathImportFolder, url.Values{"folder": {"a"}, "name": {"A"}, "game_version_typed": {"2.0.99"}})
	if rep.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", rep.StatusCode)
	}
	g.wantContains("refusal", rep.body, "Factorio 2.0.99 is not a downloaded game image and is not on Docker Hub&#39;s list of versions.")

	// Downloaded, though not on the list.
	g.wantRedirectTo("/worlds/b?imported=1", g.post(PathImportFolder, url.Values{"folder": {"b"}, "name": {"B"}, "game_version_typed": {"2.1.24"}}))
	// On the list.
	g.wantRedirectTo("/worlds/c?imported=1", g.post(PathImportFolder, url.Values{"folder": {"c"}, "name": {"C"}, "game_version": {"2.1.25"}}))

	rel.mu.Lock()
	rel.versionsErr = errors.New("unreachable")
	rel.mu.Unlock()
	g.wantRedirectTo("/worlds/d?imported=1&unchecked=1", g.post(PathImportFolder, url.Values{"folder": {"d"}, "name": {"D"}, "game_version_typed": {"2.0.98"}}))
	g.wantContains("world page", g.get("/worlds/d?imported=1&unchecked=1"),
		"<strong>Warning:</strong> Foreman could not reach Docker Hub to check that Factorio 2.0.98 exists.")
}

// #130: a single import with the box ticked downloads the missing image
// through the world's own download, which its page then reports.
func TestImportDownloadsTheImage(t *testing.T) {
	g := newImportRig(t, nil)
	g.inboxSave("plain", "2.0.77-no-space-age.zip")
	g.fake.Daemon().FailPull(image20, "manifest unknown")
	g.wantRedirectTo("/worlds/plain?imported=1", g.post(PathImportFolder, url.Values{"folder": {"plain"}, "name": {"Plain"}, "download": {"on"}}))
	if err := g.waitResult("plain", game.AuditImageDownload); err == nil {
		t.Fatal("the download worked, want the failure the daemon gave")
	}
	g.wantContains("world page", g.get("/worlds/plain"), `id="image-download"`, "could not be downloaded")
	if !g.hasAudit(game.AuditImageDownload) {
		t.Error("the download is not audited")
	}

	// Unticked, nothing is downloaded.
	g.inboxSave("other", "2.0.77-no-space-age.zip")
	g.wantRedirectTo("/worlds/other?imported=1", g.post(PathImportFolder, url.Values{"folder": {"other"}, "name": {"Other"}}))
	if _, ok := g.h.(*Handler).runs.result("other"); ok {
		t.Error("a download ran with the box unticked")
	}
}

// SPEC 17.9: the "Import on its own" link for a folder whose save cannot be
// read asks for the version and focuses that field; a readable folder does not.
func TestImportOnItsOwnAsksForAnUnknownVersion(t *testing.T) {
	g := newImportRig(t, nil)
	g.inboxSave("plain", "2.0.77-no-space-age.zip")
	putInboxSave(t, g.harness, "broken", saveZip(t))

	unknown := g.get(PathImport + "?folder=broken")
	g.wantContains("unreadable folder", unknown, "Type the version this save was made with.",
		`id="folder-version-ask"`, `spellcheck="false" autofocus`, `aria-describedby="folder-version-hint folder-version-ask"`)

	for what, body := range map[string]string{
		"readable folder": g.get(PathImport + "?folder=plain"),
		"plain page":      g.get(PathImport),
	} {
		g.wantMissing(what, body, "Type the version this save was made with.", "autofocus")
	}
}
