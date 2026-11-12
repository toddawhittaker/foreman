package resource

import (
	"slices"
	"testing"
)

// box makes a patch of a resource whose box spans x0..x1 on row 0.
func box(name string, x0, x1 int, amount int64) Patch {
	return Patch{Name: name, X: x0, Amount: amount, Box: [4]int{x0, 0, x1, 0}}
}

// SPEC 5.6, "Resource amounts": a patch's label is hidden beside a patch of
// the same resource at least 4 times its amount within 32 tiles.
func TestHiddenLabels(t *testing.T) {
	rich := box("iron-ore", 0, 10, 4000)
	cases := []struct {
		name  string
		small Patch
		want  []bool
	}{
		{"4x within 32 tiles", box("iron-ore", 42, 48, 1000), []bool{false, true}},
		{"33 tiles away", box("iron-ore", 43, 48, 1000), []bool{false, false}},
		{"80x at 30 tiles", box("iron-ore", 40, 46, 50), []bool{false, true}},
		{"80x at 40 tiles", box("iron-ore", 50, 56, 50), []bool{false, false}},
		{"3.9x", box("iron-ore", 42, 48, 1026), []bool{false, false}},
		{"another resource", box("copper-ore", 42, 48, 1000), []bool{false, false}},
		{"32 tiles on the other axis", Patch{Name: "iron-ore", Amount: 1000, Box: [4]int{0, 32, 3, 38}}, []bool{false, true}},
	}
	for _, c := range cases {
		if got := HiddenLabels([]Patch{rich, c.small}); !slices.Equal(got, c.want) {
			t.Errorf("%s: hidden %v, want %v", c.name, got, c.want)
		}
	}
}

// The richest patch of each resource is never hidden, even among equals.
func TestHiddenLabelsKeepTheRichest(t *testing.T) {
	ps := []Patch{
		box("iron-ore", 0, 1, 10), box("iron-ore", 2, 3, 100), box("iron-ore", 4, 5, 1000),
		box("copper-ore", 0, 1, 0), box("copper-ore", 2, 3, 0),
	}
	if got, want := HiddenLabels(ps), []bool{true, true, false, false, false}; !slices.Equal(got, want) {
		t.Errorf("hidden %v, want %v", got, want)
	}
}
