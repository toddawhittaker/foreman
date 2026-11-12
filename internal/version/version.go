// Package version says which build of Foreman is running: its version and
// commit, the Go version it was built with, its licence and its home
// (DESIGN 6 "User menu"; SPEC 17). The About dialog and page show
// it. Nothing here is spelled out in the code: the build sets the first two,
// and the rest come from the toolchain.
package version

import (
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

// Version and Commit are set at build time by the Makefile and the
// Dockerfile:
//
//	-ldflags "-X github.com/toddawhittaker/foreman/internal/version.Version=... -X .../version.Commit=..."
//
// Version is the nearest release tag from `git describe --tags --match
// 'v[0-9]*'` (empty when the history has none). Commit is the commit hash,
// with "-dirty" after it when the tree had uncommitted changes. Either may
// stay empty, and then Go's build info is read.
var (
	Version string
	Commit  string
)

// Licence and Repository are the project's, the same in every build.
const (
	Licence    = "MIT"
	Repository = "https://github.com/toddawhittaker/foreman"
)

// shortCommit is how many characters of a commit hash are shown.
const shortCommit = 12

// dirtySuffix is what a build puts after a commit hash for a tree with
// uncommitted changes.
const dirtySuffix = "-dirty"

// Info is what About shows.
type Info struct {
	// Version is a release version, or empty when the build has none: a
	// build from a commit that no release tag reaches is named by its commit.
	Version string
	// Commit is the first twelve characters of the commit hash, or empty
	// when unknown.
	Commit string
	// Modified is true when the tree had uncommitted changes.
	Modified   bool
	Go         string
	Licence    string
	Repository string
}

// buildInfo reads Go's build info once.
var buildInfo = sync.OnceValue(func() *debug.BuildInfo {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return bi
})

// Get returns this build's values. The build variables come first; for
// what they leave empty, Go's build info has the module version and the
// vcs.revision and vcs.modified settings.
func Get() Info {
	i := resolve(Version, Commit, buildInfo())
	i.Go = runtime.Version()
	i.Licence = Licence
	i.Repository = Repository
	return i
}

// pseudoVersion matches the version Go makes up for a module built from an
// untagged commit, such as v0.0.0-20261112173800-0fe3bb26e6e1.
var pseudoVersion = regexp.MustCompile(`[-.]\d{14}-[0-9a-f]{12}(\+dirty)?$`)

// resolve picks the version and commit from the build variables, then from
// build info (which may be nil). A module version of "(devel)" or a
// pseudo-version is not a release, so it gives no version.
func resolve(version, commit string, bi *debug.BuildInfo) Info {
	var i Info
	i.Version = strings.TrimSpace(version)
	commit = strings.TrimSpace(commit)
	if c, ok := strings.CutSuffix(commit, dirtySuffix); ok {
		commit, i.Modified = c, true
	}
	if commit != "" {
		i.Commit = abbreviate(commit)
	}
	if bi == nil {
		return i
	}
	if v := bi.Main.Version; i.Version == "" && v != "" && v != "(devel)" && !pseudoVersion.MatchString(v) {
		i.Version = v
	}
	if i.Commit == "" {
		var rev, modified string
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				modified = s.Value
			}
		}
		if rev != "" {
			i.Commit = abbreviate(rev)
			i.Modified = modified == "true"
		}
	}
	return i
}

func abbreviate(hash string) string {
	if len(hash) > shortCommit {
		return hash[:shortCommit]
	}
	return hash
}
