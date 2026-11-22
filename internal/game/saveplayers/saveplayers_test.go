package saveplayers

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gameenv"
)

// The mod folder is named after the mod and its version.
func TestModDir(t *testing.T) {
	if ModDir != "foreman-saveplayers_1.0.0" {
		t.Errorf("ModDir %q", ModDir)
	}
}

// code is the script without its comments.
func code() []byte {
	return regexp.MustCompile(`(?m)--.*$`).ReplaceAll(Control, nil)
}

// The script runs on 1.1, 2.0 and 2.1, runs once at load, and writes the file
// the container copies out, with the fields of the output (SPEC 8.16, 14.6).
func TestControl(t *testing.T) {
	for _, want := range []string{
		`script.on_init(`,
		`helpers or game`,
		`write_file("` + gameenv.SavePlayersFile + `"`,
		`table_to_json(`,
		`script.active_mods`,
		`game.players`,
		`online_time`,
		`game.tick`,
		`format = 1`,
		`game_version`,
		`online_ticks`,
		`"active_mods"`,
		`"players"`,
	} {
		if !bytes.Contains(Control, []byte(want)) {
			t.Errorf("control.lua lacks %s", want)
		}
	}
	if bytes.Contains(code(), []byte("helpers.")) {
		t.Error("control.lua calls helpers directly, which 1.1 lacks")
	}
}

// The save's online flag and last-online date are wrong for a player who
// was online when the save was made (SPEC 8.14), so the script never reads
// them.
func TestControlNeverReadsOnlineState(t *testing.T) {
	for _, bad := range []string{"connected", "last_online", "afk_time"} {
		if bytes.Contains(code(), []byte(bad)) {
			t.Errorf("control.lua reads %s", bad)
		}
	}
}

// The script writes no prototypes and no event beyond the two that read.
func TestControlAddsNothing(t *testing.T) {
	for _, bad := range []string{"data:extend", "data.raw", "game.print", "rcon", "game.write_file", "remote."} {
		if bytes.Contains(code(), []byte(bad)) {
			t.Errorf("control.lua has %s", bad)
		}
	}
}
