package resource

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// withSpots holds one ore patch, two spots of methane and one of a fluid
// that shares the ore's name, as the count writes the spots of fluid tiles
// tied to a resource control (SPEC 5.6): a tile count and no amount.
const withSpots = `{
  "planet": "nauvis",
  "resources": {
    "iron-ore": {"total": 300, "infinite": false, "normal": 0, "color": [104, 132, 146]}
  },
  "fluids": {
    "methane":  {"controls": ["tritium"], "color": [131, 110, 189]},
    "iron-ore": {"controls": ["deuterium", "other"], "color": [202, 196, 59]},
    "unseen":   {"controls": ["unseen"], "color": [0, 0, 0]}
  },
  "patches": [
    {"name": "iron-ore", "x": 30, "y": -30, "amount": 300, "n": 40, "box": [20, -40, 40, -20]},
    {"name": "methane", "fluid": true, "x": -20, "y": 10, "n": 12, "box": [-22, 8, -18, 12]},
    {"name": "methane", "fluid": true, "x": 0, "y": 0, "n": 312, "box": [-10, -10, 10, 10]},
    {"name": "iron-ore", "fluid": true, "x": 50, "y": 50, "n": 1, "box": [50, 50, 50, 50]}
  ]
}`

// A spot names a fluid, not a resource, so a fluid may share an ore's name,
// and the ore's total counts only the ore's patches.
func TestParseSpots(t *testing.T) {
	r, err := Parse([]byte(withSpots), 128, "nauvis")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Fluids) != 3 || len(r.Patches) != 4 {
		t.Fatalf("got %+v", r)
	}
	if got := r.Fluids["methane"]; !slices.Equal(got.Controls, []string{"tritium"}) || got.Color != [3]uint8{131, 110, 189} {
		t.Errorf("methane is %+v", got)
	}
	var spots []string
	for _, p := range r.Patches {
		if !p.Fluid {
			continue
		}
		if p.Amount != 0 || p.Normal != 0 {
			t.Errorf("spot %+v has an amount", p)
		}
		if _, ok := Yield(p); ok {
			t.Errorf("spot %+v has a yield", p)
		}
		spots = append(spots, fmt.Sprintf("%s:%d", p.Name, p.Entities))
	}
	// Sorted with the patches, largest box first.
	if want := []string{"methane:312", "methane:12", "iron-ore:1"}; !slices.Equal(spots, want) {
		t.Errorf("spots %v, want %v", spots, want)
	}
}

// A file written before spots were counted has no fluids and still decodes;
// an empty fluid table may be written as [] or {}.
func TestParseNoFluids(t *testing.T) {
	r, err := Parse([]byte(good), 128, "nauvis")
	if err != nil || len(r.Fluids) != 0 {
		t.Fatalf("got %+v, %v", r.Fluids, err)
	}
	for _, empty := range []string{`[]`, `{}`} {
		in := strings.Replace(good, `"patches"`, `"fluids": `+empty+`, "patches"`, 1)
		if _, err := Parse([]byte(in), 128, "nauvis"); err != nil {
			t.Errorf("fluids %s: %v", empty, err)
		}
	}
}

