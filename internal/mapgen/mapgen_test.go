package mapgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestLineAcceptsOnlyExactPatchReleasesOfSupportedLines(t *testing.T) {
	good := map[string]string{
		"1.1.110": "1.1", "1.1.0": "1.1", "2.0.77": "2.0", "2.0.66": "2.0",
		"2.1.9": "2.1", "2.1.24": "2.1",
	}
	for v, want := range good {
		if got, ok := Line(v); !ok || got != want {
			t.Errorf("Line(%q) = %q, %v; want %q, true", v, got, ok, want)
		}
	}
	// SPEC 10.7: only an exact patch release; SPEC 10.8: only the three lines.
	for _, v := range []string{
		"", "1.1", "2.0", "2.1", "2.0.", "2.2.0", "3.0.1", "1.0.1", "0.17.79", "2.10.1", "2.11.1",
		"2.0.077", "2.0.77.1", "2.0.77-1", " 2.0.77", "2.0.77 ", "2.0.77\n", "v2.0.77",
		"2.0.x", "2.0.-1", "2.0.７７", "stable", "latest", "2.0.77/../x",
	} {
		if got, ok := Line(v); ok {
			t.Errorf("Line(%q) = %q, true; want it refused", v, got)
		}
	}
}

func TestEveryLineHasData(t *testing.T) {
	for _, l := range Lines {
		if len(Presets(l)) == 0 || len(Planets(l, true)) == 0 || len(Controls(l, "nauvis")) == 0 {
			t.Errorf("line %s is missing data", l)
		}
		if DefaultMapGen(l) == nil {
			t.Errorf("line %s is missing its default map-gen file", l)
		}
	}
}

func TestUnknownLineHasNoData(t *testing.T) {
	for _, l := range []string{"", "2.2", "1.0", "2.0.77", "../2.1"} {
		if Presets(l) != nil || Planets(l, true) != nil || Controls(l, "nauvis") != nil ||
			DefaultMapGen(l) != nil {
			t.Errorf("line %q should have no data", l)
		}
	}
}

func TestPresets(t *testing.T) {
	for _, l := range Lines {
		names := Presets(l)
		if names[0] != "default" {
			t.Errorf("%s: first preset is %q, want default", l, names[0])
		}
		seen := map[string]bool{}
		for _, n := range names {
			if seen[n] || n == "" || strings.ContainsAny(n, " /\\") {
				t.Errorf("%s: bad or repeated preset name %q", l, n)
			}
			seen[n] = true
		}
		for _, want := range []string{"default", "rich-resources", "marathon", "death-world", "death-world-marathon", "rail-world", "ribbon-world", "island"} {
			if !seen[want] {
				t.Errorf("%s: missing preset %s", l, want)
			}
		}
		// Lakes were added in 2.0.
		if seen["lakes"] != (l != "1.1") {
			t.Errorf("%s: lakes present = %v", l, seen["lakes"])
		}
	}
}

func TestPresetsReturnsACopy(t *testing.T) {
	a := Presets("2.1")
	a[0] = "changed"
	if Presets("2.1")[0] != "default" {
		t.Error("changing the returned slice changed the package data")
	}
}

func TestPlanets(t *testing.T) {
	spaceAge := []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo"}
	for _, sa := range []bool{false, true} {
		if got := Planets("1.1", sa); !slices.Equal(got, []string{"nauvis"}) {
			t.Errorf("1.1 spaceAge=%v: %v, want only nauvis (no Space Age before 2.0)", sa, got)
		}
	}
	for _, l := range []string{"2.0", "2.1"} {
		if got := Planets(l, false); !slices.Equal(got, []string{"nauvis"}) {
			t.Errorf("%s without Space Age: %v, want only nauvis", l, got)
		}
		if got := Planets(l, true); !slices.Equal(got, spaceAge) {
			t.Errorf("%s with Space Age: %v, want %v", l, got, spaceAge)
		}
	}
}

func TestControls(t *testing.T) {
	resources := []string{"iron-ore", "copper-ore", "stone", "coal", "crude-oil", "uranium-ore"}
	for _, l := range Lines {
		byName := map[string]Control{}
		for _, c := range Controls(l, "nauvis") {
			byName[c.Name] = c
		}
		for _, r := range resources {
			c, ok := byName[r]
			if !ok || c.Category != "resource" || !c.Richness {
				t.Errorf("%s: %s = %+v, want a resource with richness", l, r, c)
			}
		}
		if c := byName["enemy-base"]; c.Category != "enemy" || c.Richness {
			t.Errorf("%s: enemy-base = %+v", l, c)
		}
		if _, ok := byName["trees"]; !ok {
			t.Errorf("%s: no trees control", l)
		}
	}
	if Controls("1.1", "vulcanus") != nil || Controls("2.1", "mars") != nil || Controls("2.1", "") != nil {
		t.Error("a planet the line does not have must return nil")
	}
}

