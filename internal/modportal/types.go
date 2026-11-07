// Package modportal talks to the Factorio mod portal (SPEC 9.1, 9.12,
// 14.10).
package modportal

import "errors"

// Release is one release of a mod on the portal.
type Release struct {
	Version         string
	FileName        string // always <name>_<version>.zip
	DownloadURL     string // path on the portal, starting with /download/
	SHA1            string
	FactorioVersion string   // the game line, such as "2.0"
	Dependencies    []string // raw dependency strings from info.json
}

// Mod is a mod with all its releases.
type Mod struct {
	Name       string
	Title      string
	Summary    string
	Downloads  int
	Deprecated bool
	Releases   []Release // oldest first
}

// Hit is one search result.
type Hit struct {
	Name            string
	Title           string
	Summary         string
	LatestVersion   string
	FactorioVersion string
	Downloads       int
}

// Errors the client returns.
var (
	ErrNotOnPortal  = errors.New("modportal: mod not on the portal")
	ErrUnreachable  = errors.New("modportal: portal unreachable")
	ErrChecksum     = errors.New("modportal: checksum mismatch")
	ErrUnauthorized = errors.New("modportal: username or token refused")
)
