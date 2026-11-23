package mods

import (
	"slices"

	"github.com/toddawhittaker/foreman/internal/world"
)

// OriginKind says how an installed mod relates to the mods that name it.
// A larger kind wins over a smaller one.
type OriginKind int

// Origin kinds, weakest first.
const (
	NoOrigin OriginKind = iota
	OfferedBy
	RecommendedBy
	RequiredBy
)

// Origin says why a mod is installed besides the player's own choice: the
// strongest way other installed mods name it, and which of them do.
type Origin struct {
	Kind OriginKind
	By   []string // mod names, sorted
}

// originIgnored are the base game and its expansions, which are neither
// parents nor children of an origin (SPEC 9.6).
var originIgnored = []string{"base", "space-age", "quality", "elevated-rails"}

func ignoredForOrigin(m world.InstalledMod) bool {
	return m.BuiltIn || slices.Contains(originIgnored, m.Name)
}

// Chosen returns the names of the mods the admin chose (SPEC 9.6): the
// world's record when it has one, or else every mod no other enabled mod
// requires or recommends, which errs towards calling a mod chosen.
func Chosen(installed []world.InstalledMod) map[string]bool {
	out := map[string]bool{}
	for _, m := range installed {
		if m.Recorded {
			for _, m := range installed {
				if m.Chosen {
					out[m.Name] = true
				}
			}
			return out
		}
	}
	named := map[string]bool{}
	for _, p := range installed {
		if !p.Enabled || ignoredForOrigin(p) {
			continue
		}
		for _, raw := range p.Dependencies {
			if d, err := ParseDep(raw); err == nil && d.Name != p.Name && (d.required() || d.Kind == Recommended) {
				named[d.Name] = true
			}
		}
	}
	for _, m := range installed {
		if !ignoredForOrigin(m) && !named[m.Name] {
			out[m.Name] = true
		}
	}
	return out
}

// Origins works out, from the enabled installed mods' info.json
// dependencies, which mods are there because others name them (SPEC 9.6).
// A chosen mod and a mod nobody names have no entry, and a hidden optional
// dependency names nothing. Required beats recommended beats offered, and
// only the parents of the winning kind are listed.
func Origins(installed []world.InstalledMod) map[string]Origin {
	chosen := Chosen(installed)
	present := make(map[string]bool, len(installed))
	for _, m := range installed {
		if m.Enabled && !ignoredForOrigin(m) {
			present[m.Name] = true
		}
	}
	// parents[child][kind] is the set of parents of that kind.
	parents := map[string]map[OriginKind][]string{}
	for _, p := range installed {
		if !present[p.Name] {
			continue
		}
		for _, raw := range p.Dependencies {
			d, err := ParseDep(raw)
			if err != nil || d.Name == p.Name || !present[d.Name] || chosen[d.Name] {
				continue
			}
			var kind OriginKind
			switch d.Kind {
			case Required, NoOrder:
				kind = RequiredBy
			case Recommended:
				kind = RecommendedBy
			case Optional:
				kind = OfferedBy
			default:
				continue
			}
			if parents[d.Name] == nil {
				parents[d.Name] = map[OriginKind][]string{}
			}
			if !slices.Contains(parents[d.Name][kind], p.Name) {
				parents[d.Name][kind] = append(parents[d.Name][kind], p.Name)
			}
		}
	}
	out := make(map[string]Origin, len(parents))
	for child, byKind := range parents {
		for _, kind := range []OriginKind{RequiredBy, RecommendedBy, OfferedBy} {
			if by := byKind[kind]; len(by) > 0 {
				slices.Sort(by)
				out[child] = Origin{Kind: kind, By: by}
				break
			}
		}
	}
	return out
}
