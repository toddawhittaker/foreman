package web

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/fakegame"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Tests for get_new_map and set_new_map (SPEC 21.7, 5.6, 12.9). They run on
// the mods rig, whose fake game answers the planet run, with an admin token
// named Claude.

type nmRig struct {
	*modsRig
	cs *mcp.ClientSession
}

func newNMRig(t *testing.T) *nmRig {
	t.Helper()
	g, cs := modsMCP(t, false)
	return &nmRig{modsRig: g, cs: cs}
}

// newMapWorld makes a world that waits for its first map, with the mods'
// zips written, and returns its id.
func (r *nmRig) newMapWorld(name, version string, spaceAge bool, mods ...string) string {
	r.t.Helper()
	id, err := r.worlds.Generate(context.Background(), world.NewWorld{Name: name, GameVersion: version, SpaceAge: spaceAge})
	if err != nil {
		r.t.Fatal(err)
	}
	if len(mods) == 0 {
		return id
	}
	list := `{"mods":[{"name":"base","enabled":true}`
	if spaceAge {
		list += `,{"name":"space-age","enabled":true}`
	}
	for _, m := range mods {
		r.writeMod(id, m, "1.0.0")
		list += `,{"name":"` + m + `","enabled":true}`
	}
	if err := os.WriteFile(filepath.Join(r.modsDir(id), world.ModListFile), []byte(list+`]}`), 0o600); err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *nmRig) meta(id string) world.Meta {
	r.t.Helper()
	root, err := r.worlds.OpenWorld(id)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	m, err := world.ReadMeta(root)
	if err != nil {
		r.t.Fatal(err)
	}
	return m
}

func (r *nmRig) config(id, file string) world.ConfigRead {
	r.t.Helper()
	c, err := r.worlds.ReadConfig(id, file)
	if err != nil {
		r.t.Fatal(err)
	}
	return c
}

func (r *nmRig) newMapAudits() []string {
	var out []string
	for _, e := range r.audit() {
		if e.Action == AuditNewMap {
			out = append(out, e.TokenName+"|"+e.Username+"|"+e.WorldID+"|"+e.Detail)
		}
	}
	return out
}

// planetControl finds one control of one planet in get_new_map's result.
func planetControl(t *testing.T, out map[string]any, planet, control string) map[string]any {
	t.Helper()
	planets, _ := out["planets"].([]any)
	for _, p := range planets {
		pm, _ := p.(map[string]any)
		if pm["name"] != planet {
			continue
		}
		cs, _ := pm["controls"].([]any)
		for _, c := range cs {
			cm, _ := c.(map[string]any)
			if cm["name"] == control {
				return cm
			}
		}
	}
	t.Fatalf("no control %s on planet %s in %s", control, planet, asJSON(out))
	return nil
}

func hasPlanet(out map[string]any, name string) bool {
	planets, _ := out["planets"].([]any)
	for _, p := range planets {
		if pm, _ := p.(map[string]any); pm["name"] == name {
			return true
		}
	}
	return false
}

func controlArgs(planet, control string, kv ...any) map[string]any {
	c := map[string]any{"planet": planet, "control": control}
	for i := 0; i+1 < len(kv); i += 2 {
		c[kv[i].(string)] = kv[i+1]
	}
	return c
}

// SPEC 5.6, 21.7: get_new_map lists the planets, each control with what it
// supports and its values, marks labels untrusted, is read-only and changes
// nothing.
func TestMCPGetNewMap(t *testing.T) {
	r := newNMRig(t)
	id := r.newMapWorld("Fresh", "2.1.24", true)
	tool := mcpTools(t, r.cs)["get_new_map"]
	if tool == nil || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Fatalf("get_new_map: %+v", tool)
	}
	tree, audits := dataStat(t, r.worlds.Dirs()), len(r.audit())
	res, out := call(t, r.cs, "get_new_map", map[string]any{"world_id": id})
	if res.IsError {
		t.Fatalf("get_new_map: %s", resultText(res))
	}
	if diff := diffStat(tree, dataStat(t, r.worlds.Dirs())); diff != "" || len(r.audit()) != audits {
		t.Errorf("get_new_map changed something: %s", diff)
	}
	for _, p := range []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo"} {
		if !hasPlanet(out, p) {
			t.Errorf("no planet %s in %s", p, asJSON(out))
		}
	}
	iron := planetControl(t, out, "nauvis", "iron-ore")
	if iron["category"] != "resource" || iron["has_richness"] != true || iron["can_be_disabled"] != true ||
		iron["frequency"] != float64(100) || iron["size"] != float64(100) || iron["richness"] != float64(100) ||
		iron["enabled"] != true || iron["untrusted_label"] != "Iron ore" {
		t.Errorf("iron-ore: %s", asJSON(iron))
	}
	enemy := planetControl(t, out, "nauvis", "enemy-base")
	if enemy["category"] != "enemy" || enemy["has_richness"] != false || enemy["can_be_disabled"] != false || enemy["enabled"] != nil || enemy["richness"] != nil {
		t.Errorf("enemy-base: %s", asJSON(enemy))
	}
	if out["preset"] != "default" || out["modified"] != false || out["built_in_planets"] != false {
		t.Errorf("state: %s", asJSON(out))
	}
	if !strings.Contains(tool.Description, "untrusted_label") {
		t.Errorf("the description does not mark labels untrusted: %q", tool.Description)
	}
}

