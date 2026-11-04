package game

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The game may print the command it was given; the relayed line must not
// show the new password (SPEC 14.8).
func TestMaskSecretCommand(t *testing.T) {
	cases := map[string]string{
		"2026-11-03 10:00:00 [COMMAND] <server> (command): /config set password hunter2": "2026-11-03 10:00:00 [COMMAND] <server> (command): /config set password " + Mask,
		"... /Config  Set Password hunter2":                                              "... /Config  Set Password " + Mask,
		"2026-11-03 10:00:00 [CHAT] alice: hello":                                        "2026-11-03 10:00:00 [CHAT] alice: hello",
	}
	for line, want := range cases {
		if got := maskSecretCommand(line); got != want {
			t.Errorf("maskSecretCommand(%q) = %q, want %q", line, got, want)
		}
	}
}

// SPEC 8.6, 14.8: every line the controller keeps or relays has a printed
// password command masked, crash lines included.
func TestRelayedLinesMaskSecretCommands(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Masked")
	r.start(id)
	_, ch, stop := r.c.Watch(id)
	defer stop()
	line := "2026-11-03 10:00:00 [COMMAND] <server> (command): /config set password s3cret"
	r.game.Daemon().EmitLog(r.container(id).ID, docker.Stdout, line)
	want := "2026-11-03 10:00:00 [COMMAND] <server> (command): /config set password " + Mask
	for u := range ch {
		if u.Kind != UpdateLine {
			continue
		}
		if strings.Contains(u.Line, "s3cret") {
			t.Fatalf("relayed %q", u.Line)
		}
		if u.Line == want {
			break
		}
	}
	if !slices.Contains(r.c.Lines(id), want) {
		t.Error("the kept log does not hold the masked line")
	}
	if err := r.game.Crash(ContainerName(id), 1, false); err != nil {
		t.Fatal(err)
	}
	st := r.waitState(id, StateCrashed)
	if strings.Contains(st.CrashReason, "s3cret") {
		t.Errorf("crash reason %q", st.CrashReason)
	}
}

// SPEC 12.9, 14.8: the world's known passwords never reach the audit log or
// the history, even when typed into a command the caller did not mask.
func TestConsoleRedactsKnownPasswords(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Typed secret")
	r.writeFile(id, filepath.Join(world.ConfigDir, "server-settings.json"), []byte(`{"game_password":"hunter2-game"}`))
	r.start(id)
	cmd := "/c game.print('hunter2-game')"
	shown, _, err := r.c.Console(context.Background(), id, admin, cmd, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if shown != "/c game.print('"+Mask+"')" {
		t.Errorf("shown %q", shown)
	}
	entries := r.audit.list()
	if last := entries[len(entries)-1]; last.Detail != shown {
		t.Errorf("audit detail %q, want %q", last.Detail, shown)
	}
	if !slices.Contains(r.commands(id), cmd) {
		t.Error("the game was not sent the command as typed")
	}
}

// SPEC 8.6, 14.8: the image's entrypoint prints the game's command line,
// so any --rcon-password value in a line is masked, whatever password the
// world has and however the shell quoted it.
func TestMaskRCONPasswordFlag(t *testing.T) {
	cases := map[string]string{
		"+ exec /opt/factorio/bin/x64/factorio --start-server-load-latest --rcon-port 27015 --rcon-password hunter2 --server-settings x": "+ exec /opt/factorio/bin/x64/factorio --start-server-load-latest --rcon-port 27015 --rcon-password " + Mask + " --server-settings x",
		"+ exec factorio --rcon-password 'pass word' --mod-directory /factorio/mods":                                                     "+ exec factorio --rcon-password " + Mask + " --mod-directory /factorio/mods",
		`+ exec factorio --rcon-password 'it'\''s' --port 1`:                                                                             "+ exec factorio --rcon-password " + Mask + " --port 1",
		`+ exec factorio --rcon-password "dq pw" --port 1`:                                                                               "+ exec factorio --rcon-password " + Mask + " --port 1",
		"+ exec factorio --rcon-password=eq-pw":                                                                                          "+ exec factorio --rcon-password " + Mask,
		"2026-11-03 10:00:00 [CHAT] alice: hello":                                                                                        "2026-11-03 10:00:00 [CHAT] alice: hello",
	}
	for line, want := range cases {
		if got := maskSecretCommand(line); got != want {
			t.Errorf("maskSecretCommand(%q) = %q, want %q", line, got, want)
		}
	}
}
