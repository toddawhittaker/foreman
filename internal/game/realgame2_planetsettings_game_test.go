//go:build game

package game

// A startup setting that adds a planet, against the real 2.1 game (SPEC 5.6,
// 9.6): the planets run reads the world's saved mod-settings.dat, so saving
// the setting changes the planet list, while the definitions run still reports
// the mod's default. Run by `make test-game`, never by `make verify`.

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/world"
)

func defsPlanets(defs *ModSettingDefs) []string {
	var names []string
	for _, p := range defs.Catalogue("en").Planets {
		names = append(names, p.Name)
	}
	return names
}

func TestRealGameModSettingsPlanetFollowsSetting(t *testing.T) {
	e, version := realGame21(t)
	c, _ := e.controller(t)
	ctx := context.Background()
	const (
		mod    = "fmsetplanet"
		planet = "fmsetplanet-planet"
		name   = "fmsetplanet-add"
	)
	id := e.planetModWorld(t, version, "fmsetplanet_1.0.0", mod)

	// No saved settings: the definitions run answers, on the default (false).
	defs, err := c.ModSettingDefs(ctx, id)
	if err != nil {
		t.Fatalf("the definitions run failed: %v", err)
	}
	if defs.PlanetsErr != nil {
		t.Fatalf("planets error: %v", defs.PlanetsErr)
	}
	if got := defsPlanets(defs); slices.Contains(got, planet) {
		t.Fatalf("planets %v without a saved mod-settings.dat, want %s absent", got, planet)
	}
	var def *modsettings.Definition
	for i := range defs.Defs {
		if defs.Defs[i].Name == name {
			def = &defs.Defs[i]
		}
	}
	if def == nil {
		t.Fatalf("the definitions run lists no %s: %+v", name, defs.Defs)
	}
	if _, err := os.Stat(e.modsPath(id, world.ModSettingsFile)); err == nil {
		t.Fatal("the world already has a mod-settings.dat")
	}

	// Save the setting as true, the way Generate step 3 does: a mod change
	// that carries the new file.
	file, err := modsettings.Decode(defs.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := modsettings.Decode(defs.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	v, verr := modsettings.Validate(*def, "true")
	if verr != nil {
		t.Fatalf("Validate: %v", verr)
	}
	if err := file.Set(modsettings.Startup, name, v, defaults); err != nil {
		t.Fatalf("Set: %v", err)
	}
	edited, err := file.Encode()
	if err != nil {
		t.Fatal(err)
	}
	op := e.modsOp(t, id, "edit mod settings", world.ModChange{Settings: edited, SettingsSeen: world.SettingsHash(nil)})
	if err := c.ApplyMods(ctx, id, admin, op, StopOptions{Countdown: 2 * time.Second}); err != nil {
		t.Fatalf("ApplyMods: %v", err)
	}

	// The planets run, on the saved file, lists the planet.
	cat, err := c.WorldCatalogue(ctx, id, "en")
	if err != nil {
		t.Fatalf("the planets run failed: %v", err)
	}
	var names []string
	for _, p := range cat.Planets {
		names = append(names, p.Name)
	}
	t.Logf("planets with the setting true: %v", names)
	if !slices.Contains(names, planet) {
		t.Errorf("planets %v after saving %s=true, want %s among them", names, name, planet)
	}

	// The definitions run is untouched by the saved value.
	defs2, err := c.ModSettingDefs(ctx, id)
	if err != nil {
		t.Fatalf("the definitions run failed after the save: %v", err)
	}
	for _, d := range defs2.Defs {
		if d.Name == name && (d.Default == nil || d.Default.Bool) {
			t.Errorf("definitions report default %+v for %s, want false", d.Default, name)
		}
	}
	if got := defsPlanets(defs2); slices.Contains(got, planet) {
		t.Errorf("the definitions run lists %s (%v), want the defaults' planets only", planet, got)
	}
}
