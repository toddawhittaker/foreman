package gamesettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxNameBytes is the longest admin name accepted.
const MaxNameBytes = 64

// ValidateAdminName refuses a name that could not be sent safely in a
// /promote or /demote command (SPEC 14.9): empty, too long, not UTF-8, with
// surrounding spaces, or holding a control, line-break or invisible
// formatting character. Quotes and other punctuation pass here; the game
// layer escapes them.
func ValidateAdminName(name string) error {
	switch {
	case name == "":
		return errors.New("admin name is empty")
	case len(name) > MaxNameBytes:
		return fmt.Errorf("admin name is longer than %d bytes", MaxNameBytes)
	case !utf8.ValidString(name):
		return errors.New("admin name is not valid UTF-8")
	case strings.TrimSpace(name) != name:
		return errors.New("admin name has a space at its start or end")
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return errors.New("admin name has a control or invisible character")
		}
	}
	return nil
}

// ParseAdminList reads server-adminlist.json, an array of player names.
// Names are compared without regard to case, as the game does, so a later
// repeat is dropped. A file that is not an array of valid names is refused
// whole rather than partly read.
func ParseAdminList(b []byte) ([]string, error) {
	if len(b) > MaxBytes {
		return nil, errors.New("admin list is too large")
	}
	if t := bytes.TrimSpace(b); len(t) == 0 || t[0] != '[' {
		return nil, errors.New("admin list is not an array of names")
	}
	var raw []string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, errors.New("admin list is not an array of names")
	}
	seen := map[string]bool{}
	names := []string{}
	for _, n := range raw {
		if err := ValidateAdminName(n); err != nil {
			return nil, err
		}
		if k := strings.ToLower(n); !seen[k] {
			seen[k] = true
			names = append(names, n)
		}
	}
	return names, nil
}

// AdminDiff returns who to promote (in next but not in current) and who to
// demote (in current but not in next), comparing without regard to case.
// Each keeps the spelling and order of the list it came from.
func AdminDiff(current, next []string) (promote, demote []string) {
	return missingFrom(current, next), missingFrom(next, current)
}

// missingFrom returns the names in a that b lacks.
func missingFrom(b, a []string) []string {
	have := map[string]bool{}
	for _, n := range b {
		have[strings.ToLower(n)] = true
	}
	var out []string
	for _, n := range a {
		k := strings.ToLower(n)
		if !have[k] {
			have[k] = true
			out = append(out, n)
		}
	}
	return out
}
