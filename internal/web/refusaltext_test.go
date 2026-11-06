package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

// The advice never repeats what the lead already says the game drops, and
// mods that are only turned off get none (SPEC 7.4). It names the world's own
// mods folder on the host, never the import folder, since importing again
// makes a second world.
func TestAnywayAdvice(t *testing.T) {
	spaceAge := []world.Problem{{Kind: world.ProblemSpaceAgeOff}}
	missing := []world.Problem{{Kind: world.ProblemModMissing, Mods: []string{"a"}}}
	both := []world.Problem{{Kind: world.ProblemModMissing, Mods: []string{"a"}}, {Kind: world.ProblemModMissing, Mods: []string{"c"}}, {Kind: world.ProblemModDisabled, Mods: []string{"b"}}}
	off := []world.Problem{{Kind: world.ProblemModDisabled, Mods: []string{"a", "b"}}}
	cases := []struct {
		name     string
		problems []world.Problem
		want     string
	}{
		{"one missing mod", missing, "Foreman cannot add mods to a world yet. To keep what that mod built, copy its zip file into Base's own mods folder, /srv/foreman/worlds/base/mods/ on the host, then start Base again."},
		{"two missing mods", both, "Foreman cannot add mods to a world yet. To keep what those mods built, copy their zip files into Base's own mods folder, /srv/foreman/worlds/base/mods/ on the host, then start Base again."},
		{"mods only turned off", off, ""},
		{"Space Age", spaceAge, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := english(anywayAdvice(c.problems, "Base", hostModsFolder("/srv/foreman", "base"))); got != c.want {
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
