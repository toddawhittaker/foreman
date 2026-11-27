package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 5.6, 8.7, issue 965: a planets read the game failed names the world's
// mod its error names, by title, with the plan that removes it; without one
// it says a kept failure waits for a change of mods, not "try again later".
func TestBuiltinFailure(t *testing.T) {
	installed := []world.InstalledMod{
		{Name: "base", Title: "Base", Version: "2.1.24", BuiltIn: true},
		{Name: "even-distribution", Title: "Even Distribution", Version: "2.1.0"},
		{Name: "untitled", Version: "1.0.0"},
	}
	crash := func(line string) error {
		pe := &game.PreviewError{ExitCode: 1, ErrorLine: line}
		return fmt.Errorf("%w: %w", game.ErrModSettingsKept, fmt.Errorf("%w: %w", game.ErrModSettingsBadOutput, pe))
	}
	cases := []struct {
		name, key, mod string
		err            error
	}{
		{"named", "newmap.builtin.mod_failed", "Even Distribution", crash(`Error Util.cpp:81: Failed to load mod "even-distribution": __base__/x.lua:1 __even-distribution__/scripts/setup.lua:297: boom`)},
		{"no title", "newmap.builtin.mod_failed", "untitled", crash(`Error Util.cpp:81: Failed to load mod "untitled": __untitled__/data.lua:1: boom`)},
		// A control-stage error: the event line names the mod, or else its
		// title and version in the error line match one of the world's mods.
		{"control stage, event line", "newmap.builtin.mod_failed", "Even Distribution", controlCrash("Even Distribution", "2.1.0", "even-distribution")},
		{"control stage, title only", "newmap.builtin.mod_failed", "Even Distribution", controlCrash("Even Distribution", "2.1.0", "")},
		{"control stage, untitled", "newmap.builtin.mod_failed", "untitled", controlCrash("untitled", "1.0.0", "")},
		{"control stage, other version", "newmap.builtin.failed_kept", "", controlCrash("Even Distribution", "2.0.0", "")},
		{"control stage, built-in", "newmap.builtin.failed_kept", "", controlCrash("Base", "2.1.24", "")},
		{"not the world's mod", "newmap.builtin.failed_kept", "", crash(`Error Util.cpp:81: Failed to load mod "elsewhere": __elsewhere__/data.lua:1: boom`)},
		{"kept, no mod", "newmap.builtin.failed_kept", "", crash("Error Util.cpp:81: boom")},
		{"not kept", "newmap.builtin.failed", "", game.ErrModSettingsTimeout},
		{"no wrap", "newmap.builtin.failed", "", errors.New("x")},
	}
	for _, tc := range cases {
		why, fix := builtinFailure(installed, "w1", tc.err)
		if why.Key != tc.key {
			t.Errorf("%s: key %q, want %q", tc.name, why.Key, tc.key)
		}
		if tc.mod == "" {
			if fix != nil {
				t.Errorf("%s: a fix %+v", tc.name, fix)
			}
			continue
		}
		if fix == nil || fix.Label.Key != "mods.flag.fix_remove" {
			t.Fatalf("%s: fix %+v", tc.name, fix)
		}
		name := map[string]string{"Even Distribution": "even-distribution", "untitled": "untitled"}[tc.mod]
		if want := kindPlanPath("w1", mods.KindRemove, name); fix.Path != want {
			t.Errorf("%s: path %q, want %q", tc.name, fix.Path, want)
		}
		if fmt.Sprint(why.Args["mod"]) != fmt.Sprint(fix.Label.Args["mod"]) || !strings.Contains(fmt.Sprint(why.Args["mod"]), tc.mod) {
			t.Errorf("%s: mod %v, want %q", tc.name, why.Args["mod"], tc.mod)
		}
	}
}

// controlCrash is a kept planets read that failed in a mod's control stage:
// the game's error line names the mod by title and version, and the event
// line, when event is not empty, by name.
func controlCrash(title, version, event string) error {
	pe := &game.PreviewError{ExitCode: 1, ErrorLine: fmt.Sprintf(`Error ServerMultiplayerManager.cpp:85: MultiplayerManager failed: "The mod %s (%s) caused a non-recoverable error.`, title, version)}
	if event != "" {
		pe.EventLine = "Error while running event " + event + "::on_init()"
	}
	return fmt.Errorf("%w: %w", game.ErrModSettingsKept, fmt.Errorf("%w: %w", game.ErrModSettingsBadOutput, pe))
}

// evilTitle is a mod title that would run script if it reached the page
// unescaped.
const evilTitle = `<img src=x onerror=alert(1)>`

