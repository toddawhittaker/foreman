package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The three game lines Foreman supports, by a version of each.
var settingsGameLines = []string{"1.1.110", "2.0.77", "2.1.24"}

// bareFile is a server-settings.json that sets none of the form's optional
// keys, as a world made by an older Foreman holds.
const bareFile = `{"name": "Old", "description": "", "visibility": {"public": false, "lan": false}}` + "\n"

// bareForm is what the page posts for bareFile when nothing is touched.
func bareForm() url.Values {
	return withDefaultBoxes(url.Values{"name": {"Old"}, "description": {""}, "visibility": {"hidden"}, "auto_pause": {"on"}})
}

// SPEC 17, 11.1: every unset field shows the value that applies, as its
// placeholder and in a hint, on each game line; the game's default of each
// is the one in that line's example file.
func TestSettingsUnsetFieldsShowTheGameDefault(t *testing.T) {
	for _, version := range settingsGameLines {
		t.Run(version, func(t *testing.T) {
			r := newSettingsRigOn(t, version)
			r.write(world.ServerSettingsFile, bareFile)
			body := r.getAs(r.cookie, r.path("")).Body.String()
			for _, want := range []string{
				`id="max-players" name="max_players" type="text" inputmode="numeric" value="" placeholder="0"`,
				`name="afk_autokick_interval" type="text" inputmode="numeric" value="" placeholder="0"`,
				`name="autosave_interval" type="text" inputmode="numeric" value="" placeholder="10"`,
				`name="autosave_slots" type="text" inputmode="numeric" value="" placeholder="5"`,
				`name="max_upload_in_kilobytes_per_second" type="text" inputmode="numeric" value="" placeholder="0"`,
				`name="max_upload_slots" type="text" inputmode="numeric" value="" placeholder="5"`,
				"Game default: 5", "Game default: 10", "Foreman gives new worlds 20 slots.",
				`name="require_user_verification" checked`, `name="only_admins_can_pause_the_game" checked`,
				`name="auto_pause" checked`, `<option value="admins-only" selected>`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("the form lacks %q", want)
				}
			}
			for _, off := range []string{`name="ignore_player_limit_for_returning_players" checked`, `name="non_blocking_saving" checked`,
				`name="auto_pause_when_players_connect" checked`} {
				if strings.Contains(body, off) {
					t.Errorf("a box the game leaves off is ticked: %q", off)
				}
			}
			if strings.Contains(body, "Game default: on") == false || strings.Contains(body, "Game default: off") == false {
				t.Error("the checkboxes do not say the game's default")
			}
			// Posting what the page shows changes nothing.
			if rec := r.postAs(r.cookie, r.path(""), bareForm()); !strings.Contains(rec.Body.String(), "Nothing changed") {
				t.Errorf("an untouched form changed the file:\n%s", rec.Body.String())
			}
		})
	}
}

// SPEC 11.1: a world's first settings save writes 20 autosave slots, and the
// form shows 20 as the value before then.
func TestSettingsFirstFormShowsForemanSlots(t *testing.T) {
	r := newSettingsRig(t)
	r.removeSettingsFile()
	body := r.getAs(r.cookie, r.path("")).Body.String()
	if !strings.Contains(body, `name="autosave_slots" type="text" inputmode="numeric" value="20"`) {
		t.Errorf("a world with no file does not show 20 slots:\n%s", body)
	}
}

// SPEC 11.1: an older file without autosave_slots shows the game's 5, is not
// written at start, and keeps it until the person types a number.
func TestSettingsOlderFileKeepsTheGameSlots(t *testing.T) {
	r := newSettingsRig(t)
	r.write(world.ServerSettingsFile, bareFile)
	form := bareForm()
	form.Set("autosave_slots", "7")
	rec := r.postAs(r.cookie, r.path(""), form)
	if conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, rec.Body.String())}}); conf.Code != http.StatusSeeOther {
		t.Fatalf("confirm %d", conf.Code)
	}
	if got := r.read(world.ServerSettingsFile); !strings.Contains(got, `"autosave_slots": 7`) {
		t.Errorf("typed slots not written:\n%s", got)
	}
}

