package web

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/world"
)

// SPEC 5.6, 8.7, issue 965: a planets read the game failed names the world's
// mod its error names, by title, with the plan that removes it; without one
// it says a kept failure waits for a change of mods, not "try again later".
func TestBuiltinFailure(t *testing.T) {
	installed := []world.InstalledMod{
		{Name: "base", BuiltIn: true},
		{Name: "even-distribution", Title: "Even Distribution", Version: "2.1.0"},
		{Name: "untitled", Version: "1.0.0"},
	}
	crash := func(log string) error {
		pe := &game.PreviewError{ExitCode: 1, Log: log}
		return fmt.Errorf("%w: %w", game.ErrModSettingsKept, fmt.Errorf("%w: %w", game.ErrModSettingsBadOutput, pe))
	}
	cases := []struct {
		name, key, mod string
		err            error
	}{
		{"named", "newmap.builtin.mod_failed", "Even Distribution", crash("Error: __base__/x.lua:1 | __even-distribution__/scripts/setup.lua:297: boom")},
		{"no title", "newmap.builtin.mod_failed", "untitled", crash("__untitled__/data.lua:1: boom")},
		{"not the world's mod", "newmap.builtin.failed_kept", "", crash("__elsewhere__/data.lua:1: boom")},
		{"kept, no mod", "newmap.builtin.failed_kept", "", crash("Error Util.cpp:81: boom")},
		{"not kept", "newmap.builtin.failed", "", game.ErrModSettingsTimeout},
		{"no wrap", "newmap.builtin.failed", "", errors.New("x")},
	}
	for _, tc := range cases {
		why, fix := builtinFailure(installed, "w1", tc.err)
		if why.Key != tc.key {
			t.Errorf("%s: key %q, want %q", tc.name, why.Key, tc.key)
		}
		if tc.mod == "" {
			if fix != nil {
				t.Errorf("%s: a fix %+v", tc.name, fix)
			}
			continue
		}
		if fix == nil || fix.Label.Key != "mods.flag.fix_remove" {
			t.Fatalf("%s: fix %+v", tc.name, fix)
		}
		name := map[string]string{"Even Distribution": "even-distribution", "untitled": "untitled"}[tc.mod]
		if want := kindPlanPath("w1", mods.KindRemove, name); fix.Path != want {
			t.Errorf("%s: path %q, want %q", tc.name, fix.Path, want)
		}
		if fmt.Sprint(why.Args["mod"]) != fmt.Sprint(fix.Label.Args["mod"]) || !strings.Contains(fmt.Sprint(why.Args["mod"]), tc.mod) {
			t.Errorf("%s: mod %v, want %q", tc.name, why.Args["mod"], tc.mod)
		}
	}
}
