package web

import (
	"bytes"
	"encoding/xml"
	"html/template"
	"math"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/text/width"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/store"
)

// The star map's drawing (SPEC 5.6): a square picture about starMapSide
// units on a side. Positions scale to fit it; every size keeps the game's own
// ratio to the distances, measured from the game's star map, where a
// distance unit is about 15.85 pixels (DESIGN 8.23). Labels and small
// circles have a floor, so they still read on a map that spans far.
const (
	starMapSide = 1000.0
	// starMapPad is the room the scale leaves for circles and labels; the
	// view box then fits what is drawn, starMapMargin beyond it.
	starMapPad    = 40.0
	starMapMargin = 16.0
	// The sun's radius in game units (60 px in the game). It never reaches
	// past starMapSunReach of the nearest place about it, so a modded system
	// with places very near the sun still shows them; starMapSunMin is its
	// floor in picture units.
	starMapSun      = 3.8
	starMapSunReach = 0.75
	starMapSunMin   = 10.0
	// A location's circle, to its outer ring, is starMapRadius game units
	// times its magnitude (Nauvis 11 px, Vulcanus 16 px at 1.5), within
	// limits in picture units. Its picture fills starMapIcon of that radius,
	// with a dark gap to the ring, as the game draws it.
	starMapRadius    = 0.68
	starMapRadiusMin = 3.0
	starMapRadiusMax = 56.0
	starMapIcon      = 0.69
	// The label's font in game units (11.5 px, caps 8 px), with a floor of
	// starMapFontMin picture units: an eightieth of the picture, 12.5 px in
	// the large view at its first zoom on a 1000 px screen. starMapFontMax
	// keeps a system that spans only a few units from drawing huge labels.
	starMapFont    = 0.73
	starMapFontMin = starMapSide / 80
	starMapFontMax = starMapSide / 30
	// A label's guessed size, in fonts: a letter about 0.6 of it, a line 1.2.
	// starMapLabelGap is the space between a circle and its label (5 px).
	starMapCharWidth  = 0.6
	starMapLineHeight = 1.2
	starMapLabelGap   = 0.4
	// starMapArcStep is the largest turn, in radians, from one piece of a
	// curved route to the next: 2 degrees, smooth at the large view's closest
	// zoom. starMapArcMax caps the pieces of one route.
	starMapArcStep = math.Pi / 90
	starMapArcMax  = 1024
)

// wikiStarMapFiles are the space locations whose pictures the Factorio wiki
// holds, by the game's name: the base game's planets and Space Age's other
// two places. The wiki has no picture of the sun (SPEC 14.10).
var wikiStarMapFiles = map[string]string{
	"nauvis":            "Nauvis",
	"vulcanus":          "Vulcanus",
	"gleba":             "Gleba",
	"fulgora":           "Fulgora",
	"aquilo":            "Aquilo",
	"solar-system-edge": "Solar_system_edge",
	"shattered-planet":  "Shattered_planet",
}

// starMapPlaceNames are the game's own names for Space Age's places that are
// not planets.
var starMapPlaceNames = map[string]i18n.Msg{
	"solar-system-edge": i18n.M("mapgen.place.solar_system_edge"),
	"shattered-planet":  i18n.M("mapgen.place.shattered_planet"),
}

// StarMapView is a world's star map laid out for drawing: every position is
// in the picture's own units, inside ViewBox.
type StarMapView struct {
	ViewBox string
	Sun     float64 // the sun's radius; it sits at 0,0
	// Font is the labels' size; Halo the width of the dark edge about each.
	Font, Halo float64
	// Rings are the faint circles about the sun through each location
	// without a parent; Orbits the faint circles about a parent through its
	// moon.
	Rings  []float64
	Orbits []StarMapOrbit
	// Routes are SVG path data, one a route.
	Routes    []string
	Locations []StarMapLocation
	// File is true for the picture as a file: it carries its own namespace
	// and no images, so it opens offline (SPEC 5.6).
	File bool
}

