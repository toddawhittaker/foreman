package web

import (
	"encoding/json"
	"maps"
	"net/url"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// A control gets an on/off box by rule, never by planet name: the game says
// it can be disabled and it is a resource or, on 2.x, cliffs or enemies
// (SPEC 5.6, DESIGN "Cliffs off").
func TestControlToggleFollowsTheRule(t *testing.T) {
	cases := []struct {
		line string
		c    mapgen.Control
		want bool
	}{
		{"2.1", mapgen.Control{Name: "muluna_cliff", Category: "cliff", CanBeDisabled: true}, true},
		{"2.1", mapgen.Control{Name: "corrundum-cliff", Category: "cliff", CanBeDisabled: true}, true},
		{"2.1", mapgen.Control{Name: "carna_cliff", Category: "cliff", CanBeDisabled: false}, false},
		{"2.1", mapgen.Control{Name: "foliax_cliff", Category: "cliff", CanBeDisabled: false}, false},
		{"2.1", mapgen.Control{Name: "test_enemy_base", Category: "enemy", CanBeDisabled: true}, true},
		{"2.0", mapgen.Control{Name: "test_enemy_base", Category: "enemy", CanBeDisabled: true}, true},
		{"2.1", mapgen.Control{Name: "lunar_rocks", Category: "terrain", CanBeDisabled: true}, false},
		{"2.1", mapgen.Control{Name: "iron-ore", Category: "resource", CanBeDisabled: true}, true},
		{"2.1", mapgen.Control{Name: "some-ore", Category: "resource", CanBeDisabled: false}, false},
		// 1.1 keeps its own path: enemy-base has no box.
		{"1.1", mapgen.Control{Name: "enemy-base", Category: "enemy", CanBeDisabled: true}, false},
	}
	for _, tc := range cases {
		mc := controlFor(tc.line, tc.c)
		if mc.toggle != tc.want {
			t.Errorf("%s %s (%s, can_be_disabled %v): box = %v, want %v", tc.line, tc.c.Name, tc.c.Category, tc.c.CanBeDisabled, mc.toggle, tc.want)
		}
	}
}

// A modded cliff or enemy box shows on the form and takes "None" out of its
// size select, as for the built-in planets.
func TestModdedCliffAndEnemyBoxesOnTheForm(t *testing.T) {
	cat := mapgen.Builtin("2.1", true)
	p := mapgen.Planet{Name: "testplanet"}
	for _, c := range []mapgen.Control{
		{Name: "testplanet-cliff", Category: "cliff", CanBeDisabled: true},
		{Name: "testplanet_rock_cliff", Category: "cliff", CanBeDisabled: false},
		{Name: "testplanet_enemy", Category: "enemy", CanBeDisabled: true},
		{Name: "testplanet_rocks", Category: "terrain", CanBeDisabled: true},
	} {
		p.Controls = append(p.Controls, c.Name)
		cat.Controls[c.Name] = mapgen.CatalogueControl{Control: c, Planets: []string{p.Name}}
	}
	cat.Planets = append(cat.Planets, p)
	f := buildMapGenFormIn("2.1", cat, func(string) (string, bool) { return "", false })
	groups := map[string]MapGenGroup{}
	for _, tab := range [][]MapGenPlanet{f.Terrain} {
		for _, p := range tab {
			for _, g := range p.Groups {
				groups[g.ID] = g
			}
		}
	}
	for _, g := range f.Enemy {
		groups[g.ID] = g
	}
	for id, want := range map[string]bool{
		"mg-testplanet-testplanet-cliff": true, "mg-testplanet-testplanet_rock_cliff": false,
		"mg-testplanet-testplanet_enemy": true, "mg-testplanet-testplanet_rocks": false,
		"mg-nauvis-nauvis_cliff": true, "mg-gleba-gleba_cliff": true, "mg-fulgora-fulgora_cliff": true,
		"mg-nauvis-enemy-base": false, "mg-gleba-gleba_enemy_base": false,
	} {
		g, ok := groups[id]
		if !ok {
			t.Errorf("%s: no group", id)
			continue
		}
		if (g.Toggle != nil) != want {
			t.Errorf("%s: box = %v, want %v", id, g.Toggle != nil, want)
		}
		for _, s := range g.Selects {
			if s.ID != id+"-size" {
				continue
			}
			none := false
			for _, o := range s.Options {
				if o.Value == mgNone {
					none = true
				}
			}
			if want && none {
				t.Errorf("%s: size still offers None beside its box", id)
			}
		}
	}
	if len(f.toggles()) == 0 {
		t.Error("toggles lists no box")
	}
}

// A modded enemy control with its box unticked saves at size 0, and the form
// read back shows the box off (SPEC 5.6).
func TestModdedEnemyBoxOffSavesSizeZero(t *testing.T) {
	cat := mapgen.Builtin("2.1", true)
	cat.Controls = maps.Clone(cat.Controls)
	c := mapgen.Control{Name: "testplanet_enemy", Category: mapgen.CategoryEnemy, CanBeDisabled: true}
	cat.Controls[c.Name] = mapgen.CatalogueControl{Control: c, Planets: []string{"testplanet"}}
	cat.Planets = append(slices.Clone(cat.Planets), mapgen.Planet{Name: "testplanet", Controls: []string{c.Name}})

	f, out, problems := parseMapGenIn(url.Values{fieldName("testplanet", c.Name, mgOn): {"off"}}, "2.1", cat)
	if len(problems) > 0 {
		t.Fatalf("problems %v", problems)
	}
	var saved struct {
		Controls map[string]struct {
			Size *float64 `json:"size"`
		} `json:"autoplace_controls"`
	}
	if err := json.Unmarshal(out, &saved); err != nil {
		t.Fatalf("saved settings are not JSON: %v\n%s", err, out)
	}
	if got := saved.Controls[c.Name].Size; got == nil || *got != 0 {
		t.Errorf("%s size = %v, want 0\n%s", c.Name, got, out)
	}
	if f.resourceOn("testplanet", c.Name) {
		t.Error("the form read back shows the enemy box ticked")
	}
}
