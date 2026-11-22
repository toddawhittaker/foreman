package mapgen

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

// Limits on a world's planet file (SPEC 14.6). A file
// past any of them is refused whole and the built-in catalogue is used.
const (
	MaxPlanetFileBytes = 1 << 20
	MaxPlanets         = 64
	MaxControls        = 512
	MaxPlanetControls  = 128
	MaxResources       = 512
)

var (
	errPlanetsSize  = errors.New("mapgen: the planet file is too large")
	errPlanetsJSON  = errors.New("mapgen: the planet file is not valid")
	errPlanetsCount = errors.New("mapgen: the planet file lists too many entries")
)

// nameRE is the shape Foreman accepts for a planet, control or resource name
// from a world's mods; others are left out and counted.
var nameRE = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)

// Catalogue is what a world's map generator offers: its planets, each with
// its autoplace controls, and the resources the mods define. Builtin gives
// the game's own; ParseWorldPlanets reads one exported from a world's mods.
type Catalogue struct {
	// NoPlanets is true on 1.1: Planets then holds one entry, the world's
	// single surface, named "nauvis".
	NoPlanets bool
	Planets   []Planet
	// Controls holds every control any planet names, keyed by name.
	Controls map[string]CatalogueControl
	// Resources are the resource entities, in the file's order.
	Resources []Resource
	// Dropped counts planets, controls and resources left out for a bad or
	// repeated name, or for naming a control the file does not define.
	Dropped int
}

// Planet is one planet and the controls its map generator uses, in the
// game's order.
type Planet struct {
	Name  string
	Label string
	// Controls names the planet's controls; look each up in
	// Catalogue.Controls.
	Controls []string
	// Defaults holds the planet's own default for a control, when the file
	// gives one (Moshine sets steam_geyser richness to 150, not 1).
	Defaults map[string]ControlDefault
	// Resources names the resource entities the planet places, when known.
	Resources []string
}

// ControlDefault is a planet's default frequency, size and richness for one
// control. A nil field is the game's default of 1.
type ControlDefault struct {
	Frequency *float64 `json:"frequency"`
	Size      *float64 `json:"size"`
	Richness  *float64 `json:"richness"`
}

// CatalogueControl is a Control with what the form shows for it.
type CatalogueControl struct {
	Control
	Order string
	Label string
	// Planets lists every planet that uses the control, in planet order. A
	// control on two or more planets is one value, shown under each with
	// "Also changes ..." (SPEC 5.6).
	Planets []string
}

// Resource is one resource entity and the control that places it, when the
// game names one (on 2.x most name none; the planet's list links them).
type Resource struct {
	Name    string
	Control string
	Label   string
}

// PlanetControls returns a planet's controls in its order, as Controls(line,
// planet) does for the built-in data. It returns nil for an unknown planet.
func (c Catalogue) PlanetControls(planet string) []Control {
	for _, p := range c.Planets {
		if p.Name != planet {
			continue
		}
		out := make([]Control, 0, len(p.Controls))
		for _, n := range p.Controls {
			out = append(out, c.Controls[n].Control)
		}
		return out
	}
	return nil
}

// PlanetNames returns the planets' names in order.
func (c Catalogue) PlanetNames() []string {
	out := make([]string, len(c.Planets))
	for i, p := range c.Planets {
		out[i] = p.Name
	}
	return out
}

// AlsoChanges returns the other planets a control on planet also changes
// (SPEC 5.6), or nil when it is the planet's alone.
func (c Catalogue) AlsoChanges(control, planet string) []string {
	var out []string
	for _, p := range c.Controls[control].Planets {
		if p != planet {
			out = append(out, p)
		}
	}
	return out
}

// Builtin returns the game's own catalogue for a line, from the embedded
// data: the planets Planets(line, spaceAge) lists, with Controls(line, p)
// for each. Labels are the control names; it returns an empty catalogue
// for an unknown line.
func Builtin(line string, spaceAge bool) Catalogue {
	c := Catalogue{NoPlanets: line == "1.1", Controls: map[string]CatalogueControl{}}
	for _, name := range Planets(line, spaceAge) {
		p := Planet{Name: name, Label: name}
		for _, ctl := range Controls(line, name) {
			p.Controls = append(p.Controls, ctl.Name)
			cc, ok := c.Controls[ctl.Name]
			if !ok {
				cc = CatalogueControl{Control: ctl, Label: ctl.Name}
			}
			cc.Planets = append(cc.Planets, name)
			c.Controls[ctl.Name] = cc
		}
		c.Planets = append(c.Planets, p)
	}
	return c
}

// The planet file a world's mod-settings run writes (planets.json). Its
// shape, which internal/game/modsettings/control.lua writes:
//
//	{
//	  "planets": [                       // absent or {} on 1.1
//	    {
//	      "name": "vulcanus",
//	      "localised_name": ["space-location-name.vulcanus"],
//	      "autoplace_controls": {        // map_gen_settings.autoplace_controls
//	        "calcite": {"frequency": 1, "size": 1, "richness": 1}
//	      },
//	      "resources": ["calcite"]       // optional: autoplace_settings entities
//	    }
//	  ],
//	  "autoplace_controls": [            // prototypes.autoplace_control
//	    {
//	      "name": "calcite",
//	      "category": "resource",        // resource, terrain, cliff or enemy
//	      "richness": true,
//	      "can_be_disabled": true,
//	      "order": "b",
//	      "localised_name": ["autoplace-control-names.calcite"]
//	    }
//	  ],
//	  "resources": [                     // resource entity prototypes
//	    {"name": "calcite", "control": "", "localised_name": ["entity-name.calcite"]}
//	  ]
//	}
//
// An empty list may be written as {} (Lua's empty table). Unknown fields are
// ignored. On 1.1 the file has no planets: every control belongs to the one
// surface, and each resource names its control.
type rawPlanetFile struct {
	Planets   luaList[rawPlanet]   `json:"planets"`
	Controls  luaList[rawControl]  `json:"autoplace_controls"`
	Resources luaList[rawResource] `json:"resources"`
}

