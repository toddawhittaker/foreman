package mapgen

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

func TestBuiltinMatchesEmbeddedData(t *testing.T) {
	for _, line := range Lines {
		for _, sa := range []bool{false, true} {
			c := Builtin(line, sa)
			if got, want := c.PlanetNames(), Planets(line, sa); !slices.Equal(got, want) {
				t.Errorf("%s sa=%v planets = %v, want %v", line, sa, got, want)
			}
			for _, p := range Planets(line, sa) {
				if got, want := c.PlanetControls(p), Controls(line, p); !reflect.DeepEqual(got, want) {
					t.Errorf("%s %s controls = %v, want %v", line, p, got, want)
				}
			}
			if c.NoPlanets != (line == "1.1") {
				t.Errorf("%s NoPlanets = %v", line, c.NoPlanets)
			}
		}
	}
}

func TestBuiltinSharedControl(t *testing.T) {
	c := Builtin("2.0", true)
	for name, cc := range c.Controls {
		for _, p := range cc.Planets {
			if len(c.AlsoChanges(name, p)) != len(cc.Planets)-1 {
				t.Fatalf("AlsoChanges(%s, %s) wrong", name, p)
			}
		}
	}
}

const twoX = `{
 "planets": [
  {"name": "vulcanus", "localised_name": ["space-location-name.vulcanus"],
   "autoplace_controls": {"calcite": {"richness": 1}, "vulcanus_volcanism": {}}, "resources": ["calcite"]},
  {"name": "corrundum", "autoplace_controls": {"calcite": {}, "missing": {}}},
  {"name": "Bad Name", "autoplace_controls": {}},
  {"name": "maraxsis-trench", "autoplace_controls": {}},
  {"name": "moshine", "autoplace_controls": {"steam_geyser": {"richness": 150}}, "extra": [1,2]}
 ],
 "autoplace_controls": [
  {"name": "vulcanus_volcanism", "category": "terrain", "can_be_disabled": false, "order": "b"},
  {"name": "calcite", "category": "resource", "richness": true, "can_be_disabled": true, "order": "a",
   "localised_name": ["autoplace-control-names.calcite"]},
  {"name": "steam_geyser", "category": "resource", "richness": true, "order": "c"},
  {"name": "BAD!", "category": "resource"}
 ],
 "resources": [{"name": "calcite", "control": "", "localised_name": ["entity-name.calcite"]}],
 "unknown": {"deep": [[[1]]]}
}`

func TestParseWorldPlanets2x(t *testing.T) {
	sec := modsettings.LocaleSections{
		"space-location-name":     {"vulcanus": "[planet=vulcanus] Vulcanus"},
		"autoplace-control-names": {"calcite": "[entity=calcite] Calcite"},
	}
	c, err := ParseWorldPlanets([]byte(twoX), sec)
	if err != nil {
		t.Fatal(err)
	}
	if c.NoPlanets {
		t.Fatal("NoPlanets on a 2.x file")
	}
	if got, want := c.PlanetNames(), []string{"vulcanus", "corrundum", "maraxsis-trench", "moshine"}; !slices.Equal(got, want) {
		t.Fatalf("planets = %v, want %v", got, want)
	}
	// Bad planet, bad control, corrundum's "missing".
	if c.Dropped != 3 {
		t.Errorf("Dropped = %d, want 3", c.Dropped)
	}
	if c.Planets[0].Label != "Vulcanus" || c.Controls["calcite"].Label != "Calcite" {
		t.Errorf("labels %q %q", c.Planets[0].Label, c.Controls["calcite"].Label)
	}
	if got := c.Planets[0].Controls; !slices.Equal(got, []string{"calcite", "vulcanus_volcanism"}) {
		t.Errorf("vulcanus controls in order = %v", got)
	}
	if got := c.Controls["calcite"].Planets; !slices.Equal(got, []string{"vulcanus", "corrundum"}) {
		t.Errorf("calcite planets = %v", got)
	}
	if got := c.AlsoChanges("calcite", "vulcanus"); !slices.Equal(got, []string{"corrundum"}) {
		t.Errorf("AlsoChanges = %v", got)
	}
	if c.AlsoChanges("vulcanus_volcanism", "vulcanus") != nil {
		t.Error("unshared control reports others")
	}
	if len(c.Planets[2].Controls) != 0 {
		t.Error("trench should have no controls")
	}
	if r := c.Planets[3].Defaults["steam_geyser"].Richness; r == nil || *r != 150 {
		t.Errorf("moshine steam_geyser richness = %v", r)
	}
	if cc := c.Controls["vulcanus_volcanism"]; cc.CanBeDisabled || cc.Category != CategoryTerrain {
		t.Errorf("volcanism = %+v", cc)
	}
	if len(c.Resources) != 1 || c.Resources[0].Label != "calcite" {
		t.Errorf("resources = %+v", c.Resources)
	}
}

