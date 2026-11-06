package mapgen

import (
	"slices"
	"testing"
)

func TestLinesAreTheNamesOfTheSupportedList(t *testing.T) {
	var names []string
	for _, l := range supported {
		names = append(names, l.Name)
	}
	if !slices.Equal(Lines, names) {
		t.Errorf("Lines = %v, want %v", Lines, names)
	}
}

func TestLineOfAcceptsALineOrARelease(t *testing.T) {
	for v, want := range map[string]string{"1.1": "1.1", "2.0": "2.0", "2.0.72": "2.0", "2.1.24": "2.1", "1.1.110.3": "1.1"} {
		if got, ok := LineOf(v); !ok || got.Name != want {
			t.Errorf("LineOf(%q) = %q, %v; want %q, true", v, got.Name, ok, want)
		}
	}
	for _, v := range []string{"", "2", "2.2", "2.2.0", "2.10.1", "3.0.1", "0.17.79", "v2.0.1", "2.0.x", " 2.0", "2.0.", "stable"} {
		if got, ok := LineOf(v); ok {
			t.Errorf("LineOf(%q) = %q, true; want it refused", v, got.Name)
		}
	}
}

func TestLineOfGivesEachLineItsMods(t *testing.T) {
	cases := map[string]struct{ spaceAge, alwaysOn []string }{
		"1.1": {nil, nil},
		"2.0": {[]string{"elevated-rails", "quality", "space-age"}, nil},
		"2.1": {[]string{"elevated-rails", "quality", "space-age"}, []string{"recycler"}},
	}
	for v, want := range cases {
		got, _ := LineOf(v)
		if !slices.Equal(got.SpaceAgeMods, want.spaceAge) || !slices.Equal(got.AlwaysOnMods, want.alwaysOn) {
			t.Errorf("LineOf(%s) = %+v", v, got)
		}
	}
}

func TestLineOfReturnsCopiesOfTheModLists(t *testing.T) {
	l, _ := LineOf("2.1")
	l.SpaceAgeMods[0] = "changed"
	again, _ := LineOf("2.1")
	if again.SpaceAgeMods[0] == "changed" {
		t.Error("a caller changed the list's mods")
	}
}

// Adding a line to the list is all it takes for the helpers to know it.
func TestALineAddedToTheListIsSeen(t *testing.T) {
	old := supported
	t.Cleanup(func() { supported = old })
	supported = append(slices.Clone(old), GameLine{Name: "2.2"})
	if l, ok := LineOf("2.2.3"); !ok || l.Name != "2.2" {
		t.Errorf("LineOf(2.2.3) = %q, %v", l.Name, ok)
	}
	if got, ok := Line("2.2.3"); !ok || got != "2.2" {
		t.Errorf("Line(2.2.3) = %q, %v", got, ok)
	}
}
