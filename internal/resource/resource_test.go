package resource

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
)

// good is a valid file for a 128 picture (tiles -64 to 63) of nauvis.
const good = `{
  "planet": "nauvis",
  "resources": {
    "iron-ore":  {"total": 300, "infinite": false, "normal": 0, "color": [104, 132, 146]},
    "crude-oil": {"total": 900000, "infinite": true, "normal": 300000, "color": [194, 52, 173]}
  },
  "patches": [
    {"name": "iron-ore", "x": -20, "y": 10, "amount": 100, "n": 4, "box": [-22, 8, -18, 12]},
    {"name": "iron-ore", "x": 30, "y": -30, "amount": 200, "n": 40, "box": [20, -40, 40, -20]},
    {"name": "crude-oil", "x": 5, "y": 5, "amount": 900000, "n": 3, "box": [0, 0, 10, 10]}
  ]
}`

func parseErr(t *testing.T, in string) error {
	t.Helper()
	_, err := Parse([]byte(in), 128, "nauvis")
	if err == nil {
		t.Fatalf("Parse accepted:\n%s", in)
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error %v does not wrap ErrInvalid", err)
	}
	return err
}

func TestParseGood(t *testing.T) {
	r, err := Parse([]byte(good), 128, "nauvis")
	if err != nil {
		t.Fatal(err)
	}
	if r.Planet != "nauvis" || len(r.Resources) != 2 || len(r.Patches) != 3 {
		t.Fatalf("got %+v", r)
	}
	if got := r.Resources["iron-ore"]; got.Total != 300 || got.Infinite || got.Normal != 0 || got.Color != [3]uint8{104, 132, 146} {
		t.Errorf("iron-ore is %+v", got)
	}
	if got := r.Resources["crude-oil"]; got.Total != 900000 || !got.Infinite || got.Normal != 300000 || got.Color != [3]uint8{194, 52, 173} {
		t.Errorf("crude-oil is %+v", got)
	}
	// Sorted by box area, largest first: 21x21, 11x11, 5x5.
	var names []string
	for _, p := range r.Patches {
		names = append(names, fmt.Sprintf("%s@%d,%d", p.Name, p.X, p.Y))
	}
	want := []string{"iron-ore@30,-30", "crude-oil@5,5", "iron-ore@-20,10"}
	if !slices.Equal(names, want) {
		t.Errorf("order %v, want %v", names, want)
	}
	p := r.Patches[0]
	if p.Amount != 200 || p.Entities != 40 || p.Box != [4]int{20, -40, 40, -20} {
		t.Errorf("first patch %+v", p)
	}
}

func TestParseSortTies(t *testing.T) {
	in := `{"planet":"nauvis","resources":{
		"b":{"total":1,"infinite":false,"normal":0,"color":[0,0,0]},
		"a":{"total":3,"infinite":false,"normal":0,"color":[0,0,0]}},
	"patches":[
		{"name":"b","x":1,"y":1,"amount":1,"n":1,"box":[0,0,2,2]},
		{"name":"a","x":2,"y":1,"amount":1,"n":1,"box":[0,0,2,2]},
		{"name":"a","x":1,"y":2,"amount":1,"n":1,"box":[0,0,2,3]},
		{"name":"a","x":1,"y":1,"amount":1,"n":1,"box":[0,0,2,2]}]}`
	r, err := Parse([]byte(in), 128, "nauvis")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range r.Patches {
		got = append(got, fmt.Sprintf("%s%d%d", p.Name, p.X, p.Y))
	}
	// The 3x4 box first, then the 3x3 boxes by name, x, then y.
	want := []string{"a12", "a11", "a21", "b11"}
	if !slices.Equal(got, want) {
		t.Errorf("order %v, want %v", got, want)
	}
}

