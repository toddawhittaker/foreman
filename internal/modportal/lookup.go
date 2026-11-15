package modportal

import (
	"context"
	"errors"
	"strings"
)

// ErrNoRelease says the mod is on the portal but has no release for the game
// line asked about.
var ErrNoRelease = errors.New("modportal: mod has no release for the game line")

// Lookup finds the one mod with exactly this name for a game line: in the
// cached list first, then with the mod's own /full reply, which tells a mod
// the portal does not know (ErrNotOnPortal) from one with no release for the
// line (ErrNoRelease).
func (c *Client) Lookup(ctx context.Context, line, name string) (Hit, error) {
	list, err := c.modList(ctx, line)
	if err != nil {
		return Hit{}, err
	}
	want := strings.ToLower(name)
	for _, e := range list {
		if e.name == want {
			return e.hit, nil
		}
	}
	// A miss in the list falls back to the name as pasted; the portal matches
	// names as it matches them.
	m, err := c.Mod(ctx, name)
	if err != nil {
		return Hit{}, err
	}
	// Releases are oldest first, so the last one for the line is the newest.
	for i := len(m.Releases) - 1; i >= 0; i-- {
		if r := m.Releases[i]; r.FactorioVersion == line {
			return Hit{Name: m.Name, Title: m.Title, Summary: m.Summary, LatestVersion: r.Version,
				FactorioVersion: line, Downloads: m.Downloads}, nil
		}
	}
	return Hit{}, ErrNoRelease
}