// StarMapOrbit is a moon's orbit: a circle about its parent.
type StarMapOrbit struct{ X, Y, R float64 }

// StarMapLocation is one location's circle, picture and label.
type StarMapLocation struct {
	Name  string
	Label MapGenName
	// LabelCells is the label's width as the page shows it, in character
	// cells: a wide (CJK) character counts two.
	LabelCells int
	X, Y, R    float64
	// Icon is the picture drawn over the circle, or empty for none, in the
	// circle's square: IconX and IconY its corner, IconSize its side.
	Icon                   string
	IconX, IconY, IconSize float64
	// LabelX and LabelY are where the label's anchor sits; Anchor and
	// Baseline are its SVG text-anchor and dominant-baseline.
	LabelX, LabelY   float64
	Anchor, Baseline string
	font             float64 // the label's size, for its box
}

// starMapFile is what the "starmap-svg" template draws: one star map, on
// the page or as a file. The template is in the map preview's partial, so
// it is drawn with the template set of step 3, the page that shows it.
type starMapFile struct{ StarMapView }

func (starMapFile) templateName() string { return NewMapPage{}.templateName() }

// StarMapPicture is the star map as the map preview shows it: the SVG with
// its images, and where its file is served.
type StarMapPicture struct {
	SVG  template.HTML
	Path string
}

// DownloadPath is where the picture is served as a download.
func (p StarMapPicture) DownloadPath() string { return p.Path + "/download" }

// starMapName is the star map's file name in a preview's paths; a planet's
// name holds no dot, so it never names a planet.
const starMapName = "starmap.svg"

// worldStarMap is a world's star map from its cached planets run, without
// starting one; tests replace it. False when the run is not cached.
var worldStarMap = func(s *server, id string) (mapgen.StarMap, bool) {
	if s.game == nil {
		return mapgen.StarMap{}, false
	}
	defs, ok := s.game.CachedModPlanetDefs(id)
	if !ok || defs == nil {
		return mapgen.StarMap{}, false
	}
	return defs.StarMap, true
}

// starMapFor is the star map of a world on a game version (SPEC 5.6). A
// world with a cached planets run has that run's map, which holds Space Age
// and every mod. Without one, a world whose planets were read (catErr nil;
// a world with other mods than the game's own has its run read by then) and
// that has Space Age on has Space Age's own embedded map. Any other world
// has none: the plain Space Age map never stands for a modded system, and
// 1.1 has no star map.
func (s *server) starMapFor(id, version string, cat mapgen.Catalogue, catErr error) (mapgen.StarMap, bool) {
	if m, ok := worldStarMap(s, id); ok {
		return m, true
	}
	if catErr != nil || !cat.SpaceAge {
		return mapgen.StarMap{}, false
	}
	line, ok := mapgen.Line(version)
	if !ok {
		return mapgen.StarMap{}, false
	}
	return mapgen.SpaceAgeStarMap(line)
}

// drawStarMap draws a world's star map twice: for the page, with the
// location pictures, and as the file a download gives, without them. ok is
// false when the world has no map of two locations or more. version is the
// world's game version and catErr why its planets could not be read.
func (s *server) drawStarMap(r *http.Request, id, version string, cat mapgen.Catalogue, catErr error) (page template.HTML, file []byte, ok bool) {
	m, ok := s.starMapFor(id, version, cat, catErr)
	if !ok {
		return "", nil, false
	}
	v, ok := layoutStarMap(m, cat, localeOf(r), starMapIcons{world: id, wiki: wikiIconsOf(s.readWikiIcons(r))})
	if !ok {
		return "", nil, false
	}
	loc := localeOf(r)
	var pb, fb bytes.Buffer
	if err := s.render.Fragment(&pb, loc, starMapFile{v}, "starmap-svg"); err != nil {
		s.log.Error("draw the star map", "world", id, "error", err)
		return "", nil, false
	}
	v.File = true
	fb.WriteString(xml.Header)
	if err := s.render.Fragment(&fb, loc, starMapFile{v}, "starmap-svg"); err != nil {
		s.log.Error("draw the star map file", "world", id, "error", err)
		return "", nil, false
	}
	fb.WriteString("\n")
	return template.HTML(pb.String()), fb.Bytes(), true //nolint:gosec // drawn by our own template, which escapes every value
}