func TestParseWorldPlanets11(t *testing.T) {
	data := `{"planets": {}, "autoplace_controls": [
	  {"name": "stone", "category": "resource", "richness": true, "order": "b"},
	  {"name": "iron-ore", "category": "resource", "richness": true, "order": "a"}],
	 "resources": [{"name": "iron-ore", "control": "iron-ore"}, {"name": "x", "control": "nope"}]}`
	c, err := ParseWorldPlanets([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if !c.NoPlanets || len(c.Planets) != 1 || c.Planets[0].Name != "nauvis" {
		t.Fatalf("1.1 surface = %+v", c.Planets)
	}
	if got := c.Planets[0].Controls; !slices.Equal(got, []string{"iron-ore", "stone"}) {
		t.Errorf("surface controls = %v", got)
	}
	if c.Resources[0].Control != "iron-ore" || c.Resources[1].Control != "" {
		t.Errorf("resource controls = %+v", c.Resources)
	}
}

func TestParseWorldPlanetsHostile(t *testing.T) {
	list := func(n int, f func(i int) string) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = f(i)
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	ctl := func(i int) string { return fmt.Sprintf(`{"name":"c%d"}`, i) }
	perPlanet := "{" + strings.TrimSuffix(strings.Repeat(`"a":{},`, 1), ",")
	for i := range MaxPlanetControls + 1 {
		perPlanet += fmt.Sprintf(`,"c%d":{}`, i)
	}
	perPlanet += "}"
	cases := map[string]string{
		"oversized":      `{"x":"` + strings.Repeat("a", MaxPlanetFileBytes) + `"}`,
		"planets":        `{"planets":` + list(MaxPlanets+1, func(i int) string { return fmt.Sprintf(`{"name":"p%d"}`, i) }) + `}`,
		"controls":       `{"autoplace_controls":` + list(MaxControls+1, ctl) + `}`,
		"resources":      `{"resources":` + list(MaxResources+1, ctl) + `}`,
		"per planet":     `{"planets":[{"name":"p","autoplace_controls":` + perPlanet + `}]}`,
		"deep nesting":   `{"x":` + strings.Repeat("[", 20000) + strings.Repeat("]", 20000) + `}`,
		"wrong type":     `{"planets":"nauvis"}`,
		"name not text":  `{"autoplace_controls":[{"name":5}]}`,
		"bool as string": `{"autoplace_controls":[{"name":"a","richness":"yes"}]}`,
		"not object":     `[]`,
		"trailing":       `{} {}`,
		"nonempty map":   `{"planets":{"a":1}}`,
		"empty":          ``,
	}
	for name, data := range cases {
		if _, err := ParseWorldPlanets([]byte(data)); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "nauvis") {
			t.Errorf("%s: error carries file text", name)
		}
	}
	// Bad names are left out and counted, not refused.
	c, err := ParseWorldPlanets([]byte(`{"autoplace_controls":[{"name":"` + strings.Repeat("a", 65) +
		`"},{"name":"ok"},{"name":"ok"},{"name":"<script>"},{"name":""}]}`))
	if err != nil || c.Dropped != 4 || len(c.Controls) != 1 {
		t.Errorf("bad names: err=%v dropped=%d controls=%d", err, c.Dropped, len(c.Controls))
	}
	// A long localised name is cut.
	c, err = ParseWorldPlanets([]byte(`{"autoplace_controls":[{"name":"a","localised_name":["s.a"]}]}`),
		modsettings.LocaleSections{"s": {"a": strings.Repeat("x", 300)}})
	if err != nil || len([]rune(c.Controls["a"].Label)) != modsettings.MaxLabelChars {
		t.Errorf("long label: err=%v label len=%d", err, len(c.Controls["a"].Label))
	}
}
