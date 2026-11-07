package territory

import (
	"cmp"
	"slices"
)

// Overlay is what is drawn over a picture of Size pixels, in picture pixels.
type Overlay struct {
	Size   int
	Shapes []Shape
	// Borders run between chunks of different owners, the free start area
	// counting as one. The picture's edge has none.
	Borders []Segment
	// Counts holds the number of territories of each size, in Sizes order,
	// leaving out sizes with none.
	Counts []Count
}

// Shape is one territory: its cells as rectangles, one per run of chunks in
// a row, and the point its label is centred on.
type Shape struct {
	Size           string
	Runs           []Rect
	LabelX, LabelY float64
}

// Rect is a rectangle of pixels from X, Y, W wide and H high.
type Rect struct{ X, Y, W, H int }

// Segment is a horizontal or vertical line from X0, Y0 to X1, Y1, along the
// pixel boundary between two chunks.
type Segment struct{ X0, Y0, X1, Y1 int }

// Count is how many territories of one size the picture shows.
type Count struct {
	Size string
	N    int
}

// Geometry works out the overlay of r for a picture of size pixels. Chunk c
// covers tiles 32c to 32c+31, and the pixel of tile t is t + size/2. Chunks
// outside the picture are ignored, and a size Parse refuses has no overlay.
func Geometry(r Result, size int) Overlay {
	if checkSize(size) != nil {
		return Overlay{Size: size}
	}
	rad := chunkRadius(size)
	n := 2 * rad
	// owner holds 1 + the territory's index for each chunk, 0 for none.
	owner := make([]int, n*n)
	ov := Overlay{Size: size}
	counts := map[string]int{}
	for i, t := range r.Territories {
		var cells []Chunk
		for _, c := range t.Chunks {
			if inside(c, rad) {
				cells = append(cells, Chunk{c.X + rad, c.Y + rad})
			}
		}
		if len(cells) == 0 {
			continue
		}
		for _, c := range cells {
			owner[c.Y*n+c.X] = i + 1
		}
		ov.Shapes = append(ov.Shapes, shape(t.Size, cells))
		counts[t.Size]++
	}
	ov.Borders = borders(owner, n)
	for _, s := range Sizes {
		if counts[s] > 0 {
			ov.Counts = append(ov.Counts, Count{Size: s, N: counts[s]})
		}
	}
	return ov
}

// shape turns cells, in chunks from the picture's top left, into row runs
// and a label point at the mean of the chunk centres.
func shape(size string, cells []Chunk) Shape {
	slices.SortFunc(cells, func(a, b Chunk) int {
		return cmp.Or(cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
	})
	s := Shape{Size: size}
	var sumX, sumY float64
	for i, c := range cells {
		sumX += float64(c.X*ChunkTiles + ChunkTiles/2)
		sumY += float64(c.Y*ChunkTiles + ChunkTiles/2)
		if i > 0 && c.Y == cells[i-1].Y && c.X == cells[i-1].X+1 {
			s.Runs[len(s.Runs)-1].W += ChunkTiles
			continue
		}
		s.Runs = append(s.Runs, Rect{X: c.X * ChunkTiles, Y: c.Y * ChunkTiles, W: ChunkTiles, H: ChunkTiles})
	}
	s.LabelX = sumX / float64(len(cells))
	s.LabelY = sumY / float64(len(cells))
	return s
}

// borders finds the lines between neighbouring chunks of different owners
// in an n by n grid, joining neighbours along the same line into one.
func borders(owner []int, n int) []Segment {
	var out []Segment
	// Vertical lines: between columns x-1 and x, down the rows.
	for x := 1; x < n; x++ {
		start := -1
		for y := 0; y <= n; y++ {
			differ := y < n && owner[y*n+x-1] != owner[y*n+x]
			switch {
			case differ && start < 0:
				start = y
			case !differ && start >= 0:
				out = append(out, Segment{x * ChunkTiles, start * ChunkTiles, x * ChunkTiles, y * ChunkTiles})
				start = -1
			}
		}
	}
	// Horizontal lines: between rows y-1 and y, across the columns.
	for y := 1; y < n; y++ {
		start := -1
		for x := 0; x <= n; x++ {
			differ := x < n && owner[(y-1)*n+x] != owner[y*n+x]
			switch {
			case differ && start < 0:
				start = x
			case !differ && start >= 0:
				out = append(out, Segment{start * ChunkTiles, y * ChunkTiles, x * ChunkTiles, y * ChunkTiles})
				start = -1
			}
		}
	}
	return out
}