// handleStarMap serves the preview's star map as an SVG file, to the user
// whose preview it is: shown, or with download true, saved (SPEC 5.6).
func (s *server) handleStarMap(download bool) func(http.ResponseWriter, *http.Request, store.User) {
	return func(w http.ResponseWriter, r *http.Request, u store.User) {
		e, ok := s.previews.entry(r.PathValue("token"), u.ID)
		if !ok || e.starmap == nil {
			s.notFound(w, r, u)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "image/svg+xml")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		// The file is a picture: nothing in it may run or load.
		h.Set("Content-Security-Policy", "default-src 'none'")
		if download {
			h.Set("Content-Disposition", attachment(downloadStem(e.name, e.seed, "starmap")+".svg"))
		}
		h.Set("Content-Length", strconv.Itoa(len(e.starmap)))
		_, _ = w.Write(e.starmap)
	}
}

// starMapIcons says which picture goes over a location's circle.
type starMapIcons struct {
	world string // the world whose mods' icons are served
	wiki  bool   // the wiki's pictures are on (SPEC 14.10)
}

// icon is the picture over a location's circle: the wiki's for a base-game
// place while the setting is on, the mod's own starmap icon for a modded one
// that has one, else none. A base-game place never asks for a mod's icon, as
// its file is the game's own and is never served.
func (ic starMapIcons) icon(l mapgen.Location) string {
	if file, ok := wikiStarMapFiles[l.Name]; ok {
		if ic.wiki {
			return modsettings.WikiImageBase + file + ".png"
		}
		return ""
	}
	if l.HasIcon && ic.world != "" {
		return PathWorlds + "/" + ic.world + "/icons/starmap/" + l.Name + ".png"
	}
	return ""
}

