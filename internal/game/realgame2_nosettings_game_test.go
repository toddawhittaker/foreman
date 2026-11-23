//go:build game

package game

// Worlds whose mods define no setting, and controls named by the game's own
// text, against the real 2.1 game (SPEC 5.6, 9.6): the definitions run must
// still list the mods' planets when the game writes no mod-settings.dat, and
// must label a control from the game's locale files in its image. Run by
// `make test-game`, never by `make verify`.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// planetModWorld makes a 2.1 world with the folder mod of internal/game/
// testdata/planets, and a mod list that turns on base and that mod.
func (e *gameEnv) planetModWorld(t *testing.T, version, folder, mod string) string {
	t.Helper()
	id := e.generate(t, "planet mod "+mod, world.NewWorld{GameVersion: version})
	src := filepath.Join("testdata", "planets", folder)
	dst := e.modsPath(id, folder)
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range entries {
		b, err := os.ReadFile(filepath.Join(src, f.Name())) //nolint:gosec // a fixed test fixture
		if err != nil {
			t.Fatal(err)
		}
		if f.Name() == "info.json" {
			// 2.1 refuses a mod made for "2.0".
			var info map[string]any
			if err := json.Unmarshal(b, &info); err != nil {
				t.Fatal(err)
			}
			info["factorio_version"] = version[:3]
			if b, err = json.Marshal(info); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dst, f.Name()), b, 0o600); err != nil { //nolint:gosec // inside the test's own world
			t.Fatal(err)
		}
	}
	list := `{"mods":[{"name":"base","enabled":true},{"name":"` + mod + `","enabled":true}]}`
	if err := os.WriteFile(e.modsPath(id, world.ModListFile), []byte(list), 0o640); err != nil { //nolint:gosec // the test's own world
		t.Fatal(err)
	}
	return id
}

func realGame21(t *testing.T) (*gameEnv, string) {
	t.Helper()
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && only != "2.1" {
		t.Skip("runs on 2.1 only")
	}
	const version = "2.1.24"
	e := newGameEnv(t)
	e.requireImage(t, ImageRef(version))
	return e, version
}

// A mod that defines no setting leaves the game with no mod-settings.dat;
// its planet must still reach the catalogue.
func TestRealGameModSettingsNoSettings(t *testing.T) {
	e, version := realGame21(t)
	c, _ := e.controller(t)
	id := e.planetModWorld(t, version, "fmnoset_1.0.0", "fmnoset")
	defs, err := c.ModSettingDefs(context.Background(), id)
	if err != nil {
		t.Fatalf("the definitions run failed: %v", err)
	}
	if len(defs.Defs) != 0 {
		t.Errorf("%d settings, want none", len(defs.Defs))
	}
	if defs.PlanetsErr != nil {
		t.Fatalf("planets error: %v", defs.PlanetsErr)
	}
	cat := defs.Catalogue("en")
	var names []string
	for _, p := range cat.Planets {
		names = append(names, p.Name)
	}
	t.Logf("planets %v", names)
	if !slices.Contains(names, "fmnoset-planet") {
		t.Errorf("planets %v, want fmnoset-planet among them", names)
	}
}

// A control named by a base-game key gets that key's text from the game's own
// locale files in the world's image.
func TestRealGameBaseLocale(t *testing.T) {
	e, version := realGame21(t)
	c, _ := e.controller(t)
	id := e.planetModWorld(t, version, "fmstonelabel_1.0.0", "fmstonelabel")
	for lang, want := range map[string]string{"en": "Stone", "de": "Stein"} {
		cat, err := c.WorldCatalogue(context.Background(), id, lang)
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		got := cat.Controls["fmstone-stone"].Label
		t.Logf("%s: label %q", lang, got)
		if !strings.EqualFold(got, want) {
			t.Errorf("%s: label %q, want %q", lang, got, want)
		}
	}
}
