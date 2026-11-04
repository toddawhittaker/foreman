package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

func TestAnywayText(t *testing.T) {
	spaceAge := []world.Problem{{Kind: world.ProblemSpaceAgeOff}}
	one := []world.Problem{{Kind: world.ProblemModMissing, Mods: []string{"a"}}}
	two := []world.Problem{{Kind: world.ProblemModMissing, Mods: []string{"a"}}, {Kind: world.ProblemModDisabled, Mods: []string{"b"}}}
	cases := []struct {
		name      string
		problems  []world.Problem
		switching bool
		want      string
	}{
		{"start, one mod", one, false, "Foreman cannot add or turn on a world's mods yet. To keep what that mod built, put the mod files in the import folder's mods/ and import the world again. Or start anyway: Foreman takes a snapshot first, so you can undo it from the Snapshots page."},
		{"switch, two mods", two, true, "Foreman cannot add or turn on a world's mods yet. To keep what those mods built, put the mod files in the import folder's mods/ and import the world again. Or switch anyway: Foreman takes a snapshot of both worlds first, so you can undo it from the Snapshots page."},
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
