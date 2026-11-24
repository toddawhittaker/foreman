package game

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	modsettingsmod "github.com/toddawhittaker/foreman/internal/game/modsettings"
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
	script := c.Spec.Cmd[len(c.Spec.Cmd)-1]
	r.finish(c, defsJSON, defaultsDat(t))
	if res := r.result(ch); res.err != nil {
		t.Fatal(res.err)
	}
	lines := strings.Split(script, "\n")
	n := len(lines)
	if n < 4 || lines[n-4] != optionalCopy(gameenv.PlanetsFile) || lines[n-3] != optionalCopy(gameenv.IconsFile) ||
		lines[n-2] != optionalCopy(gameenv.StarMapFile) || !strings.HasPrefix(lines[n-1], "cp ") ||
		!strings.Contains(lines[n-1], gameenv.ModSettingsDefsFile) {
		t.Fatalf("the script does not copy the planet, icon and star map files just before the definitions:\n%s", script)
	}

	// The last four lines in a real shell, with the container's folders
	// replaced: the definitions copy decides the exit status, and a missing
	// planet, icon or star map file does not.
	tail := strings.Join(lines[n-4:], "\n")
	shell := func(t *testing.T, files ...string) error {
		t.Helper()
		write, out := t.TempDir(), t.TempDir()
		if err := os.MkdirAll(filepath.Join(write, "script-output"), 0o750); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(write, "script-output", f), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		cmd := strings.ReplaceAll(strings.ReplaceAll(tail, previewWrite, write), gameenv.PreviewMount+"/", out+"/")
		return exec.Command("/bin/sh", "-c", cmd).Run() //nolint:gosec // the test's own script
	}
	if err := shell(t, gameenv.ModSettingsDefsFile); err != nil {
		t.Errorf("no planet file failed the run: %v", err)
	}
	if err := shell(t, gameenv.PlanetsFile, gameenv.IconsFile, gameenv.StarMapFile); err == nil {
		t.Error("a missing definitions file did not fail the run")
	}
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
			if !errors.Is(got.PlanetsErr, ErrPlanetsRefused) {
				t.Fatalf("planets error %v, want ErrPlanetsRefused", got.PlanetsErr)
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

// wcResult is what WorldCatalogue returned.
type wcResult struct {
	cat mapgen.Catalogue
	err error
}

func (r *modSettingsRig) askCatalogue(lang string) <-chan wcResult {
	ch := make(chan wcResult, 1)
	go func() {
		cat, err := r.c.WorldCatalogue(context.Background(), r.id, lang)
		ch <- wcResult{cat, err}
	}()
	return ch
}

func (r *modSettingsRig) catalogueResult(ch <-chan wcResult) wcResult {
	r.t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(10 * time.Second):
		r.t.Fatal("WorldCatalogue did not return")
		return wcResult{}
	}
}

// SPEC 5.6: a world whose enabled mods are all built in gets the built-in
// catalogue without running the game.
func TestWorldCatalogueBuiltinModsOnly(t *testing.T) {
	r := newModSettingsRig(t)
	res := r.catalogueResult(r.askCatalogue("en"))
	if res.err != nil {
		t.Errorf("reason %v, want none", res.err)
	}
	if !reflect.DeepEqual(res.cat, mapgen.Builtin("2.1", false)) {
		t.Error("not the built-in catalogue")
	}
	if n := r.creates(); n != 0 {
		t.Errorf("%d containers made, want none", n)
	}
}

