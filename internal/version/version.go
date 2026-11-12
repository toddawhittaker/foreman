// Package version says which build of Foreman is running: its version and
// commit, the Go version it was built with, its licence and its home
// (docs/EPIC-fixes-7.md, ruling 7; SPEC 17). The About dialog and page show
// it. Nothing here is spelled out in the code: the build sets the first two,
// and the rest come from the toolchain.
package version

import (
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

// Version and Commit are set at build time by the Makefile and the
// Dockerfile, from `git describe --tags --always --dirty` and the commit
// hash:
//
//	-ldflags "-X github.com/toddawhittaker/foreman/internal/version.Version=... -X .../version.Commit=..."
//
// Either may stay empty, and then Go's build info is read.
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

// Info is what About shows.
type Info struct {
	// Version is empty when the build did not record one.
	Version string
	// Commit is the first twelve characters of the commit hash, with
	// "-dirty" after it when the tree had uncommitted changes (only the
	// build info says so). Empty when unknown.
	Commit     string
	Go         string
	Licence    string
	Repository string
}

// Development is true when the build recorded neither a version nor a
// commit: a build the page calls a development build.
func (i Info) Development() bool { return i.Version == "" && i.Commit == "" }

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

// resolve picks the version and commit from the build variables, then from
// build info (which may be nil).
func resolve(version, commit string, bi *debug.BuildInfo) Info {
	var i Info
	i.Version = strings.TrimSpace(version)
	if commit = strings.TrimSpace(commit); commit != "" {
		i.Commit = abbreviate(commit)
	}
	if bi == nil {
		return i
	}
	if i.Version == "" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		i.Version = bi.Main.Version
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
			if modified == "true" {
				i.Commit += "-dirty"
			}
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
