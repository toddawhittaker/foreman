package mapgen

import "testing"

func TestCliffToggle(t *testing.T) {
	for _, tc := range []struct {
		line, planet string
		want         bool
	}{
		{"1.1", "nauvis", true},
		{"2.0", "nauvis", true},
		{"2.1", "nauvis", true},
		{"2.0", "gleba", true},
		{"2.1", "gleba", true},
		{"2.0", "fulgora", true},
		{"2.1", "fulgora", true},
		{"2.0", "vulcanus", false},
		{"2.1", "vulcanus", false},
		{"2.0", "aquilo", false},
		{"2.1", "aquilo", false},
		{"1.1", "gleba", false},
		{"1.1", "fulgora", false},
		{"1.1", "vulcanus", false},
		{"3.0", "nauvis", false},
		{"", "nauvis", false},
		{"2.1", "mars", false},
		{"2.1", "", false},
	} {
		if got := CliffToggle(tc.line, tc.planet); got != tc.want {
			t.Errorf("CliffToggle(%q, %q) = %v, want %v", tc.line, tc.planet, got, tc.want)
		}
	}
}

// On 2.x a planet can be toggled exactly when the game's data gives it a
// "<planet>_cliff" control, so a new planet in the data is noticed.
func TestCliffToggleMatchesTheCliffControlsOn2x(t *testing.T) {
	for _, line := range []string{"2.0", "2.1"} {
		for _, p := range Planets(line, true) {
			has := false
			for _, c := range Controls(line, p) {
				if c.Category == CategoryCliff && c.Name == p+"_cliff" {
					has = true
				}
			}
			if got := CliffToggle(line, p); got != has {
				t.Errorf("%s %s: CliffToggle = %v, cliff control present = %v", line, p, got, has)
			}
		}
	}
}