type rawPlanet struct {
	Name      string                    `json:"name"`
	LocName   json.RawMessage           `json:"localised_name"`
	Controls  map[string]ControlDefault `json:"autoplace_controls"`
	Resources luaList[string]           `json:"resources"`
}

type rawControl struct {
	Name          string          `json:"name"`
	Category      string          `json:"category"`
	Richness      bool            `json:"richness"`
	CanBeDisabled bool            `json:"can_be_disabled"`
	Order         string          `json:"order"`
	LocName       json.RawMessage `json:"localised_name"`
}

type rawResource struct {
	Name    string          `json:"name"`
	Control string          `json:"control"`
	LocName json.RawMessage `json:"localised_name"`
}

// luaList reads a JSON array, or {} or null for an empty one.
type luaList[T any] []T

func (l *luaList[T]) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" || string(b) == "{}" {
		*l = nil
		return nil
	}
	if len(b) > 0 && b[0] == '{' {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil || len(m) != 0 {
			return errPlanetsJSON
		}
		*l = nil
		return nil
	}
	var v []T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*l = v
	return nil
}

// ParseWorldPlanets reads a world's planet file under the SPEC 14.6 limits: a file over
// MaxPlanetFileBytes or any count limit, or not of the shape above, is
// refused whole. A planet, control or resource whose name is not
// ^[a-z0-9_-]{1,64}$, or repeats one before it, is left out and counted
// in Dropped, as is a planet's control the file does not define. Labels
// are resolved from the locale sections, stripped of rich text and cut to
// 100 characters; a name that does not resolve is its own label. A planet's
// controls are in the game's order (order, then name).
func ParseWorldPlanets(data []byte, sections ...modsettings.LocaleSections) (Catalogue, error) {
	if len(data) > MaxPlanetFileBytes {
		return Catalogue{}, errPlanetsSize
	}
	var raw rawPlanetFile
	// Errors are replaced, so no text from the file reaches a log.
	if err := json.Unmarshal(data, &raw); err != nil {
		return Catalogue{}, errPlanetsJSON
	}
	if len(raw.Planets) > MaxPlanets || len(raw.Controls) > MaxControls || len(raw.Resources) > MaxResources {
		return Catalogue{}, errPlanetsCount
	}
	for _, p := range raw.Planets {
		if len(p.Controls) > MaxPlanetControls {
			return Catalogue{}, errPlanetsCount
		}
	}
	c := Catalogue{NoPlanets: len(raw.Planets) == 0, Controls: map[string]CatalogueControl{}}
	label := func(loc json.RawMessage, name string) string {
		if s, ok := modsettings.ResolveLocalised(loc, sections...); ok {
			if s = modsettings.StripRichText(s); s != "" {
				return s
			}
		}
		return name
	}
	defined := map[string]CatalogueControl{}
	var order []string
	for _, r := range raw.Controls {
		if _, dup := defined[r.Name]; dup || !nameRE.MatchString(r.Name) {
			c.Dropped++
			continue
		}
		defined[r.Name] = CatalogueControl{
			Control: Control{Name: r.Name, Category: r.Category, Richness: r.Richness, CanBeDisabled: r.CanBeDisabled},
			Order:   r.Order,
			Label:   label(r.LocName, r.Name),
		}
		order = append(order, r.Name)
	}
	byOrder := func(a, b string) int {
		return cmp.Or(cmp.Compare(defined[a].Order, defined[b].Order), cmp.Compare(a, b))
	}
	if c.NoPlanets {
		p := Planet{Name: "nauvis", Label: "nauvis", Controls: slices.SortedFunc(slices.Values(order), byOrder)}
		c.Planets = []Planet{p}
	}
	seen := map[string]bool{}
	for _, r := range raw.Planets {
		if seen[r.Name] || !nameRE.MatchString(r.Name) {
			c.Dropped++
			continue
		}
		seen[r.Name] = true
		p := Planet{Name: r.Name, Label: label(r.LocName, r.Name), Defaults: map[string]ControlDefault{}}
		for n, d := range r.Controls {
			if _, ok := defined[n]; !ok {
				c.Dropped++
				continue
			}
			p.Controls = append(p.Controls, n)
			p.Defaults[n] = d
		}
		slices.SortFunc(p.Controls, byOrder)
		for _, n := range r.Resources {
			if nameRE.MatchString(n) && !slices.Contains(p.Resources, n) {
				p.Resources = append(p.Resources, n)
			} else {
				c.Dropped++
			}
		}
		c.Planets = append(c.Planets, p)
	}
	for _, p := range c.Planets {
		for _, n := range p.Controls {
			cc := defined[n]
			cc.Planets = append(cc.Planets, p.Name)
			defined[n] = cc
			c.Controls[n] = cc
		}
	}
	seenRes := map[string]bool{}
	for _, r := range raw.Resources {
		if seenRes[r.Name] || !nameRE.MatchString(r.Name) {
			c.Dropped++
			continue
		}
		seenRes[r.Name] = true
		ctl := r.Control
		if _, ok := defined[ctl]; !ok {
			ctl = ""
		}
		c.Resources = append(c.Resources, Resource{Name: r.Name, Control: ctl, Label: label(r.LocName, r.Name)})
	}
	return c, nil
}