// SPEC 5.6: get_new_map shows the preset and seed the world waits with, and
// the values of the saved files.
func TestMCPGetNewMapShowsWhatIsSaved(t *testing.T) {
	r := newNMRig(t)
	id := r.newMapWorld("Fresh", "2.1.24", true)
	args := map[string]any{"world_id": id, "preset": "rich-resources", "seed": "1234"}
	if res, _ := call(t, r.cs, "set_new_map", args); res.IsError {
		t.Fatalf("set: %s", resultText(res))
	}
	_, out := call(t, r.cs, "get_new_map", map[string]any{"world_id": id})
	if out["preset"] != "rich-resources" || out["seed"] != "1234" || out["modified"] != false {
		t.Errorf("preset state: %s", asJSON(out))
	}
	// A change writes the files, and the next read shows their values.
	args = map[string]any{"world_id": id, "seed": "99", "controls": []any{
		controlArgs("nauvis", "iron-ore", "richness", 200, "frequency", 50),
		controlArgs("gleba", "gleba_stone", "enabled", false)}}
	if res, _ := call(t, r.cs, "set_new_map", args); res.IsError {
		t.Fatalf("set: %s", resultText(res))
	}
	_, out = call(t, r.cs, "get_new_map", map[string]any{"world_id": id})
	iron := planetControl(t, out, "nauvis", "iron-ore")
	if out["modified"] != true || out["seed"] != "99" || iron["richness"] != float64(200) || iron["frequency"] != float64(50) {
		t.Errorf("after a change: %s", asJSON(out))
	}
	if stone := planetControl(t, out, "gleba", "gleba_stone"); stone["enabled"] != false {
		t.Errorf("gleba_stone: %s", asJSON(stone))
	}
}

// SPEC 5.6, 12.9: an unchanged preset passes the preset alone and writes no
// file; the audit entry names the preset with the token's name.
func TestMCPSetNewMapPreset(t *testing.T) {
	r := newNMRig(t)
	id := r.newMapWorld("Fresh", "2.0.77", true)
	tool := mcpTools(t, r.cs)["set_new_map"]
	gen := mcpTools(t, r.cs)["generate_world"]
	if tool == nil || tool.Annotations == nil || *tool.Annotations.DestructiveHint != *gen.Annotations.DestructiveHint || tool.Annotations.ReadOnlyHint {
		t.Errorf("set_new_map annotations %+v, generate_world %+v", tool, gen.Annotations)
	}
	before := len(r.snapshotsOf(id))
	res, out := call(t, r.cs, "set_new_map", map[string]any{"world_id": id, "preset": "rich-resources", "seed": "1234"})
	if res.IsError {
		t.Fatalf("set_new_map: %s", resultText(res))
	}
	if out["preset"] != "rich-resources" || out["seed"] != "1234" || out["modified"] != false {
		t.Errorf("result: %s", asJSON(out))
	}
	m := r.meta(id)
	if m.NewMap == nil || m.NewMap.Preset != "rich-resources" || m.NewMap.Seed == nil || *m.NewMap.Seed != 1234 {
		t.Errorf("new_map %+v", m.NewMap)
	}
	if r.config(id, world.MapGenSettingsFile).Found || r.config(id, world.MapSettingsFile).Found {
		t.Error("an unchanged preset wrote a file")
	}
	if want := []string{"Claude|alice|" + id + "|preset rich-resources"}; strings.Join(r.newMapAudits(), ";") != strings.Join(want, ";") {
		t.Errorf("audit %q, want %q", r.newMapAudits(), want)
	}
	if len(r.snapshotsOf(id)) != before {
		t.Error("saving the map settings took a snapshot")
	}
}

