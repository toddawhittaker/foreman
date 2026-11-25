package web

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// starMapOf decodes a star map file, failing the test on an error.
func starMapOf(t *testing.T, js string) mapgen.StarMap {
	t.Helper()
	m, err := mapgen.DecodeStarMap([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// spaceAgeStarMap is Space Age's planets about where the game puts them, a
// moon of Nauvis, a hidden place and routes, one of them to the hidden place.
// Muluna is as 2.1 and PlanetsLib give it: 1.5 at 0.75 about Nauvis, with the
// game's own position just left of Nauvis.
const spaceAgeStarMap = `{"locations":[
 {"name":"nauvis","distance":15,"orientation":0.275},
 {"name":"vulcanus","distance":10,"orientation":0.1,"magnitude":1.5},
 {"name":"gleba","distance":20,"orientation":0.175,"magnitude":1.1},
 {"name":"fulgora","distance":25,"orientation":0.325,"magnitude":0.9},
 {"name":"aquilo","distance":35,"orientation":0.225},
 {"name":"solar-system-edge","distance":50,"orientation":0.25,"label_orientation":0.15},
 {"name":"muluna","distance":1.5,"orientation":0.75,"position":{"x":13.3153,"y":2.3465},"magnitude":0.4,"parent":"nauvis","label_orientation":0.75,"starmap_icon":true},
 {"name":"secret","distance":40,"orientation":0.5,"hidden":true}
],"routes":[
 {"from":"nauvis","to":"vulcanus","length":15000},
 {"from":"nauvis","to":"fulgora","length":15000},
 {"from":"nauvis","to":"muluna","length":5000},
 {"from":"aquilo","to":"solar-system-edge","length":50000},
 {"from":"nauvis","to":"secret","length":1}
]}`

// pathPoints reads "M x y L x y ..." back into points.
func pathPoints(t *testing.T, d string) []point {
	t.Helper()
	f := strings.Fields(strings.NewReplacer("M", "", "L", "").Replace(d))
	if len(f)%2 != 0 {
		t.Fatalf("path %q", d)
	}
	var out []point
	for i := 0; i < len(f); i += 2 {
		x, err1 := strconv.ParseFloat(f[i], 64)
		y, err2 := strconv.ParseFloat(f[i+1], 64)
		if err1 != nil || err2 != nil {
			t.Fatalf("path %q", d)
		}
		out = append(out, point{x, y})
	}
	return out
}

func viewBoxOf(t *testing.T, v StarMapView) (x, y, w, h float64) {
	t.Helper()
	f := strings.Fields(v.ViewBox)
	if len(f) != 4 {
		t.Fatalf("view box %q", v.ViewBox)
	}
	var n [4]float64
	for i, s := range f {
		var err error
		if n[i], err = strconv.ParseFloat(s, 64); err != nil {
			t.Fatalf("view box %q", v.ViewBox)
		}
	}
	return n[0], n[1], n[2], n[3]
}

// SPEC 5.6: hidden locations and every route touching one are left out; the
// rest are all drawn, and the picture is square.
func TestStarMapLeavesHiddenOut(t *testing.T) {
	v, ok := layoutStarMap(starMapOf(t, spaceAgeStarMap), mapgen.Builtin("2.1", true), i18n.English, starMapIcons{})
	if !ok {
		t.Fatal("no star map")
	}
	var names []string
	for _, l := range v.Locations {
		names = append(names, l.Name)
	}
	if want := []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo", "solar-system-edge", "muluna"}; !slices.Equal(names, want) {
		t.Errorf("locations %v, want %v", names, want)
	}
	if len(v.Routes) != 4 {
		t.Errorf("%d routes, want 4: the route to the hidden place is left out", len(v.Routes))
	}
	if _, _, w, h := viewBoxOf(t, v); w != h {
		t.Errorf("view box %q is not square", v.ViewBox)
	}
}

// SPEC 5.6: one location, or none, makes no map.
func TestStarMapNeedsTwoLocations(t *testing.T) {
	for _, js := range []string{`{}`, `{"locations":[{"name":"nauvis","distance":15}]}`,
		`{"locations":[{"name":"nauvis","distance":15},{"name":"x","distance":3,"hidden":true}]}`} {
		if _, ok := layoutStarMap(starMapOf(t, js), mapgen.Catalogue{}, i18n.English, starMapIcons{}); ok {
			t.Errorf("%s: a map", js)
		}
	}
}

// SPEC 5.6: the furthest location, its circle and its route's arc are inside
// the picture, with room for the label, whatever the scale of the mod's map.
func TestStarMapScaleFitsTheFurthest(t *testing.T) {
	for _, far := range []float64{50, 5000, 0.5} {
		js := `{"locations":[{"name":"a","distance":` + strconv.FormatFloat(far/5, 'f', -1, 64) + `,"orientation":0.9},` +
			`{"name":"b","distance":` + strconv.FormatFloat(far, 'f', -1, 64) + `,"orientation":0.4,"magnitude":3}],` +
			`"routes":[{"from":"a","to":"b","length":1}]}`
		v, ok := layoutStarMap(starMapOf(t, js), mapgen.Catalogue{}, i18n.English, starMapIcons{})
		if !ok {
			t.Fatal("no star map")
		}
		x, y, w, h := viewBoxOf(t, v)
		inside := func(p point, room float64) bool {
			return p.X-room >= x && p.X+room <= x+w && p.Y-room >= y && p.Y+room <= y+h
		}
		for _, l := range v.Locations {
			if !inside(point{l.X, l.Y}, l.R) {
				t.Errorf("far %v: %s at %v,%v r %v is outside %q", far, l.Name, l.X, l.Y, l.R, v.ViewBox)
			}
			// The label's far end, at the guessed width, is inside too.
			if end := l.LabelX + starMapCharWidth*v.Font*float64(l.LabelCells); l.Anchor == "start" && end > x+w {
				t.Errorf("far %v: %s's label ends at %v, outside %q", far, l.Name, end, v.ViewBox)
			}
		}
		for _, p := range pathPoints(t, v.Routes[0]) {
			if !inside(p, 0) {
				t.Errorf("far %v: the route reaches %v, outside %q", far, p, v.ViewBox)
			}
		}
		// The furthest is near the edge, not lost in the middle.
		b := v.Locations[1]
		if math.Hypot(b.X, b.Y) < w/2 {
			t.Errorf("far %v: the furthest is %v from the sun in a %v picture", far, math.Hypot(b.X, b.Y), w)
		}
	}
}

// SPEC 5.6: a route between two planets arcs about the sun, its distance and
// angle changing evenly, the shorter way round; a route to a moon from its
// own planet is straight, and so is one between places of two different
// stars.
func TestStarMapRoutes(t *testing.T) {
	m := starMapOf(t, `{"locations":[
	 {"name":"a","distance":10,"orientation":0.95},
	 {"name":"b","distance":20,"orientation":0.15},
	 {"name":"p","distance":30,"orientation":0.5},
	 {"name":"m1","distance":31,"orientation":0.5,"parent":"p"},
	 {"name":"m2","distance":30.5,"orientation":0.51,"parent":"p"},
	 {"name":"star2","distance":100,"orientation":0.25},
	 {"name":"far","distance":101,"orientation":0.25,"parent":"star2"}
	],"routes":[
	 {"from":"a","to":"b","length":1},
	 {"from":"p","to":"m1","length":1},
	 {"from":"m1","to":"m2","length":1},
	 {"from":"p","to":"far","length":1}
	]}`)
	byName := map[string]mapgen.Location{}
	for _, l := range m.Locations {
		byName[l.Name] = l
	}
	at := func(n string) mapgen.Location { return byName[n] }

	arc := routePoints(byName, at("a"), at("b"))
	if len(arc) < 4 {
		t.Fatalf("a to b is %d points, want an arc", len(arc))
	}
	// Half way: half way out, and half way round, across the top (the short way).
	mid := arc[len(arc)/2]
	tm := float64(len(arc)/2) / float64(len(arc)-1)
	if r := math.Hypot(mid.X, mid.Y); math.Abs(r-(10+10*tm)) > 0.01 {
		t.Errorf("mid point is %v from the sun, want %v", r, 10+10*tm)
	}
	if turns := math.Atan2(mid.X, -mid.Y) / (2 * math.Pi); math.Abs(turns-(-0.05+0.2*tm)) > 0.001 {
		t.Errorf("mid point at %v turns, want %v", turns, -0.05+0.2*tm)
	}
	if first, last := arc[0], arc[len(arc)-1]; first != (point{at("a").X, at("a").Y}) || last != (point{at("b").X, at("b").Y}) {
		t.Errorf("the arc runs %v to %v, not end to end", first, last)
	}

	if got := routePoints(byName, at("p"), at("m1")); len(got) != 2 {
		t.Errorf("planet to its moon is %d points, want straight", len(got))
	}
	if got := routePoints(byName, at("p"), at("far")); len(got) != 2 {
		t.Errorf("places of two stars are %d points, want straight", len(got))
	}
	// Two moons of one planet arc about the planet.
	moons := routePoints(byName, at("m1"), at("m2"))
	if len(moons) < 3 {
		t.Fatalf("moon to moon is %d points, want an arc about the planet", len(moons))
	}
	p := at("p")
	ra, rb := math.Hypot(at("m1").X-p.X, at("m1").Y-p.Y), math.Hypot(at("m2").X-p.X, at("m2").Y-p.Y)
	for i, q := range moons {
		tt := float64(i) / float64(len(moons)-1)
		if r := math.Hypot(q.X-p.X, q.Y-p.Y); math.Abs(r-(ra+(rb-ra)*tt)) > 0.01 {
			t.Errorf("moon arc point %d is %v from the planet, want %v", i, r, ra+(rb-ra)*tt)
		}
	}
}

// SPEC 5.6: a moon has a faint orbit about its planet, and each other
// location a ring about the sun at its distance.
func TestStarMapRingsAndOrbits(t *testing.T) {
	v, _ := layoutStarMap(starMapOf(t, spaceAgeStarMap), mapgen.Builtin("2.1", true), i18n.English, starMapIcons{})
	if len(v.Rings) != 6 {
		t.Errorf("%d rings, want one for each of the 6 places about the sun", len(v.Rings))
	}
	if len(v.Orbits) != 1 {
		t.Fatalf("%d orbits, want Muluna's", len(v.Orbits))
	}
	var nauvis StarMapLocation
	for _, l := range v.Locations {
		if l.Name == "nauvis" {
			nauvis = l
		}
	}
	if o := v.Orbits[0]; o.X != nauvis.X || o.Y != nauvis.Y || o.R <= 0 {
		t.Errorf("orbit %+v, want about Nauvis at %v,%v", o, nauvis.X, nauvis.Y)
	}
}

// SPEC 5.6: a moon whose distance and orientation are about its planet, as
// 2.1 gives them, is drawn where the game draws it, beside its planet; its
// orbit is a small circle about the planet, the sun's rings pass through the
// planets and not the moon, and its routes start at the moon.
func TestStarMapMoonBesideItsPlanet(t *testing.T) {
	m := starMapOf(t, `{"locations":[
	 {"name":"nauvis","distance":15,"orientation":0.275},
	 {"name":"vulcanus","distance":10,"orientation":0.1},
	 {"name":"fulgora","distance":25,"orientation":0.325},
	 {"name":"cerys","distance":1.39,"orientation":0.45,"position":{"x":22.7047,"y":12.6717},"parent":"fulgora","magnitude":0.5}
	],"routes":[
	 {"from":"fulgora","to":"cerys","length":5000},
	 {"from":"cerys","to":"nauvis","length":15000}
	]}`)
	v, ok := layoutStarMap(m, mapgen.Builtin("2.1", true), i18n.English, starMapIcons{})
	if !ok {
		t.Fatal("no map")
	}
	at := map[string]StarMapLocation{}
	for _, l := range v.Locations {
		at[l.Name] = l
	}
	cerys, fulgora := at["cerys"], at["fulgora"]
	sunToFulgora := math.Hypot(fulgora.X, fulgora.Y)
	gap := math.Hypot(cerys.X-fulgora.X, cerys.Y-fulgora.Y)
	if gap > sunToFulgora/10 {
		t.Errorf("Cerys at %v,%v is %.1f from Fulgora at %v,%v; want beside it (the sun is %.1f away)",
			cerys.X, cerys.Y, gap, fulgora.X, fulgora.Y, sunToFulgora)
	}
	if len(v.Orbits) != 1 {
		t.Fatalf("orbits %+v, want Cerys's", v.Orbits)
	}
	if o := v.Orbits[0]; o.X != fulgora.X || o.Y != fulgora.Y || math.Abs(o.R-gap) > 0.2 {
		t.Errorf("orbit %+v, want about Fulgora at %v,%v with radius %.1f", o, fulgora.X, fulgora.Y, gap)
	}
	if len(v.Rings) != 3 {
		t.Errorf("rings %v, want one for each of the 3 planets", v.Rings)
	}
	for _, r := range v.Rings {
		if math.Abs(r-math.Hypot(cerys.X, cerys.Y)) < 1 && math.Abs(r-sunToFulgora) > 1 {
			t.Errorf("ring %v passes through Cerys, not a planet", r)
		}
	}
	for i, d := range v.Routes {
		ps := pathPoints(t, d)
		from, to := at[m.Routes[i].From], at[m.Routes[i].To]
		if first, last := ps[0], ps[len(ps)-1]; math.Hypot(first.X-from.X, first.Y-from.Y) > 0.2 || math.Hypot(last.X-to.X, last.Y-to.Y) > 0.2 {
			t.Errorf("%s to %s runs %v to %v, want %v,%v to %v,%v", from.Name, to.Name, first, last, from.X, from.Y, to.X, to.Y)
		}
	}
}

// SPEC 5.6, DESIGN 8.23: sizes keep the game's own ratio to the distances,
// as measured from its star map: the sun 3.8 units across its radius, a
// circle 0.68 units a magnitude with its picture 0.69 of it, the label 0.73
// units with a gap of 0.4 of it. A map spanning very far keeps a readable
// floor, and one spanning very little a ceiling.
func TestStarMapKeepsTheGameRatios(t *testing.T) {
	v, _ := layoutStarMap(starMapOf(t, spaceAgeStarMap), mapgen.Builtin("2.1", true), i18n.English, starMapIcons{})
	at := map[string]StarMapLocation{}
	for _, l := range v.Locations {
		at[l.Name] = l
	}
	scale := math.Hypot(at["vulcanus"].X, at["vulcanus"].Y) / 10
	close := func(what string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 0.15 {
			t.Errorf("%s is %v, want %.1f (scale %.2f)", what, got, want, scale)
		}
	}
	close("the sun", v.Sun, 3.8*scale)
	close("Nauvis", at["nauvis"].R, 0.68*scale)
	close("Vulcanus", at["vulcanus"].R, 0.68*1.5*scale)
	close("Muluna", at["muluna"].R, 0.68*0.4*scale)
	close("Vulcanus's picture", at["vulcanus"].IconSize, 2*0.69*at["vulcanus"].R)
	close("the font", v.Font, 0.73*scale)
	close("the halo", v.Halo, v.Font/5)
	for _, l := range v.Locations {
		close(l.Name+"'s label gap", math.Hypot(l.LabelX-l.X, l.LabelY-l.Y), l.R+0.4*v.Font)
	}

	// Very far: every size has its floor.
	v, _ = layoutStarMap(starMapOf(t, `{"locations":[
	 {"name":"a","distance":4000,"orientation":0.25,"magnitude":0.1},
	 {"name":"b","distance":5000,"orientation":0.5}],"routes":[]}`), mapgen.Catalogue{}, i18n.English, starMapIcons{})
	if v.Sun != starMapSunMin || v.Font != starMapFontMin || v.Locations[0].R != starMapRadiusMin {
		t.Errorf("far: sun %v, font %v, radius %v; want the floors %v, %v, %v", v.Sun, v.Font, v.Locations[0].R, starMapSunMin, starMapFontMin, starMapRadiusMin)
	}
	// Very near: the font and circles have their ceilings.
	v, _ = layoutStarMap(starMapOf(t, `{"locations":[
	 {"name":"a","distance":1,"orientation":0.25,"magnitude":5},
	 {"name":"b","distance":2,"orientation":0.5}],"routes":[]}`), mapgen.Catalogue{}, i18n.English, starMapIcons{})
	if v.Font != round1(starMapFontMax) || v.Locations[0].R != starMapRadiusMax {
		t.Errorf("near: font %v, radius %v; want the ceilings %v, %v", v.Font, v.Locations[0].R, round1(starMapFontMax), starMapRadiusMax)
	}
}

// SPEC 5.6: the sun never reaches a place drawn about it, even one a mod
// puts nearer than the game's own sun is wide.
func TestStarMapSunLeavesNearPlacesClear(t *testing.T) {
	v, _ := layoutStarMap(starMapOf(t, `{"locations":[
	 {"name":"hot","distance":2,"orientation":0.25},
	 {"name":"b","distance":30,"orientation":0.5}],"routes":[]}`), mapgen.Catalogue{}, i18n.English, starMapIcons{})
	hot := v.Locations[0]
	if d := math.Hypot(hot.X, hot.Y); v.Sun > 0.75*d+0.1 {
		t.Errorf("the sun's radius %v reaches past 0.75 of hot's %v", v.Sun, d)
	}
}

// SPEC 5.6, DESIGN 8.23: the drawing carries the layout's font and halo, and
// its lines keep the game's widths on screen at any zoom.
func TestStarMapSVGSizes(t *testing.T) {
	v, _ := layoutStarMap(starMapOf(t, spaceAgeStarMap), mapgen.Builtin("2.1", true), i18n.English, starMapIcons{})
	var b bytes.Buffer
	if err := newTestRenderer(t).Fragment(&b, i18n.English, starMapFile{v}, "starmap-svg"); err != nil {
		t.Fatal(err)
	}
	svg := b.String()
	for _, want := range []string{`font-size="` + svgNum(v.Font) + `"`, `stroke-width="` + svgNum(v.Halo) + `"`,
		`<path d="M`, `stroke-width="1.3"`, `class="starmap__planet"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("the SVG lacks %s", want)
		}
	}
	shapes := strings.Count(svg, "<circle") + strings.Count(svg, "<path")
	if n := strings.Count(svg, `vector-effect="non-scaling-stroke"`); n != shapes {
		t.Errorf("%d of %d circles and routes keep their on-screen stroke", n, shapes)
	}
}

// SPEC 5.6: the label sits on the side its orientation names.
func TestStarMapLabelSide(t *testing.T) {
	cases := []struct {
		turns            float64
		anchor, baseline string
	}{
		{0.25, "start", "central"},
		{0.75, "end", "central"},
		{0, "middle", "text-after-edge"},
		{0.5, "middle", "text-before-edge"},
	}
	for _, tc := range cases {
		l := placeLocation(mapgen.Location{Magnitude: 1}, tc.turns, 1, starMapFontMin, MapGenName{}, "")
		if l.Anchor != tc.anchor || l.Baseline != tc.baseline {
			t.Errorf("%v turns: %s %s, want %s %s", tc.turns, l.Anchor, l.Baseline, tc.anchor, tc.baseline)
		}
	}
}

// SPEC 5.6, 14.10: a base-game place shows the wiki's picture only while the
// setting is on, and never asks for a mod's icon; a modded place with a
// starmap icon asks the world's icon address; any other has none.
func TestStarMapIcons(t *testing.T) {
	cases := []struct {
		loc  mapgen.Location
		wiki bool
		want string
	}{
		{mapgen.Location{Name: "nauvis", HasIcon: true}, true, "https://wiki.factorio.com/images/Nauvis.png"},
		{mapgen.Location{Name: "solar-system-edge"}, true, "https://wiki.factorio.com/images/Solar_system_edge.png"},
		{mapgen.Location{Name: "nauvis", HasIcon: true}, false, ""},
		{mapgen.Location{Name: "muluna", HasIcon: true}, false, "/worlds/w1/icons/starmap/muluna.png"},
		{mapgen.Location{Name: "muluna", HasIcon: true}, true, "/worlds/w1/icons/starmap/muluna.png"},
		{mapgen.Location{Name: "muluna"}, true, ""},
	}
	for _, tc := range cases {
		if got := (starMapIcons{world: "w1", wiki: tc.wiki}).icon(tc.loc); got != tc.want {
			t.Errorf("%s, wiki %t: %q, want %q", tc.loc.Name, tc.wiki, got, tc.want)
		}
	}
}

// withWorldStarMap makes every world's star map m.
func withWorldStarMap(t *testing.T, m mapgen.StarMap) {
	t.Helper()
	old := worldStarMap
	worldStarMap = func(*server, string) (mapgen.StarMap, bool) { return m, true }
	t.Cleanup(func() { worldStarMap = old })
}

// SPEC 5.6, 17: the map preview of every planet ends with the star map, an
// image with its name and each place's name; it is served as an SVG file,
// shown or saved, only to its user, and the zip holds the same file. The
// file has its namespace and no images, so it opens offline.
func TestPreviewStarMap(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	withWorldCatalogue(t, mapgen.Builtin("2.1", true), nil)
	withWorldStarMap(t, starMapOf(t, spaceAgeStarMap))
	id, err := hn.worlds.Create(t.Context(), "Stars", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	body := hn.postLong(PathGeneratePreview, worldPreviewForm(id), c).Body.String()
	for _, want := range []string{`<svg class="starmap"`, `role="img"`, `aria-label="Star map: the planets around the sun and the routes between them."`,
		"<title>Nauvis</title>", `<title translate="no">muluna</title>`, `href="/worlds/` + id + `/icons/starmap/muluna.png"`,
		"Download the star map (SVG)", `<g class="starmap__location" data-location="nauvis">`} {
		if !strings.Contains(body, want) {
			t.Errorf("the preview lacks %s", want)
		}
	}
	if strings.Contains(body, "secret") {
		t.Error("the hidden place is drawn")
	}
	if strings.Contains(body, "wiki.factorio.com") {
		t.Error("wiki pictures while the setting is off")
	}
	token := strings.Split(pictureOf(t, body), "/")[3]
	path := PathGeneratePreview + "/" + token + "/" + starMapName

	rec := hn.getLong(path+"/download", c)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" ||
		rec.Header().Get("Content-Security-Policy") != "default-src 'none'" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), `filename="Stars-42-starmap.svg"`) {
		t.Fatalf("download: status %d, headers %v", rec.Code, rec.Header())
	}
	file := rec.Body.Bytes()
	// The page's picture names each place for the large view's planet steps; the file needs no such mark.
	if !bytes.Contains(file, []byte(`xmlns="http://www.w3.org/2000/svg"`)) || bytes.Contains(file, []byte("<image")) || bytes.Contains(file, []byte("data-location")) {
		t.Errorf("the file lacks its namespace or holds images:\n%s", file)
	}
	if err := xml.Unmarshal(file, new(struct{})); err != nil {
		t.Errorf("the file is not well formed: %v", err)
	}
	if rec := hn.getLong(path, c); rec.Code != http.StatusOK || rec.Header().Get("Content-Disposition") != "" || !bytes.Equal(rec.Body.Bytes(), file) {
		t.Errorf("shown: status %d, headers %v", rec.Code, rec.Header())
	}

	rec = hn.getLong(PathGeneratePreview+"/"+token+"/all.zip", c)
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, f := range zr.File {
		if f.Name != "Stars-42-starmap.svg" {
			continue
		}
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		found = bytes.Equal(b, file)
	}
	if !found {
		t.Error("the zip does not hold the star map file")
	}

	hn.addUser("bob", testPassword)
	bob := sessionCookie(hn.post(PathSignIn, signInForm("bob", testPassword)))
	if rec := hn.getLong(path, bob); rec.Code != http.StatusNotFound {
		t.Errorf("another user's star map: %d", rec.Code)
	}
}

// SPEC 5.6: One planet shows that planet alone, with no star map; a world
// with no star map shows none, and its zip holds only the planets.
// One planet's view carries the star map only in a template, for the large
// view's first step, never as a picture on the page (SPEC 5.6).
func TestPreviewStarMapSeenOnlyWithAllPlanets(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	withWorldCatalogue(t, mapgen.Builtin("2.1", true), nil)
	withWorldStarMap(t, starMapOf(t, spaceAgeStarMap))
	id, err := hn.worlds.Create(t.Context(), "Stars", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	one := hn.postLong(PathGeneratePreview, worldPreviewForm(id, "auto", "1", "preview_view", "one"), c).Body.String()
	tmpl := strings.Index(one, `<template id="starmap-step">`)
	if tmpl < 0 || strings.Index(one, `class="starmap"`) < tmpl {
		t.Error("One planet shows the star map, or has none for the large view")
	}

	withWorldStarMap(t, mapgen.StarMap{})
	body := hn.postLong(PathGeneratePreview, worldPreviewForm(id), c).Body.String()
	if strings.Contains(body, `class="starmap"`) {
		t.Error("a world without a star map shows one")
	}
	token := strings.Split(pictureOf(t, body), "/")[3]
	if rec := hn.getLong(PathGeneratePreview+"/"+token+"/"+starMapName, c); rec.Code != http.StatusNotFound {
		t.Errorf("a star map served for a world without one: %d", rec.Code)
	}
	rec := hn.getLong(PathGeneratePreview+"/"+token+"/all.zip", c)
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".svg") {
			t.Errorf("the zip holds %s", f.Name)
		}
	}
}

// withNoWorldStarMap makes every world's planets run uncached.
func withNoWorldStarMap(t *testing.T) {
	t.Helper()
	old := worldStarMap
	worldStarMap = func(*server, string) (mapgen.StarMap, bool) { return mapgen.StarMap{}, false }
	t.Cleanup(func() { worldStarMap = old })
}

// SPEC 5.6: a world with Space Age and no other mod has no planets run, and
// its preview shows Space Age's own embedded map: the five planets, the solar
// system edge and the shattered planet, with pictures from the wiki only
// while the wiki setting is on.
func TestPreviewStarMapOfPlainSpaceAge(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	withWorldCatalogue(t, mapgen.Builtin("2.1", true), nil)
	withNoWorldStarMap(t)
	id, err := hn.worlds.Create(t.Context(), "Stars", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	body := hn.postLong(PathGeneratePreview, worldPreviewForm(id), c).Body.String()
	for _, want := range []string{`<svg class="starmap"`, "<title>Nauvis</title>", "<title>Vulcanus</title>", "<title>Gleba</title>",
		"<title>Fulgora</title>", "<title>Aquilo</title>", "<title>Solar system edge</title>", "<title>Shattered planet</title>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the preview lacks %s", want)
		}
	}
	svg := body[strings.Index(body, `<svg class="starmap"`):]
	svg = svg[:strings.Index(svg, "</svg>")]
	if got := strings.Count(svg, `<path d="`); got != 9 {
		t.Errorf("%d routes drawn, want 9", got)
	}
	if strings.Contains(body, "wiki.factorio.com/images/Vulcanus.png") {
		t.Error("wiki pictures while the setting is off")
	}
	if rec := hn.post(generalPath(), generalForm(map[string]string{"wiki_icons": "on"}), withCookie(c)); rec.Code != http.StatusSeeOther {
		t.Fatalf("save: %d", rec.Code)
	}
	body = hn.postLong(PathGeneratePreview, worldPreviewForm(id), c).Body.String()
	if !strings.Contains(body, "wiki.factorio.com/images/Vulcanus.png") {
		t.Error("no wiki picture while the setting is on")
	}
}

// SPEC 5.6: the embedded Space Age map is only for a world whose planets were
// read and that has Space Age on. A world whose planets could not be read (a
// modded world without its run), a world with Space Age off, and a 1.1 world
// show none; a cached run's map always wins.
func TestStarMapFallbackRule(t *testing.T) {
	s := &server{}
	spaceAge := mapgen.Builtin("2.1", true)
	cases := []struct {
		name    string
		cat     mapgen.Catalogue
		catErr  error
		version string
		want    bool
	}{
		{"plain Space Age", spaceAge, nil, "2.1.24", true},
		{"plain Space Age on 2.0", mapgen.Builtin("2.0", true), nil, "2.0.77", true},
		{"mods whose run is not read", spaceAge, errors.New("run failed"), "2.1.24", false},
		{"Space Age off", mapgen.Builtin("2.1", false), nil, "2.1.24", false},
		{"1.1", mapgen.Builtin("1.1", false), nil, "1.1.110", false},
	}
	withNoWorldStarMap(t)
	for _, tc := range cases {
		m, ok := s.starMapFor("w1", tc.version, tc.cat, tc.catErr)
		if ok != tc.want || (ok && len(m.Locations) != 7) {
			t.Errorf("%s: ok %t with %d locations, want ok %t", tc.name, ok, len(m.Locations), tc.want)
		}
	}
	// A cached run's map wins, even over a world with Space Age on.
	withWorldStarMap(t, starMapOf(t, `{"locations":[{"name":"a","distance":1},{"name":"b","distance":2}],"routes":[]}`))
	if m, ok := s.starMapFor("w1", "2.1.24", spaceAge, nil); !ok || len(m.Locations) != 2 {
		t.Errorf("a cached run's map = %d locations (ok %t), want its own 2", len(m.Locations), ok)
	}
}

// SPEC 5.6: the picture is sized for the name the page shows, translated, not
// the raw label: a place whose map holds no label, with a long name in
// Russian, at the right edge, has its whole label inside the view box.
func TestStarMapFitsTheTranslatedLabel(t *testing.T) {
	m := starMapOf(t, `{"locations":[
	 {"name":"nauvis","distance":10,"orientation":0.75,"label_orientation":0.75},
	 {"name":"solar-system-edge","distance":50,"orientation":0.25,"label_orientation":0.25}
	],"routes":[]}`)
	for _, loc := range []i18n.Locale{i18n.English, "ru", "zh-Hans"} {
		v, ok := layoutStarMap(m, mapgen.Builtin("2.1", true), loc, starMapIcons{})
		if !ok {
			t.Fatal("no star map")
		}
		x, _, w, _ := viewBoxOf(t, v)
		edge := v.Locations[1]
		shown := loc.Format(starMapLabel(mapgen.Builtin("2.1", true), mapgen.Location{Name: "solar-system-edge"}).Name)
		if want := labelCells(shown); edge.LabelCells != want || want == 0 {
			t.Fatalf("%s: %d cells for %q, want %d", loc, edge.LabelCells, shown, want)
		}
		if end := edge.LabelX + starMapCharWidth*v.Font*float64(edge.LabelCells); edge.Anchor != "start" || end > x+w {
			t.Errorf("%s: the label %q (%s) ends at %v, outside %q", loc, shown, edge.Anchor, end, v.ViewBox)
		}
	}
}

// SPEC 5.6: a wide (CJK) character is about two letters wide.
func TestLabelCells(t *testing.T) {
	for text, want := range map[string]int{"": 0, "Nauvis": 6, "太阳系": 6, "Nauvis 星": 9, "Край": 4} {
		if got := labelCells(text); got != want {
			t.Errorf("labelCells(%q) = %d, want %d", text, got, want)
		}
	}
}

// SPEC 5.6: a curved route is smooth at the large view's closest zoom: no
// piece turns more than two degrees from the one before, the spiral out from
// Aquilo to the edge of the system included; a straight route stays two
// points.
func TestStarMapRoutesAreSmooth(t *testing.T) {
	m := starMapOf(t, spaceAgeStarMap)
	byName := map[string]mapgen.Location{}
	for _, l := range m.Locations {
		byName[l.Name] = l
	}
	for _, r := range m.Routes {
		ps := routePoints(byName, byName[r.From], byName[r.To])
		if r.To == "muluna" {
			if len(ps) != 2 {
				t.Errorf("%s to %s is %d points, want straight", r.From, r.To, len(ps))
			}
			continue
		}
		if len(ps) < 3 {
			t.Errorf("%s to %s is %d points, want a curve", r.From, r.To, len(ps))
		}
		if b := maxBend(ps) * 180 / math.Pi; b > 2.0001 {
			t.Errorf("%s to %s turns %.2f degrees at a corner, want 2 at most", r.From, r.To, b)
		}
		if len(ps) > 200 {
			t.Errorf("%s to %s is %d points, too many for a small file", r.From, r.To, len(ps))
		}
	}
}

// labelOf is the laid-out location named n.
func labelOf(t *testing.T, v StarMapView, n string) StarMapLocation {
	t.Helper()
	for _, l := range v.Locations {
		if l.Name == n {
			return l
		}
	}
	t.Fatalf("no %s", n)
	return StarMapLocation{}
}

// scoreOf is the score of one label against every
// other label, every circle and every route piece in a laid-out view.
func scoreOf(v StarMapView, i int, circles []StarMapLocation) int {
	var pieces [][2]point
	for _, d := range v.Routes {
		ps := pathPointsOf(d)
		for j := 1; j < len(ps); j++ {
			pieces = append(pieces, [2]point{ps[j-1], ps[j]})
		}
	}
	var others []rect
	for j, o := range v.Locations {
		if j != i {
			others = append(others, labelBox(o))
		}
	}
	return labelScore(labelBox(v.Locations[i]), pieces, others, circles, i)
}

// pathPointsOf is pathPoints without a test, for a path the layout made.
func pathPointsOf(d string) []point {
	f := strings.Fields(strings.NewReplacer("M", "", "L", "").Replace(d))
	var out []point
	for i := 0; i+1 < len(f); i += 2 {
		x, _ := strconv.ParseFloat(f[i], 64)
		y, _ := strconv.ParseFloat(f[i+1], 64)
		out = append(out, point{x, y})
	}
	return out
}

// boxToCircle is how far a label's box is from a circle's edge, 0 when they
// overlap.
func boxToCircle(box rect, c StarMapLocation) float64 {
	nx := min(max(c.X, box.min.X), box.max.X)
	ny := min(max(c.Y, box.min.Y), box.max.Y)
	return max(0, math.Hypot(nx-c.X, ny-c.Y)-c.R)
}

// SPEC 5.6, DESIGN 8.23: a label takes the first of eight spots about its
// circle that covers no label and no circle, the game's side first.
func TestStarMapLabelAvoidsRoutesAndLabels(t *testing.T) {
	// a's label is on its right, where its route out to b leaves: it moves
	// round to a spot the route does not cross. b's and c's are clear on the
	// game's side and stay. d sits just below c with its label on the right,
	// over c's: it moves.
	v, _ := layoutStarMap(starMapOf(t, `{"locations":[
	 {"name":"a","distance":10,"orientation":0.25,"label_orientation":0.25},
	 {"name":"b","distance":20,"orientation":0.25,"label_orientation":0.25},
	 {"name":"c","distance":10,"orientation":0.5,"label_orientation":0.25},
	 {"name":"d","distance":10.3,"orientation":0.5,"label_orientation":0.25}
	],"routes":[{"from":"a","to":"b","length":1}]}`), mapgen.Catalogue{}, i18n.English, starMapIcons{})
	// c and d are closer than a font height, so no spot is clear of the
	// other's circle: only labels and routes are checked here.
	for i, l := range v.Locations {
		if got := scoreOf(v, i, nil); got != 0 {
			t.Errorf("%s's label scores %d, want no label or route covered", l.Name, got)
		}
	}
	for _, n := range []string{"b", "c"} {
		if got := labelOf(t, v, n).Anchor; got != "start" {
			t.Errorf("%s's label is anchored %s, want the game's side (start)", n, got)
		}
	}
	if a, d := labelOf(t, v, "a"), labelOf(t, v, "d"); a.Anchor == "start" && a.Baseline == "central" {
		t.Errorf("a's label stayed on the game's side over its route")
	} else if d.Anchor == "start" && d.Baseline == "central" {
		t.Errorf("d's label stayed on the game's side over c's label")
	}
	// No label covers a route, a label or a circle in the Space Age map.
	v, _ = layoutStarMap(starMapOf(t, spaceAgeStarMap), mapgen.Builtin("2.1", true), i18n.English, starMapIcons{})
	for i, l := range v.Locations {
		if got := scoreOf(v, i, v.Locations); got != 0 {
			t.Errorf("%s's label scores %d, want 0", l.Name, got)
		}
	}
}

// SPEC 5.6, DESIGN 8.23: when every spot covers something, a label takes the
// one that covers least, and a route crossed costs less than a label or a
// circle overlapped.
func TestStarMapLabelTakesTheLeastCovered(t *testing.T) {
	box := rect{point{0, 0}, point{10, 10}}
	pieces := [][2]point{{{-5, 5}, {15, 5}}, {{-5, 6}, {15, 6}}}
	over := []rect{{point{5, 5}, point{20, 20}}}
	if r, l := labelScore(box, pieces, nil, nil, 0), labelScore(box, nil, over, nil, 0); r != 2 || l != 10 {
		t.Errorf("two routes score %d and one label %d, want 2 and 10", r, l)
	}
	// The circle is the label's own (index 0) and plain, or another's and
	// grown by a font height (6 here) on every side.
	c := []StarMapLocation{{X: 20, Y: 5, R: 5, font: 5}}
	if own, other := labelScore(box, nil, nil, c, 0), labelScore(box, nil, nil, c, 1); own != 0 || other != 10 {
		t.Errorf("a circle 5 off scores %d as its own and %d as another's, want 0 and 10", own, other)
	}

	// hub has a route out in each of eight directions, so every spot for its
	// label is crossed. twin sits on the same place, listed first, with its
	// label on the game's side: hub's game side is covered by a label as well
	// as a route, so hub's label takes a spot with a route crossing only.
	moons := [][2]string{{"22", "0"}, {"18.5", "8.5"}, {"10", "12"}, {"1.5", "8.5"}, {"-2", "0"}, {"1.5", "-8.5"}, {"10", "-12"}, {"18.5", "-8.5"}}
	js := `{"locations":[
	 {"name":"twin","distance":10,"orientation":0.25,"label_orientation":0.25},
	 {"name":"hub","distance":10,"orientation":0.25,"label_orientation":0.25}`
	routes := ""
	for i, m := range moons {
		n := "m" + strconv.Itoa(i)
		js += `,{"name":"` + n + `","distance":1,"orientation":0,"position":{"x":` + m[0] + `,"y":` + m[1] + `},"parent":"hub"}`
		if i > 0 {
			routes += ","
		}
		routes += `{"from":"hub","to":"` + n + `","length":1}`
	}
	v, _ := layoutStarMap(starMapOf(t, js+`],"routes":[`+routes+`]}`), mapgen.Catalogue{}, i18n.English, starMapIcons{})
	hub := labelOf(t, v, "hub")
	if hub.Anchor == "start" && hub.Baseline == "central" {
		t.Errorf("hub's label stayed on the game's side over twin's label")
	}
	for i, l := range v.Locations {
		if l.Name == "hub" {
			if got := scoreOf(v, i, v.Locations); got%10 != 1 {
				t.Errorf("hub's label scores %d, want a route crossing and no label overlap (circle overlaps count 10 each)", got)
			}
		}
	}
}

// SPEC 5.6, DESIGN 8.23: a planet with two moons and a stargate close by,
// all labels on the same side, is laid out with no label over another label
// or over any circle.
func TestStarMapLabelsOfACluster(t *testing.T) {
	m := starMapOf(t, `{"locations":[
	 {"name":"nauvis","distance":15,"orientation":0.275,"label_orientation":0.25},
	 {"name":"muluna","distance":1.5,"orientation":0.75,"position":{"x":13.3153,"y":2.3465},"magnitude":0.4,"parent":"nauvis","label_orientation":0.25},
	 {"name":"lignumis","distance":1.5,"orientation":0.4,"position":{"x":15.9,"y":3.6},"magnitude":0.4,"parent":"nauvis","label_orientation":0.25},
	 {"name":"senestella-stargate","distance":17,"orientation":0.28,"position":{"x":16.9,"y":2.6},"magnitude":0.5,"label_orientation":0.25},
	 {"name":"aquilo","distance":35,"orientation":0.225}
	],"routes":[]}`)
	v, ok := layoutStarMap(m, mapgen.Catalogue{}, i18n.English, starMapIcons{})
	if !ok {
		t.Fatal("no map")
	}
	for i, l := range v.Locations {
		box := labelBox(l)
		for j, o := range v.Locations {
			if j == i {
				if d := boxToCircle(box, o); d <= 0 {
					t.Errorf("%s's label covers its own circle", l.Name)
				}
				continue
			}
			if p := labelBox(o); box.min.X < p.max.X && p.min.X < box.max.X && box.min.Y < p.max.Y && p.min.Y < box.max.Y {
				t.Errorf("%s's label covers %s's label", l.Name, o.Name)
			}
			if boxToCircle(box, o) <= 0 {
				t.Errorf("%s's label covers %s's circle", l.Name, o.Name)
			}
		}
	}
}

// SPEC 5.6, DESIGN 8.23: a label keeps a font height clear of another
// location's circle, so it is not read as that body's label: a planet whose
// game label side points at its moon, just below to the right, moves.
func TestStarMapLabelKeepsClearOfAnotherBody(t *testing.T) {
	v, _ := layoutStarMap(starMapOf(t, `{"locations":[
	 {"name":"nauvis","distance":15,"orientation":0.275},
	 {"name":"fulgora","distance":25,"orientation":0.325,"label_orientation":0.4},
	 {"name":"cerys","distance":1.39,"orientation":0.45,"position":{"x":23.5,"y":12.7},"parent":"fulgora","magnitude":0.5,"label_orientation":0.7}
	],"routes":[]}`), mapgen.Catalogue{}, i18n.English, starMapIcons{})
	f, c := labelOf(t, v, "fulgora"), labelOf(t, v, "cerys")
	if d, want := boxToCircle(labelBox(f), c), starMapLineHeight*f.font; d < want {
		t.Errorf("Fulgora's label is %.1f from Cerys's circle, want %.1f or more", d, want)
	}
}

// segmentInRect finds a segment through, inside, beside and across a box.
func TestSegmentInRect(t *testing.T) {
	box := rect{point{0, 0}, point{10, 10}}
	cases := []struct {
		a, b point
		want bool
	}{
		{point{-5, 5}, point{15, 5}, true},    // through
		{point{2, 2}, point{3, 3}, true},      // inside
		{point{-5, -5}, point{-1, 20}, false}, // beside
		{point{-5, 6}, point{6, -5}, true},    // across a corner
		{point{-5, 4}, point{4, -5}, false},   // past a corner
	}
	for _, c := range cases {
		if got := segmentInRect(c.a, c.b, box); got != c.want {
			t.Errorf("%v to %v: %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
