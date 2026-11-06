package gamesettings

import (
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// A line in mapgen.Lines, the one list of supported lines, has a key table
// for every file; a line outside it has none.
func TestSchemaFollowsTheSupportedLines(t *testing.T) {
	for _, line := range mapgen.Lines {
		for _, file := range []string{FileServerSettings, FileMapGen, FileMapSettings} {
			if _, ok := schemaFor(file, line); !ok {
				t.Errorf("no table for %s on listed line %s", file, line)
			}
		}
	}
	if _, ok := schemaFor(FileServerSettings, "9.9"); ok {
		t.Error("a table for a line that is not listed")
	}
	for _, line := range []string{Line11, Line20, Line21} {
		if !slices.Contains(mapgen.Lines, line) {
			t.Errorf("constant %s is not in mapgen.Lines", line)
		}
	}
}