func (r *nmRig) snapshotsOf(id string) []world.Snapshot {
	r.t.Helper()
	s, err := r.worlds.Snapshots(id)
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

// SPEC 5.6, 12.9: any change writes both files complete and passes no
// preset; the audit entry says the preset was modified.
func TestMCPSetNewMapControlsWriteBothFiles(t *testing.T) {
	r := newNMRig(t)
	id := r.newMapWorld("Fresh", "2.1.24", true)
	res, out := call(t, r.cs, "set_new_map", map[string]any{"world_id": id, "preset": "rich-resources", "seed": "55", "controls": []any{
		controlArgs("nauvis", "iron-ore", "richness", 300, "size", 200),
		controlArgs("nauvis", "coal", "enabled", false),
		controlArgs("vulcanus", "vulcanus_coal", "frequency", 25)}})
	if res.IsError {
		t.Fatalf("set_new_map: %s", resultText(res))
	}
	if out["modified"] != true || out["preset"] != "rich-resources" {
		t.Errorf("result: %s", asJSON(out))
	}
	m := r.meta(id)
	if m.NewMap == nil || m.NewMap.Preset != "" || m.NewMap.Seed == nil || *m.NewMap.Seed != 55 {
		t.Errorf("new_map %+v", m.NewMap)
	}
	var mg struct {
		Seed     uint32 `json:"seed"`
		Controls map[string]struct {
			Frequency float64 `json:"frequency"`
			Size      float64 `json:"size"`
			Richness  float64 `json:"richness"`
		} `json:"autoplace_controls"`
	}
	file := r.config(id, world.MapGenSettingsFile)
	if !file.Found || json.Unmarshal(file.Data, &mg) != nil {
		t.Fatalf("map-gen-settings.json: %+v", file)
	}
	if iron := mg.Controls["iron-ore"]; iron.Richness != 3 || iron.Size != 2 {
		t.Errorf("iron-ore %+v in %s", iron, file.Data)
	}
	if coal := mg.Controls["coal"]; coal.Size != 0 {
		t.Errorf("coal is off: %+v", coal)
	}
	if v := mg.Controls["vulcanus_coal"]; v.Frequency != 0.25 {
		t.Errorf("vulcanus_coal %+v", v)
	}
	ms := r.config(id, world.MapSettingsFile)
	if !ms.Found {
		t.Error("map-settings.json is missing")
	}
	if want := "Claude|alice|" + id + "|preset rich-resources, modified"; strings.Join(r.newMapAudits(), ";") != want {
		t.Errorf("audit %q, want %q", r.newMapAudits(), want)
	}
}

// SPEC 5.6: a seed left out is chosen at random, and a second call replaces
// what the first saved.
func TestMCPSetNewMapReplacesWhatWasSaved(t *testing.T) {
	fixedSeed(t, 777)
	r := newNMRig(t)
	id := r.newMapWorld("Fresh", "2.1.24", true)
	call(t, r.cs, "set_new_map", map[string]any{"world_id": id, "controls": []any{controlArgs("nauvis", "iron-ore", "richness", 400)}})
	_, out := call(t, r.cs, "set_new_map", map[string]any{"world_id": id, "preset": "death-world"})
	if out["seed"] != "777" || out["modified"] != false {
		t.Errorf("second call: %s", asJSON(out))
	}
	if r.config(id, world.MapGenSettingsFile).Found {
		t.Error("the first call's file was kept")
	}
	if m := r.meta(id); m.NewMap == nil || m.NewMap.Preset != "death-world" {
		t.Errorf("new_map %+v", m.NewMap)
	}
}

// SPEC 5.6, 21.7: an unknown planet or control, a control a planet lacks,
// a setting or value it does not support, and anything else the call cannot
// place refuse the whole call with an error that names it; nothing is
// written, snapshotted or audited.
func TestMCPSetNewMapRefusesWhatItCannotPlace(t *testing.T) {
	r := newNMRig(t)
	id := r.newMapWorld("Fresh", "2.1.24", true)
	good := controlArgs("nauvis", "iron-ore", "richness", 200)
	for _, c := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"unknown planet", map[string]any{"controls": []any{good, controlArgs("moshine", "iron-ore", "size", 100)}}, `"moshine"`},
		{"unknown control", map[string]any{"controls": []any{controlArgs("nauvis", "moshine_ore", "size", 100)}}, `"moshine_ore"`},
		{"control of another planet", map[string]any{"controls": []any{controlArgs("nauvis", "vulcanus_coal", "size", 100)}}, `"vulcanus_coal"`},
		{"richness a control lacks", map[string]any{"controls": []any{controlArgs("nauvis", "enemy-base", "richness", 100)}}, "no richness"},
		{"switch off a control that stays", map[string]any{"controls": []any{controlArgs("nauvis", "enemy-base", "enabled", false)}}, "cannot be switched off"},
		{"value off the steps", map[string]any{"controls": []any{controlArgs("nauvis", "iron-ore", "size", 55)}}, "size 55"},
		{"frequency of none", map[string]any{"controls": []any{controlArgs("nauvis", "iron-ore", "frequency", 0)}}, "frequency 0"},
		{"no value", map[string]any{"controls": []any{controlArgs("nauvis", "iron-ore")}}, "gives no value"},
		{"off and sized", map[string]any{"controls": []any{controlArgs("nauvis", "iron-ore", "enabled", false, "size", 200)}}, "would be ignored"},
		{"on and none", map[string]any{"controls": []any{controlArgs("nauvis", "iron-ore", "enabled", true, "size", 0)}}, "switches it off"},
		{"two values for one", map[string]any{"controls": []any{controlArgs("nauvis", "iron-ore", "size", 200), controlArgs("nauvis", "iron-ore", "size", 300)}}, "iron-ore"},
		{"unknown preset", map[string]any{"preset": "nope"}, `"nope"`},
		{"bad seed", map[string]any{"seed": "abc"}, "seed"},
		{"string and preset", map[string]any{"map_exchange_string": "x", "preset": "default"}, "alone"},
		{"string and controls", map[string]any{"map_exchange_string": "x", "controls": []any{good}}, "alone"},
		{"string that is not one", map[string]any{"map_exchange_string": "garbage"}, "exchange string"},
		{"string of another line", map[string]any{"map_exchange_string": fixture(t, "2.0.77.txt")}, "2.0.77"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tree, audits := dataStat(t, r.worlds.Dirs()), len(r.audit())
			args := map[string]any{"world_id": id}
			for k, v := range c.args {
				args[k] = v
			}
			res, _ := call(t, r.cs, "set_new_map", args)
			if !res.IsError || !strings.Contains(resultText(res), c.want) {
				t.Errorf("%s, want an error with %q", resultText(res), c.want)
			}
			if diff := diffStat(tree, dataStat(t, r.worlds.Dirs())); diff != "" || len(r.audit()) != audits || len(r.newMapAudits()) != 0 {
				t.Errorf("a refusal changed something: %s", diff)
			}
		})
	}
}