func TestParseEmptyTables(t *testing.T) {
	for _, in := range []string{
		`{"planet":"nauvis","resources":[],"patches":[]}`,
		`{"planet":"nauvis","resources":{},"patches":{}}`,
		`{"planet":"nauvis","resources": [ ] ,"patches": { } }`,
		`{"planet":"nauvis","resources":{"iron-ore":{"total":0,"infinite":false,"normal":0,"color":[1,2,3]}},"patches":[]}`,
	} {
		r, err := Parse([]byte(in), 128, "nauvis")
		if err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if len(r.Patches) != 0 {
			t.Errorf("%s: patches %v", in, r.Patches)
		}
	}
}

func TestParseNumberForms(t *testing.T) {
	// Lua may write a whole number with an exponent or a trailing zero.
	in := strings.NewReplacer(`"total": 300`, `"total": 3e2`, `"amount": 100`, `"amount": 100.0`).Replace(good)
	if _, err := Parse([]byte(in), 128, "nauvis"); err != nil {
		t.Error(err)
	}
}

func TestParseRefusals(t *testing.T) {
	rep := func(old, new string) string {
		if !strings.Contains(good, old) {
			t.Fatalf("good lacks %q", old)
		}
		return strings.Replace(good, old, new, 1)
	}
	cases := map[string]string{
		"empty":                   ``,
		"not json":                `nope`,
		"array":                   `[]`,
		"null":                    `null`,
		"trailing data":           good + ` {}`,
		"unknown field":           rep(`"planet"`, `"extra": 1, "planet"`),
		"unknown patch field":     rep(`"n": 4,`, `"n": 4, "infinite": false,`),
		"unknown resource field":  rep(`"normal": 0,`, `"normal": 0, "x": 1,`),
		"wrong planet":            rep(`"planet": "nauvis"`, `"planet": "vulcanus"`),
		"resources null":          `{"planet":"nauvis","resources":null,"patches":[]}`,
		"patches null":            `{"planet":"nauvis","resources":[],"patches":null}`,
		"resources nonempty list": `{"planet":"nauvis","resources":[1],"patches":[]}`,
		"patches nonempty object": `{"planet":"nauvis","resources":[],"patches":{"a":1}}`,
		"resources a string":      `{"planet":"nauvis","resources":"","patches":[]}`,
		"no planet":               rep(`"planet": "nauvis",`, ``),
		"upper case name":         rep(`"name": "iron-ore", "x": -20`, `"name": "Iron-ore", "x": -20`),
		"bad name char":           rep(`"name": "iron-ore", "x": -20`, `"name": "iron ore", "x": -20`),
		"empty name":              rep(`"name": "iron-ore", "x": -20`, `"name": "", "x": -20`),
		"long name":               rep(`"name": "iron-ore", "x": -20`, `"name": "`+strings.Repeat("a", 65)+`", "x": -20`),
		"bad resource key":        rep(`"iron-ore":  {`, `"iron ore":  {`),
		"patch of unknown":        rep(`"name": "iron-ore", "x": -20`, `"name": "coal", "x": -20`),
		"fraction":                rep(`"amount": 100,`, `"amount": 100.5,`),
		"negative amount":         rep(`"amount": 100,`, `"amount": -100,`),
		"string amount":           rep(`"amount": 100,`, `"amount": "100",`),
		"huge amount":             rep(`"amount": 100,`, `"amount": 9007199254740993,`),
		"overflowing amount":      rep(`"amount": 100,`, `"amount": 1e999,`),
		"huge total":              rep(`"total": 900000`, `"total": 9007199254740993`),
		"fraction total":          rep(`"total": 300`, `"total": 300.5`),
		"missing amount":          rep(`"amount": 100, `, ``),
		"zero entities":           rep(`"n": 4`, `"n": 0`),
		"fraction entities":       rep(`"n": 4`, `"n": 4.5`),
		"negative entities":       rep(`"n": 4`, `"n": -1`),
		"x past the right edge":   rep(`"x": -20, "y": 10`, `"x": 64, "y": 10`),
		"x past the left edge":    rep(`"x": -20, "y": 10`, `"x": -65, "y": 10`),
		"y past the bottom":       rep(`"x": -20, "y": 10`, `"x": -20, "y": 64`),
		"fractional x":            rep(`"x": -20, "y": 10`, `"x": -20.5, "y": 10`),
		"box too short":           rep(`"box": [-22, 8, -18, 12]`, `"box": [-22, 8, -18]`),
		"box too long":            rep(`"box": [-22, 8, -18, 12]`, `"box": [-22, 8, -18, 12, 0]`),
		"box object":              rep(`"box": [-22, 8, -18, 12]`, `"box": {}`),
		"box min over max x":      rep(`"box": [-22, 8, -18, 12]`, `"box": [-18, 8, -22, 12]`),
		"box min over max y":      rep(`"box": [-22, 8, -18, 12]`, `"box": [-22, 12, -18, 8]`),
		"box past the edge":       rep(`"box": [-22, 8, -18, 12]`, `"box": [-22, 8, -18, 64]`),
		"box before the edge":     rep(`"box": [-22, 8, -18, 12]`, `"box": [-65, 8, -18, 12]`),
		"box fraction":            rep(`"box": [-22, 8, -18, 12]`, `"box": [-22, 8, -18, 12.5]`),
		"label outside its box":   rep(`"box": [-22, 8, -18, 12]`, `"box": [-17, 8, -10, 12]`),
		"color short":             rep(`[104, 132, 146]`, `[104, 132]`),
		"color long":              rep(`[104, 132, 146]`, `[104, 132, 146, 1]`),
		"color over 255":          rep(`[104, 132, 146]`, `[104, 256, 146]`),
		"color negative":          rep(`[104, 132, 146]`, `[104, -1, 146]`),
		"color fraction":          rep(`[104, 132, 146]`, `[104, 132.5, 146]`),
		"infinite without normal": rep(`"normal": 300000`, `"normal": 0`),
		"ore with normal":         rep(`"normal": 0`, `"normal": 5`),
		"fraction normal":         rep(`"normal": 300000`, `"normal": 300000.5`),
		"sum under total":         rep(`"total": 300`, `"total": 301`),
		"sum over total":          rep(`"total": 300`, `"total": 299`),
		"resource without patch":  rep(`"resources": {`, `"resources": {"coal": {"total": 5, "infinite": false, "normal": 0, "color": [0,0,0]},`),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if in == good {
				t.Fatal("case did not change the file")
			}
			err := parseErr(t, in)
			// The error may carry counts, never the file's own text.
			for _, leak := range []string{"vulcanus", "iron ore", "Iron-ore", "extra", "coal"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error %q repeats file content %q", err, leak)
				}
			}
		})
	}
}

