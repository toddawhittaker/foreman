package web

import (
	"bytes"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// manyModsStarMap is a real star map: the one a world with about seventy
// mods (Muluna and Lignumis about Nauvis, a stargate beside it, a second
// star) gets from the game 2.1.24, with each mod's English name put in.
func manyModsStarMap(t *testing.T, leaveOut ...string) mapgen.StarMap {
	t.Helper()
	b, err := os.ReadFile("testdata/starmap-many-mods.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := mapgen.DecodeStarMap(b)
	if err != nil {
		t.Fatal(err)
	}
	m = m.Labelled()
	m.Locations = slices.DeleteFunc(m.Locations, func(l mapgen.Location) bool { return slices.Contains(leaveOut, l.Name) })
	m.Routes = slices.DeleteFunc(m.Routes, func(r mapgen.Route) bool {
		return slices.Contains(leaveOut, r.From) || slices.Contains(leaveOut, r.To)
	})
	return m
}

func boxesOverlap(a, b rect) bool {
	return a.min.X < b.max.X && b.min.X < a.max.X && a.min.Y < b.max.Y && b.min.Y < a.max.Y
}

// labelsClear fails the test for each label among names (all when none)
// that overlaps another circle or another label, or that is not the nearest
// text to its own body and nearer its own body than any other.
func labelsClear(t *testing.T, v StarMapView, names ...string) {
	t.Helper()
	for i, l := range v.Locations {
		if len(names) > 0 && !slices.Contains(names, l.Name) {
			continue
		}
		box := labelBox(l)
		own := boxToCircle(box, l)
		if own <= 0 {
			t.Errorf("%s's label covers its own circle", l.Name)
		}
		for j, o := range v.Locations {
			if j == i {
				continue
			}
			if boxToCircle(box, o) <= 0 {
				t.Errorf("%s's label covers %s's circle", l.Name, o.Name)
			} else if boxToCircle(box, o) <= own {
				t.Errorf("%s's label is %.1f from its own circle but %.1f from %s's", l.Name, own, boxToCircle(box, o), o.Name)
			}
			if boxesOverlap(box, labelBox(o)) {
				t.Errorf("%s's label covers %s's label", l.Name, o.Name)
			}
			if d := boxToCircle(labelBox(o), l); d < own {
				t.Errorf("%s's label is %.1f from %s, nearer than its own label (%.1f)", o.Name, d, l.Name, own)
			}
		}
		for _, s := range v.Stars {
			if boxToCircle(box, StarMapLocation{X: s.X, Y: s.Y, R: v.Sun}) <= 0 {
				t.Errorf("%s's label covers a star", l.Name)
			}
		}
	}
}

// SPEC 5.6, DESIGN 8.23: on a real map, Nauvis with its moons Muluna and
// Lignumis and the Senestella Stargate close by, every label in the cluster
// finds a spot clear of every circle and every other label, and stays the
// nearest text to its own body.
func TestStarMapLabelsOfTheRealNauvisCluster(t *testing.T) {
	v, ok := layoutStarMap(manyModsStarMap(t), mapgen.Builtin("2.1", true), i18n.English, starMapIcons{})
	if !ok {
		t.Fatal("no map")
	}
	labelsClear(t, v, "nauvis", "muluna", "lignumis", "calidus-senestella-gate-calidus")
}

// SPEC 5.6, DESIGN 8.23: the same map without the moons: Nauvis and the
// stargate are still clear, so the search does not depend on moons.
func TestStarMapLabelsOfTheRealMapWithoutMoons(t *testing.T) {
	v, _ := layoutStarMap(manyModsStarMap(t, "muluna", "lignumis"), mapgen.Builtin("2.1", true), i18n.English, starMapIcons{})
	labelsClear(t, v, "nauvis", "calidus-senestella-gate-calidus")
}

// SPEC 5.6, DESIGN 8.23: on the whole real map no label overlaps a circle
// or another label.
func TestStarMapLabelsOfTheWholeRealMap(t *testing.T) {
	v, _ := layoutStarMap(manyModsStarMap(t), mapgen.Builtin("2.1", true), i18n.English, starMapIcons{})
	if len(v.Locations) < 30 {
		t.Fatalf("%d locations, want the real map's 30 or more", len(v.Locations))
	}
	labelsClear(t, v)
}

// SPEC 5.6, DESIGN 8.23: Space Age's own map, from the game, stays clear in
// every line that has one.
func TestStarMapLabelsOfSpaceAge(t *testing.T) {
	for _, line := range []string{"2.0", "2.1"} {
		m, ok := mapgen.SpaceAgeStarMap(line)
		if !ok {
			t.Fatalf("no Space Age map for %s", line)
		}
		v, _ := layoutStarMap(m, mapgen.Builtin(line, true), i18n.English, starMapIcons{})
		labelsClear(t, v)
	}
}

// SPEC 5.6, DESIGN 8.23: a label overlaps another body only when no spot in
// the whole search is clear. A hub packed about with rings of small moons, out
// past its furthest spot, leaves no spot clear: every one is checked, and the
// label then takes one that overlaps.
func TestStarMapLabelOverlapsOnlyWithNoClearSpot(t *testing.T) {
	js := `{"locations":[{"name":"far","distance":40,"orientation":0.5},{"name":"hub","distance":10,"orientation":0.25,"label_orientation":0.25}`
	n := 0
	for ring := 1; ring <= 6; ring++ {
		for i := range 32 {
			// Each moon's position about the hub, which sits at 10,0.
			a := 2 * math.Pi * float64(i) / 32
			d := 0.4 * float64(ring)
			num := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
			js += `,{"name":"m` + strconv.Itoa(n) + `","distance":` + num(d) + `,"position":{"x":` + num(10+d*math.Sin(a)) + `,"y":` + num(-d*math.Cos(a)) + `},"magnitude":0.2,"parent":"hub"}`
			n++
		}
	}
	v, _ := layoutStarMap(starMapOf(t, js+`],"routes":[]}`), mapgen.Catalogue{}, i18n.English, starMapIcons{})
	at := slices.IndexFunc(v.Locations, func(l StarMapLocation) bool { return l.Name == "hub" })
	hub := v.Locations[at]
	spots := labelCandidates(hub, 0.25)
	if len(spots) != len(labelTurns)*len(labelSteps) {
		t.Fatalf("%d spots, want %d", len(spots), len(labelTurns)*len(labelSteps))
	}
	for _, c := range spots {
		if labelOverlaps(labelBox(c), nil, v.Locations, at) == 0 {
			t.Fatalf("the hub's spot at %.1f,%.1f is clear; the test needs none", c.LabelX, c.LabelY)
		}
	}
	if labelOverlaps(labelBox(hub), nil, v.Locations, at) == 0 {
		t.Errorf("the hub's label is clear, which no spot is")
	}
}

// DESIGN 8.23: every label paints after every circle and picture, so no body
// hides a label; on the page each names its location for a click, while the
// file, which has no controls, carries no such mark.
func TestStarMapLabelsPaintLast(t *testing.T) {
	v, _ := layoutStarMap(manyModsStarMap(t), mapgen.Builtin("2.1", true), i18n.English, starMapIcons{world: "w"})
	for _, file := range []bool{false, true} {
		v.File = file
		var b bytes.Buffer
		if err := newTestRenderer(t).Fragment(&b, i18n.English, starMapFile{v}, "starmap-svg"); err != nil {
			t.Fatal(err)
		}
		svg := b.String()
		first := strings.Index(svg, "<text")
		if first < 0 || first < strings.LastIndex(svg, "<circle") || first < strings.LastIndex(svg, "<image") {
			t.Errorf("file %v: a label comes before a circle or picture", file)
		}
		if n := strings.Count(svg, "<text"); n != len(v.Locations) {
			t.Errorf("file %v: %d labels, want %d", file, n, len(v.Locations))
		}
		want := len(v.Locations)
		if file {
			want = 0
		}
		if got := strings.Count(svg, `data-label="`); got != want {
			t.Errorf("file %v: %d labels name their location, want %d", file, got, want)
		}
	}
}