// SPEC 5.6: a map exchange string of the world's own line sets the map, and
// the audit entry says so.
func TestMCPSetNewMapExchangeString(t *testing.T) {
	r := newNMRig(t)
	id := r.newMapWorld("Fresh", "2.0.77", true)
	str := fixture(t, "2.0.77.txt")
	res, out := call(t, r.cs, "set_new_map", map[string]any{"world_id": id, "map_exchange_string": str})
	if res.IsError {
		t.Fatalf("set_new_map: %s", resultText(res))
	}
	if out["seed"] == "" || out["seed"] == nil {
		t.Errorf("no seed in %s", asJSON(out))
	}
	if !r.config(id, world.MapGenSettingsFile).Found || !r.config(id, world.MapSettingsFile).Found {
		t.Error("the string's two files were not written")
	}
	if want := "Claude|alice|" + id + "|map exchange string"; strings.Join(r.newMapAudits(), ";") != want {
		t.Errorf("audit %q, want %q", r.newMapAudits(), want)
	}
}

// SPEC 5.6: a world with a save has its map made, so both tools refuse it;
// so do a world that never waited for a map and an archived one, with no
// planet run and no change.
func TestMCPNewMapRefusesAWorldThatCannotTakeIt(t *testing.T) {
	r := newNMRig(t)
	made := r.newMapWorld("Made", "2.0.77", false)
	r.copySave(made, "2.0.77-no-space-age.zip")
	plain := r.world20("Plain")
	archived := r.newMapWorld("Archived", "2.0.77", false)
	if _, err := r.worlds.SetArchived(context.Background(), archived, true); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, id, set, get string
	}{
		{"with a save", made, "already has a save", "already has a save"},
		{"not waiting for a map", plain, "not waiting for a new map", "not waiting for a new map"},
		{"archived", archived, "archived", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			tree, audits := dataStat(t, r.worlds.Dirs()), len(r.audit())
			res, _ := call(t, r.cs, "set_new_map", map[string]any{"world_id": c.id, "preset": "rich-resources"})
			if !res.IsError || !strings.Contains(resultText(res), c.set) {
				t.Errorf("set_new_map: %q, want %q", resultText(res), c.set)
			}
			res, _ = call(t, r.cs, "get_new_map", map[string]any{"world_id": c.id})
			if c.get == "" {
				if res.IsError {
					t.Errorf("get_new_map of an archived world: %s", resultText(res))
				}
			} else if !res.IsError || !strings.Contains(resultText(res), c.get) {
				t.Errorf("get_new_map: %q, want %q", resultText(res), c.get)
			}
			if diff := diffStat(tree, dataStat(t, r.worlds.Dirs())); diff != "" || len(r.audit()) != audits {
				t.Errorf("a refusal changed something: %s", diff)
			}
		})
	}
	for _, name := range []string{"get_new_map", "set_new_map"} {
		if res, _ := call(t, r.cs, name, map[string]any{"world_id": "nope"}); !res.IsError || !strings.Contains(resultText(res), "no world") {
			t.Errorf("%s of a missing world: %s", name, resultText(res))
		}
		if res, _ := call(t, r.cs, name, map[string]any{"world_id": ""}); !res.IsError {
			t.Errorf("%s without a world_id succeeded", name)
		}
	}
}

