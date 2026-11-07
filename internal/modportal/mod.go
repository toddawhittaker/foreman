package modportal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Mod fetches one mod with all its releases from /api/mods/{name}/full. A
// mod the portal does not know is ErrNotOnPortal. At most four of these
// requests run at once, however many callers there are.
func (c *Client) Mod(ctx context.Context, name string) (Mod, error) {
	if !safeName(name) {
		return Mod{}, ErrNotOnPortal
	}
	if m, ok := c.mods.get(name, c.now()); ok {
		return cloneMod(m), nil
	}
	select {
	case c.full <- struct{}{}:
		defer func() { <-c.full }()
	case <-ctx.Done():
		return Mod{}, requestError(ctx.Err())
	}
	body, err := c.get(ctx, c.base+"/api/mods/"+url.PathEscape(name)+"/full", maxFull)
	if err != nil {
		var se *statusError
		if errors.As(err, &se) && se.code == 404 {
			return Mod{}, ErrNotOnPortal
		}
		return Mod{}, err
	}
	m, err := parseMod(body)
	if err != nil {
		return Mod{}, err
	}
	c.mods.put(name, m, c.now())
	return cloneMod(m), nil
}

// ModResult is the outcome of one mod in Mods.
type ModResult struct {
	Mod Mod
	Err error
}

// Mods fetches many mods at once for the planner (the four-request limit of
// Mod still holds). Every name asked for has an entry in the result.
func (c *Client) Mods(ctx context.Context, names []string) map[string]ModResult {
	out := make(map[string]ModResult, len(names))
	seen := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := c.Mod(ctx, name)
			mu.Lock()
			out[name] = ModResult{Mod: m, Err: err}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func parseMod(body []byte) (Mod, error) {
	var doc struct {
		Name       string `json:"name"`
		Title      string `json:"title"`
		Summary    string `json:"summary"`
		Downloads  int    `json:"downloads_count"`
		Deprecated bool   `json:"deprecated"`
		Releases   []struct {
			DownloadURL string `json:"download_url"`
			FileName    string `json:"file_name"`
			SHA1        string `json:"sha1"`
			Version     string `json:"version"`
			Info        struct {
				FactorioVersion string   `json:"factorio_version"`
				Dependencies    []string `json:"dependencies"`
			} `json:"info_json"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Name == "" {
		return Mod{}, fmt.Errorf("%w: the mod reply is not valid", ErrUnreachable)
	}
	m := Mod{
		Name:       doc.Name,
		Title:      doc.Title,
		Summary:    doc.Summary,
		Downloads:  doc.Downloads,
		Deprecated: doc.Deprecated,
	}
	for _, r := range doc.Releases {
		if r.Version == "" {
			continue
		}
		m.Releases = append(m.Releases, Release{
			Version:         r.Version,
			FileName:        r.FileName,
			DownloadURL:     r.DownloadURL,
			SHA1:            r.SHA1,
			FactorioVersion: r.Info.FactorioVersion,
			Dependencies:    r.Info.Dependencies,
		})
	}
	// The portal lists oldest first; the contract says so, so sort rather
	// than trust it.
	sort.SliceStable(m.Releases, func(i, j int) bool {
		return versionLess(m.Releases[i].Version, m.Releases[j].Version)
	})
	return m, nil
}

// versionLess compares dotted numbers; a part that is not a number counts
// as 0.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// cloneMod copies the slices, so a caller cannot change what the cache holds.
func cloneMod(m Mod) Mod {
	rels := make([]Release, len(m.Releases))
	for i, r := range m.Releases {
		r.Dependencies = append([]string(nil), r.Dependencies...)
		rels[i] = r
	}
	m.Releases = rels
	return m
}

// safeName accepts a name that is safe as a URL path part and as a file name
// part: not empty, no slash or backslash, no "..", no leading dot, no control
// character.
func safeName(s string) bool {
	if s == "" || s[0] == '.' || strings.Contains(s, "..") || strings.ContainsAny(s, `/\`) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
