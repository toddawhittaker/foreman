package mapgen

import (
	"fmt"
	"strconv"
	"strings"
)

// The 2.1 patches at which the map-settings layout changes (SPEC 5.6). The
// map exchange decoder reads its line-specific fields by the same constants.
const (
	DispatchCooldownAddedIn   = 13 // adds enemy_expansion.build_base_unit_dispatch_cooldown
	PollutionRestoreRemovedIn = 18 // removes pollution.max_pollution_to_restore_trees
)

// layoutChanges lists, per line, the patch numbers at which the map-settings
// layout changes, in order. A line not listed has one layout throughout.
var layoutChanges = map[string][]int{
	"2.1": {DispatchCooldownAddedIn, PollutionRestoreRemovedIn},
}

// splitPatch returns the line and patch of an exact patch release.
func splitPatch(version string) (string, int, bool) {
	line, ok := Line(version)
	if !ok {
		return "", 0, false
	}
	patch, err := strconv.Atoi(strings.TrimPrefix(version, line+"."))
	if err != nil {
		return "", 0, false
	}
	return line, patch, true
}

// MapSettingsLayout names the map-settings layout an exact game version
// ("2.1.24") uses, as the first version that has it ("2.1.18"; "2.0.0" for
// all of 2.0). Two versions with the same name take the same map-settings
// keys. It reports false for an unsupported line or a malformed version.
func MapSettingsLayout(version string) (string, bool) {
	line, patch, ok := splitPatch(version)
	if !ok {
		return "", false
	}
	start := 0
	for _, c := range layoutChanges[line] {
		if c <= patch {
			start = c
		}
	}
	return fmt.Sprintf("%s.%d", line, start), true
}

// MapSettingsLayoutEnd returns the first version after an exact version that
// has a different map-settings layout, or "" when the layout never changes
// again or the version is not supported.
func MapSettingsLayoutEnd(version string) string {
	line, patch, ok := splitPatch(version)
	if !ok {
		return ""
	}
	for _, c := range layoutChanges[line] {
		if c > patch {
			return fmt.Sprintf("%s.%d", line, c)
		}
	}
	return ""
}