// SPEC 5.6: set_new_map is refused while the world's server is running or
// starting, and nothing changes.
func TestMCPSetNewMapRefusedWhileTheServerRuns(t *testing.T) {
	r := newNMRig(t)
	id := r.newMapWorld("Fresh", "2.1.24", false)
	r.startGame(id)
	tree, audits := dataStat(t, r.worlds.Dirs()), len(r.audit())
	res, _ := call(t, r.cs, "set_new_map", map[string]any{"world_id": id, "preset": "rich-resources"})
	if !res.IsError || !strings.Contains(resultText(res), "running or starting") {
		t.Errorf("set_new_map: %q", resultText(res))
	}
	if diff := diffStat(tree, dataStat(t, r.worlds.Dirs())); diff != "" || len(r.audit()) != audits {
		t.Errorf("a refusal changed something: %s", diff)
	}
}

// SPEC 5.6, 14.12: the planet list is the world's own: a mod's planet and
// control are listed, take values, and are unknown to a world without the mod.
func TestMCPNewMapOfAModdedWorld(t *testing.T) {
	r := newNMRig(t)
	modded := r.newMapWorld("Modded", "2.1.24", true, fakegame.FakePlanetMod)
	plain := r.newMapWorld("Plain", "2.1.24", true)

	_, out := call(t, r.cs, "get_new_map", map[string]any{"world_id": modded})
	if !hasPlanet(out, fakegame.FakePlanet) {
		t.Fatalf("no %s in %s", fakegame.FakePlanet, asJSON(out))
	}
	ore := planetControl(t, out, fakegame.FakePlanet, fakegame.FakeOre)
	if ore["has_richness"] != true || ore["can_be_disabled"] != true || ore["category"] != "resource" || ore["untrusted_label"] == "" {
		t.Errorf("%s: %s", fakegame.FakeOre, asJSON(ore))
	}
	_, out = call(t, r.cs, "get_new_map", map[string]any{"world_id": plain})
	if hasPlanet(out, fakegame.FakePlanet) {
		t.Errorf("the plain world lists %s", fakegame.FakePlanet)
	}

	args := func(id string) map[string]any {
		return map[string]any{"world_id": id, "seed": "5", "controls": []any{controlArgs(fakegame.FakePlanet, fakegame.FakeOre, "richness", 200, "enabled", true)}}
	}
	if res, _ := call(t, r.cs, "set_new_map", args(modded)); res.IsError {
		t.Fatalf("set on the modded world: %s", resultText(res))
	}
	if file := r.config(modded, world.MapGenSettingsFile); !strings.Contains(string(file.Data), `"`+fakegame.FakeOre+`"`) {
		t.Errorf("the mod's control is not in %s", file.Data)
	}
	res, _ := call(t, r.cs, "set_new_map", args(plain))
	if !res.IsError || !strings.Contains(resultText(res), `"`+fakegame.FakePlanet+`"`) {
		t.Errorf("set on the plain world: %q", resultText(res))
	}
	if r.config(plain, world.MapGenSettingsFile).Found {
		t.Error("the plain world was written")
	}
}

