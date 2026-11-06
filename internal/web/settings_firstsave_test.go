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

// autoPauseBox is the auto-pause checkbox as the page renders it.
const autoPauseBox = `name="auto_pause" checked`

// SPEC 11.1: the first save on a world with no file writes auto_pause on,
// the game's own default, with the three keys the game needs; unticking the
// box writes it off.
func TestSettingsFirstSaveWritesAutoPause(t *testing.T) {
	for name, tc := range map[string]struct {
		form url.Values
		want string
	}{
		"left on":  {url.Values{"name": {"Base"}, "description": {""}, "visibility": {"hidden"}, "auto_pause": {"on"}}, `"auto_pause": true`},
		"unticked": {url.Values{"name": {"Base"}, "description": {""}, "visibility": {"hidden"}}, `"auto_pause": false`},
	} {
		t.Run(name, func(t *testing.T) {
			r := newSettingsRig(t)
			r.removeSettingsFile()
			if body := r.getAs(r.cookie, r.path("")).Body.String(); !strings.Contains(body, autoPauseBox) {
				t.Errorf("a world with no file shows auto-pause off:\n%s", body)
			}
			rec := r.postAs(r.cookie, r.path(""), tc.form)
			if rec.Code != http.StatusOK {
				t.Fatalf("preview status %d:\n%s", rec.Code, rec.Body.String())
			}
			conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, rec.Body.String())}})
			if conf.Code != http.StatusSeeOther {
				t.Fatalf("confirm status %d:\n%s", conf.Code, conf.Body.String())
			}
			got := r.read(world.ServerSettingsFile)
			requiredKeys(t, got)
			if !strings.Contains(got, tc.want) {
				t.Errorf("file lacks %q:\n%s", tc.want, got)
			}
		})
	}
}

// SPEC 11.1: a file that has no auto_pause key reads as on and is left as it
// is when the box stays ticked; an explicit false is shown off and kept.
func TestSettingsMissingAutoPauseReadsAsOn(t *testing.T) {
	const partial = `{"name": "Old", "description": "", "visibility": {"public": false, "lan": false}`
	r := newSettingsRig(t)
	r.write(world.ServerSettingsFile, partial+"}\n")
	if body := r.getAs(r.cookie, r.path("")).Body.String(); !strings.Contains(body, autoPauseBox) {
		t.Errorf("a file with no auto_pause shows it off:\n%s", body)
	}
	same := url.Values{"name": {"Old"}, "description": {""}, "visibility": {"hidden"}, "auto_pause": {"on"}}
	if rec := r.postAs(r.cookie, r.path(""), same); !strings.Contains(rec.Body.String(), "Nothing changed") {
		t.Errorf("an untouched box changed the file:\n%s", rec.Body.String())
	}

	r.write(world.ServerSettingsFile, partial+`, "auto_pause": false}`+"\n")
	if body := r.getAs(r.cookie, r.path("")).Body.String(); strings.Contains(body, autoPauseBox) {
		t.Errorf("an explicit false shows on:\n%s", body)
	}
	off := url.Values{"name": {"Old"}, "description": {""}, "visibility": {"hidden"}}
	if rec := r.postAs(r.cookie, r.path(""), off); !strings.Contains(rec.Body.String(), "Nothing changed") {
		t.Errorf("an explicit false was changed:\n%s", rec.Body.String())
	}
}
