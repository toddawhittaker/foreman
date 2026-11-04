package world

import (
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/savefile"
)

// ParseGameVersion reads a game version such as "2.0.72". It reports false
// for anything that is not three dotted numbers that each fit.
func ParseGameVersion(v string) (savefile.Version, bool) {
	if !ValidGameVersion(v) {
		return savefile.Version{}, false
	}
	var n [3]uint16
	for i, p := range strings.SplitN(v, ".", 3) {
		x, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return savefile.Version{}, false
		}
		n[i] = uint16(x)
	}
	return savefile.Version{Major: n[0], Minor: n[1], Patch: n[2]}, true
}

// CompareGameVersions compares two game versions by major, minor and patch.
// A version that does not parse counts as 0.0.0.
func CompareGameVersions(a, b string) int {
	return compareVersion(parseGameVersion(a), parseGameVersion(b))
}

// parseGameVersion is ParseGameVersion for a version whose shape the caller
// has checked.
func parseGameVersion(v string) savefile.Version {
	out, _ := ParseGameVersion(v)
	return out
}

// compareVersion compares major, minor and patch, ignoring the build.
func compareVersion(a, b savefile.Version) int {
	return slices.Compare([]uint16{a.Major, a.Minor, a.Patch}, []uint16{b.Major, b.Minor, b.Patch})
}
