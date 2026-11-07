//go:build game

package game

// Space Age worlds made from the complete settings files Generate writes
// (SPEC 5.6), against the real game images. A change to one setting, with no
// --preset, must create the world; the save must hold the change; and every
// planet's own settings must be what the file says. The spoil time setting
// must run the way its label says. Run by `make test-game`, never by
// `make verify`.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// rg2Prefix starts the name of every world these tests make, so its container
// (foreman-<prefix>-...) is recognisable and removed at the end.
const rg2Prefix = "uib-rg2"

func (e *gameEnv) generateRG2(t *testing.T, label string, nw world.NewWorld) string {
	t.Helper()
	return e.generateNamed(t, rg2Prefix, label, nw)
}

// spaceAgeMapGen is the complete map-gen-settings.json the form writes for a
// Space Age world: the game's example file with an entry for every planet's
// autoplace controls, then the changes.
func spaceAgeMapGen(t *testing.T, line string, changes map[string]any) []byte {
	t.Helper()
	var base map[string]any
	if err := json.Unmarshal(mapgen.DefaultMapGen(line), &base); err != nil {
		t.Fatal(err)
	}
	controls := base["autoplace_controls"].(map[string]any)
	for _, p := range mapgen.Planets(line, true) {
		for _, c := range mapgen.Controls(line, p) {
			if _, ok := controls[c.Name]; ok {
				continue
			}
			entry := map[string]any{"frequency": 1.0, "size": 1.0}
			if c.Richness {
				entry["richness"] = 1.0
			}
			controls[c.Name] = entry
		}
	}
	for p, v := range changes {
		setDotted(base, p, v)
	}
	b, err := json.MarshalIndent(base, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// planetControls reads the autoplace controls the running game holds for a
// planet's surface, making the surface if the world has none yet.
func planetControls(t *testing.T, c *Controller, id, line, planet string) map[string]any {
	t.Helper()
	body := `local pl=game.planets["` + planet + `"]; local s=pl.surface or pl.create_surface(); ` +
		`rcon.print(` + luaJSON(line) + `({autoplace_controls=s.map_gen_settings.autoplace_controls}))`
	out := map[string]any{}
	flatten("", readLua(t, c, id, line, body), out)
	return out
}

// stopAtEnd stops the world's game when the test ends, so the next world can
// use the game port.
func stopAtEnd(t *testing.T, c *Controller, id string) {
	t.Cleanup(func() {
		if err := c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
			t.Errorf("stop: %v", err)
		}
	})
}

func TestRealGameRG2SpaceAge(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if g.tag == "1.1" || (only != "" && only != g.tag) {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runSpaceAgeSettings(t, g.tag, g.version)
		})
	}
}

func (e *gameEnv) runSpaceAgeSettings(t *testing.T, line, version string) {
	c, _ := e.controller(t)
	// One changed value on every planet, none of them the game's default.
	changes := map[string]any{
		"starting_area":                                 2.0,
		"autoplace_controls.iron-ore.richness":          2.0,
		"autoplace_controls.tungsten_ore.size":          6.0,
		"autoplace_controls.vulcanus_volcanism.size":    1.5,
		"autoplace_controls.gleba_stone.frequency":      3.0,
		"autoplace_controls.scrap.richness":             0.5,
		"autoplace_controls.fulgora_islands.frequency":  0.5,
		"autoplace_controls.lithium_brine.richness":     2.0,
		"autoplace_controls.aquilo_crude_oil.frequency": 2.0,
	}
	mapGen := spaceAgeMapGen(t, line, changes)
	settings, err := mapgen.MapSettings(version, "default", map[string]any{"pollution.diffusion_ratio": 0.05})
	if err != nil {
		t.Fatal(err)
	}
	seed := uint32(4242)
	// A changed setting writes both files complete and passes no preset.
	id := e.generateRG2(t, line+" space age", world.NewWorld{
		GameVersion: version, SpaceAge: true, Seed: &seed, MapGenSettings: mapGen, MapSettings: settings,
	})
	e.startInGame(t, c, id)
	stopAtEnd(t, c, id)

	mods := readLua(t, c, id, line, `rcon.print(`+luaJSON(line)+`({space_age=script.active_mods["space-age"]~=nil}))`)
	if mods["space_age"] != true {
		t.Fatalf("Space Age is not active in the running game: %v", mods)
	}
	fileGen := flatJSONBytes(t, mapGen)
	for _, k := range []string{"width", "height"} {
		// 0 means no limit, which the game reports as its largest map.
		if fileGen[k] == 0.0 {
			fileGen[k] = 2000000.0
		}
	}

	// Which planet owns each control, so each surface is checked against its
	// own controls.
	owner := map[string]string{}
	for _, planet := range mapgen.Planets(line, true) {
		for _, ctl := range mapgen.Controls(line, planet) {
			owner[ctl.Name] = planet
		}
	}
	ownedBy := func(path, planet string) bool {
		name, ok := strings.CutPrefix(path, "autoplace_controls.")
		if !ok {
			return planet == "nauvis"
		}
		name, _, _ = strings.Cut(name, ".")
		return owner[name] == planet
	}

	gs := readGameSettings(t, c, id, line, settings)
	wantNum(t, gs.mapSettings, "pollution.diffusion_ratio", 0.05)
	wantNum(t, gs.mapGen, "seed", float64(seed))
	for _, planet := range mapgen.Planets(line, true) {
		file := map[string]any{}
		got := gs.mapGen
		if planet != "nauvis" {
			got = planetControls(t, c, id, line, planet)
		}
		for p, v := range fileGen {
			if ownedBy(p, planet) && p != "seed" {
				file[p] = v
			}
		}
		var must []string
		for p := range changes {
			if ownedBy(p, planet) {
				must = append(must, p)
			}
		}
		if len(must) == 0 {
			t.Errorf("%s: the test changed no value on this planet", planet)
		}
		n, missing := compareToFile(t, planet, file, got, must)
		if len(missing) > 0 {
			t.Errorf("%s: the game does not report %s", planet, strings.Join(missing, ", "))
		}
		t.Logf("%s: the game reports %d of the file's %d values, all equal, including the changed %v", planet, n, len(file), must)
	}
}

