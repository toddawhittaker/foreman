package territory

import (
	"slices"
	"testing"
)

// The pixel of tile t is t + size/2, and chunk c covers tiles 32c to
// 32c+31. In a 128 picture chunk -2 starts at pixel 0 and chunk 1 at 96.
func TestChunkToPixel(t *testing.T) {
	r := Result{Territories: []Territory{
		{Small, []Chunk{{-2, -2}}},
		{Medium, []Chunk{{1, 1}}},
		{Big, []Chunk{{0, -1}}},
	}}
	ov := Geometry(r, 128)
	want := []Rect{{0, 0, 32, 32}, {96, 96, 32, 32}, {64, 32, 32, 32}}
	for i, w := range want {
		if got := ov.Shapes[i].Runs; !slices.Equal(got, []Rect{w}) {
			t.Errorf("shape %d runs %v, want %v", i, got, w)
		}
	}
}

// Neighbouring chunks of a row are one run; a gap or a new row starts
// another.
func TestRowRuns(t *testing.T) {
	r := Result{Territories: []Territory{{Small, []Chunk{{1, 0}, {-2, 0}, {0, 0}, {-1, 1}}}}}
	ov := Geometry(r, 128)
	want := []Rect{{0, 64, 32, 32}, {64, 64, 64, 32}, {32, 96, 32, 32}}
	if got := ov.Shapes[0].Runs; !slices.Equal(got, want) {
		t.Errorf("runs %v, want %v", got, want)
	}
}

func TestGeometryClips(t *testing.T) {
	r := Result{Territories: []Territory{
		{Small, []Chunk{{-3, 0}, {0, 0}}},
		{Medium, []Chunk{{2, 2}}},
	}}
	ov := Geometry(r, 128)
	if len(ov.Shapes) != 1 || !slices.Equal(ov.Shapes[0].Runs, []Rect{{64, 64, 32, 32}}) {
		t.Errorf("shapes %v, want only chunk 0,0", ov.Shapes)
	}
	if !slices.Equal(ov.Counts, []Count{{Small, 1}}) {
		t.Errorf("counts %v, want one small", ov.Counts)
	}
}

func TestGeometryBadSize(t *testing.T) {
	r := Result{Territories: []Territory{{Small, []Chunk{{0, 0}}}}}
	for _, size := range []int{0, -64, 100} {
		if ov := Geometry(r, size); len(ov.Shapes) != 0 || len(ov.Borders) != 0 {
			t.Errorf("size %d drew %v", size, ov)
		}
	}
}

// A lone territory in the corner is bordered on its two inner sides, not
// on the picture's edge.
func TestBordersCorner(t *testing.T) {
	ov := Geometry(Result{Territories: []Territory{{Small, []Chunk{{-2, -2}}}}}, 128)
	want := []Segment{{32, 0, 32, 32}, {0, 32, 32, 32}}
	if !slices.Equal(ov.Borders, want) {
		t.Errorf("borders %v, want %v", ov.Borders, want)
	}
}

// Two territories side by side share one border, and each meets the free
// area; the segments along one line are joined.
func TestBordersBetweenOwners(t *testing.T) {
	// A 2x2 block of chunks -1..0 in the middle of a 128 picture: the left
	// column is one territory, the right column another.
	r := Result{Territories: []Territory{
		{Small, []Chunk{{-1, -1}, {-1, 0}}},
		{Medium, []Chunk{{0, -1}, {0, 0}}},
	}}
	ov := Geometry(r, 128)
	want := []Segment{
		// Vertical: free|left at x=32, left|right at x=64, right|free at x=96.
		{32, 32, 32, 96}, {64, 32, 64, 96}, {96, 32, 96, 96},
		// Horizontal: free above at y=32 and below at y=96, across both.
		{32, 32, 96, 32}, {32, 96, 96, 96},
	}
	if !slices.Equal(ov.Borders, want) {
		t.Errorf("borders %v, want %v", ov.Borders, want)
	}
}

func TestNoTerritoriesNoBorders(t *testing.T) {
	ov := Geometry(Result{}, 1280)
	if len(ov.Shapes) != 0 || len(ov.Borders) != 0 || len(ov.Counts) != 0 {
		t.Errorf("empty result drew %v", ov)
	}
}

// The label sits at the mean of the centres of the chunks in the picture.
func TestLabelPoint(t *testing.T) {
	r := Result{Territories: []Territory{{Small, []Chunk{{0, 0}, {1, 0}, {0, 1}, {5, 5}}}}}
	ov := Geometry(r, 128)
	// Centres (80,80), (112,80) and (80,112); chunk 5,5 is outside.
	s := ov.Shapes[0]
	if s.LabelX != (80+112+80)/3.0 || s.LabelY != (80+80+112)/3.0 {
		t.Errorf("label at %v,%v", s.LabelX, s.LabelY)
	}
}

// Counts follow Sizes order and leave out sizes with none.
func TestCounts(t *testing.T) {
	r := Result{Territories: []Territory{
		{Unknown, []Chunk{{0, 0}}},
		{Big, []Chunk{{1, 0}}},
		{Small, []Chunk{{0, 1}}},
		{Big, []Chunk{{1, 1}}},
	}}
	want := []Count{{Small, 1}, {Big, 2}, {Unknown, 1}}
	if got := Geometry(r, 128).Counts; !slices.Equal(got, want) {
		t.Errorf("counts %v, want %v", got, want)
	}
}
