//go:build game

package game

// The create step and the map preview against the real game images
// (SPEC 4.3, 5.6, 14.3). Run by `make test-game`, never by `make verify`.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/mapexchange"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// exchangeFixtures are the patches whose real exchange strings the worlds of
// each line are made from; 2.0.66 is older than the 2.0.77 world. A 2.1.9
// string is not listed: 2.1.24 refuses its map settings, which lack
// enemy_expansion.build_base_unit_dispatch_cooldown.
var exchangeFixtures = map[string][]string{
	"1.1": {"1.1.110"},
	"2.0": {"2.0.66", "2.0.77"},
	"2.1": {"2.1.24"},
}

func TestRealGameGenerate(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runGenerate(t, g.tag, g.version)
		})
	}
}

// generate makes a world with no save and removes its container at the end
// of the test, whatever the result.
func (e *gameEnv) generate(t *testing.T, label string, nw world.NewWorld) string {
	t.Helper()
	nw.Name = fmt.Sprintf("e3test %s %s", label, e.run)
	id, err := e.worlds.Generate(context.Background(), nw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ContainerName(id), "foreman-e3test-") {
		t.Fatalf("container name %s would not be recognisable as a test container", ContainerName(id))
	}
	t.Cleanup(func() {
		err := e.docker.RemoveContainer(context.Background(), ContainerName(id), true)
		if err != nil && !errors.Is(err, docker.ErrNotFound) {
			t.Errorf("could not remove test container %s: %v", ContainerName(id), err)
		}
	})
	return id
}

