package game

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/docker"
)

// SPEC 5.6, 8.7, issue 965: the mod whose script the game's error names is
// found in the log; the game's own data and Foreman's helper are not mods
// of the world.
func TestCrashedMods(t *testing.T) {
	log := "Error ModManager.cpp:1735: Error in assignID: __even-distribution__/scripts/setup.lua:297: attempt to index nil" +
		" | stack: __base__/data.lua:1 | __foreman-modsettings__/control.lua:3 | __even-distribution__/x.lua:2 | __Krastorio 2__/a.lua:1"
	cases := []struct {
		name string
		err  error
		want []string
	}{
		{"a mod's script", fmt.Errorf("%w: %w", ErrModSettingsBadOutput, &PreviewError{ExitCode: 1, Log: log}),
			[]string{"even-distribution", "Krastorio 2"}},
		{"no mod named", &PreviewError{ExitCode: 1, Log: "Error Util.cpp:81: boom"}, nil},
		{"out of memory", &PreviewError{OOM: true, Log: log}, nil},
		{"not the game", errors.New("__even-distribution__/x"), nil},
	}
	for _, tc := range cases {
		if got := CrashedMods(tc.err); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Issue 965: a run that fails in the game is kept until the world's mods
// change, so a page load never runs the game again; a change of mods runs it.
func TestModSettingsFailureKeptUntilModsChange(t *testing.T) {
	r := newModSettingsRig(t)
	ch := r.ask(context.Background())
	c := r.waitRunning()
	r.game.Daemon().EmitLog(c.ID, docker.Stderr, "Error: __fake-planet__/data.lua:1: boom")
	r.game.Daemon().Exit(c.ID, 1, false)
	res := r.result(ch)
	if !errors.Is(res.err, ErrModSettingsKept) || !slices.Equal(CrashedMods(res.err), []string{"fake-planet"}) {
		t.Fatalf("first call: %v, want a kept failure naming fake-planet", res.err)
	}
	for range 3 {
		_, err := r.c.ModSettingDefs(context.Background(), r.id)
		if !errors.Is(err, ErrModSettingsKept) || !slices.Equal(CrashedMods(err), []string{"fake-planet"}) {
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
