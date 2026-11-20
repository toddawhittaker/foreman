package web

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DESIGN 6, Shell: the status strip over every page names the live world,
// not another one, whichever page is open.
func TestStatusStripNamesTheLiveWorld(t *testing.T) {
	g := newGameRig(t, image21)
	g.newWorld("Alpha", fixtureNoSA, false)
	b := g.newWorld("Bravo", fixtureNoSA, false)
	for _, path := range []string{PathServer, PathWorlds, worldPath(b)} {
		body := g.get(path)
		g.wantContains(path, body, `<span class="site-world" aria-hidden="true">Alpha</span>`)
		g.wantMissing(path, body, `<span class="site-world" aria-hidden="true">Bravo</span>`)
	}
}

// The Server page lists the live world's newest snapshots, says when there
// are none, and when they cannot be read offers only the link to them
// rather than claiming there are none.
func TestServerRecentSnapshots(t *testing.T) {
	g := newGameRig(t, image21)
	id := g.newWorld("Base", fixtureNoSA, false)
	empty := "No snapshots yet. Take one above before you try something risky."
	link := `<a href="` + snapshotsPath(id) + `" id="act-recent-snapshots">All snapshots</a>`

	body := g.get(PathServer)
	g.wantContains("no snapshots", body, empty, link)
	g.wantMissing("no snapshots", body, `class="recent-snaps__list"`)

	for range recentSnapshotCount + 1 {
		g.wantRedirectTo(PathServerResult, g.post(gamePath(id, "take-snapshot"), url.Values{}))
		g.waitPage(`Foreman took the snapshot &#34;Manual snapshot&#34; of Base.`)
	}
	body = g.get(PathServer)
	g.wantContains("snapshots", body, `class="recent-snaps__list"`, link)
	g.wantMissing("snapshots", body, empty)
	list, _, _ := strings.Cut(body[strings.Index(body, `class="recent-snaps__list"`):], "</ul>")
	if n := strings.Count(list, "<li>"); n != recentSnapshotCount {
		t.Errorf("%d recent snapshots listed, want %d", n, recentSnapshotCount)
	}

	// A snapshots folder that cannot be read.
	dir := filepath.Join(g.worlds.Dirs().Snapshots, id)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	body = g.get(PathServer)
	g.wantContains("unreadable", body, link)
	g.wantMissing("unreadable", body, empty, `class="recent-snaps__list"`)
	if !strings.Contains(g.logs.String(), "read recent snapshots") {
		t.Errorf("the failed read is not logged:\n%s", g.logs.String())
	}
}