// spoilReading is what a spoilable item in a chest shows.
type spoilReading struct {
	// remaining is the ticks left to spoil when the item went in.
	remaining float64
	// perTick is the share of the item that spoiled per tick, 300 ticks later.
	perTick float64
}

// readSpoil puts one yumako (a fruit that spoils) in a chest in the running
// game and lets 300 ticks pass.
func readSpoil(t *testing.T, c *Controller, id, line string) spoilReading {
	t.Helper()
	tj := luaJSON(line)
	in := readLua(t, c, id, line, `local s=game.surfaces[1]; s.request_to_generate_chunks({0,0},1); s.force_generate_chunk_requests(); `+
		`local ch=s.create_entity{name="wooden-chest",position={0,0},force="player"}; local inv=ch.get_inventory(defines.inventory.chest); inv.insert{name="yumako",count=1}; `+
		`rcon.print(`+tj+`({tick=game.tick, spoil_tick=inv[1].spoil_tick}))`)
	start, _ := in["tick"].(float64)
	spoilAt, _ := in["spoil_tick"].(float64)
	// A game with no player is paused and runs one tick per RCON command, so
	// asking again is what lets the 300 ticks pass.
	var out map[string]any
	deadline := time.Now().Add(30 * time.Second)
	for {
		out = readLua(t, c, id, line, `local st=game.surfaces[1].find_entities_filtered{name="wooden-chest"}[1].get_inventory(defines.inventory.chest)[1]; `+
			`rcon.print(`+tj+`({tick=game.tick, pct=st.spoil_percent}))`)
		if tick, _ := out["tick"].(float64); tick >= start+300 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the game did not reach 300 ticks in 30 s: %v", out)
		}
	}
	tick, _ := out["tick"].(float64)
	pct, _ := out["pct"].(float64)
	return spoilReading{remaining: spoilAt - start, perTick: pct / (tick - start)}
}

// The game's "Spoiling rate" is the inverse of difficulty_settings.
// spoil_time_modifier: a rate of 200% is stored as 0.5, and items must then
// spoil twice as fast. The file is written directly (SPEC 5.6).
func TestRealGameRG2SpoilTime(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if g.tag == "1.1" || (only != "" && only != g.tag) {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			c, _ := e.controller(t)
			read := map[float64]spoilReading{}
			for _, mod := range []float64{1, 0.5} {
				settings, err := mapgen.MapSettings(g.version, "default", map[string]any{"difficulty_settings.spoil_time_modifier": mod})
				if err != nil {
					t.Fatal(err)
				}
				seed := uint32(7)
				label := g.tag + " spoil " + map[float64]string{1: "rate 100", 0.5: "rate 200"}[mod]
				id := e.generateRG2(t, label, world.NewWorld{GameVersion: g.version, SpaceAge: true, Seed: &seed, MapSettings: settings})
				e.startInGame(t, c, id)
				gs := readGameSettings(t, c, id, g.tag, settings)
				wantNum(t, gs.mapSettings, "difficulty_settings.spoil_time_modifier", mod)
				r := readSpoil(t, c, id, g.tag)
				t.Logf("spoil time set to %v: the game holds it, and a yumako spoils in %.0f ticks, %.3g of it per tick", mod, r.remaining, r.perTick)
				read[mod] = r
				if err := c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
					t.Errorf("stop: %v", err)
				}
			}
			if read[0.5].remaining != read[1].remaining/2 {
				t.Errorf("with 0.5 stored the item spoils in %.0f ticks; want half the %.0f at 1: a lower number must mean faster", read[0.5].remaining, read[1].remaining)
			}
			if ratio := read[0.5].perTick / read[1].perTick; ratio < 1.98 || ratio > 2.02 {
				t.Errorf("the spoiled share per tick with 0.5 stored is %.3g times that with 1; want 2", ratio)
			}
		})
	}
}