// layoutStarMap lays out a world's star map (SPEC 5.6), or returns false when
// it has fewer than two locations, which make no map. cat gives the planets'
// labels; a location it does not name keeps the map's own label. loc is the
// language the labels are shown in, so their width is measured as shown.
func layoutStarMap(m mapgen.StarMap, cat mapgen.Catalogue, loc i18n.Locale, ic starMapIcons) (StarMapView, bool) {
	if len(m.Locations) < 2 {
		return StarMapView{}, false
	}
	byName := make(map[string]mapgen.Location, len(m.Locations))
	for _, l := range m.Locations {
		byName[l.Name] = l
	}
	// Routes first, in game units, as an arc can reach past its two ends.
	routes := make([][]point, 0, len(m.Routes))
	for _, r := range m.Routes {
		routes = append(routes, routePoints(byName, byName[r.From], byName[r.To]))
	}
	// The bounds of everything drawn, the sun included, in game units.
	b := bounds{minX: 0, maxX: 0, minY: 0, maxY: 0}
	for _, l := range m.Locations {
		b.add(point{l.X, l.Y})
	}
	for _, r := range routes {
		for _, p := range r {
			b.add(p)
		}
	}
	span := max(b.maxX-b.minX, b.maxY-b.minY)
	scale := 1.0
	if span > 0 {
		scale = (starMapSide - 2*starMapPad) / span
	}
	font := min(starMapFontMax, max(starMapFontMin, starMapFont*scale))
	v := StarMapView{Sun: round1(max(starMapSunMin, starMapSun*scale)), Font: round1(font), Halo: round1(font / 5)}
	for _, l := range m.Locations {
		if d := math.Hypot(l.X, l.Y) * scale; l.Parent == "" && d > 0 {
			v.Sun = min(v.Sun, round1(starMapSunReach*d))
		}
	}
	seen := map[float64]bool{}
	for _, l := range m.Locations {
		if p, ok := byName[l.Parent]; ok && l.Parent != "" {
			r := math.Hypot(l.X-p.X, l.Y-p.Y) * scale
			v.Orbits = append(v.Orbits, StarMapOrbit{X: round1(p.X * scale), Y: round1(p.Y * scale), R: round1(r)})
			continue
		}
		// The ring passes through the location as drawn: from 2.1 its
		// distance is about its origin, which need not be the sun.
		d := round1(math.Hypot(l.X, l.Y) * scale)
		if d > 0 && !seen[d] {
			seen[d] = true
			v.Rings = append(v.Rings, d)
		}
	}
	var pieces [][2]point // every route's pieces, in the picture's units
	for _, r := range routes {
		v.Routes = append(v.Routes, pathData(r, scale))
		for i := 1; i < len(r); i++ {
			pieces = append(pieces, [2]point{{r[i-1].X * scale, r[i-1].Y * scale}, {r[i].X * scale, r[i].Y * scale}})
		}
	}
	var labels []rect
	for _, l := range m.Locations {
		label := starMapLabel(cat, l)
		cells := labelCells(loc.Format(label.Name))
		place := func(turns float64) StarMapLocation {
			sl := placeLocation(l, turns, scale, font, label, ic.icon(l))
			sl.LabelCells = cells
			return sl
		}
		// The label sits on the game's side unless it would cover a route or
		// a label already placed and the opposite side covers neither.
		sl := place(l.LabelOrientation)
		if labelClashes(labelBox(sl), pieces, labels) {
			if other := place(l.LabelOrientation + 0.5); !labelClashes(labelBox(other), pieces, labels) {
				sl = other
			}
		}
		labels = append(labels, labelBox(sl))
		v.Locations = append(v.Locations, sl)
	}
	v.ViewBox = fitStarMap(v, routes, scale)
	return v, true
}

// fitStarMap is the square view box about everything drawn: the sun, each
// circle, each route and each label, so a long label at the edge is not cut
// off. A label's width is guessed from the name the page shows, as the font
// is the reader's.
func fitStarMap(v StarMapView, routes [][]point, scale float64) string {
	b := bounds{minX: -v.Sun, maxX: v.Sun, minY: -v.Sun, maxY: v.Sun}
	for _, r := range routes {
		for _, p := range r {
			b.add(point{p.X * scale, p.Y * scale})
		}
	}
	for _, l := range v.Locations {
		b.add(point{l.X - l.R, l.Y - l.R})
		b.add(point{l.X + l.R, l.Y + l.R})
		box := labelBox(l)
		b.add(box.min)
		b.add(box.max)
	}
	side := max(b.maxX-b.minX, b.maxY-b.minY) + 2*starMapMargin
	cx, cy := (b.minX+b.maxX)/2, (b.minY+b.maxY)/2
	return svgNum(cx-side/2) + " " + svgNum(cy-side/2) + " " + svgNum(side) + " " + svgNum(side)
}

// rect is a box in the picture's units, min its top left corner.
type rect struct{ min, max point }

// labelBox is the box a location's label covers. Its width is guessed from
// the name the page shows, as the font is the reader's.
func labelBox(l StarMapLocation) rect {
	w, h := starMapCharWidth*l.font*float64(l.LabelCells), starMapLineHeight*l.font
	x0, y0 := l.LabelX-w/2, l.LabelY-h/2
	switch l.Anchor {
	case "start":
		x0 = l.LabelX
	case "end":
		x0 = l.LabelX - w
	}
	switch l.Baseline {
	case "text-after-edge":
		y0 = l.LabelY - h
	case "text-before-edge":
		y0 = l.LabelY
	}
	return rect{point{x0, y0}, point{x0 + w, y0 + h}}
}

