//go:build game

package game

// Preview runs with a world's mods (SPEC 5.6, 14.3, ADR 0033) against the
// real 2.1.24 image and one planet mod, corrundum, which needs no other mod:
// the fast preview draws Nauvis and Corrundum, a resource count reads
// Corrundum and a territory run reads Vulcanus, each within its limits, and
// the world's mods folder is left as it was. The zip is read from the folder
// FOREMAN_TEST_PLANET_MODS names; the test skips when it is unset, and
// nothing is downloaded. Run by `make test-game`, never by `make verify`.

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/world"
)

func TestRealGamePreviewWithWorldMods(t *testing.T) {
	if only := os.Getenv("FOREMAN_TEST_GAME_TAG"); only != "" && only != "2.1" {
		t.Skip("runs on 2.1 only")
	}
	dir := os.Getenv("FOREMAN_TEST_PLANET_MODS")
	if dir == "" {
		t.Skip("FOREMAN_TEST_PLANET_MODS is not set")
	}
	zips, err := filepath.Glob(filepath.Join(dir, "corrundum_*.zip"))
	if err != nil || len(zips) != 1 {
		t.Fatalf("want one corrundum zip in %s, found %v (%v)", dir, zips, err)
	}
	const version = "2.1.24"
	e := newGameEnv(t)
	e.requireImage(t, ImageRef(version))
	c, _ := e.controller(t)

	id, err := e.worlds.Create(context.Background(), "previewmods "+e.run, version, true)
	if err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(e.worlds.Dirs().Worlds, id, world.ModsDir)
	if err := os.MkdirAll(mods, 0o750); err != nil {
		t.Fatal(err)
	}
	zip, err := os.ReadFile(zips[0]) //nolint:gosec // the tester names the folder
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mods, filepath.Base(zips[0])), zip, 0o640); err != nil { //nolint:gosec // the test's own world
		t.Fatal(err)
	}
	list := `{"mods":[{"name":"base","enabled":true},{"name":"elevated-rails","enabled":true},{"name":"quality","enabled":true},` +
		`{"name":"recycler","enabled":true},{"name":"space-age","enabled":true},{"name":"corrundum","enabled":true}]}`
	if err := os.WriteFile(filepath.Join(mods, world.ModListFile), []byte(list), 0o640); err != nil { //nolint:gosec // the test's own world
		t.Fatal(err)
	}
	before := listFolder(t, mods)

	req := PreviewRequest{
		GameVersion: version, SpaceAge: true, Seed: 123456, Size: PreviewSizeMin, World: id,
		Planets:      []string{"nauvis", "corrundum"},
		KnownPlanets: []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo", "corrundum"},
	}
	t.Run("without the world, corrundum is not a planet", func(t *testing.T) {
		q := req
		q.World = ""
		if _, err := c.Preview(context.Background(), q); !errors.Is(err, ErrPreviewInvalid) {
			t.Fatalf("error %v, want ErrPreviewInvalid", err)
		}
	})
	t.Run("preview", func(t *testing.T) {
		begin := time.Now()
		pngs, err := c.Preview(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		took := time.Since(begin)
		t.Logf("TIMING preview of %v with corrundum: %s", req.Planets, took.Round(100*time.Millisecond))
		if len(pngs) != 2 || pngs[1].Planet != "corrundum" || len(pngs[1].Data) == 0 {
			t.Fatalf("%d pictures", len(pngs))
		}
		if took > time.Duration(len(req.Planets))*PreviewLimit {
			t.Errorf("took %s, more than the limit", took)
		}
		e.previewLeftovers(t, "after the preview")
	})
	t.Run("resource count on corrundum", func(t *testing.T) {
		begin := time.Now()
		res, err := c.Resources(context.Background(), req, "corrundum")
		if err != nil {
			t.Fatal(err)
		}
		names := slices.Sorted(maps.Keys(res.Resources))
		t.Logf("TIMING resources on corrundum: %s, %d patches, resources %v", time.Since(begin).Round(100*time.Millisecond), len(res.Patches), names)
		// Chalcopyrite is corrundum's own ore, so it shows the mod loaded.
		if len(res.Patches) == 0 || !slices.Contains(names, "chalcopyrite-ore") {
			t.Errorf("%d patches of %v on corrundum, want some with chalcopyrite-ore", len(res.Patches), names)
		}
		e.previewLeftovers(t, "after the resource count")
	})
	t.Run("territories on vulcanus", func(t *testing.T) {
		begin := time.Now()
		res, err := c.Territories(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("TIMING territories with corrundum: %s, %d territories", time.Since(begin).Round(100*time.Millisecond), len(res.Territories))
		if len(res.Territories) == 0 {
			t.Error("no territories on vulcanus")
		}
		e.previewLeftovers(t, "after the territory run")
	})
	if after := listFolder(t, mods); !slices.Equal(after, before) {
		t.Errorf("the world's mods folder changed: %v, was %v", after, before)
	}
}

// listFolder lists the names, sizes and times of the files in dir.
func listFolder(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range entries {
		info, err := en.Info()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s %d %d", en.Name(), info.Size(), info.ModTime().UnixNano()))
	}
	return out
}
