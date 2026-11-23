//go:build game

package game

// The star map and icon export against the real game (SPEC 9.6, 14.6, ADR
// 0036), on the oldest and newest supported 2.x images with Space Age: the
// mod settings run writes Space Age's own positions and routes, and its icon
// export holds no path under the game's own mods. With the planet mods in
// the folder FOREMAN_TEST_PLANET_MODS names (FOREMAN_TEST_PLANET_MODS_2_0
// for 2.0), a modded planet's starmap icon opens from its zip and a
// PlanetsLib moon names its parent; the run's time and peak memory are
// logged against its limits. Run by `make test-game`, never by `make verify`.

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGameStarMapAndIcons(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	c, _ := e.controller(t)
	for _, g := range gameTags[1:] {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			t.Run("space age", func(t *testing.T) {
				id := e.msWorld(t, g.tag, g.version, "starmap")
				// Every built-in mod on, so the world has Space Age.
				var list []string
				for _, m := range append(world.BuiltinMods(g.version), fmtestMod) {
					list = append(list, `{"name":"`+m+`","enabled":true}`)
				}
				if err := os.WriteFile(e.modsPath(id, world.ModListFile), []byte(`{"mods":[`+strings.Join(list, ",")+`]}`), 0o640); err != nil { //nolint:gosec // the test's own world
					t.Fatal(err)
				}
				begin := time.Now()
				defs, err := c.ModSettingDefs(context.Background(), id)
				t.Logf("TIMING mod settings run with Space Age: %s", time.Since(begin).Round(100*time.Millisecond))
				if err != nil {
					t.Fatal(err)
				}
				if defs.StarMapErr != nil || defs.IconsErr != nil {
					t.Fatalf("star map error %v, icons error %v", defs.StarMapErr, defs.IconsErr)
				}
				m := defs.StarMap
				t.Logf("%d locations, %d routes, %d dropped", len(m.Locations), len(m.Routes), m.Dropped)
				v, ok := m.Location("vulcanus")
				if !ok || v.Distance != 10 || math.Abs(v.Orientation-0.1) > 1e-6 {
					t.Errorf("vulcanus %+v, %t; want distance 10, orientation 0.1", v, ok)
				}
				found := false
				for _, r := range m.Routes {
					if (r.From == "nauvis" && r.To == "vulcanus") || (r.From == "vulcanus" && r.To == "nauvis") {
						found = true
						if r.Length != 15000 {
							t.Errorf("Nauvis to Vulcanus is %g km, want 15,000", r.Length)
						}
					}
				}
				if !found {
					t.Errorf("no route from Nauvis to Vulcanus in %+v", m.Routes)
				}
				// The game's own mods' icons are left out by the export
				// itself, so the decoder drops nothing, and the test mod has
				// no icons of its own.
				if defs.Icons.Dropped != 0 || defs.Icons.Len() != 0 {
					t.Errorf("icons: %d kept, %d dropped; want none of either", defs.Icons.Len(), defs.Icons.Dropped)
				}
			})
			t.Run("planet mods", func(t *testing.T) {
				id := e.planetSetWorld(t, g.tag, g.version, "starmap")
				stop := e.peakMemory()
				begin := time.Now()
				defs, err := c.ModSettingDefs(context.Background(), id)
				took, peak := time.Since(begin), stop()
				t.Logf("TIMING mod settings run with the planet mods: %s, peak %d MiB (limits %s, %d MiB)",
					took.Round(100*time.Millisecond), peak>>20, ModSettingsLimit, ModSettingsMemory>>20)
				if err != nil {
					t.Fatal(err)
				}
				if defs.StarMapErr != nil || defs.IconsErr != nil {
					t.Fatalf("star map error %v, icons error %v", defs.StarMapErr, defs.IconsErr)
				}
				t.Logf("%d icons (%d dropped), %d locations, %d routes", defs.Icons.Len(), defs.Icons.Dropped,
					len(defs.StarMap.Locations), len(defs.StarMap.Routes))
				opened, moons := 0, 0
				for _, l := range defs.StarMap.Locations {
					if l.Parent != "" {
						moons++
						t.Logf("%s orbits %s", l.Name, l.Parent)
					}
					if !l.HasIcon {
						continue
					}
					ref, ok := defs.Icons.Lookup("starmap", l.Name)
					if !ok {
						t.Errorf("%s has a starmap icon the index does not hold", l.Name)
						continue
					}
					png, err := e.worlds.ModIcon(id, ref.Mod, ref.Version, "__"+ref.Mod+"__/"+ref.Path, ref.Size, 64)
					if err != nil {
						t.Errorf("%s's starmap icon %s from %s %s: %v", l.Name, ref.Path, ref.Mod, ref.Version, err)
						continue
					}
					opened++
					t.Logf("%s's starmap icon: %s %s %s, %d px, %d bytes as PNG", l.Name, ref.Mod, ref.Version, ref.Path, ref.Size, len(png))
				}
				if opened == 0 {
					t.Error("no modded planet's starmap icon opened")
				}
				if moons == 0 {
					t.Error("no PlanetsLib moon names its parent")
				}
				if took > ModSettingsLimit {
					t.Errorf("took %s, more than %s", took, ModSettingsLimit)
				}
			})
		})
	}
}
