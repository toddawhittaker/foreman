package world

import (
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// Every line in mapgen.Lines, the one list of supported lines, is supported
// here with the built-in mods the list gives it (SPEC 9.7, 10.8).
func TestBuiltinModsFollowTheSupportedLines(t *testing.T) {
	for _, name := range mapgen.Lines {
		line, _ := mapgen.LineOf(name)
		v := name + ".0"
		if !SupportedGameVersion(v) {
			t.Errorf("%s is listed but not supported", v)
		}
		if !slices.Equal(SpaceAgeMods(v), line.SpaceAgeMods) {
			t.Errorf("SpaceAgeMods(%s) = %v, want %v", v, SpaceAgeMods(v), line.SpaceAgeMods)
		}
		want := slices.Concat([]string{ModBase}, line.AlwaysOnMods, line.SpaceAgeMods)
		if got := BuiltinMods(v); !slices.Equal(got, want) {
			t.Errorf("BuiltinMods(%s) = %v, want %v", v, got, want)
		}
	}
	if SupportedGameVersion("9.9.0") || BuiltinMods("9.9.0") != nil || SpaceAgeMods("9.9.0") != nil {
		t.Error("a line that is not listed is supported")
	}
}
