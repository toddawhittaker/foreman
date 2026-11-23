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

// Origins works out, from the enabled installed mods' info.json
// dependencies, which mods are there because others name them (SPEC 9.6).
// A mod nobody names has no entry. Required beats recommended beats offered,
// and only the parents of the winning kind are listed. It stores nothing.
func Origins(installed []world.InstalledMod) map[string]Origin {
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
			if err != nil || d.Name == p.Name || !present[d.Name] {
				continue
			}
			var kind OriginKind
			switch d.Kind {
			case Required, NoOrder:
				kind = RequiredBy
			case Recommended:
				kind = RecommendedBy
			case Optional, HiddenOptional:
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
