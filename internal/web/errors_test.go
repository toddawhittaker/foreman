package web

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		{world.ErrWorldChanged, http.StatusConflict, msgWorldChanged},
		{fmt.Errorf("%w: mods/link is a symlink", world.ErrUnsupportedFile), http.StatusConflict, "mods/link"},
		{world.ErrRestoreNameTaken, http.StatusConflict, "Rename that world"},
		{fmt.Errorf("%w: saves/a.zip has changed", world.ErrDamaged), http.StatusConflict, "saves/a.zip has changed"},
		{world.ErrBusy, http.StatusConflict, "Another operation is running"},
		{world.ErrActive, http.StatusConflict, "active world cannot be deleted"},
		{world.ErrNewerFormat, http.StatusConflict, "newer version of Foreman"},
		{world.ErrSaveExists, http.StatusConflict, "Rename the file"},
		{fmt.Errorf("%w: this needs 3 GB", world.ErrNoSpace), http.StatusInsufficientStorage, "Nothing was changed"},
		{world.ErrSaveName, http.StatusUnprocessableEntity, "must end in .zip"},
		{uploadError{"This file is not a zip file."}, http.StatusUnprocessableEntity, "not a zip file"},
		{world.ErrNameTaken, http.StatusBadRequest, "Choose a different name."},
		{fs.ErrNotExist, http.StatusNotFound, ""},
		{world.ErrNoSnapshot, http.StatusNotFound, ""},
		{fmt.Errorf("disk on fire"), http.StatusInternalServerError, ""},
	} {
		status, msg := worldError(tc.err)
		if status != tc.status || !strings.Contains(msg, tc.want) {
			t.Errorf("%v: %d %q, want %d with %q", tc.err, status, msg, tc.status, tc.want)
		}
		if msg != "" && (strings.ToUpper(msg[:1]) != msg[:1] || !strings.HasSuffix(msg, ".")) {
			t.Errorf("%v: %q is not a sentence", tc.err, msg)
		}
	}
}

// A world or snapshot folder that is a symlink is not a world or snapshot:
// its pages answer 404, not a server fault (R17).
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