// startAndCheck starts a generated world, checks that the first start made
// and loaded its map, asks the game for its map seed, and stops the game so
// the next world can use the port. It returns the seed the game reports.
func (e *gameEnv) startAndCheck(t *testing.T, c *Controller, id string) string {
	t.Helper()
	ctx := context.Background()
	begin := time.Now()
	if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	t.Logf("TIMING first start: %s", time.Since(begin).Round(100*time.Millisecond))
	st := c.Status(id)
	if st.State != StateRunning || st.Save != id+".zip" {
		t.Fatalf("state %s, save %q; want running with %s.zip", st.State, st.Save, id)
	}
	if !logHas(c, id, "Loading map /factorio/saves/"+id+".zip") {
		t.Error("the log has no line for loading the new save")
	}
	meta, err := c.readMeta(id)
	if err != nil || meta.NewMap != nil {
		t.Errorf("new_map after the first start: %+v, %v", meta.NewMap, err)
	}
	// The command is printed by the game; no secret is in it.
	cmd := "/silent-command rcon.print(game.surfaces[1].map_gen_settings.seed)"
	// The first Lua command of a game only warns about achievements on 1.1;
	// repeating it runs it.
	var seed string
	for range 2 {
		if _, seed, err = c.Console(ctx, id, admin, cmd, cmd); err != nil {
			t.Fatalf("RCON: %v", err)
		}
	}
	e.noSecretInLog(t, c, id)
	if err := c.Stop(ctx, id, admin, StopOptions{}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	return strings.TrimSpace(seed)
}

func (e *gameEnv) runGenerate(t *testing.T, tag, version string) {
	c, _ := e.controller(t)
	line := tag

	t.Run("preset and seed", func(t *testing.T) {
		seed := uint32(4294967295)
		id := e.generate(t, tag+" preset", world.NewWorld{GameVersion: version, Preset: "rich-resources", Seed: &seed})
		if got := e.startAndCheck(t, c, id); !sameNumber(got, "4294967295") {
			t.Errorf("the game's seed is %q, want 4294967295", got)
		}
	})

	t.Run("custom map-gen settings", func(t *testing.T) {
		id := e.generate(t, tag+" custom", world.NewWorld{
			GameVersion:    version,
			MapGenSettings: []byte(`{"seed": 99, "width": 2000, "height": 2000, "starting_area": 2, "peaceful_mode": true}`),
		})
		if got := e.startAndCheck(t, c, id); !sameNumber(got, "99") {
			t.Errorf("the game's seed is %q, want 99", got)
		}
	})

	for _, from := range exchangeFixtures[line] {
		for _, name := range []string{"custom-1", "custom-2"} {
			t.Run(fmt.Sprintf("exchange string %s %s", from, name), func(t *testing.T) {
				res := e.decodeFixture(t, from, name)
				id := e.generate(t, tag+" "+name+" "+from, world.NewWorld{
					GameVersion: version, MapGenSettings: res.MapGenSettings, MapSettings: res.MapSettings,
				})
				var settings struct {
					Seed json.Number `json:"seed"`
				}
				if err := json.Unmarshal(res.MapGenSettings, &settings); err != nil {
					t.Fatal(err)
				}
				got := e.startAndCheck(t, c, id)
				if !sameNumber(got, settings.Seed.String()) {
					t.Errorf("the game's seed is %q, want %s", got, settings.Seed)
				}
			})
		}
	}

	t.Run("settings save on a running game", func(t *testing.T) {
		ctx := context.Background()
		id := e.generate(t, tag+" settings", world.NewWorld{GameVersion: version})
		if err := c.Start(ctx, id, admin, StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}
		defer func() { _ = c.Stop(ctx, id, admin, StopOptions{}) }()
		save := func(file, data string) SettingsResult {
			read, err := e.worlds.ReadConfig(id, file)
			if err != nil {
				t.Fatal(err)
			}
			res, err := c.SaveSettings(ctx, id, admin, []SettingsChange{{File: file, Data: []byte(data), SeenHash: read.Hash}})
			if err != nil {
				t.Fatalf("save %s: %v", file, err)
			}
			return res
		}
		if res := save(world.ServerSettingsFile, `{"name": "Changed name", "description": "Changed", "visibility": {"public": false, "lan": false}}`); !res.RestartNeeded {
			t.Error("a running game's server settings need a restart")
		}
		save(world.ServerAdminListFile, `["alice_x", "bob;y"]`)
		// The game wrote the file itself, as it does for /promote.
		waitFor(t, 5*time.Second, "the game to write the admin list", func() bool {
			b, _ := os.ReadFile(filepath.Join(e.worldDir(id), world.ConfigDir, world.ServerAdminListFile)) //nolint:gosec // a test's own folder
			return strings.Contains(string(b), "alice_x") && strings.Contains(string(b), "bob;y")
		})
		snaps, err := e.worlds.Snapshots(id)
		if err != nil {
			t.Fatal(err)
		}
		reasons := 0
		for _, s := range snaps {
			if s.Reason == world.ReasonSettings {
				reasons++
			}
		}
		if reasons != 2 {
			t.Errorf("%d settings snapshots, want 2", reasons)
		}
		e.noSecretInLog(t, c, id)
	})

	t.Run("preview", func(t *testing.T) {
		e.checkPreview(t, c, version, line, false)
		if line != "1.1" {
			e.checkPreview(t, c, version, line, true)
		}
	})

	if line == "2.1" {
		t.Run("a Space Age world", func(t *testing.T) {
			id := e.generate(t, tag+" space age", world.NewWorld{GameVersion: version, SpaceAge: true, Preset: "default"})
			e.startAndCheck(t, c, id)
		})
	}
}

// sameNumber compares two numbers as the game prints them, which may be in
// exponent form.
func sameNumber(got, want string) bool {
	a, err1 := strconv.ParseFloat(got, 64)
	b, err2 := strconv.ParseFloat(want, 64)
	return err1 == nil && err2 == nil && a == b
}

func (e *gameEnv) decodeFixture(t *testing.T, version, name string) mapexchange.Result {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "mapexchange", "testdata", version, name+".txt")) //nolint:gosec // a fixed test fixture
	if err != nil {
		t.Fatal(err)
	}
	res, err := mapexchange.Decode(string(b))
	if err != nil {
		t.Fatalf("decode %s/%s: %v", version, name, err)
	}
	return res
}

// checkPreview draws every planet the world would have and checks the
// pictures, that the folder and container are gone, and the time taken.
func (e *gameEnv) checkPreview(t *testing.T, c *Controller, version, line string, spaceAge bool) {
	t.Helper()
	planets := mapgen.Planets(line, spaceAge)
	req := PreviewRequest{GameVersion: version, SpaceAge: spaceAge, Seed: 1234, Preset: "island", Planets: planets}
	begin := time.Now()
	pngs, err := c.Preview(context.Background(), req)
	if err != nil {
		t.Fatalf("preview of %v (Space Age %t): %v", planets, spaceAge, err)
	}
	t.Logf("TIMING preview %v: %s", planets, time.Since(begin).Round(100*time.Millisecond))
	if len(pngs) != len(planets) {
		t.Fatalf("%d pictures for %d planets", len(pngs), len(planets))
	}
	for _, p := range pngs {
		img, err := png.Decode(bytes.NewReader(p.Data))
		if err != nil {
			t.Errorf("%s: %v", p.Planet, err)
			continue
		}
		t.Logf("%s: %d bytes, %dx%d", p.Planet, len(p.Data), img.Bounds().Dx(), img.Bounds().Dy())
		if img.Bounds().Dx() < 512 {
			t.Errorf("%s: picture is only %d pixels wide", p.Planet, img.Bounds().Dx())
		}
	}
	// Only this run's containers: another checkout's run may have left one.
	e.previewLeftovers(t, "after the preview")
}
