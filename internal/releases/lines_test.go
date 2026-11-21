package releases

import (
	"context"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// A line added to the supported list must be asked for, with no second list
// here to forget (SPEC 10.8).
func TestVersionsAsksForEverySupportedLine(t *testing.T) {
	hub := newHub(t)
	c := testClient(hub.URL(), "http://unused.invalid/", nil)
	if _, err := c.Versions(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, line := range mapgen.Lines {
		if hub.count(line+".") == 0 {
			t.Errorf("Docker Hub was never asked for line %s", line)
		}
	}
}