// Each new field writes its own key, and only when it differs from what the
// page showed.
func TestSettingsNewFieldsWriteTheirKeys(t *testing.T) {
	for _, version := range settingsGameLines {
		t.Run(version, func(t *testing.T) {
			r := newSettingsRigOn(t, version)
			r.write(world.ServerSettingsFile, bareFile)
			form := bareForm()
			form.Set("ignore_player_limit_for_returning_players", "on")
			form.Set("afk_autokick_interval", "15")
			form.Del("only_admins_can_pause_the_game")
			form.Set("allow_commands", "false")
			form.Set("non_blocking_saving", "on")
			form.Set("max_upload_in_kilobytes_per_second", "512")
			form.Set("max_upload_slots", "3")
			form.Set("auto_pause_when_players_connect", "on")
			rec := r.postAs(r.cookie, r.path(""), form)
			if rec.Code != http.StatusOK {
				t.Fatalf("preview %d:\n%s", rec.Code, rec.Body.String())
			}
			if conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, rec.Body.String())}}); conf.Code != http.StatusSeeOther {
				t.Fatalf("confirm %d:\n%s", conf.Code, conf.Body.String())
			}
			got := r.read(world.ServerSettingsFile)
			want := []string{`"ignore_player_limit_for_returning_players": true`, `"afk_autokick_interval": 15`,
				`"only_admins_can_pause_the_game": false`, `"allow_commands": false`, `"non_blocking_saving": true`,
				`"max_upload_in_kilobytes_per_second": 512`, `"max_upload_slots": 3`}
			if gamesettings.HasPauseWhenConnect(lineOf(version)) {
				want = append(want, `"auto_pause_when_players_connect": true`)
			} else if strings.Contains(got, "auto_pause_when_players_connect") {
				t.Errorf("1.1 has no such setting, but it was written:\n%s", got)
			}
			for _, w := range want {
				if !strings.Contains(got, w) {
					t.Errorf("file lacks %q:\n%s", w, got)
				}
			}
			if errs := gamesettings.Validate(gamesettings.FileServerSettings, lineOf(version), []byte(got)); len(errs) != 0 {
				t.Errorf("the game would refuse the file: %+v", errs)
			}
		})
	}
}

func lineOf(version string) string { return version[:strings.LastIndex(version, ".")] }

// SPEC 11.1: the pause-while-joining setting is offered from 2.0, not on 1.1.
func TestSettingsPauseOnJoinOnlyFrom20(t *testing.T) {
	for _, version := range settingsGameLines {
		r := newSettingsRigOn(t, version)
		r.write(world.ServerSettingsFile, bareFile)
		has := strings.Contains(r.getAs(r.cookie, r.path("")).Body.String(), `name="auto_pause_when_players_connect"`)
		if want := version != "1.1.110"; has != want {
			t.Errorf("%s: pause-while-joining offered = %v, want %v", version, has, want)
		}
	}
}

// A file that holds a key on 1.1 which the form does not offer there stays as
// it is, even though the form posts nothing for it.
func TestSettingsHiddenFieldIsKept(t *testing.T) {
	r := newSettingsRigOn(t, "1.1.110")
	r.write(world.ServerSettingsFile, strings.Replace(bareFile, `"description"`, `"auto_pause_when_players_connect": true, "description"`, 1))
	if rec := r.postAs(r.cookie, r.path(""), bareForm()); !strings.Contains(rec.Body.String(), "Nothing changed") {
		t.Errorf("a key the 1.1 form hides was changed:\n%s", rec.Body.String())
	}
}

