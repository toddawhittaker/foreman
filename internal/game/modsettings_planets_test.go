package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

// planetsJSON is a planet file as the control mod writes it on 2.x, with one
// modded planet whose names come from the mod's locale file.
const planetsJSON = `{
  "planets": [
    {"name": "nauvis", "localised_name": ["space-location-name.nauvis"],
     "autoplace_controls": {"iron-ore": {"frequency": 1, "size": 1, "richness": 1}}, "resources": ["iron-ore"]},
    {"name": "moon", "localised_name": ["space-location-name.moon"],
     "autoplace_controls": {"moon-rock": {"frequency": 1, "size": 1, "richness": 150}}, "resources": ["moon-rock"]}
  ],
  "autoplace_controls": [
    {"name": "iron-ore", "category": "resource", "richness": true, "can_be_disabled": true, "order": "b",
     "localised_name": ["autoplace-control-names.iron-ore"]},
    {"name": "moon-rock", "category": "resource", "richness": true, "can_be_disabled": true, "order": "c",
     "localised_name": ["autoplace-control-names.moon-rock"]}
  ],
  "resources": {}
}`

// runWithPlanets asks once and ends the container with planets as its planet
// file.
func (r *modSettingsRig) runWithPlanets(planets string) *ModSettingDefs {
	r.t.Helper()
	ch := r.ask(context.Background())
	c := r.waitRunning()
	if err := os.WriteFile(filepath.Join(r.workDir(c), gameenv.PlanetsFile), []byte(planets), 0o600); err != nil {
		r.t.Fatal(err)
	}
	r.finish(c, defsJSON, defaultsDat(r.t))
	res := r.result(ch)
	if res.err != nil {
		r.t.Fatal(res.err)
	}
	return res.defs
}

// SPEC 5.6: the one mod-settings run also copies the planet file out, and a
// missing one does not fail the run.
func TestModSettingDefsScriptCopiesThePlanetFile(t *testing.T) {
	r := newModSettingsRig(t)
	ch := r.ask(context.Background())
	c := r.waitRunning()
	defer r.finish(c, defsJSON, defaultsDat(t))
	script := strings.Join(c.Spec.Cmd, " ")
	want := `cp /tmp/factorio/script-output/planets.json /out/planets.json 2>/dev/null || :`
	if !strings.HasSuffix(script, want) {
		t.Errorf("the script does not end by copying the planet file:\n%s", script)
	}
	_ = ch
}

// SPEC 5.6, 9.6: the world's planets come with the settings, labelled from
// the mods' locale files in each language with English behind, rich text
// removed.
func TestModSettingDefsCatalogue(t *testing.T) {
	r := newModSettingsRig(t)
	put := func(rel, text string) {
		p := filepath.Join(r.worldDir(r.id), world.ModsDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put("moon/info.json", `{"name":"moon","version":"1.0.0","title":"x","author":"x","factorio_version":"2.1"}`)
	put("moon/locale/en/a.cfg", "[space-location-name]\nmoon=The Moon\n[autoplace-control-names]\nmoon-rock=[item=stone] Moon rock\n")
	put("moon/locale/de/a.cfg", "[space-location-name]\nmoon=Der Mond\n")
	r.writeFile(r.id, filepath.Join(world.ModsDir, world.ModListFile),
		[]byte(`{"mods":[{"name":"base","enabled":true},{"name":"moon","enabled":true}]}`))

	got := r.runWithPlanets(planetsJSON)
	if got.PlanetsErr != nil {
		t.Fatalf("planets error %v", got.PlanetsErr)
	}
	if len(got.Catalogues) != len(modsettings.FactorioLocales) {
		t.Errorf("%d catalogues, want one for each of Foreman's languages", len(got.Catalogues))
	}
	en := got.Catalogue("en")
	if names := en.PlanetNames(); !reflect.DeepEqual(names, []string{"nauvis", "moon"}) {
		t.Errorf("planets %v", names)
	}
	if l := en.Planets[1].Label; l != "The Moon" {
		t.Errorf("English planet label %q", l)
	}
	if l := en.Controls["moon-rock"].Label; l != "Moon rock" {
		t.Errorf("control label %q, want rich text removed", l)
	}
	de := got.Catalogue("de")
	if l := de.Planets[1].Label; l != "Der Mond" {
		t.Errorf("German planet label %q", l)
	}
	if l := de.Controls["moon-rock"].Label; l != "Moon rock" {
		t.Errorf("German control label %q, want English behind", l)
	}
	if l := en.Planets[0].Label; l != "nauvis" {
		t.Errorf("a name with no locale text is its own label, got %q", l)
	}
	if c := got.Catalogue("xx"); !reflect.DeepEqual(c, en) {
		t.Error("an unknown language does not fall back to English")
	}
}

// SPEC 5.6, 14.6: a missing or refused planet file gives the built-in
// planets of the world's line and the reason, never a failed run.
func TestModSettingDefsCatalogueFallsBack(t *testing.T) {
	builtin := mapgen.Builtin("2.1", false)
	t.Run("missing", func(t *testing.T) {
		r := newModSettingsRig(t)
		got := r.run()
		if !errors.Is(got.PlanetsErr, ErrPlanetsMissing) {
			t.Errorf("planets error %v, want ErrPlanetsMissing", got.PlanetsErr)
		}
		if !reflect.DeepEqual(got.Catalogue("de"), builtin) {
			t.Error("the catalogue is not the built-in one")
		}
		if len(got.Defs) != 2 {
			t.Error("the settings were lost")
		}
	})
	for name, file := range map[string]string{
		"not json":   `{"planets": [`,
		"wrong type": `{"planets": "nauvis"}`,
		"too large":  `{"planets": [], "pad": "` + strings.Repeat("x", mapgen.MaxPlanetFileBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newModSettingsRig(t)
			got := r.runWithPlanets(file)
			if got.PlanetsErr == nil {
				t.Fatal("no planets error")
			}
			if !reflect.DeepEqual(got.Catalogue("en"), builtin) {
				t.Error("the catalogue is not the built-in one")
			}
			if strings.Contains(got.PlanetsErr.Error(), "nauvis") || strings.Contains(got.PlanetsErr.Error(), "xxx") {
				t.Errorf("the reason %q repeats the file", got.PlanetsErr)
			}
		})
	}
}

// SPEC 14.6: the planet file must be a regular file; a link is refused.
func TestModSettingDefsPlanetFileLink(t *testing.T) {
	r := newModSettingsRig(t)
	ch := r.ask(context.Background())
	c := r.waitRunning()
	secret := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(secret, []byte(planetsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(r.workDir(c), gameenv.PlanetsFile)); err != nil {
		t.Fatal(err)
	}
	r.finish(c, defsJSON, defaultsDat(t))
	res := r.result(ch)
	if res.err != nil {
		t.Fatal(res.err)
	}
	if !errors.Is(res.defs.PlanetsErr, ErrPlanetsMissing) {
		t.Errorf("planets error %v, want the link refused", res.defs.PlanetsErr)
	}
}
