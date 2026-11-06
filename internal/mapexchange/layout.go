package mapexchange

import (
	"fmt"
	"strconv"
	"strings"
)

// layoutChanges lists, per line, the patch numbers at which the
// map-settings layout changes. A line not listed has one layout throughout.
var layoutChanges = map[gameLine][]uint16{
	line21: {dispatchCooldownAddedIn, pollutionRestoreRemovedIn},
}

var lineNames = map[gameLine]string{line11: "1.1", line20: "2.0", line21: "2.1"}

// layoutEnd returns the first version after patch on the line that has a
// different map-settings layout, or "" when the layout never changes again.
func layoutEnd(line gameLine, patch uint16) string {
	for _, c := range layoutChanges[line] {
		if c > patch {
			return fmt.Sprintf("%s.%d", lineNames[line], c)
		}
	}
	return ""
}

// parseWorldVersion reads an exact "major.minor.patch" version.
func parseWorldVersion(s string) ([3]uint16, bool) {
	parts := strings.Split(s, ".")
	var v [3]uint16
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return v, false
		}
		v[i] = uint16(n)
	}
	return v, true
}

// Compatible reports whether a decoded string can be used for a world on the
// given game version (SPEC 5.6). The game refuses a map-settings.json that
// lacks a key its own version knows, so the string's map-settings layout must
// be the world's. It refuses a different line, a string newer than the world,
// and a layout change between the two. The error is an *UnsupportedError
// whose From and Until bound the world versions that would take the string.
func Compatible(stringVersion [4]uint16, worldVersion string) error {
	sv := [3]uint16{stringVersion[0], stringVersion[1], stringVersion[2]}
	refuse := func(until, reason string) error {
		return &UnsupportedError{
			Version: stringVersion,
			Reason:  reason,
			From:    fmt.Sprintf("%d.%d.%d", sv[0], sv[1], sv[2]),
			Until:   until,
		}
	}

	sLine, ok := lineOf(stringVersion)
	if !ok {
		return refuse("", "only game lines 1.1, 2.0 and 2.1 are supported")
	}
	until := layoutEnd(sLine, sv[2])

	wv, ok := parseWorldVersion(worldVersion)
	if !ok {
		return refuse(until, "the world's game version is not a number like 2.1.24")
	}
	wLine, ok := lineOf([4]uint16{wv[0], wv[1], wv[2], 0})
	if !ok {
		return refuse(until, "the world's game line is not supported")
	}
	if wLine != sLine {
		return refuse(until, "the string is from a different game line than the world")
	}
	if wv[2] < sv[2] {
		return refuse(until, "the string is from a newer game version than the world")
	}
	if layoutEnd(wLine, wv[2]) != until {
		return refuse(until, "the string's settings layout differs from the world's game version")
	}
	return nil
}