func TestParseTotalsAtTheCap(t *testing.T) {
	max := int64(1) << 53
	in := fmt.Sprintf(`{"planet":"nauvis","resources":{"a":{"total":%d,"infinite":false,"normal":0,"color":[0,0,0]}},
	"patches":[{"name":"a","x":0,"y":0,"amount":%d,"n":1,"box":[0,0,1,1]}]}`, max, max)
	if _, err := Parse([]byte(in), 128, "nauvis"); err != nil {
		t.Fatal(err)
	}
	// Two patches that together wrap a 64-bit sum must not pass as equal.
	in = fmt.Sprintf(`{"planet":"nauvis","resources":{"a":{"total":5,"infinite":false,"normal":0,"color":[0,0,0]}},
	"patches":[{"name":"a","x":0,"y":0,"amount":%d,"n":1,"box":[0,0,1,1]},{"name":"a","x":0,"y":0,"amount":%d,"n":1,"box":[0,0,1,1]}]}`, max, max)
	_ = parseErr(t, in)
}

func TestParseEdges(t *testing.T) {
	// The picture covers [-64, 64): -64 is in, 63 is in.
	in := `{"planet":"nauvis","resources":{"a":{"total":2,"infinite":false,"normal":0,"color":[0,255,0]}},
	"patches":[{"name":"a","x":-64,"y":-64,"amount":1,"n":1,"box":[-64,-64,-64,-64]},
	{"name":"a","x":63,"y":63,"amount":1,"n":1,"box":[63,63,63,63]}]}`
	if _, err := Parse([]byte(in), 128, "nauvis"); err != nil {
		t.Fatal(err)
	}
}

