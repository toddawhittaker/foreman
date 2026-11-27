package game

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// libraryTraceback is a traceback whose first frame is a library mod's, as
// when a mod's script calls flib and the call fails there. The frames past
// the error line push it out of the log's five-line end.
var libraryTraceback = []string{
	"stack traceback:",
	"\t[C]: in function 'error'",
	"\t__flib__/data-util.lua:12: in function 'copy_prototype'",
	"\t__flib__/data-util.lua:40: in function 'build'",
	"\t__core__/lualib/util.lua:60: in function 'merge'",
	"\t__fake-planet__/prototypes/planet.lua:7: in main chunk",
	"\t[C]: in function 'require'",
	"\t__fake-planet__/data.lua:1: in main chunk",
}

// SPEC 5.6, 8.7, issue 965: the mod named is the one in the game's error
// line, never one from a traceback frame or the log's end, and never the
// game's own data or Foreman's helper.
func TestCrashedMod(t *testing.T) {
	cases := []struct {
		name string
		log  []string
		want string
	}{
		// Real output: Factorio 2.1.24, a mod whose data.lua fails.
		{"a real data-stage error", capturedRun(t, "2.1.24/run-mod-lua-error.log"), "brokenlua"},
		{"a real 2.0 data-stage error", capturedRun(t, "2.0.77/run-mod-lua-error.log"), "brokenlua"},
		// Real output: a control-stage error. Its error line names the mod by
		// title only; the paths are in the lines below it, so none is named.
		{"a real control-stage error", capturedRun(t, "2.1.24/run-mod-runtime-error.log"), ""},
		// Modelled on the real data-stage error, with a library frame first
		// in a traceback long enough to push the error line out of the end.
		{"a data-stage error with a library traceback", append([]string{
			"   0.241 Loading mod fake-planet 1.0.0 (data.lua)",
			`   0.263 Error Util.cpp:81: Failed to load mod "fake-planet": __fake-planet__/prototypes/planet.lua:7: bad argument #1`,
		}, libraryTraceback...), "fake-planet"},
		// Modelled on the real control-stage error, with the same traceback.
		{"a control-stage error with a library traceback", append([]string{
			`   1.268 Error ServerMultiplayerManager.cpp:85: MultiplayerManager failed: "The mod Fake planet (1.0.0) caused a non-recoverable error.`,
			"Please report this error to the mod author.",
			"",
			"Error while running event fake-planet::on_init()",
			"__flib__/data-util.lua:12: attempt to index a nil value",
		}, libraryTraceback...), ""},
		{"the game's own path first", []string{
			`   0.263 Error Util.cpp:81: Failed to load mod "fake-planet": __core__/lualib/util.lua:60: in __fake-planet__/data.lua:1`,
		}, "fake-planet"},
		{"no error line", []string{"__fake-planet__/data.lua:1: boom", "Goodbye"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := crashRun(t, tc.log, 1)
			if got := CrashedMod(err); got != tc.want {
				t.Errorf("CrashedMod(%v) = %q, want %q", err, got, tc.want)
			}
		})
	}
}

// The log's end, for the server log, is the same five lines as before.
func TestPreviewErrorLogStaysTheEnd(t *testing.T) {
	log := append([]string{`   0.263 Error Util.cpp:81: Failed to load mod "fake-planet": __fake-planet__/data.lua:1: boom`}, libraryTraceback...)
	var pe *PreviewError
	if err := crashRun(t, log, 1); !errors.As(err, &pe) {
		t.Fatalf("%v is not a *PreviewError", err)
	}
	var end []string
	for _, l := range log[len(log)-5:] {
		end = append(end, strings.TrimSpace(l))
	}
	if want := strings.Join(end, " | "); pe.Log != want {
		t.Errorf("Log = %q, want %q", pe.Log, want)
	}
	if strings.Contains(pe.Error(), "Failed to load mod") {
		t.Errorf("Error() %q holds more than the log's end", pe.Error())
	}
}

// Out of memory names no mod, whatever the log says.
func TestCrashedModOutOfMemory(t *testing.T) {
	err := &PreviewError{OOM: true, ErrorLine: "Error Util.cpp:81: __fake-planet__/data.lua:1: boom"}
	if got := CrashedMod(err); got != "" {
		t.Errorf("got %q for an out-of-memory run", got)
	}
	if got := CrashedMod(errors.New("Error Util.cpp:81: __fake-planet__/data.lua:1")); got != "" {
		t.Errorf("got %q for an error that is not the game's", got)
	}
}

// crashRun is a mod settings run whose game writes log and exits with code.
func crashRun(t *testing.T, log []string, code int) error {
	t.Helper()
	r := newModSettingsRig(t)
	ch := r.ask(context.Background())
	c := r.waitRunning()
	for _, l := range log {
		r.game.Daemon().EmitLog(c.ID, docker.Stderr, l)
	}
	r.game.Daemon().Exit(c.ID, code, false)
	return r.result(ch).err
}

// Issue 965: a run that fails in the game is kept until the world's mods
// change, so a page load never runs the game again; a change of mods runs it.
func TestModSettingsFailureKeptUntilModsChange(t *testing.T) {
	r := newModSettingsRig(t)
	ch := r.ask(context.Background())
	c := r.waitRunning()
	r.game.Daemon().EmitLog(c.ID, docker.Stderr, `   0.263 Error Util.cpp:81: Failed to load mod "fake-planet": __fake-planet__/data.lua:1: boom`)
	r.game.Daemon().Exit(c.ID, 1, false)
	res := r.result(ch)
	if !errors.Is(res.err, ErrModSettingsKept) || CrashedMod(res.err) != "fake-planet" {
		t.Fatalf("first call: %v, want a kept failure naming fake-planet", res.err)
	}
	for range 3 {
		_, err := r.c.ModSettingDefs(context.Background(), r.id)
		if !errors.Is(err, ErrModSettingsKept) || CrashedMod(err) != "fake-planet" {
			t.Fatalf("a later call: %v", err)
		}
	}
	if n := r.creates(); n != 1 {
		t.Fatalf("%d runs, want 1", n)
	}
	r.changeMods(`{"name":"other","enabled":true}`)
	ch = r.ask(context.Background())
	r.finish(r.waitRunning(), defsJSON, defaultsDat(t))
	if res := r.result(ch); res.err != nil || r.creates() != 2 {
		t.Fatalf("after a change of mods: %v after %d runs, want a new run", res.err, r.creates())
	}
}
