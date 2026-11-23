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
	// Dropped counts locations and routes left out for a bad or repeated
	// name, an out-of-range number, a hidden end, or an end the file does
	// not list.
	Dropped int
}

// Location is one space location: a planet, a moon, the solar system edge.
type Location struct {
	Name string
	// Label is the name until Labelled resolves the localised name.
	Label string
	// LocName is the localised name exactly as the file gave it.
	LocName json.RawMessage
	// Distance and Orientation are the location's polar position about the
	// universe origin. Orientation is turns, 0 to 1: 0 is straight up and it
	// grows clockwise.
	Distance    float64
	Orientation float64
	// X and Y are the position the game derives from them: X to the right
	// and Y downward, in the game's own units (Y is -Distance at
	// orientation 0).
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
//	      "parent": "fake-planet",        // optional: the location it orbits
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
// ignored. X and Y are not in the file: they are worked out as the game does,
// X = Distance * sin(2π * Orientation) and Y = -Distance * cos(2π *
// Orientation).
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
	Hidden           bool            `json:"hidden"`
	StarmapIcon      bool            `json:"starmap_icon"`
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
// one before it, that is hidden, or whose distance, magnitude or label
// orientation is negative or past 1e12, is left out and counted in Dropped. A
// route to or from a location not kept, or with a negative or oversized
// length, is left out the same way. A parent that is not a kept location, or
// that would make a loop, is cleared, so Location.Parent is always safe to
// follow. An empty map is not an error.
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
		if !finite(l.Distance, l.Orientation) || !finitePtr(l.Magnitude) || !finitePtr(l.LabelOrientation) {
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
	for _, l := range raw.Locations {
		mag, lab := 1.0, defaultLabelOrientation
		if l.Magnitude != nil {
			mag = *l.Magnitude
		}
		if l.LabelOrientation != nil {
			lab = *l.LabelOrientation
		}
		if kept[l.Name] || !ValidName(l.Name) || l.Hidden ||
			!inStarMapRange(l.Distance, mag, lab) {
			m.Dropped++
			continue
		}
		kept[l.Name] = true
		angle := 2 * math.Pi * l.Orientation
		m.Locations = append(m.Locations, Location{
			Name: l.Name, Label: l.Name, LocName: l.LocName,
			Distance: l.Distance, Orientation: l.Orientation,
			X: l.Distance * math.Sin(angle), Y: -l.Distance * math.Cos(angle),
			Magnitude: mag, LabelOrientation: lab,
			Parent: l.Parent, HasIcon: l.StarmapIcon,
		})
	}
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
