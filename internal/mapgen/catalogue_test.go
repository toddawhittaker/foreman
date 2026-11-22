package mapgen

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

// One decode serves every language: each labelled copy has its own labels
// and leaves the others as they were.
func TestDecodeOnceLabelPerLanguage(t *testing.T) {
	data := []byte(`{"planets":[{"name":"moon","localised_name":["space-location-name.moon"],"autoplace_controls":{"rock":{}}}],
	  "autoplace_controls":[{"name":"rock","category":"resource","localised_name":["autoplace-control-names.rock"]}],
	  "resources":[{"name":"rock","control":"rock","localised_name":["entity-name.rock"]}]}`)
	f, err := DecodeWorldPlanets("2.1", data)
	if err != nil {
		t.Fatal(err)
	}
	en := f.Labelled(modsettings.LocaleSections{"space-location-name": {"moon": "Moon"}, "autoplace-control-names": {"rock": "Rock"}, "entity-name": {"rock": "Rock ore"}})
	de := f.Labelled(modsettings.LocaleSections{"space-location-name": {"moon": "Mond"}})
	if en.Planets[0].Label != "Moon" || en.Controls["rock"].Label != "Rock" || en.Resources[0].Label != "Rock ore" {
		t.Errorf("English labels %+v", en)
	}
	if de.Planets[0].Label != "Mond" || de.Controls["rock"].Label != "rock" || de.Resources[0].Label != "rock" {
		t.Errorf("German labels %+v", de)
	}
	if en.Planets[0].Label != "Moon" {
		t.Error("labelling German changed the English copy")
	}
	want, _ := ParseWorldPlanets("2.1", data, modsettings.LocaleSections{"space-location-name": {"moon": "Mond"}})
	if !reflect.DeepEqual(want, de) {
		t.Error("ParseWorldPlanets differs from decode then label")
	}
}

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
	c, err := ParseWorldPlanets("2.0", []byte(twoX), sec)
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
	c, err := ParseWorldPlanets("1.1", []byte(data))
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
	perPlanet := `{"a":{}`
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
		if _, err := ParseWorldPlanets("2.0", []byte(data)); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "nauvis") {
			t.Errorf("%s: error carries file text", name)
		}
	}
	// Bad names are left out and counted, not refused.
	const res = `,"category":"resource"`
	c, err := ParseWorldPlanets("1.1", []byte(`{"autoplace_controls":[{"name":"`+strings.Repeat("a", 65)+
		`"`+res+`},{"name":"ok"`+res+`},{"name":"ok"`+res+`},{"name":"<script>"`+res+`},{"name":""`+res+`}]}`))
	if err != nil || c.Dropped != 4 || len(c.Controls) != 1 {
		t.Errorf("bad names: err=%v dropped=%d controls=%d", err, c.Dropped, len(c.Controls))
	}
	// A long localised name is cut.
	c, err = ParseWorldPlanets("1.1", []byte(`{"autoplace_controls":[{"name":"a","category":"resource","localised_name":["s.a"]}]}`),
		modsettings.LocaleSections{"s": {"a": strings.Repeat("x", 300)}})
	if err != nil || len([]rune(c.Controls["a"].Label)) != modsettings.MaxLabelChars {
		t.Errorf("long label: err=%v label len=%d", err, len(c.Controls["a"].Label))
	}
}