// A bad number in a new field is refused with the field's own error.
func TestSettingsNewNumberFieldsAreChecked(t *testing.T) {
	r := newSettingsRig(t)
	r.write(world.ServerSettingsFile, bareFile)
	for _, name := range []string{"afk_autokick_interval", "max_upload_in_kilobytes_per_second", "max_upload_slots"} {
		form := bareForm()
		form.Set(name, "-3")
		rec := r.postAs(r.cookie, r.path(""), form)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Enter a whole number") {
			t.Errorf("%s: %d\n%s", name, rec.Code, rec.Body.String())
		}
	}
}

// SPEC 8.7, 11.1: fork saving is off by default and carries the note; where
// Docker reports a host that is not Linux, it is disabled with the reason,
// and a posted tick is never written.
func TestSettingsForkSaving(t *testing.T) {
	for _, tc := range []struct {
		os       string
		disabled bool
	}{{"linux", false}, {"", false}, {"windows", true}} {
		t.Run("os "+tc.os, func(t *testing.T) {
			g := newGameRig(t, image21)
			id := g.newWorld("Base", "2.1.24-no-space-age.zip", false)
			g.fake.Daemon().SetOS(tc.os)
			g.writeConfig(id, world.ServerSettingsFile, bareFile)
			body := g.get(settingsPath(id))
			input := between(body, `<input type="checkbox" id="non-blocking-saving"`, ">")
			if input == "" {
				t.Fatalf("no fork saving checkbox:\n%s", body)
			}
			if got := strings.Contains(input, " disabled"); got != tc.disabled {
				t.Errorf("disabled = %v, want %v: %s", got, tc.disabled, input)
			}
			for _, want := range []string{"highly experimental", "close to double"} {
				if !strings.Contains(body, want) {
					t.Errorf("the note lacks %q", want)
				}
			}
			if reason := strings.Contains(body, "Docker reports this host as windows."); reason != tc.disabled {
				t.Errorf("reason shown = %v, want %v", reason, tc.disabled)
			}
			form := bareForm()
			form.Set("non_blocking_saving", "on")
			rep := g.post(settingsPath(id), form)
			wrote := !strings.Contains(rep.body, "Nothing changed")
			if wrote == tc.disabled {
				t.Errorf("a posted tick wrote the key = %v, want %v:\n%s", wrote, !tc.disabled, rep.body)
			}
		})
	}
}

// allow_commands takes three choices; anything else posted leaves the file
// as it is.
func TestSettingsAllowCommands(t *testing.T) {
	for post, want := range map[string]string{"true": `"allow_commands": true`, "false": `"allow_commands": false`, "admins-only": "", "bogus": ""} {
		t.Run(post, func(t *testing.T) {
			r := newSettingsRig(t)
			r.write(world.ServerSettingsFile, bareFile)
			form := bareForm()
			form.Set("allow_commands", post)
			rec := r.postAs(r.cookie, r.path(""), form)
			if want == "" {
				if !strings.Contains(rec.Body.String(), "Nothing changed") {
					t.Errorf("%q changed the file:\n%s", post, rec.Body.String())
				}
				return
			}
			if conf := r.postAs(r.cookie, r.path("/confirm"), url.Values{"token": {tokenOf(t, rec.Body.String())}}); conf.Code != http.StatusSeeOther {
				t.Fatalf("confirm %d", conf.Code)
			}
			if got := r.read(world.ServerSettingsFile); !strings.Contains(got, want) {
				t.Errorf("file lacks %q:\n%s", want, got)
			}
		})
	}
	// An existing text value shows as selected.
	r := newSettingsRig(t)
	r.write(world.ServerSettingsFile, strings.Replace(bareFile, `"description"`, `"allow_commands": "true", "description"`, 1))
	if body := r.getAs(r.cookie, r.path("")).Body.String(); !strings.Contains(body, fmt.Sprintf(`<option value=%q selected>`, "true")) {
		t.Errorf("allow_commands text true not selected")
	}
}
