package mapgen

import (
	"encoding/json"
	"errors"
	"math"
	"slices"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

// Limits on a world's star map file (SPEC 14.6). A file past any of them is
// refused whole.
const (
	MaxStarMapFileBytes = 256 << 10
	MaxStarMapLocations = 256
	MaxStarMapRoutes    = 1024
	// maxStarMapValue bounds a distance, magnitude or route length. The game's
	// own are in the thousands; a larger number is a broken mod, and the
	// bound keeps every derived coordinate finite.
	maxStarMapValue = 1e12
	// defaultLabelOrientation is the game's default for a location that
	// gives none: the label to the right of the location.
	defaultLabelOrientation = 0.25
	// maxStarMapStars bounds the stars rebuilt from their bodies' orbits.
	maxStarMapStars = 16
	// sunReach is how near 0,0 a rebuilt star may land and still be taken
	// for the sun itself.
	sunReach = 1.0
)

var (
	errStarMapSize   = errors.New("mapgen: the star map file is too large")
	errStarMapJSON   = errors.New("mapgen: the star map file is not valid")
	errStarMapCount  = errors.New("mapgen: the star map file lists too many entries")
	errStarMapNumber = errors.New("mapgen: the star map file holds a number that is not finite")
)

// StarMap is a world's space locations and the routes between them, as the
// game places them: the mod set's map, not a save's progress. Hidden
// locations, and every route that touches one, are not in it.
type StarMap struct {
	// Locations are in the file's order.
	Locations []Location
	Routes    []Route
	// Stars are stars other than the sun that the file does not list, as a
	// mod draws them only as a picture (PlanetsLib sprite_only); each is
	// rebuilt from where its bodies are and their orbit about it.
	Stars []Star
	// Dropped counts locations and routes left out for a bad or repeated
	// name, an out-of-range number, a hidden end, or an end the file does
	// not list, and orbit offsets left out for an out-of-range number.
	Dropped int
}

// Star is a rebuilt star: its name and where the game draws it, in the same
// units as Location.X and Y.
type Star struct {
	Name string
	X, Y float64
}

// Location is one space location: a planet, a moon, the solar system edge.
type Location struct {
	Name string
	// Label is the name until Labelled resolves the localised name.
	Label string
	// LocName is the localised name exactly as the file gave it.
	LocName json.RawMessage
	// Distance and Orientation are the location's polar position about
	// what it orbits: the sun, or from 2.1 a moon's planet. Orientation is
	// turns, 0 to 1: 0 is straight up and it grows clockwise.
	Distance    float64
	Orientation float64
	// X and Y are where the game draws the location on its star map, about
	// the sun: X to the right and Y downward, in the game's own units (Y is
	// -Distance at orientation 0 for a location about the sun).
	X, Y float64
	// Magnitude scales how big the game draws the location; 1 is a planet.
	Magnitude float64
	// LabelOrientation is where the label sits around the location, in the
	// same turns as Orientation.
	LabelOrientation float64
	// Parent is the name of the location this one orbits, such as a
	// PlanetsLib moon's planet, or "" when it has none. It always names
	// another location in Locations, and following Parent never loops, so
	// a caller can walk the chain to find two locations' nearest common
	// parent.
	Parent string
	// Star names the entry in StarMap.Stars this location orbits, or is ""
	// when it orbits the sun or a Parent. A location never has both.
	Star string
	// HasIcon is true when the location's prototype has a starmap icon.
	HasIcon bool
}

// Route is one connection between two locations in Locations.
type Route struct {
	From, To string
	// Length is the route's length in kilometres.
	Length float64
}

// Location returns the named location, and false when there is none.
func (m StarMap) Location(name string) (Location, bool) {
	for _, l := range m.Locations {
		if l.Name == name {
			return l, true
		}
	}
	return Location{}, false
}

// Labelled returns the map with each location's Label resolved from the
// locale sections, stripped of rich text; a name that does not resolve is its
// own label. The result shares no slice with m.
func (m StarMap) Labelled(sections ...modsettings.LocaleSections) StarMap {
	m.Locations = slices.Clone(m.Locations)
	for i, l := range m.Locations {
		if s, ok := modsettings.ResolveLocalised(l.LocName, sections...); ok {
			if s = modsettings.StripRichText(s); s != "" {
				m.Locations[i].Label = s
			}
		}
	}
	return m
}

// The star map file a world's mod-settings run writes (starmap.json). Its
// shape, which internal/game/modsettings/control.lua writes:
//
//	{
//	  "locations": [                      // space locations the game lists
//	    {
//	      "name": "vulcanus",
//	      "localised_name": ["space-location-name.vulcanus"],
//	      "distance": 10,                 // prototype distance
//	      "orientation": 0.1,             // prototype orientation, 0 to 1
//	      "magnitude": 1,                 // optional, default 1
//	      "label_orientation": 0.25,      // optional, default 0.25
//	      "position": {"x": 5.9, "y": -8.1}, // optional: the game's own
//	      "parent": "fake-planet",        // optional: the location it orbits
//	      "orbit": {"distance": 10, "orientation": 0.8}, // optional: PlanetsLib's
//	                                      // offset from the parent
//	      "hidden": false,                // optional: true leaves it out
//	      "starmap_icon": true            // optional: it has a starmap icon
//	    }
//	  ],
//	  "routes": [                         // space connections
//	    {"from": "nauvis", "to": "vulcanus", "length": 15000}   // km
//	  ]
//	}
//
// An empty list may be written as {} (Lua's empty table). Unknown fields are
// ignored. X and Y are the position. A file without one (written before
// Foreman read it) has them worked out as the game does about the sun, X =
// Distance * sin(2π * Orientation) and Y = -Distance * cos(2π *
// Orientation): right on 2.0, where every location's distance is about the
// sun, but not for a 2.1 moon, whose distance is about its planet.
type rawStarMap struct {
	Locations luaList[rawLocation] `json:"locations"`
	Routes    luaList[rawRoute]    `json:"routes"`
}

type rawLocation struct {
	Name             string          `json:"name"`
	LocName          json.RawMessage `json:"localised_name"`
	Distance         float64         `json:"distance"`
	Orientation      float64         `json:"orientation"`
	Magnitude        *float64        `json:"magnitude"`
	LabelOrientation *float64        `json:"label_orientation"`
	Parent           string          `json:"parent"`
	Orbit            *rawOrbit       `json:"orbit"`
	Position         *rawPosition    `json:"position"`
	Hidden           bool            `json:"hidden"`
	StarmapIcon      bool            `json:"starmap_icon"`
}

type rawPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type rawOrbit struct {
	Distance    float64 `json:"distance"`
	Orientation float64 `json:"orientation"`
}

type rawRoute struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Length float64 `json:"length"`
}

