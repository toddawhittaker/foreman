package mapgen

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

const spaceAgeMap = `{
 "locations": [
  {"name": "nauvis", "localised_name": ["space-location-name.nauvis"], "distance": 15, "orientation": 0.25, "magnitude": 1.2, "label_orientation": 0.5, "starmap_icon": true},
  {"name": "vulcanus", "distance": 10, "orientation": 0.1},
  {"name": "secret", "distance": 5, "orientation": 0.5, "hidden": true},
  {"name": "fake-moon", "distance": 11, "orientation": 0.12, "parent": "vulcanus"}
 ],
 "routes": [
  {"from": "nauvis", "to": "vulcanus", "length": 15000},
  {"from": "nauvis", "to": "secret", "length": 1},
  {"from": "nauvis", "to": "nowhere", "length": 1},
  {"from": "vulcanus", "to": "fake-moon", "length": 40}
 ]
}`

func TestDecodeStarMapHappyPath(t *testing.T) {
	m, err := DecodeStarMap([]byte(spaceAgeMap))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Locations) != 3 || len(m.Routes) != 2 {
		t.Fatalf("got %d locations, %d routes: %+v", len(m.Locations), len(m.Routes), m)
	}
	// secret, plus two routes to it and to an unknown end.
	if m.Dropped != 3 {
		t.Errorf("Dropped = %d, want 3", m.Dropped)
	}
	n, _ := m.Location("nauvis")
	// Orientation 0.25 is a quarter turn clockwise from up: due right.
	if !near(n.X, 15) || !near(n.Y, 0) || !near(n.Distance, 15) || n.Magnitude != 1.2 || n.LabelOrientation != 0.5 || !n.HasIcon || n.Label != "nauvis" {
		t.Errorf("nauvis = %+v", n)
	}
	v, _ := m.Location("vulcanus")
	if !near(v.X, 10*math.Sin(0.2*math.Pi)) || !near(v.Y, -10*math.Cos(0.2*math.Pi)) {
		t.Errorf("vulcanus position = %v, %v", v.X, v.Y)
	}
	if v.Magnitude != 1 || v.LabelOrientation != 0.25 || v.HasIcon || v.Parent != "" {
		t.Errorf("vulcanus defaults = %+v", v)
	}
	moon, _ := m.Location("fake-moon")
	if moon.Parent != "vulcanus" {
		t.Errorf("moon parent = %q", moon.Parent)
	}
	if m.Routes[0] != (Route{From: "nauvis", To: "vulcanus", Length: 15000}) {
		t.Errorf("route = %+v", m.Routes[0])
	}
	if _, ok := m.Location("secret"); ok {
		t.Error("a hidden location was kept")
	}
}