func TestControlsAreWellFormed(t *testing.T) {
	valid := map[string]bool{"resource": true, "terrain": true, CategoryCliff: true, "enemy": true}
	for _, l := range Lines {
		// The game keeps one flat list of controls per map, so a name may
		// belong to only one planet.
		owner := map[string]string{}
		for _, p := range Planets(l, true) {
			cs := Controls(l, p)
			if len(cs) == 0 {
				t.Errorf("%s/%s: no controls", l, p)
			}
			for _, c := range cs {
				if !valid[c.Category] {
					t.Errorf("%s/%s/%s: category %q", l, p, c.Name, c.Category)
				}
				if c.Richness && c.Category != "resource" && c.Category != "terrain" {
					t.Errorf("%s/%s/%s: richness on a %s control", l, p, c.Name, c.Category)
				}
				if prev, dup := owner[c.Name]; dup {
					t.Errorf("%s: control %s belongs to both %s and %s", l, c.Name, prev, p)
				}
				owner[c.Name] = p
			}
		}
	}
}

func TestControlsReturnsACopy(t *testing.T) {
	a := Controls("2.1", "nauvis")
	a[0].Name = "changed"
	if Controls("2.1", "nauvis")[0].Name == "changed" {
		t.Error("changing the returned slice changed the package data")
	}
}

func TestDefaultMapGenIsAJSONObjectThatAgreesWithTheControls(t *testing.T) {
	for _, l := range Lines {
		var gen struct {
			AutoplaceControls map[string]json.RawMessage `json:"autoplace_controls"`
			StartingArea      *float64                   `json:"starting_area"`
		}
		if err := json.Unmarshal(DefaultMapGen(l), &gen); err != nil {
			t.Fatalf("%s: map-gen default is not JSON: %v", l, err)
		}
		if gen.StartingArea == nil || len(gen.AutoplaceControls) == 0 {
			t.Errorf("%s: map-gen default lacks starting_area or autoplace_controls", l)
		}
		known := map[string]bool{}
		for _, c := range Controls(l, "nauvis") {
			known[c.Name] = true
		}
		for name := range gen.AutoplaceControls {
			if !known[name] {
				t.Errorf("%s: the default map-gen file has control %s that Controls does not", l, name)
			}
		}
	}
}

func TestDefaultsReturnACopy(t *testing.T) {
	a := DefaultMapGen("2.1")
	a[0] = 'X'
	if DefaultMapGen("2.1")[0] == 'X' {
		t.Error("changing the returned bytes changed the package data")
	}
}

// The golden fixtures are made by the real game. Every control the game
// reports in a map's settings must be one this package knows, and the reverse:
// a control missing here would be missing from the map-gen form (SPEC 5.6).
func TestDataMatchesGoldenFixtures(t *testing.T) {
	root := filepath.Join("..", "mapexchange", "testdata")
	dirs, _ := filepath.Glob(filepath.Join(root, "*", "default.json"))
	if len(dirs) == 0 {
		t.Skip("no golden fixtures present")
	}
	for _, d := range dirs {
		dir := filepath.Dir(d)
		version := filepath.Base(dir)
		line, ok := Line(version)
		if !ok {
			t.Fatalf("fixture directory %s is not a supported patch release", version)
		}
		fixtures, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		sort.Strings(fixtures)
		for _, f := range fixtures {
			name := strings.TrimSuffix(filepath.Base(f), ".json")
			spaceAge := strings.HasSuffix(name, "_space-age")
			b, err := os.ReadFile(f) // #nosec G304 -- paths come from a glob of our own testdata
			if err != nil {
				t.Fatal(err)
			}
			var game struct {
				MapGen struct {
					AutoplaceControls map[string]json.RawMessage `json:"autoplace_controls"`
				} `json:"map_gen_settings"`
			}
			if err := json.Unmarshal(b, &game); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
			want := map[string]bool{}
			for _, p := range Planets(line, spaceAge) {
				for _, c := range Controls(line, p) {
					want[c.Name] = true
				}
			}
			for n := range game.MapGen.AutoplaceControls {
				if !want[n] {
					t.Errorf("%s/%s: the game reports control %s that mapgen does not have", version, name, n)
				}
				delete(want, n)
			}
			for n := range want {
				t.Errorf("%s/%s: mapgen has control %s that the game does not report", version, name, n)
			}
		}
		for _, p := range Presets(line) {
			if _, err := os.Stat(filepath.Join(dir, p+".txt")); err != nil {
				t.Errorf("%s: no golden fixture for preset %s", version, p)
			}
		}
	}
}
