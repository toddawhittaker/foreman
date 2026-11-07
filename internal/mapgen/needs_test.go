package mapgen

import "testing"

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
		for _, name := range NeededResources() {
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
	NeededResources()[0] = "x"
	if NeededResources()[0] == "x" {
		t.Error("the caller changed the table")
	}
}