func TestDecodeStarMapOrientationZeroIsUp(t *testing.T) {
	m, err := DecodeStarMap([]byte(`{"locations":[{"name":"a","distance":10,"orientation":0}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if l := m.Locations[0]; !near(l.X, 0) || !near(l.Y, -10) {
		t.Errorf("position = %v, %v", l.X, l.Y)
	}
}

// From 2.1 a location's distance and orientation are about its origin, a
// moon's planet (Muluna: 1.5 at 0.75 about Nauvis); the game's own position
// is where it is drawn. Without one, X and Y come from distance and
// orientation, as a 2.0 file and an older file give them (SPEC 5.6).
func TestDecodeStarMapPositionIsTheGames(t *testing.T) {
	m, err := DecodeStarMap([]byte(`{"locations":[
	 {"name":"nauvis","distance":15,"orientation":0.25,"position":{"x":15,"y":0}},
	 {"name":"muluna","distance":1.5,"orientation":0.75,"parent":"nauvis","position":{"x":13.5,"y":0}},
	 {"name":"vulcanus","distance":10,"orientation":0}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := m.Location("muluna"); !near(l.X, 13.5) || !near(l.Y, 0) || !near(l.Distance, 1.5) || !near(l.Orientation, 0.75) {
		t.Errorf("muluna = %+v, want drawn at 13.5, 0 beside Nauvis", l)
	}
	if l, _ := m.Location("vulcanus"); !near(l.X, 0) || !near(l.Y, -10) {
		t.Errorf("vulcanus without a position = %v, %v", l.X, l.Y)
	}
	if _, err := DecodeStarMap([]byte(`{"locations":[{"name":"a","position":{"x":1e999,"y":0}}]}`)); err == nil {
		t.Error("a position past a float was accepted")
	}
	if _, err := DecodeStarMap([]byte(`{"locations":[{"name":"a","position":{"x":"1","y":0}}]}`)); err == nil {
		t.Error("a string position was accepted")
	}
	m, err = DecodeStarMap([]byte(`{"locations":[{"name":"a","position":{"x":2e12,"y":0}},{"name":"b","position":{"x":0,"y":-2e12}}]}`))
	if err != nil || len(m.Locations) != 0 || m.Dropped != 2 {
		t.Errorf("positions past 1e12: %+v, %v; want both dropped", m, err)
	}
}

func TestDecodeStarMapEmptyListsAreFine(t *testing.T) {
	for _, in := range []string{`{}`, `{"locations":{},"routes":{}}`, `{"locations":null}`, `{"locations":[]}`} {
		m, err := DecodeStarMap([]byte(in))
		if err != nil || len(m.Locations) != 0 || len(m.Routes) != 0 {
			t.Errorf("%s: %+v, %v", in, m, err)
		}
	}
}

func TestDecodeStarMapRefusesWholeFile(t *testing.T) {
	many := func(n int, one string) string {
		return strings.TrimSuffix(strings.Repeat(one+",", n), ",")
	}
	tests := []struct{ name, in string }{
		{"not json", `not json`},
		{"wrong shape", `{"locations": 5}`},
		{"object list with entries", `{"locations": {"a": 1}}`},
		{"number too big for a float", `{"locations":[{"name":"a","distance":1e999}]}`},
		{"over the byte limit", `{"x":"` + strings.Repeat("a", MaxStarMapFileBytes) + `"}`},
		{"too many locations", `{"locations":[` + many(MaxStarMapLocations+1, `{"name":"a"}`) + `]}`},
		{"too many routes", `{"locations":[{"name":"a"}],"routes":[` + many(MaxStarMapRoutes+1, `{"from":"a","to":"a"}`) + `]}`},
		{"a string where a number goes", `{"locations":[{"name":"a","distance":"NaN"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeStarMap([]byte(tt.in)); err == nil {
				t.Error("accepted")
			}
		})
	}
}

func TestDecodeStarMapAtTheLimitIsAccepted(t *testing.T) {
	var locs, routes []string
	for i := range MaxStarMapLocations {
		locs = append(locs, fmt.Sprintf(`{"name":"p%d"}`, i))
	}
	for i := range MaxStarMapRoutes {
		routes = append(routes, fmt.Sprintf(`{"from":"p%d","to":"p%d","length":1}`, i%64, 64+i/64))
	}
	in := `{"locations":[` + strings.Join(locs, ",") + `],"routes":[` + strings.Join(routes, ",") + `]}`
	if len(in) > MaxStarMapFileBytes {
		t.Fatalf("test input is %d bytes", len(in))
	}
	m, err := DecodeStarMap([]byte(in))
	if err != nil || len(m.Locations) != MaxStarMapLocations || len(m.Routes) != MaxStarMapRoutes {
		t.Fatalf("%d locations, %d routes, %v", len(m.Locations), len(m.Routes), err)
	}
}

func TestDecodeStarMapErrorsHoldNoFileText(t *testing.T) {
	_, err := DecodeStarMap([]byte(`{"locations":[{"name":"secret-text","distance":"secret-text"}]}`))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error = %v", err)
	}
}

func TestDecodeStarMapDropsBadLocations(t *testing.T) {
	tests := []struct{ name, loc string }{
		{"empty name", `{"name":""}`},
		{"upper case", `{"name":"Nauvis"}`},
		{"slash", `{"name":"a/b"}`},
		{"too long", `{"name":"` + strings.Repeat("a", 65) + `"}`},
		{"hidden", `{"name":"a","hidden":true}`},
		{"negative distance", `{"name":"a","distance":-1}`},
		{"huge distance", `{"name":"a","distance":1e13}`},
		{"negative magnitude", `{"name":"a","magnitude":-1}`},
		{"huge magnitude", `{"name":"a","magnitude":1e13}`},
		{"huge label orientation", `{"name":"a","label_orientation":1e13}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := DecodeStarMap([]byte(`{"locations":[` + tt.loc + `,{"name":"keep"}]}`))
			if err != nil || len(m.Locations) != 1 || m.Locations[0].Name != "keep" || m.Dropped != 1 {
				t.Errorf("%+v, %v", m, err)
			}
		})
	}
}

func TestDecodeStarMapDropsRepeatedName(t *testing.T) {
	m, err := DecodeStarMap([]byte(`{"locations":[{"name":"a","distance":1},{"name":"a","distance":2}]}`))
	if err != nil || len(m.Locations) != 1 || m.Locations[0].Distance != 1 || m.Dropped != 1 {
		t.Errorf("%+v, %v", m, err)
	}
}

func TestDecodeStarMapDropsBadRoutes(t *testing.T) {
	tests := []struct{ name, route string }{
		{"unknown from", `{"from":"x","to":"a","length":1}`},
		{"unknown to", `{"from":"a","to":"x","length":1}`},
		{"hidden end", `{"from":"a","to":"h","length":1}`},
		{"to itself", `{"from":"a","to":"a","length":1}`},
		{"negative length", `{"from":"a","to":"b","length":-1}`},
		{"huge length", `{"from":"a","to":"b","length":1e13}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := `{"locations":[{"name":"a"},{"name":"b"},{"name":"h","hidden":true}],"routes":[` + tt.route + `,{"from":"a","to":"b","length":2}]}`
			m, err := DecodeStarMap([]byte(in))
			if err != nil || len(m.Routes) != 1 || m.Routes[0].Length != 2 || m.Dropped != 2 {
				t.Errorf("%+v, %v", m, err)
			}
		})
	}
}

func TestDecodeStarMapParents(t *testing.T) {
	loc := func(name, parent string) string {
		return fmt.Sprintf(`{"name":%q,"parent":%q}`, name, parent)
	}
	tests := []struct {
		name string
		locs []string
		want map[string]string
	}{
		{"chain kept", []string{loc("sun", ""), loc("a", "sun"), loc("b", "a")}, map[string]string{"a": "sun", "b": "a"}},
		{"unknown parent cleared", []string{loc("a", "x")}, map[string]string{"a": ""}},
		{"hidden parent cleared", []string{`{"name":"h","hidden":true}`, loc("a", "h")}, map[string]string{"a": ""}},
		{"own parent cleared", []string{loc("a", "a")}, map[string]string{"a": ""}},
		{"two-loop cleared", []string{loc("a", "b"), loc("b", "a")}, map[string]string{"a": "", "b": ""}},
		{"child of a loop keeps its parent", []string{loc("a", "b"), loc("b", "a"), loc("c", "a")}, map[string]string{"a": "", "b": "", "c": "a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := DecodeStarMap([]byte(`{"locations":[` + strings.Join(tt.locs, ",") + `]}`))
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tt.want {
				if l, ok := m.Location(name); !ok || l.Parent != want {
					t.Errorf("%s parent = %q (found %v), want %q", name, l.Parent, ok, want)
				}
			}
			// Every chain ends.
			for _, l := range m.Locations {
				p := l
				for i := 0; p.Parent != ""; i++ {
					if i > len(m.Locations) {
						t.Fatalf("%s's chain loops", l.Name)
					}
					p, _ = m.Location(p.Parent)
				}
			}
		})
	}
}

func TestStarMapLabelled(t *testing.T) {
	m, err := DecodeStarMap([]byte(`{"locations":[
	  {"name":"nauvis","localised_name":["space-location-name.nauvis"]},
	  {"name":"vulcanus","localised_name":["space-location-name.vulcanus"]},
	  {"name":"plain"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	loc := modsettings.LocaleSections{"space-location-name": {"nauvis": "[planet=nauvis] Nauvis"}}
	got := m.Labelled(loc)
	want := map[string]string{"nauvis": "Nauvis", "vulcanus": "vulcanus", "plain": "plain"}
	for _, l := range got.Locations {
		if l.Label != want[l.Name] {
			t.Errorf("%s label = %q, want %q", l.Name, l.Label, want[l.Name])
		}
	}
	if m.Locations[0].Label != "nauvis" {
		t.Error("Labelled changed the original")
	}
}

func TestDecodeStarMapDropsRepeatedRoutes(t *testing.T) {
	in := `{"locations":[{"name":"a"},{"name":"b"}],"routes":[{"from":"a","to":"b","length":1},{"from":"b","to":"a","length":1},{"from":"a","to":"b","length":2}]}`
	m, err := DecodeStarMap([]byte(in))
	if err != nil || len(m.Routes) != 1 || m.Dropped != 2 {
		t.Fatalf("%d routes, %d dropped, %v", len(m.Routes), m.Dropped, err)
	}
}

// TestSpaceAgeStarMap pins the embedded maps (SPEC 5.6): each Space Age line
// ships the game's own places and routes, and 1.1 has none.
func TestSpaceAgeStarMap(t *testing.T) {
	if _, ok := SpaceAgeStarMap("1.1"); ok {
		t.Error("1.1 has no Space Age star map")
	}
	if _, ok := SpaceAgeStarMap("9.9"); ok {
		t.Error("an unknown line has no star map")
	}
	for _, line := range []string{"2.0", "2.1"} {
		t.Run(line, func(t *testing.T) {
			m, ok := SpaceAgeStarMap(line)
			if !ok {
				t.Fatal("no star map")
			}
			var names []string
			for _, l := range m.Locations {
				names = append(names, l.Name)
			}
			want := []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo", "solar-system-edge", "shattered-planet"}
			if !slices.Equal(names, want) {
				t.Errorf("locations = %v, want %v", names, want)
			}
			if len(m.Routes) != 9 {
				t.Errorf("%d routes, want 9", len(m.Routes))
			}
			v, _ := m.Location("vulcanus")
			if v.Distance != 10 || math.Abs(v.Orientation-0.1) > 1e-6 {
				t.Errorf("vulcanus at distance %v, orientation %v; want 10, 0.1", v.Distance, v.Orientation)
			}
			var nv *Route
			for i, r := range m.Routes {
				if r.From == "nauvis" && r.To == "vulcanus" {
					nv = &m.Routes[i]
				}
			}
			if nv == nil || nv.Length != 15000 {
				t.Errorf("nauvis-vulcanus route = %+v, want 15000 km", nv)
			}
			for _, l := range m.Locations {
				if l.HasIcon {
					t.Errorf("%s has an icon: no art is shipped", l.Name)
				}
			}
		})
	}
}

// A far star a mod draws only as a picture (PlanetsLib sprite_only) is not in
// the file; its bodies' orbit offsets rebuild it (SPEC 5.6).
const farStarMap = `{"locations":[
 {"name":"nauvis","distance":15,"orientation":0},
 {"name":"nix","position":{"x":100,"y":-50},"parent":"redstar","orbit":{"distance":10,"orientation":0.25}},
 {"name":"mirandus","position":{"x":90,"y":-40},"parent":"redstar","orbit":{"distance":10,"orientation":0.5}}
]}`

func TestDecodeStarMapRebuildsMissingStar(t *testing.T) {
	m, err := DecodeStarMap([]byte(farStarMap))
	if err != nil {
		t.Fatal(err)
	}
	if m.Dropped != 0 || len(m.Locations) != 3 {
		t.Fatalf("Dropped = %d, %d locations", m.Dropped, len(m.Locations))
	}
	// nix is 10 to the right of its star: the star is at 90,-50.
	if len(m.Stars) != 1 || m.Stars[0].Name != "redstar" || !near(m.Stars[0].X, 90) || !near(m.Stars[0].Y, -50) {
		t.Fatalf("Stars = %+v", m.Stars)
	}
	for _, n := range []string{"nix", "mirandus"} {
		if l, _ := m.Location(n); l.Star != "redstar" || l.Parent != "" {
			t.Errorf("%s = %+v", n, l)
		}
	}
	if l, _ := m.Location("nauvis"); l.Star != "" {
		t.Errorf("nauvis star = %q", l.Star)
	}
}

func TestDecodeStarMapStarAtTheSunIsNotRebuilt(t *testing.T) {
	m, err := DecodeStarMap([]byte(`{"locations":[
	 {"name":"a","position":{"x":0,"y":-10},"parent":"star","orbit":{"distance":10,"orientation":0}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Stars) != 0 || m.Locations[0].Star != "" || m.Locations[0].Parent != "" || m.Dropped != 0 {
		t.Errorf("map = %+v", m)
	}
}

func TestDecodeStarMapDropsBadOrbit(t *testing.T) {
	for _, orbit := range []string{`{"distance":-1,"orientation":0}`, `{"distance":2e12,"orientation":0}`, `{"distance":1,"orientation":2e12}`} {
		m, err := DecodeStarMap([]byte(`{"locations":[
		 {"name":"a","position":{"x":50,"y":0},"parent":"far","orbit":` + orbit + `}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Stars) != 0 || len(m.Locations) != 1 || m.Locations[0].Star != "" || m.Dropped != 1 {
			t.Errorf("%s: map = %+v", orbit, m)
		}
	}
	if _, err := DecodeStarMap([]byte(`{"locations":[{"name":"a","orbit":{"distance":1e999,"orientation":0}}]}`)); err == nil {
		t.Error("an orbit number past float64 was accepted")
	}
}