// DecodeStarMap reads a world's star map file under the SPEC 14.6 limits: a
// file over MaxStarMapFileBytes, with more than MaxStarMapLocations
// locations or MaxStarMapRoutes routes, with a number that is not finite, or
// not of the shape above is refused whole, with an error that holds no text
// from the file. A location whose name is not ^[a-z0-9_-]{1,64}$ or repeats
// one before it, that is hidden, whose distance, magnitude or label
// orientation is negative or past 1e12, or whose position is past 1e12 either
// way, is left out and counted in Dropped. A
// route to or from a location not kept, or with a negative or oversized
// length, is left out the same way. A parent that is not a kept location, or
// that would make a loop, is cleared, so Location.Parent is always safe to
// follow. A parent that is not a kept location but that the location gives
// an orbit offset about is rebuilt as a star (see StarMap.Stars), unless it
// lands on the sun; an offset with a negative or oversized number is left
// out and counted in Dropped. An empty map is not an error.
func DecodeStarMap(data []byte) (StarMap, error) {
	if len(data) > MaxStarMapFileBytes {
		return StarMap{}, errStarMapSize
	}
	var raw rawStarMap
	// Errors are replaced, so no text from the file reaches a log. A number
	// too big for a float64 fails here too.
	if err := json.Unmarshal(data, &raw); err != nil {
		return StarMap{}, errStarMapJSON
	}
	if len(raw.Locations) > MaxStarMapLocations || len(raw.Routes) > MaxStarMapRoutes {
		return StarMap{}, errStarMapCount
	}
	for _, l := range raw.Locations {
		if !finite(l.Distance, l.Orientation) || !finitePtr(l.Magnitude) || !finitePtr(l.LabelOrientation) ||
			(l.Position != nil && !finite(l.Position.X, l.Position.Y)) ||
			(l.Orbit != nil && !finite(l.Orbit.Distance, l.Orbit.Orientation)) {
			return StarMap{}, errStarMapNumber
		}
	}
	for _, r := range raw.Routes {
		if !finite(r.Length) {
			return StarMap{}, errStarMapNumber
		}
	}
	var m StarMap
	kept := map[string]bool{}
	orbits := map[string]rawOrbit{} // by kept location
	for _, l := range raw.Locations {
		mag, lab := 1.0, defaultLabelOrientation
		if l.Magnitude != nil {
			mag = *l.Magnitude
		}
		if l.LabelOrientation != nil {
			lab = *l.LabelOrientation
		}
		angle := 2 * math.Pi * l.Orientation
		x, y := l.Distance*math.Sin(angle), -l.Distance*math.Cos(angle)
		if l.Position != nil {
			x, y = l.Position.X, l.Position.Y
		}
		if kept[l.Name] || !ValidName(l.Name) || l.Hidden ||
			!inStarMapRange(l.Distance, mag, lab) || math.Abs(x) > maxStarMapValue || math.Abs(y) > maxStarMapValue {
			m.Dropped++
			continue
		}
		kept[l.Name] = true
		if o := l.Orbit; o != nil && l.Parent != "" {
			if o.Distance < 0 || o.Distance > maxStarMapValue || math.Abs(o.Orientation) > maxStarMapValue {
				m.Dropped++
			} else {
				orbits[l.Name] = *o
			}
		}
		m.Locations = append(m.Locations, Location{
			Name: l.Name, Label: l.Name, LocName: l.LocName,
			Distance: l.Distance, Orientation: l.Orientation,
			X: x, Y: y,
			Magnitude: mag, LabelOrientation: lab,
			Parent: l.Parent, HasIcon: l.StarmapIcon,
		})
	}
	m.Stars = rebuildStars(m.Locations, kept, orbits)
	clearBadParents(m.Locations, kept)
	// A route is drawn once: a repeat, in either direction, is dropped.
	seen := map[[2]string]bool{}
	for _, r := range raw.Routes {
		pair := [2]string{min(r.From, r.To), max(r.From, r.To)}
		if !kept[r.From] || !kept[r.To] || r.From == r.To || r.Length < 0 || r.Length > maxStarMapValue || seen[pair] {
			m.Dropped++
			continue
		}
		seen[pair] = true
		m.Routes = append(m.Routes, Route(r))
	}
	return m, nil
}

