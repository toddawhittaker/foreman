package gamesettings

import (
	"encoding/json"
	"strconv"
)

// ForemanAutosaveSlots is how many autosaves Foreman keeps in a
// server-settings.json it creates. The game's own default is 5 (SPEC 11.1).
const ForemanAutosaveSlots = 20

// GameDefaultCommands is the game's own default for allow_commands.
const GameDefaultCommands = "admins-only"

// The game's own values for the keys the settings form shows, read from the
// example files of 1.1.110, 2.0.77 and 2.1.24, which agree on all of them.
// A key the file lacks takes this value, so the form shows it instead of an
// empty box.
var (
	gameWholes = map[string]uint32{
		"max_players":                        0,
		"autosave_interval":                  10,
		"autosave_slots":                     5,
		"afk_autokick_interval":              0,
		"max_upload_in_kilobytes_per_second": 0,
		"max_upload_slots":                   5,
	}
	gameFlags = map[string]bool{
		"require_user_verification":                 true,
		"ignore_player_limit_for_returning_players": false,
		"auto_pause":                      true,
		"auto_pause_when_players_connect": false,
		"only_admins_can_pause_the_game":  true,
		"non_blocking_saving":             false,
	}
)

// GameDefaultWhole is the game's default for a whole-number key of the form.
func GameDefaultWhole(key string) uint32 { return gameWholes[key] }

// GameDefaultFlag is the game's default for a true-or-false key of the form.
func GameDefaultFlag(key string) bool { return gameFlags[key] }

// HasPauseWhenConnect is true when the game line knows
// auto_pause_when_players_connect: 2.0 and later, not 1.1.
func HasPauseWhenConnect(line string) bool { return line != Line11 }

// NewServerSettings is the text of a server-settings.json Foreman creates
// for a world called name: the three keys the game refuses to start
// without (SPEC 11.1), Hidden, auto-pause on and Foreman's autosave slots.
func NewServerSettings(name string) []byte {
	n, _ := json.Marshal(name) // a string always encodes
	return []byte("{\n  \"name\": " + string(n) + ",\n  \"description\": \"\",\n" +
		"  \"visibility\": {\"public\": false, \"lan\": false},\n" +
		"  \"auto_pause\": true,\n  \"autosave_slots\": " + strconv.Itoa(ForemanAutosaveSlots) + "\n}\n")
}

// WithAutosaveSlots returns b with Foreman's autosave slots added when the
// file has no such key. It returns b as it is when b is not one JSON
// object, which the caller checks.
func WithAutosaveSlots(b []byte) []byte {
	if HasKey(b, "autosave_slots") {
		return b
	}
	out, err := SetFields(b, []Field{{Key: "autosave_slots", JSON: []byte(strconv.Itoa(ForemanAutosaveSlots))}})
	if err != nil {
		return b
	}
	return out
}
