package game

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
	"github.com/toddawhittaker/foreman/internal/world"
)

// catalogueRun answers one WorldCatalogue run and returns the
// mod-settings.dat the run's folder held before the game wrote its own, or
// nil.
func (r *modSettingsRig) catalogueRun() []byte {
	r.t.Helper()
	ch := r.askCatalogue("en")
	c := r.waitRunning()
	seen, err := os.ReadFile(filepath.Join(r.workDir(c), world.ModsDir, world.ModSettingsFile)) //nolint:gosec // the test's own folder
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.workDir(c), gameenv.PlanetsFile), []byte(planetsJSON), 0o600); err != nil {
		r.t.Fatal(err)
	}
	r.finish(c, defsJSON, defaultsDat(r.t))
	if res := r.catalogueResult(ch); res.err != nil {
		r.t.Fatal(res.err)
	}
	eventually(r.t, func() bool { return len(r.previewContainers()) == 0 }, "the container to be removed")
	return seen
}

// SPEC 5.6, 9.6: the planets follow the world's mod-settings.dat, so their
// run reads the file and runs again when it changes, while the definitions
// keep the defaults and their own run.
func TestWorldCatalogueReadsTheWorldsSettings(t *testing.T) {
	r := newModSettingsRig(t)
	r.writeFile(r.id, filepath.Join(world.ModsDir, "moon_1.0.0.zip"), []byte("zip"))
	r.writeFile(r.id, filepath.Join(world.ModsDir, world.ModSettingsFile), []byte("first"))
	if seen := r.catalogueRun(); string(seen) != "first" {
		t.Fatalf("the planets run saw %q, want the world's file", seen)
	}
	// The world's file is a copy in the folder, never written through.
	if b, _ := os.ReadFile(filepath.Join(r.worldDir(r.id), world.ModsDir, world.ModSettingsFile)); string(b) != "first" { //nolint:gosec // the test's own folder
		t.Fatalf("the world's file became %q", b)
	}
	r.catalogueResult(r.askCatalogue("en"))
	if r.creates() != 1 {
		t.Fatalf("%d runs, want the planets cached", r.creates())
	}
	ch := r.ask(context.Background())
	c := r.waitRunning()
	if _, err := os.Stat(filepath.Join(r.workDir(c), world.ModsDir, world.ModSettingsFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the definitions run saw the world's settings: %v", err)
	}
	r.finish(c, defsJSON, defaultsDat(t))
	if res := r.result(ch); res.err != nil {
		t.Fatal(res.err)
	}
	eventually(t, func() bool { return len(r.previewContainers()) == 0 }, "the container to be removed")

	r.writeFile(r.id, filepath.Join(world.ModsDir, world.ModSettingsFile), []byte("second"))
	if seen := r.catalogueRun(); string(seen) != "second" {
		t.Fatalf("after a save the planets run saw %q", seen)
	}
	if _, err := r.c.ModSettingDefs(context.Background(), r.id); err != nil || r.creates() != 3 {
		t.Errorf("the definitions ran again for a settings change: %v, %d runs", err, r.creates())
	}
}

// SPEC 5.6: a world with no mod-settings.dat has its planets from the
// definitions run, so the game runs once.
func TestWorldCatalogueWithoutSettingsSharesTheDefinitionsRun(t *testing.T) {
	r := newModSettingsRig(t)
	r.writeFile(r.id, filepath.Join(world.ModsDir, "moon_1.0.0.zip"), []byte("zip"))
	if seen := r.catalogueRun(); seen != nil {
		t.Fatalf("the run saw a settings file %q", seen)
	}
	if _, err := r.c.ModSettingDefs(context.Background(), r.id); err != nil || r.creates() != 1 {
		t.Errorf("%v, %d runs, want one", err, r.creates())
	}
	if _, ok := r.c.cachedModPlanetDefs(r.id); !ok {
		t.Error("the cached planets were not found")
	}
}