// labelClashes is true when a label's box covers part of a route's piece or
// of another label's box.
func labelClashes(box rect, pieces [][2]point, labels []rect) bool {
	for _, o := range labels {
		if box.min.X < o.max.X && o.min.X < box.max.X && box.min.Y < o.max.Y && o.min.Y < box.max.Y {
			return true
		}
	}
	for _, p := range pieces {
		if segmentInRect(p[0], p[1], box) {
			return true
		}
	}
	return false
}

// segmentInRect is true when the segment a to b passes through the box: it
// clips the segment to each of the box's four sides in turn (Liang-Barsky).
func segmentInRect(a, b point, r rect) bool {
	t0, t1 := 0.0, 1.0
	dx, dy := b.X-a.X, b.Y-a.Y
	for _, e := range [4][2]float64{{-dx, a.X - r.min.X}, {dx, r.max.X - a.X}, {-dy, a.Y - r.min.Y}, {dy, r.max.Y - a.Y}} {
		p, q := e[0], e[1]
		if p == 0 {
			if q < 0 {
				return false
			}
			continue
		}
		t := q / p
		if p < 0 {
			t0 = max(t0, t)
		} else {
			t1 = min(t1, t)
		}
		if t0 > t1 {
			return false
		}
	}
	return true
}

// labelCells is the width of text in character cells: a wide or fullwidth
// (CJK) character is two, any other one.
func labelCells(text string) int {
	n := 0
	for _, r := range text {
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			n += 2
		default:
			n++
		}
	}
	return n
}

// starMapLabel is a location's name as the page shows it: the game's own
// name in the person's language for a base-game place, else the mod's label.
func starMapLabel(cat mapgen.Catalogue, l mapgen.Location) MapGenName {
	if m, ok := planetNames[l.Name]; ok {
		return MapGenName{Name: m}
	}
	if m, ok := starMapPlaceNames[l.Name]; ok {
		return MapGenName{Name: m}
	}
	label := l.Label
	for _, p := range cat.Planets {
		if p.Name == l.Name && p.Label != "" {
			label = p.Label
		}
	}
	return MapGenName{Name: i18n.M("mapgen.mod_name", "name", modLabel(label, l.Name)), Foreign: true}
}

// placeLocation is a location's circle and its label, which sits beside the
// circle in the direction turns names: turns from straight up, clockwise, as
// the game has a label orientation. font is the label's size.
func placeLocation(l mapgen.Location, turns, scale, font float64, label MapGenName, icon string) StarMapLocation {
	r := min(starMapRadiusMax, max(starMapRadiusMin, starMapRadius*l.Magnitude*scale))
	x, y := l.X*scale, l.Y*scale
	a := 2 * math.Pi * turns
	dx, dy := math.Sin(a), -math.Cos(a)
	gap := r + starMapLabelGap*font
	out := StarMapLocation{
		Name: l.Name, Label: label, Icon: icon,
		X: round1(x), Y: round1(y), R: round1(r),
		IconX: round1(x - starMapIcon*r), IconY: round1(y - starMapIcon*r), IconSize: round1(2 * starMapIcon * r),
		LabelX: round1(x + dx*gap), LabelY: round1(y + dy*gap),
		Anchor: "middle", Baseline: "central", font: font,
	}
	switch {
	case dx > 0.3:
		out.Anchor = "start"
	case dx < -0.3:
		out.Anchor = "end"
	}
	switch {
	case dy < -0.3:
		out.Baseline = "text-after-edge"
	case dy > 0.3:
		out.Baseline = "text-before-edge"
	}
	return out
}

type point struct{ X, Y float64 }

type bounds struct{ minX, maxX, minY, maxY float64 }

func (b *bounds) add(p point) {
	b.minX, b.maxX = min(b.minX, p.X), max(b.maxX, p.X)
	b.minY, b.maxY = min(b.minY, p.Y), max(b.maxY, p.Y)
}

