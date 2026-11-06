package mapexchange

import (
	"fmt"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// The decoder's lines follow mapgen.Lines, the one list of supported lines.
func TestLineOfFollowsTheSupportedLines(t *testing.T) {
	for i, name := range mapgen.Lines {
		var major, minor uint16
		if _, err := fmt.Sscanf(name, "%d.%d", &major, &minor); err != nil {
			t.Fatal(err)
		}
		got, ok := lineOf([4]uint16{major, minor, 5, 0})
		if !ok || got != gameLine(i) {
			t.Errorf("lineOf(%s) = %d, %v; want %d, true", name, got, ok, i)
		}
		if lineNames[gameLine(i)] != name {
			t.Errorf("lineNames[%d] = %q, want %q", i, lineNames[gameLine(i)], name)
		}
		if !strings.Contains(linesReason(), name) {
			t.Errorf("%q does not name line %s", linesReason(), name)
		}
	}
	if _, ok := lineOf([4]uint16{9, 9, 0, 0}); ok {
		t.Error("an unlisted line was accepted")
	}
	if got, want := linesReason(), "only game lines 1.1, 2.0 and 2.1 are supported"; got != want {
		t.Errorf("linesReason = %q, want %q", got, want)
	}
}
