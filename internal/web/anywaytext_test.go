package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

func TestAnywayText(t *testing.T) {
	spaceAge := []world.Problem{{Kind: world.ProblemSpaceAgeOff}}
	missing := []world.Problem{{Kind: world.ProblemModMissing, Mods: []string{"a"}}}
	both := []world.Problem{{Kind: world.ProblemModMissing, Mods: []string{"a"}}, {Kind: world.ProblemModDisabled, Mods: []string{"b"}}}
	off := []world.Problem{{Kind: world.ProblemModDisabled, Mods: []string{"a", "b"}}}
	cases := []struct {
		name      string
		problems  []world.Problem
		switching bool
		want      string
	}{
		{"start, one missing mod", missing, false, "Foreman cannot add or turn on a world's mods yet. To keep what that mod built, put the mod files in the import folder's mods/ and import the world again. Or start anyway: Foreman takes a snapshot first, so you can undo it from the Snapshots page."},
		{"switch, missing and turned-off mods", both, true, "Foreman cannot add or turn on a world's mods yet. To keep what those mods built, put the mod files in the import folder's mods/ and import the world again. Or switch anyway: Foreman takes a snapshot of both worlds first, so you can undo it from the Snapshots page."},
		{"start, mods only turned off", off, false, "Foreman cannot turn on a world's mods yet. Or start anyway: Foreman takes a snapshot first, so you can undo it from the Snapshots page."},
		{"switch, mods only turned off", off, true, "Foreman cannot turn on a world's mods yet. Or switch anyway: Foreman takes a snapshot of both worlds first, so you can undo it from the Snapshots page."},
		{"start, Space Age", spaceAge, false, "You can fix the world first, or start anyway. Starting anyway takes a snapshot first, and the game then deletes what is listed above."},
		{"switch, Space Age", spaceAge, true, "You can fix the world first, or switch anyway. Switching anyway takes a snapshot of both worlds first, and the game then deletes what is listed above."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := anywayText(c.problems, c.switching); got != c.want {
				t.Errorf("got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestPrecheckLeadFor(t *testing.T) {
	modVersion := world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemModGameVersion}}}
	mixed := world.PreCheck{Problems: []world.Problem{{Kind: world.ProblemModGameVersion}, {Kind: world.ProblemModMissing}}}
	cases := []struct {
		name      string
		check     world.PreCheck
		switching bool
		world     string
		want      string
	}{
		{"start", modVersion, false, "Base", "Foreman did not start the game, because Base cannot run in this game:"},
		{"switch", modVersion, true, "Beta", "Foreman did not switch, because Beta cannot run in this game:"},
		{"no name", modVersion, false, "", "Foreman did not start the game, because the world cannot run in this game:"},
		{"other problems too", mixed, false, "Base", precheckLead},
		{"other problems, switch", mixed, true, "Base", switchPrecheckLead},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := precheckLeadFor(c.check, c.switching, c.world); got != c.want {
				t.Errorf("got %q\nwant %q", got, c.want)
			}
		})
	}
}