// routePoints is a route as the game draws it, in game units. It arcs about
// the two ends' nearest common parent, the sun for two planets: a spiral
// whose distance from that centre and angle about it each change evenly from
// one end to the other, the shorter way round. It is straight when the
// common parent is one of the ends, or when the ends share nothing but the
// universe's origin (planets of two different stars).
func routePoints(byName map[string]mapgen.Location, a, b mapgen.Location) []point {
	straight := []point{{a.X, a.Y}, {b.X, b.Y}}
	centre, ok := commonParent(byName, a, b)
	if !ok {
		return straight
	}
	ax, ay := a.X-centre.X, a.Y-centre.Y
	bx, by := b.X-centre.X, b.Y-centre.Y
	ra, rb := math.Hypot(ax, ay), math.Hypot(bx, by)
	if ra == 0 || rb == 0 {
		return straight
	}
	ta := math.Atan2(ay, ax)
	turn := math.Remainder(math.Atan2(by, bx)-ta, 2*math.Pi)
	spiral := func(n int) []point {
		out := make([]point, 0, n+1)
		out = append(out, point{a.X, a.Y})
		for i := 1; i < n; i++ {
			t := float64(i) / float64(n)
			r, th := ra+(rb-ra)*t, ta+turn*t
			out = append(out, point{centre.X + r*math.Cos(th), centre.Y + r*math.Sin(th)})
		}
		return append(out, point{b.X, b.Y})
	}
	// A circle turns as much from piece to piece as it does about its centre;
	// a spiral whose distance changes fast turns more where it bends, so the
	// pieces halve until no corner is sharper than the step.
	n := max(2, int(math.Ceil(math.Abs(turn)/starMapArcStep)))
	out := spiral(n)
	for maxBend(out) > starMapArcStep && 2*n <= starMapArcMax {
		n *= 2
		out = spiral(n)
	}
	return out
}

// maxBend is the sharpest turn, in radians, from one piece of a line to the
// next.
func maxBend(ps []point) float64 {
	m := 0.0
	for i := 2; i < len(ps); i++ {
		a := math.Atan2(ps[i-1].Y-ps[i-2].Y, ps[i-1].X-ps[i-2].X)
		b := math.Atan2(ps[i].Y-ps[i-1].Y, ps[i].X-ps[i-1].X)
		m = max(m, math.Abs(math.Remainder(b-a, 2*math.Pi)))
	}
	return m
}

// commonParent is the centre a route between a and b arcs about: their
// nearest common parent, or the sun at the origin for two locations that have
// none. It is false when the route is straight: the common parent is one of
// the ends, or only the origin is shared and one end has a parent of its own.
func commonParent(byName map[string]mapgen.Location, a, b mapgen.Location) (point, bool) {
	// Each chain starts at the location itself, so a moon and its own planet
	// find the planet: one of the ends. The decoder keeps chains finite.
	above := map[string]bool{}
	for n := a.Name; n != ""; n = byName[n].Parent {
		above[n] = true
	}
	for n := b.Name; n != ""; n = byName[n].Parent {
		if above[n] {
			if n == a.Name || n == b.Name {
				return point{}, false
			}
			p := byName[n]
			return point{p.X, p.Y}, true
		}
	}
	if a.Parent == "" && b.Parent == "" {
		return point{}, true
	}
	return point{}, false
}

// pathData is SVG path data for points in game units, scaled.
func pathData(ps []point, scale float64) string {
	var b strings.Builder
	for i, p := range ps {
		if i == 0 {
			b.WriteString("M")
		} else {
			b.WriteString(" L")
		}
		b.WriteString(svgNum(p.X*scale) + " " + svgNum(p.Y*scale))
	}
	return b.String()
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

// svgNum writes a coordinate with one decimal at most.
func svgNum(f float64) string { return strconv.FormatFloat(round1(f), 'f', -1, 64) }
