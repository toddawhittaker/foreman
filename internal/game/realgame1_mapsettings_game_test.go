//go:build game

package game

// The map settings Generate will write, against the real game images (SPEC
// 4.3, 5.6): the complete map-gen-settings.json and map-settings.json, with no
// --preset, must be accepted by --create through Foreman's start script, and
// the running game must hold the preset's values and any field changed after
// it. Run by `make test-game`, never by `make verify`.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// uibPrefix starts the name of every world these tests make, so its
// container (foreman-<id>) is recognisable and removed at the end.
const uibPrefix = "uib-realgame1"

// generateUIB is generate with a name of its own.
func (e *gameEnv) generateUIB(t *testing.T, label string, nw world.NewWorld) string {
	t.Helper()
	return e.generateNamed(t, uibPrefix, label, nw)
}

// generateNamed makes a world whose name starts with prefix, so its container
// (foreman-<prefix>-...) is recognisable, and removes the container at the
// end of the test.
func (e *gameEnv) generateNamed(t *testing.T, prefix, label string, nw world.NewWorld) string {
	t.Helper()
	nw.Name = fmt.Sprintf("%s %s %s", prefix, label, e.run)
	id, err := e.worlds.Generate(context.Background(), nw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ContainerName(id), "foreman-"+prefix+"-") {
		t.Fatalf("container name %s is not recognisable as this test's", ContainerName(id))
	}
	t.Cleanup(func() { e.removeContainer(t, ContainerName(id)) })
	return id
}

func (e *gameEnv) removeContainer(t *testing.T, name string) {
	t.Helper()
	if err := e.docker.RemoveContainer(context.Background(), name, true); err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("could not remove test container %s: %v", name, err)
	}
}

// flatten reads a JSON value as its dotted paths. Keys that start with "_"
// are the example files' comments. Arrays and null are not descended into
// or kept.
func flatten(prefix string, v any, out map[string]any) {
	switch x := v.(type) {
	case map[string]any:
		for k, w := range x {
			if strings.HasPrefix(k, "_") {
				continue
			}
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			flatten(p, w, out)
		}
	case nil:
	default:
		out[prefix] = v
	}
}

func flatJSONBytes(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, b)
	}
	out := map[string]any{}
	flatten("", v, out)
	return out
}