func TestParseSpotRefusals(t *testing.T) {
	rep := func(old, new string) string {
		if !strings.Contains(withSpots, old) {
			t.Fatalf("withSpots lacks %q", old)
		}
		return strings.Replace(withSpots, old, new, 1)
	}
	cases := map[string]string{
		"spot with an amount":    rep(`"fluid": true, "x": -20, "y": 10, "n": 12`, `"fluid": true, "x": -20, "y": 10, "amount": 5, "n": 12`),
		"spot of no fluid":       rep(`"name": "methane", "fluid": true, "x": -20`, `"name": "coal", "fluid": true, "x": -20`),
		"spot without tiles":     rep(`"n": 12`, `"n": 0`),
		"spot outside its box":   rep(`"x": -20, "y": 10, "n": 12`, `"x": -30, "y": 10, "n": 12`),
		"spot box past the edge": rep(`"n": 12, "box": [-22, 8, -18, 12]`, `"n": 12, "box": [-22, 8, -18, 64]`),
		"fluid as an ore":        rep(`"name": "methane", "fluid": true, "x": -20`, `"name": "methane", "x": -20`),
		"bad fluid name":         rep(`"methane":  {`, `"Methane":  {`),
		"no controls":            rep(`["tritium"]`, `[]`),
		"bad control name":       rep(`["tritium"]`, `["Tritium"]`),
		"control not a string":   rep(`["tritium"]`, `[1]`),
		"too many controls":      rep(`["tritium"]`, `["`+strings.Repeat(`a", "`, MaxFluidControls)+`a"]`),
		"fluid color short":      rep(`[131, 110, 189]`, `[131, 110]`),
		"fluid color over 255":   rep(`[131, 110, 189]`, `[131, 256, 189]`),
		"unknown fluid field":    rep(`"controls": ["tritium"],`, `"controls": ["tritium"], "total": 1,`),
		"fluids null":            `{"planet":"nauvis","resources":[],"fluids":null,"patches":[]}`,
		"fluids nonempty list":   `{"planet":"nauvis","resources":[],"fluids":[1],"patches":[]}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			err := parseErr(t, in)
			for _, leak := range []string{"methane", "Methane", "tritium", "Tritium", "coal"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error %q repeats file content %q", err, leak)
				}
			}
		})
	}
}

func TestParseMaxFluids(t *testing.T) {
	build := func(n int) string {
		fs := make([]string, n)
		for i := range fs {
			fs[i] = fmt.Sprintf(`"f%d":{"controls":["c"],"color":[0,0,0]}`, i)
		}
		return fmt.Sprintf(`{"planet":"nauvis","resources":{},"fluids":{%s},"patches":[]}`, strings.Join(fs, ","))
	}
	if _, err := Parse([]byte(build(MaxFluids)), 128, "nauvis"); err != nil {
		t.Errorf("MaxFluids fluids: %v", err)
	}
	if _, err := Parse([]byte(build(MaxFluids+1)), 128, "nauvis"); !errors.Is(err, ErrInvalid) {
		t.Errorf("over MaxFluids: %v", err)
	}
}

// A spot's label is hidden as a patch's is, by its tile count, and only
// beside a spot of the same fluid: an ore of the fluid's name never hides
// it, nor it the ore (SPEC 5.6).
func TestHiddenLabelsSpots(t *testing.T) {
	spot := func(name string, x0, x1, tiles int) Patch {
		return Patch{Name: name, Fluid: true, X: x0, Entities: tiles, Box: [4]int{x0, 0, x1, 0}}
	}
	ps := []Patch{
		spot("methane", 0, 10, 400),
		spot("methane", 20, 22, 100),
		spot("methane", 30, 32, 101),
		box("methane", 40, 42, 1),
		spot("iron-ore", 50, 52, 1),
		box("iron-ore", 60, 62, 100000),
	}
	if got, want := HiddenLabels(ps), []bool{false, true, false, false, false, false}; !slices.Equal(got, want) {
		t.Errorf("hidden %v, want %v", got, want)
	}
}

// SPEC 5.6: the count keeps the largest spots that fit Parse's limits beside
// the ores and says how many it left out. A file at MaxPatches with spots
// and spots_cut decodes, and spots_cut is read back; a negative one is
// refused, and a file without it has none cut.
func TestParseSpotsAtTheCap(t *testing.T) {
	build := func(cut string) string {
		ps := make([]string, MaxPatches)
		ps[0] = `{"name":"a","x":0,"y":0,"amount":1,"n":1,"box":[0,0,0,0]}`
		for i := 1; i < MaxPatches; i++ {
			ps[i] = fmt.Sprintf(`{"name":"methane","fluid":true,"x":%d,"y":%d,"n":%d,"box":[%d,%d,%d,%d]}`,
				i%64, i/64%64, 1+i%9, i%64, i/64%64, i%64, i/64%64)
		}
		return `{"planet":"nauvis","resources":{"a":{"total":1,"infinite":false,"normal":0,"color":[0,0,0]}},` +
			`"fluids":{"methane":{"controls":["tritium"],"color":[1,2,3]}},"spots_cut":` + cut +
			`,"patches":[` + strings.Join(ps, ",") + `]}`
	}
	in := build("1234")
	if len(in) > MaxJSON {
		t.Fatalf("the file is %d bytes, over MaxJSON", len(in))
	}
	r, err := Parse([]byte(in), 128, "nauvis")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Patches) != MaxPatches || r.SpotsCut != 1234 {
		t.Errorf("%d patches, %d spots cut", len(r.Patches), r.SpotsCut)
	}
	if _, err := Parse([]byte(build("-1")), 128, "nauvis"); !errors.Is(err, ErrInvalid) {
		t.Errorf("a negative spots_cut: %v", err)
	}
	r, err = Parse([]byte(withSpots), 128, "nauvis")
	if err != nil || r.SpotsCut != 0 {
		t.Errorf("no spots_cut: %d, %v", r.SpotsCut, err)
	}
}
