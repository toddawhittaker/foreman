//go:build game

package game

// A mod's cliff and enemy controls against the real 2.1 game (SPEC 5.6,
// DESIGN "Cliffs off"): the form gives each its on/off box because the game
// says it can be disabled, and off is size 0. This checks size 0 really
// removes that planet's cliffs and enemies. testdata/mapgentoggle holds a
// tiny folder mod with the planet fmplanet, its control fmplanet_cliff
// (category cliff) and fmplanet_enemy (category enemy, placing the spawner
// fmplanet-spawner). A second surface made from the planet's own settings
// with both controls at size 1 is the control run. Run by `make test-game`,
// never by `make verify`.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

const fmplanetFolder = "fmplanet_1.0.0"

func TestRealGameModdedCliffAndEnemyOff(t *testing.T) {
	const version = "2.1.24"
	e := newGameEnv(t)
	e.requireImage(t, ImageRef(version))
	// What the form writes with both boxes unticked.
	mapGen := []byte(`{"autoplace_controls":{"fmplanet_cliff":{"frequency":1,"size":0},"fmplanet_enemy":{"frequency":1,"size":0,"richness":1}}}`)
	id := e.generate(t, "mgtoggle 2.1", world.NewWorld{GameVersion: version, MapGenSettings: mapGen})
	copyFolderMod(t, filepath.Join("testdata", "mapgentoggle", fmplanetFolder), e.modsPath(id, fmplanetFolder))

	c, stop := e.controller(t)
	defer stop()
	ctx := context.Background()
	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	defer func() { _ = c.Stop(ctx, id, admin, StopOptions{}) }()
	lua := func(body string) string {
		t.Helper()
		cmd := "/silent-command " + body
		_, reply, err := c.Console(ctx, id, admin, cmd, cmd)
		if err != nil {
			t.Fatalf("RCON: %v", err)
		}
		return strings.TrimSpace(reply)
	}
	// The first Lua command only warns that achievements are off.
	lua(`rcon.print("warm")`)
	if got := lua(`rcon.print("warm")`); got != "warm" {
		t.Fatalf("the second Lua command printed %q, want warm", got)
	}
	count := func(surface string) (cliffs, spawners int) {
		t.Helper()
		reply := lua(surface + `; s.request_to_generate_chunks({0,0},8); s.force_generate_chunk_requests(); ` +
			`rcon.print(s.count_entities_filtered{type="cliff"} .. " " .. s.count_entities_filtered{name="fmplanet-spawner"})`)
		f := strings.Fields(reply)
		if len(f) != 2 {
			t.Fatalf("the count printed %q", reply)
		}
		a, err1 := strconv.Atoi(f[0])
		b, err2 := strconv.Atoi(f[1])
		if err1 != nil || err2 != nil {
			t.Fatalf("the count printed %q", reply)
		}
		return a, b
	}
	offC, offE := count(`local pl=game.planets["fmplanet"]; local s=pl.surface or pl.create_surface()`)
	onC, onE := count(`local m=game.planets["fmplanet"].prototype.map_gen_settings; ` +
		`m.autoplace_controls={fmplanet_cliff={frequency=1,size=1,richness=1},fmplanet_enemy={frequency=1,size=1,richness=1}}; ` +
		`local s=game.surfaces["fmon"] or game.create_surface("fmon", m)`)
	t.Logf("FMPLANET off: %d cliffs, %d spawners; on: %d cliffs, %d spawners", offC, offE, onC, onE)
	if offC != 0 || offE != 0 {
		t.Errorf("with both controls at size 0 the planet has %d cliffs and %d spawners, want 0 and 0", offC, offE)
	}
	if onC == 0 || onE == 0 {
		t.Errorf("with both controls at size 1 the planet has %d cliffs and %d spawners, want some of each", onC, onE)
	}
}

// copyFolderMod copies a folder mod from the test data into a world's mods.
func copyFolderMod(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		b, err := os.ReadFile(p) //nolint:gosec // a fixed test fixture
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o600) //nolint:gosec // inside the test's data folder
	})
	if err != nil {
		t.Fatal(err)
	}
}
