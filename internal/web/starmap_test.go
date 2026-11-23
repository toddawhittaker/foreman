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
const spaceAgeStarMap = `{"locations":[
 {"name":"nauvis","distance":15,"orientation":0.275},
 {"name":"vulcanus","distance":10,"orientation":0.1,"magnitude":1.5},
 {"name":"gleba","distance":20,"orientation":0.175,"magnitude":1.1},
 {"name":"fulgora","distance":25,"orientation":0.325,"magnitude":0.9},
 {"name":"aquilo","distance":35,"orientation":0.225},
 {"name":"solar-system-edge","distance":50,"orientation":0.25,"label_orientation":0.15},
 {"name":"muluna","distance":15.3,"orientation":0.27,"magnitude":0.4,"parent":"nauvis","label_orientation":0.75,"starmap_icon":true},
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
	v, ok := layoutStarMap(starMapOf(t, spaceAgeStarMap), mapgen.Builtin("2.1", true), starMapIcons{})
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
		if _, ok := layoutStarMap(starMapOf(t, js), mapgen.Catalogue{}, starMapIcons{}); ok {
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
		v, ok := layoutStarMap(starMapOf(t, js), mapgen.Catalogue{}, starMapIcons{})
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
			if end := l.LabelX + starMapCharWidth*float64(len(l.Name)); l.Anchor == "start" && end > x+w {
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
	v, _ := layoutStarMap(starMapOf(t, spaceAgeStarMap), mapgen.Builtin("2.1", true), starMapIcons{})
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
		l := placeLocation(mapgen.Location{Magnitude: 1, LabelOrientation: tc.turns}, 1, MapGenName{}, "")
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
		"Download the star map (SVG)"} {
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
	if !bytes.Contains(file, []byte(`xmlns="http://www.w3.org/2000/svg"`)) || bytes.Contains(file, []byte("<image")) {
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
func TestPreviewStarMapOnlyWithAllPlanets(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	withWorldCatalogue(t, mapgen.Builtin("2.1", true), nil)
	withWorldStarMap(t, starMapOf(t, spaceAgeStarMap))
	id, err := hn.worlds.Create(t.Context(), "Stars", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	one := hn.postLong(PathGeneratePreview, worldPreviewForm(id, "auto", "1", "preview_view", "one"), c).Body.String()
	if strings.Contains(one, `class="starmap"`) {
		t.Error("One planet shows the star map")
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