// rebuildStars sets Star on each location whose parent is not kept but which
// gives an orbit offset about it, and returns those stars. A star sits at the
// first such location's position less its offset; one on the sun, past the
// limit, or with a bad name is not rebuilt, and its bodies orbit the sun.
func rebuildStars(locs []Location, kept map[string]bool, orbits map[string]rawOrbit) []Star {
	var stars []Star
	// found is true for a star rebuilt, false for one refused, so every body
	// of one star is judged by the first.
	found := map[string]bool{}
	for i, l := range locs {
		o, ok := orbits[l.Name]
		if !ok || kept[l.Parent] || !ValidName(l.Parent) {
			continue
		}
		rebuilt, seen := found[l.Parent]
		if !seen {
			angle := 2 * math.Pi * o.Orientation
			x, y := l.X-o.Distance*math.Sin(angle), l.Y+o.Distance*math.Cos(angle)
			if math.Hypot(x, y) <= sunReach || math.Abs(x) > maxStarMapValue || math.Abs(y) > maxStarMapValue ||
				len(stars) >= maxStarMapStars {
				found[l.Parent] = false
				continue
			}
			found[l.Parent], rebuilt = true, true
			stars = append(stars, Star{Name: l.Parent, X: x, Y: y})
		}
		if rebuilt {
			locs[i].Star = l.Parent
		}
	}
	return stars
}

// finite reports whether every value is neither NaN nor infinite.
func finite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

func finitePtr(v *float64) bool { return v == nil || finite(*v) }

// inStarMapRange reports whether a location's distance, magnitude and label
// orientation are within what Foreman will draw. Orientations wrap, so they
// only need to be finite; the label's is bounded to keep it sane.
func inStarMapRange(distance, magnitude, label float64) bool {
	return distance >= 0 && distance <= maxStarMapValue &&
		magnitude >= 0 && magnitude <= maxStarMapValue &&
		label >= -maxStarMapValue && label <= maxStarMapValue
}

// clearBadParents clears each Parent that names a location not kept, itself,
// or one whose chain comes back round, so no chain loops.
func clearBadParents(locs []Location, kept map[string]bool) {
	parent := make(map[string]string, len(locs))
	for _, l := range locs {
		if kept[l.Parent] && l.Parent != l.Name {
			parent[l.Name] = l.Parent
		}
	}
	for i, l := range locs {
		locs[i].Parent = parent[l.Name]
		// A location is on a loop when walking up its parents comes back to
		// it. Only those are cleared; a child of a loop keeps its parent,
		// which is itself cleared.
		p := parent[l.Name]
		for steps := 0; p != "" && steps <= len(locs); steps++ {
			if p == l.Name {
				locs[i].Parent = ""
				break
			}
			p = parent[p]
		}
	}
}
