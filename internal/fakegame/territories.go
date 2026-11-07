package fakegame

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// makePreview does what a preview container was started for: pictures, or
// a territory run's JSON.
func makePreview(c dockertest.Container) error {
	if envValue(c.Spec.Env, gameenv.PreviewKind) == gameenv.KindTerritories {
		return writeTerritories(c)
	}
	return drawPreview(c)
}

// writeTerritories is a territory run: it writes gameenv.TerritoryFile to
// the work folder, shaped as the game's script writes it. The territories
// are canned: a small one in the picture's top-left corner, a medium one in
// its top-right corner and a big one in its bottom-left corner, three
// chunks square each, so a test knows what to expect at any radius from 3.
func writeTerritories(c dockertest.Container) error {
	dir := mountSource(c, gameenv.PreviewMount)
	if dir == "" {
		return fmt.Errorf("the fake game's territory container has no folder bound at %s", gameenv.PreviewMount)
	}
	r, err := strconv.Atoi(envValue(c.Spec.Env, gameenv.TerritoryRadius))
	if err != nil || r < 3 {
		return fmt.Errorf("the fake game's territory container has a bad %s", gameenv.TerritoryRadius)
	}
	type territory struct {
		Unit   string   `json:"unit"`
		Chunks [][2]int `json:"chunks"`
	}
	square := func(unit string, x0, y0 int) territory {
		t := territory{Unit: unit}
		for y := y0; y < y0+3; y++ {
			for x := x0; x < x0+3; x++ {
				t.Chunks = append(t.Chunks, [2]int{x, y})
			}
		}
		return t
	}
	b, err := json.Marshal(struct {
		Territories []territory `json:"territories"`
	}{[]territory{
		square("small-demolisher", -r, -r),
		square("medium-demolisher", r-3, -r),
		square("big-demolisher", -r, r-3),
	}})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, gameenv.TerritoryFile), b, world.FileMode) //nolint:gosec // the fake's own test folder
}
