// Package mods parses mod dependencies and resolves a mod change into a
// plan (SPEC 9.2, 9.7, 9.8, 9.10). It does no input or output.
package mods

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DepKind is the kind of a dependency, from its prefix.
type DepKind int

// Dependency kinds.
const (
	Required       DepKind = iota // no prefix
	NoOrder                       // "~": required, without load order
	Optional                      // "?"
	HiddenOptional                // "(?)"
	Incompatible                  // "!"
)

// Dep is one parsed dependency string from a mod's info.json.
type Dep struct {
	Kind    DepKind
	Name    string
	Op      string // "", "<", "<=", "=", ">=" or ">"
	Version string // empty when Op is empty
}

// ErrBadDep is returned for a dependency string that cannot be parsed.
var ErrBadDep = errors.New("mods: malformed dependency")

var (
	depVersionRE = regexp.MustCompile(`^(.*?)\s*(<=|>=|<|>|=)\s*([0-9]+(?:\.[0-9]+){1,2})$`)
	depNameRE    = regexp.MustCompile(`^[A-Za-z0-9_.-](?:[A-Za-z0-9_. -]*[A-Za-z0-9_.-])?$`)
)

// maxNameLen bounds a mod name; the portal's names are far shorter.
const maxNameLen = 100

// ParseDep parses a dependency such as "? some mod >= 1.2.0".
func ParseDep(s string) (Dep, error) {
	var d Dep
	rest := strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(rest, "(?)"):
		d.Kind, rest = HiddenOptional, rest[3:]
	case strings.HasPrefix(rest, "!"):
		d.Kind, rest = Incompatible, rest[1:]
	case strings.HasPrefix(rest, "?"):
		d.Kind, rest = Optional, rest[1:]
	case strings.HasPrefix(rest, "~"):
		d.Kind, rest = NoOrder, rest[1:]
	}
	rest = strings.TrimSpace(rest)
	if m := depVersionRE.FindStringSubmatch(rest); m != nil {
		rest, d.Op, d.Version = m[1], m[2], m[3]
		for _, p := range strings.Split(d.Version, ".") {
			if _, err := strconv.ParseUint(p, 10, 16); err != nil {
				return Dep{}, fmt.Errorf("%w: %q", ErrBadDep, s)
			}
		}
	}
	if len(rest) > maxNameLen || !depNameRE.MatchString(rest) {
		return Dep{}, fmt.Errorf("%w: %q", ErrBadDep, s)
	}
	d.Name = rest
	return d, nil
}

// String writes the dependency back in info.json form.
func (d Dep) String() string {
	prefix := map[DepKind]string{NoOrder: "~ ", Optional: "? ", HiddenOptional: "(?) ", Incompatible: "! "}[d.Kind]
	if d.Op == "" {
		return prefix + d.Name
	}
	return prefix + d.Name + " " + d.Op + " " + d.Version
}

// Allows reports whether a version of the named mod meets the dependency's
// version constraint.
func (d Dep) Allows(version string) bool {
	c := compareVersions(version, d.Version)
	switch d.Op {
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	case "=":
		return c == 0
	case ">=":
		return c >= 0
	case ">":
		return c > 0
	}
	return true
}

// required reports whether the dependency must be present and enabled.
func (d Dep) required() bool { return d.Kind == Required || d.Kind == NoOrder }

// compareVersions compares dotted numbers such as "1.10.2"; a missing or
// non-numeric part counts as 0.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range max(len(pa), len(pb)) {
		var x, y uint64
		if i < len(pa) {
			x, _ = strconv.ParseUint(pa[i], 10, 32)
		}
		if i < len(pb) {
			y, _ = strconv.ParseUint(pb[i], 10, 32)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
