package web

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Every refusal from the world manager has one status and words that say
// what to do next; anything else is a 500 with no words (SPEC 14.8).
func TestWorldErrorMapsEveryRefusal(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		want   string
	}{
		{world.ErrWorldChanged, http.StatusConflict, "This world was changed or deleted while your upload was running."},
		{world.ErrUnreadable, http.StatusConflict, "world.json cannot be read"},
		{&world.UnsupportedFileError{Path: "mods/link"}, http.StatusConflict, "mods/link is a symlink or special file"},
		{world.ErrRestoreNameTaken, http.StatusConflict, "Rename that world"},
		{&world.DamagedError{Kind: world.DamageChanged, Path: "saves/a.zip"}, http.StatusConflict, "restore it: saves/a.zip has changed. The world was not changed. Restore a different snapshot."},
		{&world.DamagedError{Kind: world.DamageMissing, Path: "a"}, http.StatusConflict, "restore it: a is missing."},
		{&world.DamagedError{Kind: world.DamageBadPath, Path: "../a"}, http.StatusConflict, "restore it: bad path \"../a\"."},
		{&world.DamagedError{Kind: world.DamageChangedSince, Path: "a"}, http.StatusConflict, "a has changed since the snapshot was taken."},
		{world.ErrBusy, http.StatusConflict, "Another operation is running"},
		{world.ErrActive, http.StatusConflict, "active world cannot be deleted"},
		{world.ErrNewerFormat, http.StatusConflict, "newer version of Foreman"},
		{world.ErrSaveExists, http.StatusConflict, "Rename the file"},
		{fmt.Errorf("take the snapshot: %w", &world.NoSpaceError{Need: 3e9, Reserve: 2e9, Free: 1e9}), http.StatusInsufficientStorage,
			"Nothing was changed: Not enough free disk space: this needs 3.0 GB, including 2.0 GB kept free for the game, but only 1.0 GB is free. Free some space and try again."},
		{world.ErrSaveName, http.StatusUnprocessableEntity, "must end in .zip"},
		{uploadError{i18n.M("worlds.upload.not_zip")}, http.StatusUnprocessableEntity, "not a zip file"},
		{world.ErrNameTaken, http.StatusBadRequest, "Choose a different name."},
		{world.SaveClashError{Name: "a.zip"}, http.StatusBadRequest, "The save a.zip is in both the folder and its saves folder"},
		{fs.ErrNotExist, http.StatusNotFound, ""},
		{world.ErrNoSnapshot, http.StatusNotFound, ""},
		{fmt.Errorf("disk on fire"), http.StatusInternalServerError, ""},
	} {
		status, msg := worldError(tc.err)
		text := i18n.English.Format(msg)
		if status != tc.status || !strings.Contains(text, tc.want) {
			t.Errorf("%v: %d %q, want %d with %q", tc.err, status, text, tc.status, tc.want)
		}
		if tc.want == "" && !msg.IsZero() {
			t.Errorf("%v: has words %q, want none", tc.err, text)
		}
		if text != "" && (strings.ToUpper(text[:1]) != text[:1] || !strings.HasSuffix(text, ".")) {
			t.Errorf("%v: %q is not a sentence", tc.err, text)
		}
	}
}

// A refusal reads in every language, with its figures filled in.
func TestWorldErrorIsTranslated(t *testing.T) {
	_, msg := worldError(&world.NoSpaceError{Need: 3e9, Reserve: 2e9, Free: 1e9})
	for _, l := range i18n.Languages {
		text := l.Tag.Format(msg)
		if text == "" || strings.Contains(text, "{") || strings.Contains(text, "worlds.error") {
			t.Errorf("%s: %q", l.Tag, text)
		}
	}
	if got := i18n.Locale("de").Format(msg); !strings.Contains(got, "3,0 GB") {
		t.Errorf("de: sizes are not in the language's format: %q", got)
	}
}

// A world or snapshot folder that is a symlink is not a world or snapshot:
// its pages answer 404, not a server fault (SPEC 14.5).
func TestSymlinkedFoldersAreNotFound(t *testing.T) {
	sc := newSecCase(t)
	hn := sc.ls.harness
	dirs := hn.worlds.Dirs()
	if err := os.Symlink(filepath.Join(dirs.Worlds, sc.id), filepath.Join(dirs.Worlds, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dirs.Snapshots, sc.id), filepath.Join(dirs.Snapshots, "linked")); err != nil {
		t.Fatal(err)
	}
	// A real world whose snapshot folder is a symlink to another's.
	if err := os.Symlink(filepath.Join(dirs.Snapshots, sc.id), filepath.Join(dirs.Snapshots, sc.other)); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		snapshotsPath(sc.other), restorePath(sc.other, sc.snap),
		worldPath("linked"), deletePath("linked"), snapshotsPath("linked"),
		restorePath("linked", sc.snap), "/worlds/linked/saves",
	} {
		if rec := hn.get(p, func(r *http.Request) { r.Header.Set("Cookie", sc.cookie.Get("Cookie")) }); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", p, rec.Code)
		}
	}
}
