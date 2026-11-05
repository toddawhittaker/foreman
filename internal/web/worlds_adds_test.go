package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var addsFigure = regexp.MustCompile(`<dt>Snapshots add</dt><dd>([^<]*)</dd>`)

// SPEC 6.6: the list's "Snapshots add" for a world is the figure on that
// world's Snapshots page, and a world with no snapshots adds nothing.
func TestWorldListShowsWhatEachWorldsSnapshotsAdd(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	id, _, _ := hn.seedTwoSnapshots()
	// The save the snapshots hold is the world's no longer, so they alone hold it.
	if err := os.Remove(filepath.Join(hn.worlds.Dirs().Worlds, id, "saves", "base.zip")); err != nil {
		t.Fatal(err)
	}
	if _, err := hn.worlds.Create(t.Context(), "Empty", "2.0.72", false); err != nil {
		t.Fatal(err)
	}

	snaps := addsFigure.FindStringSubmatch(hn.get(snapshotsPath(id), withCookie(c)).Body.String())
	if snaps == nil || snaps[1] == "0 B" {
		t.Fatalf("the Snapshots page figure is %q, want a space above zero", snaps)
	}
	list := hn.get(PathWorlds, withCookie(c)).Body.String()
	figures := addsFigure.FindAllStringSubmatch(list, -1)
	if len(figures) != 2 {
		t.Fatalf("want one figure per world, got %q", figures)
	}
	// Base is the first world made, so it is listed first.
	if figures[0][1] != snaps[1] || figures[1][1] != "0 B" {
		t.Errorf("list shows %q and %q, want %q and 0 B", figures[0][1], figures[1][1], snaps[1])
	}
}

// If the count fails the list still renders, says "unknown", and logs one error.
func TestWorldListSurvivesAFailedSnapshotCount(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a root user can read any folder")
	}
	hn := newHarness(t, nil)
	c := hn.signIn()
	hn.seedTwoSnapshots()
	if _, err := hn.worlds.Create(t.Context(), "Other", "2.0.72", false); err != nil {
		t.Fatal(err)
	}
	dir := hn.worlds.Dirs().Snapshots
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	rec := hn.get(PathWorlds, withCookie(c))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, "Other") {
		t.Fatalf("status %d, the list did not render", rec.Code)
	}
	figures := addsFigure.FindAllStringSubmatch(body, -1)
	if len(figures) != 2 {
		t.Fatalf("want one figure per world, got %q", figures)
	}
	for _, f := range figures {
		if f[1] != "unknown" {
			t.Errorf("figure %q, want unknown", f[1])
		}
	}
	if n := strings.Count(hn.logs.String(), `"msg":"count snapshots"`); n != 1 {
		t.Errorf("logged %d times, want once:\n%s", n, hn.logs.String())
	}
}
