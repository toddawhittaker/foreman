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
		// Real output: control-stage errors. The error line names the mod by
		// title only; the event line three lines below names it by name.
		{"a real control-stage error", capturedRun(t, "2.1.24/run-mod-runtime-error.log"), "even-distribution"},
		{"the real Even Distribution on_init error", capturedRun(t, "2.1.24/run-mod-real-on-init-error.log"), "even-distribution"},
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
		}, libraryTraceback...), "fake-planet"},
		// Issue 965's own text: Even Distribution fails in on_init, and the
		// traceback's first mod frame is flib's.
		{"issue 965's control-stage error", []string{
			`   1.277 Error ServerMultiplayerManager.cpp:85: MultiplayerManager failed: "The mod Even Distribution (2.1.0) caused a non-recoverable error.`,
			"Please report this error to the mod author.",
			"",
			"Error while running event even-distribution::on_init()",
			"__flib__/data-util.lua:12: attempt to index a nil value",
			"stack traceback:",
			"\t[C]: in function '__index'",
			"\t__flib__/data-util.lua:12: in function 'copy_prototype'",
			"\t__even-distribution__/scripts/setup.lua:297: in function 'generateTrashItemList'",
			"\t__even-distribution__/framework.lua:58: in function <__even-distribution__/framework.lua:58>\"",
			"   1.277 Info ServerMultiplayerManager.cpp:809: updateTick(18446744073709551615) changing state from(CreatingGame) to(InitializationFailed)",
		}, "even-distribution"},
		// The event line is used only inside the error's own message, next
		// to its first line.
		{"an event line in a later message", []string{
			`   1.268 Error ServerMultiplayerManager.cpp:85: MultiplayerManager failed: "The mod Fake planet (1.0.0) caused a non-recoverable error."`,
			"   1.269 Info CommandLineMultiplayer.cpp:216: Exit point.",
			"Error while running event fake-planet::on_init()",
		}, ""},
		{"an event line far below", []string{
			`   1.268 Error ServerMultiplayerManager.cpp:85: MultiplayerManager failed: "The mod Fake planet (1.0.0) caused a non-recoverable error.`,
			"Please report this error to the mod author.",
			"",
			"one",
			"two",
			"Error while running event fake-planet::on_init()",
		}, ""},
		{"an event line after another kind of error", []string{
			`   1.268 Error Util.cpp:81: boom`,
			"Error while running event fake-planet::on_init()",
		}, ""},
		{"an event of the game's own data", []string{
			`   1.268 Error ServerMultiplayerManager.cpp:85: MultiplayerManager failed: "The mod Base (2.1.24) caused a non-recoverable error.`,
			"Please report this error to the mod author.",
			"",
			"Error while running event base::on_init()",
		}, ""},
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

// SPEC 5.6, 8.7, issue 965: a control-stage error line gives the mod's
// title and version, for the caller to match to one of the world's mods when
// no event line names it.
func TestCrashedModTitle(t *testing.T) {
	cases := []struct {
		name           string
		log            []string
		title, version string
	}{
		{"a real control-stage error", capturedRun(t, "2.1.24/run-mod-runtime-error.log"), "Even Distribution", "2.1.0"},
		{"the title alone", []string{
			`   1.277 Error ServerMultiplayerManager.cpp:85: MultiplayerManager failed: "The mod Even Distribution (2.1.0) caused a non-recoverable error.`,
		}, "Even Distribution", "2.1.0"},
		{"a title with brackets", []string{
			`   1.277 Error ServerMultiplayerManager.cpp:85: MultiplayerManager failed: "The mod Belts (beta) (0.3.1) caused a non-recoverable error.`,
		}, "Belts (beta)", "0.3.1"},
		{"a data-stage error", capturedRun(t, "2.1.24/run-mod-lua-error.log"), "", ""},
		{"no error line", []string{"The mod Even Distribution (2.1.0) caused a non-recoverable error.", "Goodbye"}, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := crashRun(t, tc.log, 1)
			if title, version := CrashedModTitle(err); title != tc.title || version != tc.version {
				t.Errorf("CrashedModTitle(%v) = %q, %q, want %q, %q", err, title, version, tc.title, tc.version)
			}
		})
	}
	if title, _ := CrashedModTitle(&PreviewError{OOM: true, ErrorLine: "Error X.cpp:1: The mod A (1.0.0) caused a non-recoverable error."}); title != "" {
		t.Errorf("got %q for an out-of-memory run", title)
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
