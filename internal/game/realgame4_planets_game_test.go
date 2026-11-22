//go:build game

package game

// Modded planets against the real game (SPEC 5.6, 7.4, 14.3, 18.12's
// gate), with the five planet mods (maraxsis, Cerys, Moshine, Muluna,
// corrundum, and their dependencies): the mod-settings run lists every
// planet, control and resource of a world; the fast preview draws each of
// the 11 planets within its limits; a resource count and a territory run
// load the world's mods; and a first start is not stopped by map settings
// whose autoplace_settings name entities, tiles or decoratives of a mod the
// world no longer has. The zips are read from the folder
// FOREMAN_TEST_PLANET_MODS names (the 2.1 set) and from
// FOREMAN_TEST_PLANET_MODS_2_0 (the 2.0 set), each with its mod-list.json;
// a test skips when its folder is unset, and nothing is downloaded. Run by
// `make test-game`, never by `make verify`.

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

// planetSetPlanets are the planets a world with the five mods has.
var planetSetPlanets = []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo",
	"maraxsis", "maraxsis-trench", "moshine", "corrundum", "muluna", "cerys"}

// planetModsVar names the folder of the planet-mod set of a game line.
func planetModsVar(tag string) string {
	if tag == "2.0" {
		return "FOREMAN_TEST_PLANET_MODS_2_0"
	}
	return "FOREMAN_TEST_PLANET_MODS"
}

// planetSetWorld makes a world of version with no save whose mods folder
// holds every zip of the line's set and the set's mod list, less the
// feasibility test's export mod, which is a folder.
func (e *gameEnv) planetSetWorld(t *testing.T, tag, version, label string) string {
	t.Helper()
	dir := os.Getenv(planetModsVar(tag))
	if dir == "" {
		t.Skip(planetModsVar(tag) + " is not set")
	}
	id := e.generate(t, "planets "+label+" "+tag, world.NewWorld{GameVersion: version, SpaceAge: true})
	copyPlanetSet(t, dir, e.modsPath(id))
	return id
}

