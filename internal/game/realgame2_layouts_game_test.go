//go:build game

package game

// The complete map-settings.json Foreman writes for the 2.1 patches 2.1.9 to
// 2.1.23, where the file's keys change (SPEC 5.6), must be accepted by the
// real game's --create, and a file of the other layout must not be. One exact
// tag stands for each layout, with the first and last patch of a layout too.
// Run by `make test-game`, never by `make verify`.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// layoutTags are the tags tried. The first two are pinned by the Makefile.
// The others sit at the edges of a layout; they are skipped when the host
// does not have them.
var layoutTags = []struct {
	version  string
	optional bool
}{
	{"2.1.9", false}, {"2.1.12", true}, {"2.1.18", true}, {"2.1.23", false},
}

func TestRealGameRG2MapSettingsLayouts(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && only != "2.1" {
		t.Skip("these tests run on the 2.1 line")
	}
	e := newGameEnv(t)
	for _, lt := range layoutTags {
		t.Run("v"+lt.version, func(t *testing.T) {
			ref := ImageRef(lt.version)
			if _, err := e.docker.InspectImage(context.Background(), ref); err != nil && lt.optional {
				t.Skipf("the edge tag %s is not on this host; docker pull it to run this", ref)
			}
			e.requireImage(t, ref)
			e.runLayout(t, lt.version)
		})
	}
	// Versions in the middle layout have no tested image, so Foreman refuses
	// to write a file for them instead of guessing.
	for _, v := range []string{"2.1.13", "2.1.15", "2.1.17"} {
		if _, err := mapgen.MapSettings(v, "default", nil); !errors.Is(err, mapgen.ErrUntestedLayout) {
			t.Errorf("MapSettings(%s): %v; want ErrUntestedLayout", v, err)
		}
	}
}

func (e *gameEnv) runLayout(t *testing.T, version string) {
	c, _ := e.controller(t)
	layout, _ := mapgen.MapSettingsLayout(version)
	// A change, so the full file is written and no preset passed.
	settings, err := mapgen.MapSettings(version, "default", map[string]any{"pollution.diffusion_ratio": 0.05})
	if err != nil {
		t.Fatal(err)
	}
	seed := uint32(31)
	id := e.generateRG2(t, version+" layout", world.NewWorld{GameVersion: version, Seed: &seed, MapSettings: settings})
	e.startInGame(t, c, id)
	gs := readGameSettings(t, c, id, "2.1", settings)
	fileLeaves := flatJSONBytes(t, settings)
	n, missing := compareToFile(t, "map-settings", fileLeaves, gs.mapSettings, []string{"pollution.diffusion_ratio"})
	t.Logf("%s (layout %s): accepted; the game holds %d of the file's %d values, all equal; not reported: %s",
		version, layout, n, len(fileLeaves), strings.Join(missing, ", "))
	// The keys that tell the layouts apart are in the file only when the
	// version has them.
	_, hasDispatch := fileLeaves["enemy_expansion.build_base_unit_dispatch_cooldown"]
	_, hasRestore := fileLeaves["pollution.max_pollution_to_restore_trees"]
	t.Logf("%s: dispatch cooldown in file %t, max_pollution_to_restore_trees in file %t", version, hasDispatch, hasRestore)

	// The game port is free for the next world only once this one has stopped.
	if err := c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// The other layout's file must be refused, so the check above can fail.
	other := "2.1.24"
	if layout == "2.1.18" {
		other = "2.1.9"
	}
	wrong, err := mapgen.MapSettings(other, "default", map[string]any{"pollution.diffusion_ratio": 0.05})
	if err != nil {
		t.Fatal(err)
	}
	id2 := e.generateRG2(t, version+" wrong layout", world.NewWorld{GameVersion: version, Seed: &seed, MapSettings: wrong})
	if err := c.Start(context.Background(), id2, admin, StartOptions{}); err == nil {
		t.Errorf("%s started with the map settings of %s; the game should refuse a file of another layout", version, other)
		_ = c.Stop(context.Background(), id2, admin, StopOptions{})
	} else {
		t.Logf("%s refused the map settings of %s: %v", version, other, err)
	}
}