// installWorldMod puts a mod zip with name, version and title in world id's
// mods folder on hn.
func installWorldMod(t *testing.T, hn *harness, id, name, version, title string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	file := name + "_" + version
	w, err := zw.Create(file + "/info.json")
	if err != nil {
		t.Fatal(err)
	}
	info, _ := json.Marshal(map[string]any{"name": name, "version": version, "title": title, "factorio_version": "2.1"})
	_, _ = w.Write(info)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(hn.worlds.Dirs().Worlds, id, world.ModsDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, file+".zip"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// crashedWorld is a world on hn with a mod "evil" titled evilTitle, whose
// planets read failed in that mod's data stage.
func crashedWorld(t *testing.T, hn *harness, id string) {
	t.Helper()
	installWorldMod(t, hn, id, "evil", "1.0.0", evilTitle)
	pe := &game.PreviewError{ExitCode: 1, ErrorLine: `Error Util.cpp:81: Failed to load mod "evil": __evil__/data.lua:1: boom`}
	withWorldCatalogue(t, mapgen.Builtin("2.1", true), fmt.Errorf("%w: %w", game.ErrModSettingsKept, fmt.Errorf("%w: %w", game.ErrModSettingsBadOutput, pe)))
}

// wantCrashedEvil checks body names the crashed mod by its escaped title in
// step 3's warning, with the Remove link.
func wantCrashedEvil(t *testing.T, body, id string) {
	t.Helper()
	esc := html.EscapeString(evilTitle)
	warning := `<div class="message message--warning" id="newmap-builtin">`
	i := strings.Index(body, warning)
	if i < 0 {
		t.Fatalf("no warning in:\n%s", body)
	}
	box := body[i:]
	box = box[:strings.Index(box, "</div>")]
	remove := `<a href="` + html.EscapeString(kindPlanPath(id, mods.KindRemove, "evil")) + `" id="newmap-builtin-remove">`
	for _, want := range []string{
		`<span class="ident" translate="no">` + esc + `</span> failed`,
		remove + `Remove <span class="ident" translate="no">` + esc + `</span></a>`,
	} {
		if !strings.Contains(box, want) {
			t.Errorf("no %s in the warning:\n%s", want, box)
		}
	}
	if strings.Contains(body, evilTitle) {
		t.Error("the mod's title is on the page unescaped")
	}
}

// SPEC 5.6, 8.7, 14.6: step 3 names the crashed mod by its title, escaped,
// in the warning and in its Remove link.
func TestNewMapNamesTheCrashedModEscaped(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	id := newMapWorld(t, hn, c)
	crashedWorld(t, hn, id)
	wantCrashedEvil(t, getLong(hn, newMapPath(id)+"?wait=1", c).Body.String(), id)
}

// SPEC 5.6: after a preview is posted, step 3 says the same as on its load:
// the crashed mod by title, with its Remove link.
func TestPreviewPostNamesTheCrashedMod(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	id, err := hn.worlds.Create(t.Context(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	crashedWorld(t, hn, id)
	rec := hn.postLong(PathGeneratePreview, worldPreviewForm(id, "planet", "nauvis"), c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	wantCrashedEvil(t, rec.Body.String(), id)
}

// SPEC 5.6, 8.7, issue 965: a planets read that failed in a mod's control
// stage, as Even Distribution's on_init did, names that mod on step 3 by its
// title and offers to remove it, whether the event line or only the title
// and version name it.
func TestNewMapNamesTheControlStageMod(t *testing.T) {
	for _, event := range []string{"even-distribution", ""} {
		t.Run("event "+event, func(t *testing.T) {
			hn := newGenerateHarness(t, nil)
			c := hn.signIn()
			id := newMapWorld(t, hn, c)
			installWorldMod(t, hn, id, "even-distribution", "2.1.0", "Even Distribution")
			withWorldCatalogue(t, mapgen.Builtin("2.1", true), controlCrash("Even Distribution", "2.1.0", event))
			body := getLong(hn, newMapPath(id)+"?wait=1", c).Body.String()
			i := strings.Index(body, `<div class="message message--warning" id="newmap-builtin">`)
			if i < 0 {
				t.Fatalf("no warning in:\n%s", body)
			}
			box := body[i:]
			box = box[:strings.Index(box, "</div>")]
			remove := `<a href="` + html.EscapeString(kindPlanPath(id, mods.KindRemove, "even-distribution")) + `" id="newmap-builtin-remove">`
			for _, want := range []string{
				`<span class="ident" translate="no">Even Distribution</span> failed`,
				remove + `Remove <span class="ident" translate="no">Even Distribution</span></a>`,
			} {
				if !strings.Contains(box, want) {
					t.Errorf("no %s in the warning:\n%s", want, box)
				}
			}
		})
	}
}
