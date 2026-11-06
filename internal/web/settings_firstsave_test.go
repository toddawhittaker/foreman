package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// requiredKeys fails the test unless text is a server-settings.json the game
// starts with on every line it supports (SPEC 11.1, 11.2).
func requiredKeys(t *testing.T, text string) {
	t.Helper()
	for _, line := range []string{gamesettings.Line11, gamesettings.Line20, gamesettings.Line21} {
		if errs := gamesettings.Validate(gamesettings.FileServerSettings, line, []byte(text)); len(errs) != 0 {
			t.Errorf("the file the form wrote is not one the game starts with (%s): %+v\n%s", line, errs, text)
		}
	}
	if _, ok := gamesettings.ReadVisibility([]byte(text)); !ok {
		t.Errorf("no usable visibility:\n%s", text)
	}
}

// removeSettingsFile leaves the rig's world with no server-settings.json, as
// a generated world has.
func (r *settingsRig) removeSettingsFile() {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, world.ConfigDir, world.ServerSettingsFile)); err != nil {
		r.t.Fatal(err)
	}
}

// SPEC 11.1, 11.2: a world with no server-settings.json starts with the
// world's name in the form and Hidden chosen, and a first save of only a name
// and a password writes the three keys the game needs.
func TestSettingsFirstSaveWritesRequiredKeys(t *testing.T) {
	r := newSettingsRig(t)
	r.removeSettingsFile()

	body := r.getAs(r.cookie, r.path("")).Body.String()
	for _, want := range []string{`value="Base"`, `id="visibility-hidden" name="visibility" value="hidden" checked`,
		"The file has no visibility yet. Hidden will be written unless another is chosen."} {
		if !strings.Contains(body, want) {
			t.Errorf("first form lacks %q:\n%s", want, body)
		}
	}

	// What the page posts when the person types only a password.
	form := url.Values{
		"name": {"Base"}, "description": {""}, "visibility": {"hidden"}, "game_password": {"newpass-typed"},
		"max_players": {""}, "autosave_interval": {""}, "autosave_slots": {""}, "admins": {""},
	}
	rec := r.postAs(r.cookie, r.path(""), form)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status %d:\n%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"<code>name</code>", "<code>description</code>", "<code>visibility.public</code>", "<code>game_password</code>"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("preview lacks %q:\n%s", want, rec.Body.String())
		}
	}
	conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, rec.Body.String())}})
	if conf.Code != http.StatusSeeOther {
		t.Fatalf("confirm status %d:\n%s\nlogs: %s", conf.Code, conf.Body.String(), r.logs.String())
	}
	got := r.read(world.ServerSettingsFile)
	requiredKeys(t, got)
	for _, want := range []string{`"name": "Base"`, `"description": ""`, `"visibility": {"public": false, "lan": false}`, `"game_password": "newpass-typed"`} {
		if !strings.Contains(got, want) {
			t.Errorf("file lacks %q:\n%s", want, got)
		}
	}
}

// A first save that posts no visibility at all still writes Hidden, because
// the game needs one.
func TestSettingsFirstSaveWithoutVisibilityDefaultsToHidden(t *testing.T) {
	r := newSettingsRig(t)
	r.removeSettingsFile()
	rec := r.postAs(r.cookie, r.path(""), url.Values{"name": {"Mine"}, "game_password": {"newpass-typed"}})
	conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, rec.Body.String())}})
	if conf.Code != http.StatusSeeOther {
		t.Fatalf("confirm status %d:\n%s", conf.Code, conf.Body.String())
	}
	got := r.read(world.ServerSettingsFile)
	requiredKeys(t, got)
	if !strings.Contains(got, `"name": "Mine"`) || !strings.Contains(got, `"lan": false`) {
		t.Errorf("file:\n%s", got)
	}
}

// A file that exists but lacks description and visibility is completed by a
// form save, and the keys it has are kept.
func TestSettingsFormCompletesAPartialFile(t *testing.T) {
	r := newSettingsRig(t)
	r.write(world.ServerSettingsFile, "{\n  \"name\": \"Old\",\n  \"max_players\": 4\n}\n")
	rec := r.postAs(r.cookie, r.path(""), url.Values{"name": {"Old"}, "max_players": {"4"}, "game_password": {"newpass-typed"}})
	conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, rec.Body.String())}})
	if conf.Code != http.StatusSeeOther {
		t.Fatalf("confirm status %d:\n%s", conf.Code, conf.Body.String())
	}
	got := r.read(world.ServerSettingsFile)
	requiredKeys(t, got)
	if !strings.Contains(got, `"max_players": 4`) || !strings.Contains(got, `"name": "Old"`) {
		t.Errorf("file:\n%s", got)
	}
}

// A found file's own name is shown, not the world's.
func TestSettingsFormKeepsTheFilesName(t *testing.T) {
	r := newSettingsRig(t)
	body := r.getAs(r.cookie, r.path("")).Body.String()
	if !strings.Contains(body, `value="Family base"`) || strings.Contains(body, `value="Base"`) {
		t.Errorf("form:\n%s", body)
	}
}

// SPEC 11.2: the raw editor refuses a file without the keys the game needs,
// naming the line and the key.
func TestSettingsRawEditorRefusesMissingKeys(t *testing.T) {
	r := newSettingsRig(t)
	rec := r.postAs(r.cookie, r.path("/raw/server-settings.json"), url.Values{"content": {"{\n  \"name\": \"x\"\n}\n"}})
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d:\n%s", rec.Code, body)
	}
	for _, key := range []string{"description", "visibility"} {
		if !strings.Contains(body, key) {
			t.Errorf("errors do not name %q:\n%s", key, body)
		}
	}
	if !strings.Contains(body, "Line 1") {
		t.Errorf("errors do not name the line:\n%s", body)
	}
}
