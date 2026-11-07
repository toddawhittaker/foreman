//go:build game

package game

// The territory run (SPEC 5.6, 14.3) against the real game images: seed
// 123456 at 1280 has 18 territories in the picture, 12 small and 6 medium; a
// preset and a settings file reach the game and change the map; and the
// largest size territories are offered for fits the limits. Run by `make
// test-game`, never by `make verify`.

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/territory"
)

func TestRealGameD2Territories(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if g.tag == "1.1" || only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runTerritories(t, g.tag, g.version)
		})
	}
}

// territoryCounts is what the page lists under the picture.
func territoryCounts(res territory.Result, size int) map[string]int {
	out := map[string]int{}
	for _, c := range territory.Geometry(res, size).Counts {
		out[c.Size] = c.N
	}
	return out
}

func (e *gameEnv) runTerritories(t *testing.T, line, version string) {
	c, _ := e.controller(t)
	run := func(t *testing.T, req PreviewRequest) territory.Result {
		t.Helper()
		req.GameVersion, req.SpaceAge = version, true
		begin := time.Now()
		res, err := c.Territories(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("TIMING %s size %d: %s, %d territories", version, req.size(), time.Since(begin).Round(100*time.Millisecond), len(res.Territories))
		e.previewLeftovers(t, "after the territory run")
		return res
	}

	var base territory.Result
	t.Run("seed 123456 at 1280", func(t *testing.T) {
		base = run(t, PreviewRequest{Seed: 123456})
		want := map[string]int{"small": 12, "medium": 6}
		if got := territoryCounts(base, 1280); !reflect.DeepEqual(got, want) {
			t.Errorf("counts %v, want %v", got, want)
		}
	})
	// The same settings give the same map.
	t.Run("repeat", func(t *testing.T) {
		if again := run(t, PreviewRequest{Seed: 123456}); !reflect.DeepEqual(again, base) {
			t.Error("the same request gave other territories")
		}
	})
	t.Run("another seed", func(t *testing.T) {
		if other := run(t, PreviewRequest{Seed: 1}); reflect.DeepEqual(other, base) {
			t.Error("another seed gave the same territories")
		}
	})
	// A map 640 tiles wide holds fewer territories. Terrain controls and
	// presets leave the territories where they are, so the map width is what
	// shows the settings file reaches the game.
	t.Run("a settings file", func(t *testing.T) {
		req := PreviewRequest{Seed: 123456}
		req.MapGenSettings = spaceAgeMapGen(t, line, map[string]any{"width": 640, "height": 640})
		other := run(t, req)
		if len(other.Territories) == 0 || len(other.Territories) >= len(base.Territories) {
			t.Errorf("%d territories on a 640 tile map, want fewer than %d and some", len(other.Territories), len(base.Territories))
		}
	})
	// A preset the game knows is accepted; the run would fail on a bad one.
	t.Run("a preset", func(t *testing.T) {
		run(t, PreviewRequest{Seed: 123456, Preset: "death-world"})
	})
	t.Run("largest size", func(t *testing.T) {
		res := run(t, PreviewRequest{Seed: 123456, Size: territory.MaxSize})
		if n := len(res.Territories); n != 83 {
			t.Errorf("%d territories at %d, want 83", n, territory.MaxSize)
		}
	})
}
