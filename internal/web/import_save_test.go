package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
)

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "savefile", "testdata", name)) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func putInboxSave(t *testing.T, hn *harness, folder string, content []byte) {
	t.Helper()
	dir := filepath.Join(hn.worlds.Dirs().Imports, folder)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.zip"), content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestImportFolderTakesTheVersionFromTheSave(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	putInboxSave(t, hn, "real", fixtureBytes(t, "2.1.24-space-age.zip"))
	putInboxSave(t, hn, "broken", saveZip(t))

	body := hn.get(PathImport, withCookie(c)).Body.String()
	if !strings.Contains(body, "made with Factorio 2.1.24, Space Age") || !strings.Contains(body, `data-game-version="2.1.24" data-space-age`) {
		t.Errorf("radio label or data attributes missing:\n%s", body)
	}
	rec := hn.postLive(PathImportFolder, url.Values{"folder": {"broken"}, "name": {"B"}}, c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "three numbers") {
		t.Errorf("unreadable save, empty version: %d", rec.Code)
	}
	hn.wantRedirect(hn.postLive(PathImportFolder, url.Values{"folder": {"real"}, "name": {"R"}}, c), "/worlds/r?imported=1")
	list, _ := hn.worlds.List(context.Background())
	if len(list) != 1 || list[0].GameVersion != "2.1.24" || !list[0].SpaceAge {
		t.Errorf("list %+v", list)
	}
}

func TestImportFolderPrefillsWhenThereIsOneFolder(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	putInboxSave(t, hn, "only", fixtureBytes(t, "2.1.24-space-age.zip"))
	body := hn.get(PathImport, withCookie(c)).Body.String()
	for _, want := range []string{`value="only" data-name="only" data-game-version="2.1.24" data-space-age checked`,
		`name="game_version_typed" type="text" value=""`, "Same as the save (2.1.24)",
		`aria-describedby="folder-space-age-hint folder-space-age-warning" checked`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestImportUploadTakesTheVersionFromTheSave(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	rec := hn.postImportUpload(c, map[string]string{"name": "Plain"}, "a.zip", saveZip(t))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "three numbers") {
		t.Errorf("unreadable save, empty version: %d", rec.Code)
	}
	hn.noStaging()
	hn.wantRedirect(hn.postImportUpload(c, map[string]string{"name": "Real"}, "a.zip", fixtureBytes(t, "2.0.77-space-age.zip")), "/worlds/real?imported=1")
	list, _ := hn.worlds.List(context.Background())
	if len(list) != 1 || list[0].GameVersion != "2.0.77" || !list[0].SpaceAge {
		t.Errorf("list %+v", list)
	}
}

// A bulk import whose request the browser abandoned is not a server fault:
// the folders are skipped and nothing is logged at ERROR. A real failure
// still is.
func TestBulkImportLogsOnlyRealFailures(t *testing.T) {
	hn := newHarness(t, nil)
	s := &server{cfg: hn.cfg, log: hn.slog, worlds: hn.worlds, render: mustRenderer(t)}
	putInboxSave(t, hn, "real", fixtureBytes(t, "2.1.24-space-age.zip"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := s.importMany(ctx, []string{"real", "other"}, false, game.Actor{})
	if len(res.Imported) != 0 || len(res.Skipped) != 2 {
		t.Fatalf("result = %+v, want both skipped", res)
	}
	if hn.logs.String() != "" {
		t.Errorf("a cancelled import was logged:\n%s", hn.logs.String())
	}

	s.logUnlessCancelled("import folder", "real", errors.New("disk gone"))
	lines := logLines(t, hn)
	if len(lines) != 1 || lines[0]["level"] != "ERROR" {
		t.Errorf("a real failure was not logged as an error: %v", lines)
	}
}

// SPEC 17.9: a skipped folder links to the Import page with only that folder
// ticked. A name that is not an inbox folder ticks nothing.
func TestSkippedFolderLinksToImportingItAlone(t *testing.T) {
	hn := newHarness(t, nil)
	c := hn.signIn()
	putInboxSave(t, hn, "real", fixtureBytes(t, "2.1.24-space-age.zip"))
	putInboxSave(t, hn, "plain", fixtureBytes(t, "2.0.77-no-space-age.zip"))
	putInboxSave(t, hn, "broken", saveZip(t))

	rec := hn.postLive(PathImportFolder, url.Values{"folder": {"real", "broken"}}, c)
	if want := `<a href="/import?folder=broken">Import <code>broken</code> on its own</a>`; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("no link to import the skipped folder alone:\n%s", rec.Body.String())
	}

	body := hn.get(PathImport+"?folder=broken", withCookie(c)).Body.String()
	if !strings.Contains(body, `value="broken" data-name="broken" checked`) {
		t.Errorf("broken is not ticked:\n%s", body)
	}
	if strings.Contains(body, `value="plain" data-name="plain" data-game-version="2.0.77" checked`) {
		t.Error("another folder is ticked")
	}

	for _, bad := range []string{"../x", ".hidden", "missing"} {
		rec := hn.get(PathImport+"?folder="+url.QueryEscape(bad), withCookie(c))
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), " checked") {
			t.Errorf("folder=%q: status %d, or a folder is ticked", bad, rec.Code)
		}
	}
}
