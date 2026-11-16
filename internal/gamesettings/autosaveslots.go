package gamesettings

import "strconv"

// AutosaveSlots reads autosave_slots and autosave_interval from a
// server-settings.json; a missing key takes the game's default (SPEC 11.6).
// ok is false when b is not one JSON object or a value is not a whole
// number from 0 up, so the caller leaves the autosaves alone.
func AutosaveSlots(b []byte) (slots, interval int, ok bool) {
	if len(b) > MaxBytes {
		return 0, 0, false
	}
	root, fe := parse(b)
	if fe != nil || root.kind != kObject {
		return 0, 0, false
	}
	whole := func(key string) (int, bool) {
		m := lastMember(root, key)
		if m == nil {
			return int(GameDefaultWhole(key)), true
		}
		if m.node.kind != kNumber {
			return 0, false
		}
		n, err := strconv.ParseUint(m.node.num, 10, 31)
		return int(n), err == nil
	}
	slots, okS := whole("autosave_slots")
	interval, okI := whole("autosave_interval")
	if !okS || !okI {
		return 0, 0, false
	}
	return slots, interval, true
}
