package fakegame

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// fakeChart is a real-game picture run: it writes the gameenv.ChartDir folder
// to the work folder as the helper mod does (SPEC 5.6, 14.6), a manifest
// with one planet surface and a chart of every chunk in the square. The
// chart is canned, the same for every planet: a disc of land, half the
// square across, in water, so a test can tell it from the fast preview.
func fakeChart(c dockertest.Container) error {
	root := mountSource(c, gameenv.PreviewMount)
	if root == "" {
		return fmt.Errorf("the fake game's chart container has no folder bound at %s", gameenv.PreviewMount)
	}
	r, err := strconv.Atoi(envValue(c.Spec.Env, gameenv.ChartRadius))
	if err != nil || r < 1 {
		return fmt.Errorf("the fake game's chart container has a bad %s", gameenv.ChartRadius)
	}
	planet := envValue(c.Spec.Env, gameenv.ChartPlanet)
	if planet == "" {
		return fmt.Errorf("the fake game's chart container has no %s", gameenv.ChartPlanet)
	}
	dir := filepath.Join(root, gameenv.ChartDir)
	if err := os.Mkdir(dir, world.DirMode); err != nil {
		return err
	}
	const index = 1
	ms := factorymap.ManifestSurface{
		Index: index, Name: planet, Kind: "planet",
		MinX: -r, MinY: -r, MaxX: r - 1, MaxY: r - 1,
		Chart: factorymap.ChartFile(index), Entities: factorymap.EntitiesFile(index),
	}
	var lines bytes.Buffer
	enc := json.NewEncoder(&lines)
	for y := -r; y < r; y++ {
		for x := -r; x < r; x++ {
			// Chunk (x, y) spans x to x+1, so its centre is x+0.5: doubled, 2x+1.
			dx, dy := 2*x+1, 2*y+1
			water := dx*dx+dy*dy > r*r
			data, err := encodeChunk(chunkPixels(x, y, water))
			if err != nil {
				return err
			}
			if err := enc.Encode(struct {
				X    int    `json:"x"`
				Y    int    `json:"y"`
				Data string `json:"data"`
			}{x, y, data}); err != nil {
				return err
			}
			ms.Chunks++
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ms.Chart), lines.Bytes(), world.FileMode); err != nil { //nolint:gosec // the fake's own test folder
		return err
	}
	v := imageVersion(c.Spec.Image)
	return writeJSON(filepath.Join(dir, "manifest.json"), factorymap.Manifest{
		Format:      factorymap.ManifestFormat,
		GameVersion: fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]),
		Tick:        221,
		Surfaces:    []factorymap.ManifestSurface{ms},
	})
}
