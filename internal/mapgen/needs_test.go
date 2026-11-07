package mapgen

import (
	"slices"
	"testing"
)

func TestNeededResourcesOnEveryLine(t *testing.T) {
	want := []string{"coal", "copper-ore", "crude-oil", "iron-ore", "stone"}
	for _, line := range Lines {
		for _, spaceAge := range []bool{false, true} {
			got := slices.Clone(NeededResources(line, spaceAge))
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("NeededResources(%q, %v) = %v, want %v", line, spaceAge, got, want)
			}
		}
	}
}

// Every name is a Nauvis resource control of the line, so the form can find
// its toggle; uranium is not among them.
func TestNeededResourcesAreNauvisResourceControls(t *testing.T) {
	for _, line := range Lines {
		have := map[string]bool{}
		for _, c := range Controls(line, "nauvis") {
			if c.Category == CategoryResource {
				have[c.Name] = true
			}
		}
		for _, name := range NeededResources(line, true) {
			if !have[name] {
				t.Errorf("%s: %q is not a Nauvis resource control", line, name)
			}
			if name == "uranium-ore" {
				t.Errorf("%s: uranium is listed", line)
			}
		}
	}
}

func TestNeededResourcesIsACopy(t *testing.T) {
	NeededResources("2.1", true)[0] = "x"
	if NeededResources("2.1", true)[0] == "x" {
		t.Error("the caller changed the table")
	}
}
