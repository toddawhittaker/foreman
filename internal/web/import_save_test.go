package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	hn.wantRedirect(hn.postLive(PathImportFolder, url.Values{"folder": {"real"}, "name": {"R"}}, c), "/worlds/r")
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
	for _, want := range []string{`value="only" data-game-version="2.1.24" data-space-age checked`, `name="game_version" type="text" value="2.1.24"`, `aria-describedby="folder-space-age-hint" checked`} {
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
	hn.wantRedirect(hn.postImportUpload(c, map[string]string{"name": "Real"}, "a.zip", fixtureBytes(t, "2.0.77-space-age.zip")), "/worlds/real")
	list, _ := hn.worlds.List(context.Background())
	if len(list) != 1 || list[0].GameVersion != "2.0.77" || !list[0].SpaceAge {
		t.Errorf("list %+v", list)
	}
}