// A planet's own resources list is capped, and a file near the byte limit
// full of distinct names is refused quickly (SPEC 14.6).
func TestParseWorldPlanetsRefusesLongResourceList(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"planets":[{"name":"p","resources":[`)
	for i := range 100000 {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"r%d"`, i)
	}
	b.WriteString(`]}]}`)
	if b.Len() > MaxPlanetFileBytes {
		t.Fatalf("test file is %d bytes, over the limit", b.Len())
	}
	start := time.Now()
	if _, err := ParseWorldPlanets("2.0", []byte(b.String())); err == nil {
		t.Error("a planet with too many resources was accepted")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestParseWorldPlanetsControlFields(t *testing.T) {
	long := strings.Repeat("o", 65)
	data := `{"planets":[{"name":"p","autoplace_controls":{"a":{},"b":{},"c":{},"d":{},"e":{},"f":{},"g":{},"h":{}}}],
	 "autoplace_controls":[
	  {"name":"a","category":"resource"},{"name":"b","category":"terrain"},
	  {"name":"c","category":"cliff"},{"name":"d","category":"enemy"},
	  {"name":"e","category":"bogus"},{"name":"f"},
	  {"name":"g","category":"resource","order":"` + long + `"},
	  {"name":"h","category":"resource","order":"` + long[:64] + `"}]}`
	c, err := ParseWorldPlanets("2.0", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	// e, f and g are left out (3), and the planet's three references to them
	// are counted too (3).
	if c.Dropped != 6 {
		t.Errorf("Dropped = %d, want 6", c.Dropped)
	}
	if got := c.Planets[0].Controls; !slices.Equal(got, []string{"a", "b", "c", "d", "h"}) {
		t.Errorf("planet controls = %v", got)
	}
}

func TestParseWorldPlanetsDefaultsInRange(t *testing.T) {
	data := `{"planets":[{"name":"p","autoplace_controls":{
	  "a":{"frequency":0,"size":1000,"richness":150},
	  "b":{"frequency":1000.5,"size":-1,"richness":1e9},
	  "c":{"frequency":0.5}}}],
	 "autoplace_controls":[{"name":"a","category":"resource"},{"name":"b","category":"resource"},{"name":"c","category":"resource"}]}`
	c, err := ParseWorldPlanets("2.0", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	d := c.Planets[0].Defaults
	if a := d["a"]; a.Frequency == nil || *a.Frequency != 0 || a.Size == nil || *a.Size != 1000 || a.Richness == nil || *a.Richness != 150 {
		t.Errorf("in-range values were changed: %+v", a)
	}
	if b := d["b"]; b.Frequency != nil || b.Size != nil || b.Richness != nil {
		t.Errorf("out-of-range values were kept: %+v", b)
	}
	if cc := d["c"]; cc.Frequency == nil || *cc.Frequency != 0.5 || cc.Size != nil {
		t.Errorf("c = %+v", cc)
	}
}

// The line decides whether the file describes planets; an empty planet list
// on 2.x is a file that did not export them, not a 1.1 world (SPEC 14.6).
func TestParseWorldPlanetsLine(t *testing.T) {
	ctl := `"autoplace_controls":[{"name":"a","category":"resource"}]`
	for _, data := range []string{`{` + ctl + `}`, `{"planets":{},` + ctl + `}`, `{"planets":[{"name":"Bad Name"}],` + ctl + `}`} {
		if _, err := ParseWorldPlanets("2.0", []byte(data)); err == nil {
			t.Errorf("2.x file with no usable planet was accepted: %s", data)
		}
	}
	c, err := ParseWorldPlanets("1.1", []byte(`{`+ctl+`}`))
	if err != nil || !c.NoPlanets || len(c.Planets) != 1 || c.Planets[0].Name != "nauvis" {
		t.Errorf("1.1: err=%v catalogue=%+v", err, c)
	}
}

func TestParseWorldPlanetsResourceControlMustBeUsed(t *testing.T) {
	data := `{"planets":[{"name":"p","autoplace_controls":{"used":{}}}],
	 "autoplace_controls":[{"name":"used","category":"resource"},{"name":"idle","category":"resource"}],
	 "resources":[{"name":"r1","control":"used"},{"name":"r2","control":"idle"}]}`
	c, err := ParseWorldPlanets("2.0", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if c.Resources[0].Control != "used" || c.Resources[1].Control != "" {
		t.Errorf("resources = %+v", c.Resources)
	}
}

func TestValidName(t *testing.T) {
	for s, want := range map[string]bool{
		"iron-ore": true, "a_b-9": true, strings.Repeat("a", 64): true,
		"": false, strings.Repeat("a", 65): false, "Upper": false, "a b": false, "a\n": false, "é": false,
	} {
		if got := ValidName(s); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", s, got, want)
		}
	}
}