// emptyish says a value is an empty table, which Lua's JSON writes as [] or {}.
func emptyish(v any) bool {
	switch x := v.(type) {
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

func sameJSONValue(a, b any) bool {
	x, ok1 := a.(float64)
	y, ok2 := b.(float64)
	if ok1 && ok2 {
		return math.Abs(x-y) <= 1e-5*math.Max(math.Abs(y), 1e-12) || x == y
	}
	if emptyish(a) && emptyish(b) {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// mergeJSON puts the leaves of over on base, in place, descending into objects.
func mergeJSON(base, over map[string]any) {
	for k, v := range over {
		if o, ok := v.(map[string]any); ok {
			if b, ok := base[k].(map[string]any); ok {
				mergeJSON(b, o)
				continue
			}
		}
		base[k] = v
	}
}

// setDotted sets the value at a dotted path, making the objects on the way.
func setDotted(m map[string]any, path string, v any) {
	parts := strings.Split(path, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = v
}

// fullMapGen is the complete map-gen-settings.json for a preset: the game's
// example file with the preset's values on it, then the changes.
func fullMapGen(t *testing.T, line, preset string, changes map[string]any) []byte {
	t.Helper()
	var base, delta map[string]any
	if err := json.Unmarshal(mapgen.DefaultMapGen(line), &base); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mapgen.PresetMapGen(line, preset), &delta); err != nil {
		t.Fatal(err)
	}
	mergeJSON(base, delta)
	for p, v := range changes {
		setDotted(base, p, v)
	}
	b, err := json.MarshalIndent(base, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// luaJSON is how the running game turns a table into JSON on this line.
func luaJSON(line string) string {
	if line == "1.1" {
		return "game.table_to_json"
	}
	return "helpers.table_to_json"
}

// readLua asks the running game for a table as JSON over RCON. The first
// Lua command of a 1.1 game only warns about achievements, so a reply that is
// not an object is asked again once.
func readLua(t *testing.T, c *Controller, id, line, body string) map[string]any {
	t.Helper()
	cmd := "/silent-command " + body
	var reply string
	for range 2 {
		var err error
		if _, reply, err = c.Console(context.Background(), id, admin, cmd, cmd); err != nil {
			t.Fatalf("RCON: %v", err)
		}
		if strings.HasPrefix(strings.TrimSpace(reply), "{") {
			break
		}
	}
	dec := json.NewDecoder(strings.NewReader(reply))
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		t.Fatalf("the game's reply to %q is not a JSON object (%d bytes): %v\n%s", body, len(reply), err, reply)
	}
	return out
}

// gameSettings is what the running game holds, as dotted paths.
type gameSettings struct {
	mapGen, mapSettings map[string]any
}

// luaShape is a Lua table constructor that copies the values the game holds
// at expr, with the shape of the file's value v. The settings objects are
// userdata that table_to_json cannot read, and a key the game lacks is an
// error to read, so each value is read on its own and a missing one is nil.
func luaShape(path string, v any) string {
	obj, ok := v.(map[string]any)
	if !ok {
		return `r("` + path + `")`
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		if !strings.HasPrefix(k, "_") && obj[k] != nil {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		sub := k
		if path != "" {
			sub = path + "." + k
		}
		parts[i] = k + "=" + luaShape(sub, obj[k])
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// readGameSettings reads the running game's settings back over RCON. The
// map-settings part is read for the keys of file, the complete file that
// made the map.
func readGameSettings(t *testing.T, c *Controller, id, line string, file []byte) gameSettings {
	t.Helper()
	tj := luaJSON(line)
	gs := gameSettings{mapGen: map[string]any{}, mapSettings: map[string]any{}}
	merge := func(dst map[string]any, body string) {
		flatten("", readLua(t, c, id, line, body), dst)
	}
	// The map generator's settings in two parts, as one reply may be cut off
	// at 4 KB.
	merge(gs.mapGen, `local m=game.surfaces[1].map_gen_settings; m.autoplace_controls=nil; rcon.print(`+tj+`(m))`)
	merge(gs.mapGen, `rcon.print(`+tj+`({autoplace_controls=game.surfaces[1].map_gen_settings.autoplace_controls}))`)
	var top map[string]any
	if err := json.Unmarshal(file, &top); err != nil {
		t.Fatal(err)
	}
	for k, v := range top {
		if strings.HasPrefix(k, "_") || v == nil {
			continue
		}
		root := "game.map_settings"
		if k == "difficulty_settings" {
			root = "game.difficulty_settings"
		}
		// r reads one value by its dotted path; a key the game lacks is nil.
		body := `local function r(p) local ok,v=pcall(function() local x=` + root + ` for n in p:gmatch("[^.]+") do x=x[n] end return x end) if ok then return v end end ` +
			"rcon.print(" + tj + "({" + k + "=" + luaShape(k, v) + "}))"
		if k == "difficulty_settings" {
			body = strings.ReplaceAll(body, `r("difficulty_settings.`, `r("`)
		}
		if len(body) > 3900 {
			t.Fatalf("the command for %s is too long for RCON (%d bytes)", k, len(body))
		}
		merge(gs.mapSettings, body)
	}
	return gs
}

// compareToFile checks the running game against the file that made the map.
// Every value of the file the game also reports must be the same. must lists
// paths that the game has to report, so a missing one fails too. It returns
// how many values were compared and the paths the game did not report.
func compareToFile(t *testing.T, what string, file, got map[string]any, must []string) (compared int, unreported []string) {
	t.Helper()
	paths := make([]string, 0, len(file))
	for p := range file {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		want := file[p]
		have, ok := got[p]
		if !ok {
			unreported = append(unreported, p)
			continue
		}
		if _, isStr := want.(string); isStr && fmt.Sprint(have) != fmt.Sprint(want) {
			t.Errorf("%s %s: the game holds %v, the file says %v", what, p, have, want)
			continue
		}
		compared++
		if !sameJSONValue(have, want) {
			t.Errorf("%s %s: the game holds %v, the file says %v", what, p, have, want)
		}
	}
	for _, p := range must {
		if _, ok := got[p]; !ok {
			t.Errorf("%s %s: the game does not report it, so it cannot be shown to be applied", what, p)
		}
	}
	return compared, unreported
}

// leavesThatDiffer lists the dotted paths whose values are not the same in a
// and b.
func leavesThatDiffer(a, b map[string]any) []string {
	var out []string
	for p, v := range a {
		if w, ok := b[p]; !ok || !sameJSONValue(v, w) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func TestRealGameUIBMapSettings(t *testing.T) {
	only := os.Getenv("FOREMAN_TEST_GAME_TAG")
	e := newGameEnv(t)
	for _, g := range gameTags {
		if only != "" && only != g.tag {
			continue
		}
		t.Run("v"+g.tag, func(t *testing.T) {
			e.requireImage(t, ImageRef(g.version))
			e.runUIBMapSettings(t, g.tag, g.version)
		})
	}
}

// mapCase is one world to make: the preset it starts from and what is
// changed after it.
type mapCase struct {
	label, preset string
	mapGenChanges map[string]any
	settingsDelta map[string]any
	// presetOnly makes the world with --preset and no files, as an
	// unchanged preset does.
	presetOnly bool
	// check says what else must hold of the running game.
	check func(t *testing.T, gs gameSettings)
}

func (e *gameEnv) runUIBMapSettings(t *testing.T, line, version string) {
	c, _ := e.controller(t)
	var cases []mapCase
	for _, p := range mapgen.Presets(line) {
		cases = append(cases, mapCase{label: p, preset: p})
	}
	// A change to fields the preset itself owns: with --preset the game would
	// ignore them (docs/EPIC-ui-batch.md, ruling 1).
	cases = append(cases, mapCase{
		label: "death-world changed", preset: "death-world",
		mapGenChanges: map[string]any{
			"autoplace_controls.enemy-base.frequency": 3.0,
			"starting_area":                        2.0,
			"autoplace_controls.iron-ore.richness": 3.0,
		},
		settingsDelta: map[string]any{"pollution.diffusion_ratio": 0.05, "max_failed_behavior_count": 7},
		check: func(t *testing.T, gs gameSettings) {
			// The preset's own values that were not changed are still there.
			wantNum(t, gs.mapSettings, "enemy_evolution.time_factor", 0.00002)
			wantNum(t, gs.mapSettings, "enemy_evolution.pollution_factor", 0.0000012)
			// The changed values won over the preset.
			wantNum(t, gs.mapGen, "autoplace_controls.enemy-base.frequency", 3)
			wantNum(t, gs.mapGen, "starting_area", 2)
			wantNum(t, gs.mapGen, "autoplace_controls.iron-ore.richness", 3)
			wantNum(t, gs.mapSettings, "pollution.diffusion_ratio", 0.05)
			wantNum(t, gs.mapSettings, "max_failed_behavior_count", 7)
		},
	})
	seed := uint32(1000)
	for _, tc := range cases {
		seed++
		t.Run(tc.label, func(t *testing.T) {
			e.runMapCase(t, c, line, version, tc, seed)
		})
	}

	// The unchanged-preset path of the ruling: --preset alone, no files. The
	// game's own reading of the preset must be the files Foreman would write
	// for it, so the files above are shown to be the preset.
	for _, p := range mapgen.Presets(line) {
		seed++
		t.Run("preset only "+p, func(t *testing.T) {
			e.runMapCase(t, c, line, version, mapCase{label: "preset only " + p, preset: p, presetOnly: true}, seed)
		})
	}
}

func wantNum(t *testing.T, got map[string]any, path string, want float64) {
	t.Helper()
	v, ok := got[path].(float64)
	if !ok || !sameJSONValue(v, want) {
		t.Errorf("the game holds %v for %s, want %v", got[path], path, want)
	}
}

// startInGame starts a generated world through the controller (the start
// script's --create) and checks that the game reached InGame.
func (e *gameEnv) startInGame(t *testing.T, c *Controller, id string) {
	t.Helper()
	begin := time.Now()
	if err := c.Start(context.Background(), id, admin, StartOptions{}); err != nil {
		t.Fatalf("start: %v\nlog:\n%s", err, strings.Join(logLines(c, id), "\n"))
	}
	t.Logf("TIMING create and start: %s", time.Since(begin).Round(100*time.Millisecond))
	if st := c.Status(id); st.State != StateRunning || st.Save != id+".zip" {
		t.Fatalf("state %s, save %q; want running with %s.zip", st.State, st.Save, id)
	}
	if !logHas(c, id, "Loading map /factorio/saves/"+id+".zip") {
		t.Error("the log has no line for loading the new save")
	}
	if !logHas(c, id, "to(InGame)") {
		t.Errorf("the log never says the game reached InGame:\n%s", strings.Join(logLines(c, id), "\n"))
	}
	e.noSecretInLog(t, c, id)
}

func (e *gameEnv) runMapCase(t *testing.T, c *Controller, line, version string, tc mapCase, seed uint32) {
	mapGen := fullMapGen(t, line, tc.preset, tc.mapGenChanges)
	settings, err := mapgen.MapSettings(version, tc.preset, tc.settingsDelta)
	if err != nil {
		t.Fatal(err)
	}
	// The files are what Generate writes when anything is changed: both in
	// full, and no preset.
	nw := world.NewWorld{GameVersion: version, Seed: &seed, MapGenSettings: mapGen, MapSettings: settings}
	if tc.presetOnly {
		nw = world.NewWorld{GameVersion: version, Seed: &seed, Preset: tc.preset}
	}
	id := e.generateUIB(t, line+" "+tc.label, nw)
	e.startInGame(t, c, id)
	defer func() {
		if err := c.Stop(context.Background(), id, admin, StopOptions{}); err != nil {
			t.Errorf("stop: %v", err)
		}
	}()
	gs := readGameSettings(t, c, id, line, settings)

	// What the preset sets in map-gen and in map-settings must be reported by
	// the game, not only absent from the failures.
	var presetGen map[string]any
	_ = json.Unmarshal(mapgen.PresetMapGen(line, tc.preset), &presetGen)
	presetLeaves := map[string]any{}
	flatten("", presetGen, presetLeaves)
	var mustGen []string
	for p := range presetLeaves {
		mustGen = append(mustGen, p)
	}
	defaults, err := mapgen.MapSettings(version, "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	fileLeaves := flatJSONBytes(t, settings)
	mustSettings := leavesThatDiffer(fileLeaves, flatJSONBytes(t, defaults))

	genLeaves := flatJSONBytes(t, mapGen)
	for _, k := range []string{"width", "height"} {
		// 0 means no limit, which the game reports as its largest map.
		if genLeaves[k] == 0.0 {
			genLeaves[k] = 2000000.0
		}
	}
	nGen, noGen := compareToFile(t, "map-gen", genLeaves, gs.mapGen, mustGen)
	nSet, noSet := compareToFile(t, "map-settings", fileLeaves, gs.mapSettings, mustSettings)
	t.Logf("compared %d map-gen values (%d not reported) and %d map-settings values (%d not reported)", nGen, len(noGen), nSet, len(noSet))
	if len(noSet) > 0 {
		t.Logf("map-settings not reported by the game: %s", strings.Join(noSet, ", "))
	}
	if len(noGen) > 0 {
		t.Logf("map-gen not reported by the game: %s", strings.Join(noGen, ", "))
	}
	// The seed given to Generate is the seed the game used.
	wantNum(t, gs.mapGen, "seed", float64(seed))
	if tc.check != nil {
		tc.check(t, gs)
	}
	// The preset's identity survived: death-world's evolution time factor.
	if tc.preset == "death-world" {
		wantNum(t, gs.mapSettings, "enemy_evolution.time_factor", 0.00002)
	}
}