// SPEC 5.6: Space Age comes from the enabled mods, not the world's flag, so
// a world whose mod list turns on every built-in mod of 2.1 is built-in only
// and has the Space Age planets.
func TestWorldCatalogueSpaceAgeFromTheMods(t *testing.T) {
	r := newModSettingsRig(t)
	r.writeFile(r.id, filepath.Join(world.ModsDir, world.ModListFile), []byte(`{"mods":[{"name":"base","enabled":true},
	  {"name":"space-age","enabled":true},{"name":"quality","enabled":true},{"name":"elevated-rails","enabled":true},{"name":"recycler","enabled":true}]}`))
	res := r.catalogueResult(r.askCatalogue("en"))
	if res.err != nil {
		t.Errorf("reason %v, want none", res.err)
	}
	if !reflect.DeepEqual(res.cat, mapgen.Builtin("2.1", true)) {
		t.Errorf("planets %v, want the Space Age ones", res.cat.PlanetNames())
	}
	if n := r.creates(); n != 0 {
		t.Errorf("%d containers made, want none", n)
	}
}

// SPEC 5.6: recycler ships with 2.1 only, so a 2.0 world that enables it is
// not built-in only and is not given the built-in catalogue without a reason.
func TestWorldCatalogueRecyclerOn20IsAMod(t *testing.T) {
	r := newModSettingsRig(t)
	id, err := r.worlds.Create(context.Background(), "Old", "2.0.77", false)
	if err != nil {
		t.Fatal(err)
	}
	r.id = id
	r.writeFile(id, filepath.Join(world.ModsDir, world.ModListFile), []byte(`{"mods":[{"name":"base","enabled":true},{"name":"space-age","enabled":false},{"name":"recycler","enabled":true}]}`))
	ch := r.askCatalogue("en")
	var res wcResult
	select {
	case res = <-ch:
		// The run could not start, so the reason says why.
		if res.err == nil {
			t.Error("the built-in catalogue came back with no reason and no run")
		}
	case <-time.After(5 * time.Second):
		r.game.Daemon().Exit(r.waitRunning().ID, 1, false)
		res = r.catalogueResult(ch)
	}
	if !reflect.DeepEqual(res.cat, mapgen.Builtin("2.0", false)) {
		t.Error("not the built-in catalogue")
	}
}

