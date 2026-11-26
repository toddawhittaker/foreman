//go:build game

package game

// The resource count of fluid tiles tied to a resource control (SPEC 5.6,
// "Resource amounts"), on Vesta (skewer_planet_vesta), whose methane and
// hydrogen sulfide are tiles. FOREMAN_FLUID_TILE_MODS names a folder of mod
// zips with Vesta and its dependencies, and the mod-list.json and
// mod-settings.dat found there, copied into a test world as they are; the
// test skips when it is unset, and nothing is downloaded. Mod zips are never
// committed.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGameResourcesFluidTiles(t *testing.T) {
	src := os.Getenv("FOREMAN_FLUID_TILE_MODS")
	if src == "" {
		t.Skip("FOREMAN_FLUID_TILE_MODS is not set")
	}
	const version, planet = "2.1.24", "vesta"
	e := newGameEnv(t)
	e.requireImage(t, ImageRef(version))
	c, _ := e.controller(t)
	id, err := e.worlds.Create(context.Background(), "fluidtiles "+e.run, version, true)
	if err != nil {
		t.Fatal(err)
	}
	copyFolderMod(t, src, filepath.Join(e.worlds.Dirs().Worlds, id, world.ModsDir))
	req := PreviewRequest{
		GameVersion: version, SpaceAge: true, Seed: 123456, Size: PreviewSizeDefault, World: id,
		Planets: []string{planet}, KnownPlanets: []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo", planet},
	}
	res, err := c.Resources(context.Background(), req, planet)
	if err != nil {
		t.Fatal(err)
	}
	spots := map[string]int{}
	for _, p := range res.Patches {
		if !p.Fluid {
			continue
		}
		spots[p.Name]++
		if p.Amount != 0 || p.Entities < 1 {
			t.Errorf("spot %+v has an amount or no tiles", p)
		}
	}
	t.Logf("Vesta at %d: %d patches, spots by fluid %v, fluids %v", req.size(), len(res.Patches), spots, res.Fluids)
	// Vesta writes the fluids under their base names unless its startup
	// setting ske_vesta_gases is on.
	for _, want := range [][2]string{{"methane", "vesta_methane"}, {"hydrogen-sulfide", "vesta_hydrogen_sulfide"}} {
		if spots[want[0]]+spots[want[1]] == 0 {
			t.Errorf("no spot of %s on Vesta: %v", want[0], spots)
		}
	}
	// Only Vesta's two tiles are tied to resource controls alone; the game's
	// own oceans and lava are not (SPEC 5.6).
	for name, f := range res.Fluids {
		if name == "water" || name == "lava" || name == "heavy-oil" || name == "ammoniacal-solution" {
			t.Errorf("the game's own %s counts as spots, tied to %v", name, f.Controls)
		}
	}
	e.previewLeftovers(t, "after the fluid tile count")
}