// copyPlanetSet copies the set's zips and mod list from dir into mods.
func copyPlanetSet(t *testing.T, dir, mods string) {
	t.Helper()
	if err := os.MkdirAll(mods, 0o750); err != nil {
		t.Fatal(err)
	}
	zips, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil || len(zips) < 5 {
		t.Fatalf("want the planet mods' zips in %s, found %v (%v)", dir, zips, err)
	}
	for _, z := range zips {
		b, err := os.ReadFile(z) //nolint:gosec // the tester names the folder
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(mods, filepath.Base(z)), b, 0o640); err != nil { //nolint:gosec // the test's own world
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, world.ModListFile)) //nolint:gosec // the tester names the folder
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Mods []map[string]any `json:"mods"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	list.Mods = slices.DeleteFunc(list.Mods, func(m map[string]any) bool { return m["name"] == "autoplace-export" })
	if b, err = json.Marshal(list); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mods, world.ModListFile), b, 0o640); err != nil { //nolint:gosec // the test's own world
		t.Fatal(err)
	}
}

func planetNames(c []string) []string { return slices.Sorted(slices.Values(c)) }

func TestRealGamePlanetCatalogue(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	c, _ := e.controller(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			// The test mod makes the world's mods more than the shipped
			// ones, so the run happens on every line.
			t.Run("without planet mods", func(t *testing.T) {
				id := e.msWorld(t, g.tag, g.version, "planets none")
				// Every built-in mod on, so 2.x has Space Age's planets.
				var list []string
				for _, m := range append(world.BuiltinMods(g.version), fmtestMod) {
					list = append(list, `{"name":"`+m+`","enabled":true}`)
				}
				if err := os.WriteFile(e.modsPath(id, world.ModListFile), []byte(`{"mods":[`+strings.Join(list, ",")+`]}`), 0o640); err != nil { //nolint:gosec // the test's own world
					t.Fatal(err)
				}
				begin := time.Now()
				cat, err := c.WorldCatalogue(context.Background(), id, "en")
				t.Logf("TIMING planet run without planet mods: %s", time.Since(begin).Round(100*time.Millisecond))
				if err != nil {
					t.Fatal(err)
				}
				var names []string
				for _, p := range cat.Planets {
					names = append(names, p.Name)
				}
				t.Logf("%d planets %v, %d controls, %d resources", len(names), names, len(cat.Controls), len(cat.Resources))
				if g.tag == "1.1" {
					if !cat.NoPlanets || len(names) != 1 || names[0] != "nauvis" || len(cat.Controls) != 8 {
						t.Errorf("1.1: NoPlanets %t, planets %v, %d controls; want one surface and 8 controls", cat.NoPlanets, names, len(cat.Controls))
					}
					return
				}
				if len(names) != 5 || len(cat.Controls) != 28 {
					t.Errorf("planets %v, %d controls; want 5 planets and 28 controls", names, len(cat.Controls))
				}
			})
			if g.tag == "1.1" {
				return // the five mods are made for 2.x only
			}
			t.Run("with the five planet mods", func(t *testing.T) {
				id := e.planetSetWorld(t, g.tag, g.version, "catalogue")
				begin := time.Now()
				cat, err := c.WorldCatalogue(context.Background(), id, "en")
				t.Logf("TIMING planet run with the five mods: %s", time.Since(begin).Round(100*time.Millisecond))
				if err != nil {
					t.Fatal(err)
				}
				var names []string
				for _, p := range cat.Planets {
					names = append(names, p.Name)
				}
				t.Logf("%d planets %v, %d controls, %d resources, %d dropped", len(names), names, len(cat.Controls), len(cat.Resources), cat.Dropped)
				if !slices.Equal(planetNames(names), planetNames(planetSetPlanets)) {
					t.Errorf("planets %v, want %v", names, planetSetPlanets)
				}
				if len(cat.Controls) != 46 || len(cat.Resources) != 28 {
					t.Errorf("%d controls and %d resources, want 46 and 28", len(cat.Controls), len(cat.Resources))
				}
				if !cat.SpaceAge {
					t.Error("the catalogue is not laid out for Space Age")
				}
				// A planet's own default reaches the catalogue (SPEC 5.6).
				for _, p := range cat.Planets {
					if r := p.Defaults["steam_geyser"].Richness; p.Name == "moshine" && (r == nil || *r != 150) {
						t.Errorf("moshine's steam_geyser default %+v, want richness 150", p.Defaults["steam_geyser"])
					}
				}
			})
		})
	}
}

func TestRealGamePlanetPreviews(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	c, _ := e.controller(t)
	for _, g := range gameTags[1:] {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			id := e.planetSetWorld(t, g.tag, g.version, "previews")
			for _, planet := range planetSetPlanets {
				t.Run(planet, func(t *testing.T) {
					req := PreviewRequest{
						GameVersion: g.version, SpaceAge: true, Seed: 123456, Size: PreviewSizeMin, World: id,
						Planets: []string{planet}, KnownPlanets: planetSetPlanets,
					}
					stop := e.peakMemory()
					begin := time.Now()
					pngs, err := c.Preview(context.Background(), req)
					took := time.Since(begin)
					peak := stop()
					if err != nil {
						t.Fatal(err)
					}
					if len(pngs) != 1 || pngs[0].Planet != planet || len(pngs[0].Data) == 0 {
						t.Fatalf("%d pictures", len(pngs))
					}
					px := chartPixels(t, pngs[0].Data)
					t.Logf("TIMING preview of %s: %s, peak %d MiB, %d colours", planet, took.Round(100*time.Millisecond), peak>>20, px.colours)
					if took > PreviewLimit {
						t.Errorf("took %s, more than %s", took, PreviewLimit)
					}
					if peak > 1<<30 {
						t.Errorf("peak %d MiB, more than 1 GiB", peak>>20)
					}
					// The trench's middle is one colour at this size; every
					// other planet has terrain there.
					if planet != "maraxsis-trench" && px.colours < 2 {
						t.Errorf("the picture of %s is one colour", planet)
					}
					e.previewLeftovers(t, "after the preview of "+planet)
				})
			}
		})
	}
}

func TestRealGamePlanetCountsWithWorldMods(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && only != "2.1" {
		t.Skip("runs on 2.1 only")
	}
	const version = "2.1.24"
	e := newGameEnv(t)
	e.requireImage(t, ImageRef(version))
	c, _ := e.controller(t)
	id := e.planetSetWorld(t, "2.1", version, "counts")
	before := listFolder(t, e.modsPath(id))
	req := PreviewRequest{
		GameVersion: version, SpaceAge: true, Seed: 123456, Size: PreviewSizeMin, World: id,
		Planets: []string{"vulcanus"}, KnownPlanets: planetSetPlanets,
	}
	for _, p := range []struct{ planet, ore string }{{"moshine", "multi-ore"}, {"nauvis", "iron-ore"}} {
		t.Run("resource count on "+p.planet, func(t *testing.T) {
			begin := time.Now()
			res, err := c.Resources(context.Background(), req, p.planet)
			if err != nil {
				t.Fatal(err)
			}
			names := slices.Sorted(maps.Keys(res.Resources))
			t.Logf("TIMING resources on %s: %s, %d patches, resources %v", p.planet, time.Since(begin).Round(100*time.Millisecond), len(res.Patches), names)
			if len(res.Patches) == 0 || !slices.Contains(names, p.ore) {
				t.Errorf("%d patches of %v, want some with %s", len(res.Patches), names, p.ore)
			}
			e.previewLeftovers(t, "after the resource count")
		})
	}
	t.Run("territories on vulcanus", func(t *testing.T) {
		begin := time.Now()
		res, err := c.Territories(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("TIMING territories with the five mods: %s, %d territories", time.Since(begin).Round(100*time.Millisecond), len(res.Territories))
		if len(res.Territories) == 0 {
			t.Error("no territories on vulcanus")
		}
		e.previewLeftovers(t, "after the territory run")
	})
	if after := listFolder(t, e.modsPath(id)); !slices.Equal(after, before) {
		t.Errorf("the world's mods folder changed: %v, was %v", after, before)
	}
}

// SPEC 7.4 refuses only stale autoplace controls, because --create stops on
// an unknown control. This pins the other half: names in autoplace_settings
// from a mod the world no longer has are ignored by --create, so the first
// start makes the map and the check need not refuse them.
func TestRealGameRemovedModAutoplaceSettings(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	c, _ := e.controller(t)
	for _, g := range gameTags[1:] {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			// corrundum's ore and tile and Moshine's decorative, none loaded.
			mapGen := `{"autoplace_settings":{` +
				`"entity":{"treat_missing_as_default":true,"settings":{"chalcopyrite-ore":{"frequency":2,"size":2,"richness":2}}},` +
				`"tile":{"treat_missing_as_default":true,"settings":{"corrundum-dunes":{"frequency":2,"size":2,"richness":2}}},` +
				`"decorative":{"treat_missing_as_default":true,"settings":{"moshine-crater-small":{"frequency":2,"size":2,"richness":2}}}}}`
			id := e.generate(t, "autoplace "+g.tag, world.NewWorld{GameVersion: g.version, SpaceAge: true, MapGenSettings: []byte(mapGen)})
			check, err := c.precheck(context.Background(), id, StartOptions{})
			if err != nil {
				t.Fatalf("the pre-start check refused: %v", err)
			}
			for _, p := range check.Problems {
				if p.Kind == world.ProblemStaleControls {
					t.Errorf("a stale-controls problem for autoplace_settings: %s", p.Text)
				}
			}
			e.startAndCheck(t, c, id)
		})
	}
}
