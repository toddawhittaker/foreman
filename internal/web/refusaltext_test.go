package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

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
		{"other problems too", mixed, false, "Base", "Foreman did not start the game, because the save that would load does not match the world:"},
		{"other problems, switch", mixed, true, "Base", "Foreman did not switch, because the save that would load in the new world does not match that world:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := english(precheckLeadFor(c.check, c.switching, c.world)); got != c.want {
				t.Errorf("got %q\nwant %q", got, c.want)
			}
		})
	}
}