// SPEC 5.6: a world with a mod gets the run's catalogue in the language.
func TestWorldCatalogueFromTheRun(t *testing.T) {
	r := newModSettingsRig(t)
	r.writeFile(r.id, filepath.Join(world.ModsDir, "moon_1.0.0.zip"), []byte("zip"))
	ch := r.askCatalogue("de")
	c := r.waitRunning()
	if err := os.WriteFile(filepath.Join(r.workDir(c), gameenv.PlanetsFile), []byte(planetsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	r.finish(c, defsJSON, defaultsDat(t))
	res := r.catalogueResult(ch)
	if res.err != nil {
		t.Fatal(res.err)
	}
	if names := res.cat.PlanetNames(); !reflect.DeepEqual(names, []string{"nauvis", "moon"}) {
		t.Errorf("planets %v", names)
	}
}

// SPEC 5.6 (fail safe): a failed run gives the built-in catalogue and the
// reason, never a failure.
func TestWorldCatalogueFailedRun(t *testing.T) {
	r := newModSettingsRig(t)
	r.writeFile(r.id, filepath.Join(world.ModsDir, "moon_1.0.0.zip"), []byte("zip"))
	ch := r.askCatalogue("en")
	r.game.Daemon().Exit(r.waitRunning().ID, 1, false)
	res := r.catalogueResult(ch)
	if !errors.Is(res.err, ErrModSettingsBadOutput) {
		t.Errorf("reason %v", res.err)
	}
	if !reflect.DeepEqual(res.cat, mapgen.Builtin("2.1", false)) {
		t.Error("not the built-in catalogue")
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

// SPEC 5.6: a world's catalogue, and the built-in fallback, carry Space Age
// from the same source, the world's enabled mods.
func TestWorldCataloguesCarrySpaceAge(t *testing.T) {
	data := []byte(`{"planets":[{"name":"moon","localised_name":["space-location-name.moon"],"autoplace_controls":{"rock":{}}}],
	  "autoplace_controls":[{"name":"rock","category":"resource","localised_name":["autoplace-control-names.rock"]}],
	  "resources":[{"name":"rock","control":"rock","localised_name":["entity-name.rock"]}]}`)
	for _, on := range []bool{true, false} {
		cats, err := worldCatalogues(data, nil, nil, nil, "2.1", on)
		if err != nil || cats["en"].SpaceAge != on || cats["de"].SpaceAge != on {
			t.Errorf("space age %t: %v %t", on, err, cats["en"].SpaceAge)
		}
		cats, err = worldCatalogues(nil, ErrPlanetsMissing, nil, nil, "2.1", on)
		if err == nil || cats["en"].SpaceAge != on {
			t.Errorf("fallback, space age %t: %t", on, cats["en"].SpaceAge)
		}
	}
}

// runWithFiles asks once and ends the container with the given files in its
// work folder, a name with no content making a symbolic link instead.
func (r *modSettingsRig) runWithFiles(files map[string]string) *ModSettingDefs {
	r.t.Helper()
	ch := r.ask(context.Background())
	c := r.waitRunning()
	for name, text := range files {
		p := filepath.Join(r.workDir(c), name)
		var err error
		if text == "" {
			err = os.Symlink(filepath.Join(r.workDir(c), gameenv.ModSettingsDefsFile), p)
		} else {
			err = os.WriteFile(p, []byte(text), 0o600)
		}
		if err != nil {
			r.t.Fatal(err)
		}
	}
	r.finish(c, defsJSON, defaultsDat(r.t))
	res := r.result(ch)
	if res.err != nil {
		r.t.Fatal(res.err)
	}
	return res.defs
}

// Icon and star map files as the control mod writes them on 2.x.
const (
	iconsJSON = `{"mods":{"base":"2.1.24","fake-planet":"1.0.0"},"icons":[
  {"kind":"item","name":"fake-widget","path":"__fake-planet__/graphics/icons/fake-widget.png","size":64},
  {"kind":"starmap","name":"fake-planet","path":"__fake-planet__/graphics/starmap-fake-planet.png","size":64}]}`
	starMapJSON = `{"locations":[
  {"name":"nauvis","localised_name":["space-location-name.nauvis"],"distance":15,"orientation":0.275,"magnitude":1},
  {"name":"vulcanus","localised_name":["space-location-name.vulcanus"],"distance":10,"orientation":0.1,"magnitude":1.5},
  {"name":"space-location-unknown","distance":0,"orientation":0,"hidden":true}],
 "routes":[{"from":"nauvis","to":"vulcanus","length":15000}]}`
)

// SPEC 9.6, 14.6: the run's icon and star map files reach the result,
// decoded, beside the settings and planets.
func TestModSettingDefsIconsAndStarMap(t *testing.T) {
	r := newModSettingsRig(t)
	defs := r.runWithFiles(map[string]string{
		gameenv.PlanetsFile: planetsJSON, gameenv.IconsFile: iconsJSON, gameenv.StarMapFile: starMapJSON,
	})
	if defs.IconsErr != nil || defs.StarMapErr != nil || defs.PlanetsErr != nil {
		t.Fatalf("errors: icons %v, star map %v, planets %v", defs.IconsErr, defs.StarMapErr, defs.PlanetsErr)
	}
	if ref, ok := defs.Icons.Lookup("item", "fake-widget"); !ok || ref.Mod != "fake-planet" || ref.Version != "1.0.0" ||
		ref.Path != "graphics/icons/fake-widget.png" || ref.Size != 64 {
		t.Errorf("fake-widget's icon %+v, %t", ref, ok)
	}
	if defs.Icons.Len() != 2 {
		t.Errorf("%d icons, want 2", defs.Icons.Len())
	}
	if len(defs.StarMap.Locations) != 2 || len(defs.StarMap.Routes) != 1 || defs.StarMap.Routes[0].Length != 15000 {
		t.Errorf("star map %+v", defs.StarMap)
	}
	if v, ok := defs.StarMap.Location("vulcanus"); !ok || v.Distance != 10 || v.Orientation != 0.1 {
		t.Errorf("vulcanus %+v", v)
	}
	if len(defs.Defs) == 0 {
		t.Error("no settings")
	}
}

// SPEC 9.6, 14.6: a missing, refused, oversized or linked icon or star map
// file leaves that field empty with its reason, and never touches the
// settings or the planets.
func TestModSettingDefsIconAndStarMapFailures(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		files                map[string]string
		iconsErr, starMapErr error
	}{
		{"missing", map[string]string{}, ErrRunFileMissing, ErrRunFileMissing},
		{"not JSON", map[string]string{gameenv.IconsFile: "{", gameenv.StarMapFile: "nope"}, ErrRunFileRefused, ErrRunFileRefused},
		{"over the limits", map[string]string{
			gameenv.IconsFile:   strings.Repeat(" ", modsettings.MaxIconIndexBytes+1),
			gameenv.StarMapFile: strings.Repeat(" ", mapgen.MaxStarMapFileBytes+1),
		}, ErrRunFileRefused, ErrRunFileRefused},
		{"links", map[string]string{gameenv.IconsFile: "", gameenv.StarMapFile: ""}, ErrRunFileMissing, ErrRunFileMissing},
		{"one bad one good", map[string]string{gameenv.IconsFile: "[1]", gameenv.StarMapFile: starMapJSON}, ErrRunFileRefused, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newModSettingsRig(t)
			tc.files[gameenv.PlanetsFile] = planetsJSON
			defs := r.runWithFiles(tc.files)
			if !errors.Is(defs.IconsErr, tc.iconsErr) || (tc.iconsErr == nil) != (defs.IconsErr == nil) {
				t.Errorf("icons error %v, want %v", defs.IconsErr, tc.iconsErr)
			}
			if !errors.Is(defs.StarMapErr, tc.starMapErr) || (tc.starMapErr == nil) != (defs.StarMapErr == nil) {
				t.Errorf("star map error %v, want %v", defs.StarMapErr, tc.starMapErr)
			}
			if defs.IconsErr != nil && defs.Icons.Len() != 0 {
				t.Error("icons kept beside an error")
			}
			if defs.StarMapErr != nil && len(defs.StarMap.Locations) != 0 {
				t.Error("star map kept beside an error")
			}
			if defs.PlanetsErr != nil || len(defs.Catalogue("en").Planets) != 2 || len(defs.Defs) == 0 {
				t.Errorf("the settings or planets were touched: planets error %v, %d planets, %d settings",
					defs.PlanetsErr, len(defs.Catalogue("en").Planets), len(defs.Defs))
			}
		})
	}
}

// ADR 0036: the reader takes an optional dependency on every mod the game
// loads from the work folder: each the list enables and each zip or folder
// it does not turn off. Base, the reader itself and names the game might
// parse another way are left out.
func TestWorkFolderMods(t *testing.T) {
	dir := t.TempDir()
	mods := filepath.Join(dir, "w", world.ModsDir)
	if err := os.MkdirAll(filepath.Join(mods, "folder-mod_2.0.1"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mods, "bare_folder"), 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"fake-planet_1.0.0.zip", "off_1.0.0.zip", "bad name_1.0.0.zip", "notes.txt", "listed_1.0.zip"} {
		writeTestFile(t, filepath.Join(mods, f), "x")
	}
	writeTestFile(t, filepath.Join(mods, world.ModListFile), `{"mods":[{"name":"base","enabled":true},`+
		`{"name":"space-age","enabled":true},{"name":"off","enabled":false},{"name":"listed","enabled":true},`+
		`{"name":"`+modsettingsmod.ModName+`","enabled":true}]}`)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	got, err := workFolderMods(root, "w")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"bare_folder", "fake-planet", "folder-mod", "listed", "space-age"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mods %v, want %v", got, want)
	}
}
