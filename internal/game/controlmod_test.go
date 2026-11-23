package game

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// The game loads a control mod only when factorio_version is its own line,
// and the territory mod needs Space Age, which has Vulcanus. The count mod
// depends on Space Age only when the map has it. The bytes are pinned so a
// change to modInfo cannot alter what the game reads.
func TestModInfoJSON(t *testing.T) {
	for name, tc := range map[string]struct{ got, want string }{
		"territories 2.1": {string(territoryInfo("2.1")), `{
  "name": "foreman-territories",
  "version": "1.0.0",
  "title": "Foreman territories",
  "author": "Foreman",
  "factorio_version": "2.1",
  "description": "Writes Vulcanus's demolisher territories for Foreman's map preview. Adds no prototypes.",
  "dependencies": [
    "base",
    "space-age"
  ]
}
`},
		"resources 1.1": {string(resourceInfo("1.1", false)), `{
  "name": "foreman-resources",
  "version": "1.0.0",
  "title": "Foreman resources",
  "author": "Foreman",
  "factorio_version": "1.1",
  "description": "Counts one planet's resource patches for Foreman's map preview. Adds no prototypes.",
  "dependencies": [
    "base"
  ]
}
`},
		"resources 2.0 Space Age": {string(resourceInfo("2.0", true)), `{
  "name": "foreman-resources",
  "version": "1.0.0",
  "title": "Foreman resources",
  "author": "Foreman",
  "factorio_version": "2.0",
  "description": "Counts one planet's resource patches for Foreman's map preview. Adds no prototypes.",
  "dependencies": [
    "base",
    "space-age"
  ]
}
`},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: info.json is\n%s\nwant\n%s", name, tc.got, tc.want)
		}
	}
}

// A control mod writes a data-final-fixes.lua only when it has one: the mod
// settings reader does (ADR 0036), the resource counter does not.
func TestAddControlModDataFinalFixes(t *testing.T) {
	for _, m := range []controlMod{modSettingsMod, resourceMod} {
		dir := t.TempDir()
		base, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		mods := filepath.Join(dir, "p", world.ModsDir)
		if err := os.MkdirAll(mods, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mods, world.ModListFile), []byte(`{"mods":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := addControlMod(base, "p", m, []byte("{}")); err != nil {
			t.Fatal(err)
		}
		_ = base.Close()
		b, err := os.ReadFile(filepath.Join(mods, m.dir, "data-final-fixes.lua")) //nolint:gosec // the test's own folder
		switch {
		case m.dataFinalFixes == nil && err == nil:
			t.Errorf("%s: a data-final-fixes.lua was written", m.name)
		case m.dataFinalFixes != nil && (err != nil || string(b) != string(m.dataFinalFixes)):
			t.Errorf("%s: data-final-fixes.lua is not the mod's own: %v", m.name, err)
		}
	}
	if modSettingsMod.dataFinalFixes == nil {
		t.Error("the mod settings reader has no data stage")
	}
}