func TestParseSize(t *testing.T) {
	empty := `{"planet":"nauvis","resources":[],"patches":[]}`
	for _, size := range []int{0, -64, 32, 100, 3136, 4096} {
		if _, err := Parse([]byte(empty), size, "nauvis"); !errors.Is(err, ErrInvalid) {
			t.Errorf("size %d: error %v", size, err)
		}
	}
	for _, size := range []int{64, 128, 1280, 3072} {
		if _, err := Parse([]byte(empty), size, "nauvis"); err != nil {
			t.Errorf("size %d: %v", size, err)
		}
	}
}

func TestParseLimits(t *testing.T) {
	if MaxJSON != 1<<20 || MaxPatches != 5000 {
		t.Fatalf("limits are %d and %d", MaxJSON, MaxPatches)
	}
	big := good + strings.Repeat(" ", MaxJSON)
	if _, err := Parse([]byte(big), 128, "nauvis"); !errors.Is(err, ErrInvalid) {
		t.Errorf("over MaxJSON: %v", err)
	}
	patch := func(i int) string {
		return fmt.Sprintf(`{"name":"a","x":%d,"y":0,"amount":1,"n":1,"box":[%d,0,%d,0]}`, i%64, i%64, i%64)
	}
	build := func(n int) string {
		ps := make([]string, n)
		for i := range ps {
			ps[i] = patch(i)
		}
		return fmt.Sprintf(`{"planet":"nauvis","resources":{"a":{"total":%d,"infinite":false,"normal":0,"color":[0,0,0]}},"patches":[%s]}`, n, strings.Join(ps, ","))
	}
	if _, err := Parse([]byte(build(MaxPatches)), 128, "nauvis"); err != nil {
		t.Errorf("MaxPatches patches: %v", err)
	}
	if _, err := Parse([]byte(build(MaxPatches+1)), 128, "nauvis"); !errors.Is(err, ErrInvalid) {
		t.Errorf("over MaxPatches: %v", err)
	}
}

func TestYield(t *testing.T) {
	r, err := Parse([]byte(good), 128, "nauvis")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range r.Patches {
		y, ok := Yield(p)
		switch p.Name {
		case "crude-oil":
			if !ok || math.Abs(y-300) > 1e-9 {
				t.Errorf("oil yield %v, %v; want 300, true", y, ok)
			}
		default:
			if ok || y != 0 {
				t.Errorf("ore yield %v, %v; want 0, false", y, ok)
			}
		}
	}
	if _, ok := Yield(Patch{}); ok {
		t.Error("an empty patch has a yield")
	}
}

func TestControlName(t *testing.T) {
	for _, c := range []struct{ planet, entity, want string }{
		{"nauvis", "iron-ore", "iron-ore"},
		{"nauvis", "crude-oil", "crude-oil"},
		{"vulcanus", "vulcanus_coal", "coal"},
		{"gleba", "gleba_stone", "stone"},
		{"aquilo", "aquilo_crude_oil", "crude-oil"},
		{"vulcanus", "tungsten_ore", "tungsten-ore"},
		{"aquilo", "lithium_brine", "lithium-brine"},
		{"vulcanus", "gleba_stone", "gleba-stone"},
		{"vulcanus", "vulcanus_", "vulcanus-"},
		{"vulcanus", "vulcanus", "vulcanus"},
		{"vulcanus", "vulcanusx_coal", "vulcanusx-coal"},
		{"", "a_b", "a-b"},
	} {
		if got := ControlName(c.planet, c.entity); got != c.want {
			t.Errorf("ControlName(%q, %q) = %q, want %q", c.planet, c.entity, got, c.want)
		}
	}
}
