package modportal

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const maxHits = 25

// listEntry is one mod of the portal's list, with the lower-case forms the
// search compares against.
type listEntry struct {
	hit         Hit
	name, title string
}

// Search finds mods for a game line such as "2.0". The portal's own search
// needs a login, which SPEC 9.11 keeps out of search, so the whole list for
// the line is fetched once, kept for 15 minutes, and searched here: an exact
// name first, then names that start with the query, then names or titles
// that contain it, each group by downloads, at most 25 in all.
func (c *Client) Search(ctx context.Context, line, query string) ([]Hit, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, nil
	}
	list, err := c.modList(ctx, line)
	if err != nil {
		return nil, err
	}
	var exact, prefix, contains []Hit
	for _, e := range list {
		switch {
		case e.name == q:
			exact = append(exact, e.hit)
		case strings.HasPrefix(e.name, q):
			prefix = append(prefix, e.hit)
		case strings.Contains(e.name, q) || strings.Contains(e.title, q):
			contains = append(contains, e.hit)
		}
	}
	var out []Hit
	for _, group := range [][]Hit{exact, prefix, contains} {
		sort.Slice(group, func(i, j int) bool {
			if group[i].Downloads != group[j].Downloads {
				return group[i].Downloads > group[j].Downloads
			}
			return group[i].Name < group[j].Name
		})
		out = append(out, group...)
	}
	if len(out) > maxHits {
		out = out[:maxHits]
	}
	return out, nil
}

func (c *Client) modList(ctx context.Context, line string) ([]listEntry, error) {
	if !validLine(line) {
		return nil, fmt.Errorf("modportal: %q is not a game line", line)
	}
	if l, ok := c.lists.get(line, c.now()); ok {
		return l, nil
	}
	q := url.Values{"page_size": {"max"}, "version": {line}}
	body, err := c.get(ctx, c.base+"/api/mods?"+q.Encode(), maxList)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Results []struct {
			Name      string `json:"name"`
			Title     string `json:"title"`
			Summary   string `json:"summary"`
			Downloads int    `json:"downloads_count"`
			Latest    *struct {
				Version string `json:"version"`
				Info    struct {
					FactorioVersion string `json:"factorio_version"`
				} `json:"info_json"`
			} `json:"latest_release"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("%w: the mod list is not valid JSON", ErrUnreachable)
	}
	list := make([]listEntry, 0, len(doc.Results))
	for _, r := range doc.Results {
		if r.Name == "" || r.Latest == nil {
			continue
		}
		list = append(list, listEntry{
			hit: Hit{
				Name:            r.Name,
				Title:           r.Title,
				Summary:         r.Summary,
				LatestVersion:   r.Latest.Version,
				FactorioVersion: r.Latest.Info.FactorioVersion,
				Downloads:       r.Downloads,
			},
			name:  strings.ToLower(r.Name),
			title: strings.ToLower(r.Title),
		})
	}
	c.lists.put(line, list, c.now())
	return list, nil
}

// validLine accepts "major.minor" with digits only.
func validLine(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 || strings.Trim(p, "0123456789") != "" {
			return false
		}
	}
	return true
}