// SPEC 5.6: when the world's own planet list cannot be read, the built-in
// planets stand in and both tools say so.
func TestMCPNewMapSaysWhenTheBuiltInPlanetsStandIn(t *testing.T) {
	r := newNMRig(t)
	// 2.1.20 has no game image on this rig, so the planet run cannot start.
	id := r.newMapWorld("No image", "2.1.20", true, fakegame.FakePlanetMod)
	_, out := call(t, r.cs, "get_new_map", map[string]any{"world_id": id})
	if out["built_in_planets"] != true || out["note"] == nil || hasPlanet(out, fakegame.FakePlanet) || !hasPlanet(out, "nauvis") {
		t.Errorf("get_new_map: %s", asJSON(out))
	}
	res, set := call(t, r.cs, "set_new_map", map[string]any{"world_id": id, "preset": "marathon"})
	if res.IsError || set["built_in_planets"] != true {
		t.Errorf("set_new_map: %s %s", resultText(res), asJSON(set))
	}
}

// SPEC 5.6: a control two planets share is one value, set under each planet
// and listed with the others it changes; two different values for it are
// refused.
func TestNewMapSharedControl(t *testing.T) {
	cat := moddedCatalogue()
	cat.Planets[len(cat.Planets)-1].Controls = []string{"moshine_ore", "iron-ore"}
	ctl := cat.Controls["iron-ore"]
	ctl.Planets = append(slices.Clone(ctl.Planets), "moshine")
	cat.Controls["iron-ore"] = ctl
	l := layoutNewMap("2.1", cat)
	if got := l.controls["iron-ore"].planets; len(got) != 2 {
		t.Fatalf("iron-ore planets %v", got)
	}
	post, err := l.controlValues([]mcpSetNewControl{{Planet: "moshine", Control: "iron-ore", Size: new(300)}})
	if err != nil || post.Get(fieldName("nauvis", "iron-ore", "size")) != "300" || post.Get(fieldName("moshine", "iron-ore", "size")) != "300" {
		t.Errorf("post %v, err %v", post, err)
	}
	// The same value under both planets, as get_new_map lists it, is fine.
	if _, err := l.controlValues([]mcpSetNewControl{
		{Planet: "nauvis", Control: "iron-ore", Size: new(300)}, {Planet: "moshine", Control: "iron-ore", Size: new(300)}}); err != nil {
		t.Errorf("the same value twice: %v", err)
	}
	_, err = l.controlValues([]mcpSetNewControl{
		{Planet: "nauvis", Control: "iron-ore", Size: new(300)}, {Planet: "moshine", Control: "iron-ore", Size: new(200)}})
	if err == nil || !strings.Contains(err.Error(), `"nauvis"`) || !strings.Contains(err.Error(), "share") {
		t.Errorf("two values for a shared control: %v", err)
	}
}

// SPEC 5.6: the files set_new_map writes are the files the form makes from
// the same post, so the page and the tool save the same map.
func TestMCPNewMapPostMakesTheFormsFiles(t *testing.T) {
	cat := mapgen.Builtin("2.1", true)
	w := mcpNewMapWorld{meta: world.Meta{GameVersion: "2.1.24", SpaceAge: true}, line: "2.1", cat: cat, layout: layoutNewMap("2.1", cat)}
	post, err := mcpNewMapPost(w, mcpSetNewMapIn{Preset: "default", Seed: "9", Controls: []mcpSetNewControl{{Planet: "nauvis", Control: "iron-ore", Richness: new(400)}}})
	if err != nil {
		t.Fatal(err)
	}
	by := url.Values{}
	for k, v := range post {
		by[k] = v
	}
	f, files, ok := readMapForm(by, kindGenerate, mapInput{Line: "2.1", Cat: cat, MapKey: "2.1|1", Version: "2.1.24"})
	if !ok || !f.Modified || files.Preset != "" || files.Seed == nil || *files.Seed != 9 || !strings.Contains(string(files.MapGenSettings), `"richness": 4`) {
		t.Errorf("ok %v modified %v files %+v problems %v", ok, f.Modified, files, f.Problems())
	}
}
