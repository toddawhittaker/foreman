//go:build game

package game

// The resource count with a heavy mod set at the largest preview size (SPEC
// 5.6): every planet named runs within the limits of a run that loads a
// world's mods. FOREMAN_TEST_HEAVY_MODS names a mods folder, with its
// mod-list.json, that is copied into a test world; the test skips when it is
// unset, and nothing is downloaded. FOREMAN_TEST_HEAVY_PLANETS lists the
// planets to count (default corrundum) FOREMAN_TEST_HEAVY_SEED the seed, and
// FOREMAN_TEST_HEAVY_MAPGEN a map-gen-settings.json to use. FOREMAN_TEST_HEAVY_MEASURE_MIB lifts
// the memory limit to that many MiB and the time limit to ten minutes, to
// measure the peak instead of testing the limits. Run by hand, never by
// `make verify`.

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGameResourcesWithHeavyMods(t *testing.T) {
	src := os.Getenv("FOREMAN_TEST_HEAVY_MODS")
	if src == "" {
		t.Skip("FOREMAN_TEST_HEAVY_MODS is not set")
	}
	planets := strings.Fields(cmp.Or(os.Getenv("FOREMAN_TEST_HEAVY_PLANETS"), "corrundum"))
	const version = "2.1.24"
	e := newGameEnv(t)
	e.requireImage(t, ImageRef(version))
	c, _ := e.controller(t)
	measuring := false
	if mib, err := strconv.ParseInt(os.Getenv("FOREMAN_TEST_HEAVY_MEASURE_MIB"), 10, 64); err == nil && mib > 0 {
		measuring = true
		c.previewWorldMemory = mib << 20
		c.previewWorldLimit = 10 * time.Minute
	}

	id, err := e.worlds.Create(context.Background(), "heavymods "+e.run, version, true)
	if err != nil {
		t.Fatal(err)
	}
	copyFolderMod(t, src, filepath.Join(e.worlds.Dirs().Worlds, id, world.ModsDir))

	known := append([]string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo"}, planets...)
	req := PreviewRequest{
		GameVersion: version, SpaceAge: true, Seed: heavySeed(t), Size: PreviewSizeMax, World: id,
		Planets: []string{planets[0]}, KnownPlanets: known,
	}
	if f := os.Getenv("FOREMAN_TEST_HEAVY_MAPGEN"); f != "" {
		if req.MapGenSettings, err = os.ReadFile(f); err != nil { //nolint:gosec // the tester names the file
			t.Fatal(err)
		}
	}
	for _, planet := range planets {
		t.Run(planet, func(t *testing.T) {
			stop := e.peakMemory()
			begin := time.Now()
			res, err := c.Resources(context.Background(), req, planet)
			wall, peak := time.Since(begin), stop()
			t.Logf("MEASURE %s at %d with heavy mods: wall %s, peak %d MiB, %d patches, error %v",
				planet, req.size(), wall.Round(100*time.Millisecond), peak>>20, len(res.Patches), err)
			if err != nil {
				t.Fatal(err)
			}
			if !measuring && (wall > PreviewWorldLimit || peak > previewWorldMemory) {
				t.Errorf("over the limits of a run with mods: %s, %d MiB", wall.Round(100*time.Millisecond), peak>>20)
			}
			e.previewLeftovers(t, "after the resource count")
		})
	}
}

// heavySeed is FOREMAN_TEST_HEAVY_SEED, or 123456.
func heavySeed(t *testing.T) uint32 {
	s := os.Getenv("FOREMAN_TEST_HEAVY_SEED")
	if s == "" {
		return 123456
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	return uint32(n)
}
