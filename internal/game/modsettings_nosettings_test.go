package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// noSettingsDir is a run's work folder holding the definitions file defs and
// no mod-settings.dat, as the game leaves it when no loaded mod defines a
// setting (SPEC 9.6).
func noSettingsDir(t *testing.T, defs string) *os.Root {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "p", world.ModsDir), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "p", gameenv.ModSettingsDefsFile), []byte(defs), 0o600); err != nil {
		t.Fatal(err)
	}
	base, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = base.Close() })
	return base
}

// SPEC 9.6: when the definitions list no settings, the game writes no
// mod-settings.dat, and that is empty defaults, not a failed run.
func TestModSettingsNoSettingsMissingDefaultsIsEmpty(t *testing.T) {
	for name, defs := range map[string]string{
		"an empty list":      `{"settings":[]}`,
		"the {} Lua writes":  `{"settings":{}}`,
		"no settings member": `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			d, b, err := readModSettingsOutput(noSettingsDir(t, defs), "p", "2.1.24")
			if err != nil {
				t.Fatalf("a world with no settings failed: %v", err)
			}
			if string(d) != defs || string(b) != string(emptyModSettings("2.1.24")) {
				t.Errorf("definitions %q, defaults %d bytes", d, len(b))
			}
		})
	}
}

// SPEC 9.6: settings with no file are still a failed run.
func TestModSettingsNoSettingsFileWithSettingsStillFails(t *testing.T) {
	_, _, err := readModSettingsOutput(noSettingsDir(t, defsJSON), "p", "2.1.24")
	if !errors.Is(err, ErrModSettingsBadOutput) {
		t.Errorf("error %v, want ErrModSettingsBadOutput", err)
	}
	// A definitions file that does not parse is no proof there are none.
	_, _, err = readModSettingsOutput(noSettingsDir(t, `not json`), "p", "2.1.24")
	if !errors.Is(err, ErrModSettingsBadOutput) {
		t.Errorf("unparsable definitions: error %v, want ErrModSettingsBadOutput", err)
	}
}

// A defaults file that exists but cannot be used stays an error even when no
// settings are listed: only a missing file means empty defaults.
func TestModSettingsNoSettingsUnusableFileFails(t *testing.T) {
	base := noSettingsDir(t, `{"settings":[]}`)
	if err := os.Symlink("/etc/passwd", filepath.Join(base.Name(), "p", world.ModsDir, world.ModSettingsFile)); err != nil {
		t.Fatal(err)
	}
	_, _, err := readModSettingsOutput(base, "p", "2.1.24")
	if !errors.Is(err, ErrModSettingsBadOutput) {
		t.Errorf("error %v, want ErrModSettingsBadOutput", err)
	}
}

// The empty defaults the page decodes: a valid file with no settings in it,
// for any game version string.
func TestEmptyModSettingsDecodes(t *testing.T) {
	for _, v := range []string{"2.1.24", "2.0.77", "1.1.110", "", "weird"} {
		f, err := modsettings.Decode(emptyModSettings(v))
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		if _, ok, err := f.Get(modsettings.Startup, "anything"); ok || err != nil {
			t.Errorf("%q: found a setting (%t, %v)", v, ok, err)
		}
	}
}

// SPEC 5.6, 9.6: a world whose mods define no setting still gets its mods'
// planets, labelled from the mods and then the game's own text, and empty
// defaults the page can decode.
func TestModSettingDefsNoSettingsKeepsThePlanets(t *testing.T) {
	r := newModSettingsRig(t)
	ch := r.ask(context.Background())
	c := r.waitRunning()
	dir := r.workDir(c)
	// What a real run leaves: no mod-settings.dat, the planets, and the
	// game's locale copy.
	en := filepath.Join(dir, baseLocaleDir, "en")
	if err := os.MkdirAll(en, 0o750); err != nil {
		t.Fatal(err)
	}
	for p, s := range map[string]string{
		gameenv.ModSettingsDefsFile:                         `{"settings":{}}`,
		gameenv.PlanetsFile:                                 planetsJSON,
		filepath.Join(baseLocaleDir, "en", "base-base.cfg"): "[space-location-name]\nnauvis=Nauvis\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r.game.Daemon().Exit(c.ID, 0, false)
	res := r.result(ch)
	if res.err != nil {
		t.Fatalf("a world with no settings failed: %v", res.err)
	}
	if res.defs.PlanetsErr != nil {
		t.Fatalf("planets error %v", res.defs.PlanetsErr)
	}
	if _, err := modsettings.Decode(res.defs.Defaults); err != nil {
		t.Errorf("the empty defaults do not decode: %v", err)
	}
	cat := res.defs.Catalogue("en")
	var names []string
	for _, p := range cat.Planets {
		names = append(names, p.Name+"="+p.Label)
	}
	if !slices.Contains(names, "moon=moon") || !slices.Contains(names, "nauvis=Nauvis") {
		t.Errorf("planets %v, want the modded moon and the game's label for nauvis", names)
	}
}
