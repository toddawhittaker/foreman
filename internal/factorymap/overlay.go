package factorymap

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
)

// Limits of the entity file that are Foreman's own (the file's total size and
// entity count are SPEC 14.6's).
const (
	// maxEntityTypes is the most prototype types the first line may list. The
	// base game and Space Age have a few hundred.
	maxEntityTypes = 4096
	// maxLineBytes is the longest line read: the type line is the longest.
	maxLineBytes = 1 << 20
	// maxBoxTiles is the longest side of a box, in tiles. The largest real
	// building is about 20 tiles; the limit keeps the cost of drawing a file
	// of MaxEntities boxes bounded.
	maxBoxTiles = 64
	// maxCoordinate is the largest whole number the output may hold (14.6).
	maxCoordinate = 1 << 53
)

// Palette indexes of a layer: transparent, the outline, and a translucent
// fill so a large building reads as a shape and not only a frame.
const (
	pixelClear   = 0
	pixelOutline = 1
	pixelFill    = 2
)

// fillAlpha is the opacity of the translucent fill.
const fillAlpha = 90

// DrawOverlay draws one transparent layer per category from the surface's
// entity file, on the pixel grid of s (SPEC 5.16). The file's first line is a
// JSON array of the entity prototype types, and each later line is
// [type_index, x1, y1, x2, y2]: the building's box in tiles, whole numbers
// counted from the top-left corner of the picture, with x1, y1 included and
// x2, y2 not (a one-tile belt at the corner is [i,0,0,1,1]). A picture that
// scales tiles down (s.TilesPerPixel) is covered by rounding a box out to
// whole pixels. A box outside the picture is clipped or, when nothing is left,
// skipped: output outside the chart is not a reason to lose the drawing. Every
// category has a layer, empty or not, so the layers share the base's grid. It
// reads the file as untrusted: anything malformed is an error, including a
// blank line in the middle (F1's mod never writes one) and a box over 64
// tiles a side.
func DrawOverlay(entities io.Reader, s Surface) (map[Category]*image.Paletted, error) {
	layers, _, err := drawOverlay(entities, s, MaxEntities)
	return layers, err
}

// drawOverlay is DrawOverlay with the entity limit as a parameter. It also
// returns how many pixels it wrote, which is what drawing costs, so a test
// can bound the work without a clock.
func drawOverlay(entities io.Reader, s Surface, maxEntities int) (map[Category]*image.Paletted, int64, error) {
	if s.Width < 1 || s.Height < 1 || s.Width > MaxSidePixels || s.Height > MaxSidePixels || s.TilesPerPixel < 1 {
		return nil, 0, fmt.Errorf("overlay: bad surface size %dx%d at %d tiles a pixel", s.Width, s.Height, s.TilesPerPixel)
	}
	sc := bufio.NewScanner(entities)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, 0, fmt.Errorf("overlay: read entity types: %w", err)
		}
		return nil, 0, errors.New("overlay: no entity type line")
	}
	var types []string
	if err := json.Unmarshal(sc.Bytes(), &types); err != nil {
		return nil, 0, fmt.Errorf("overlay: entity type line: %w", err)
	}
	if len(types) > maxEntityTypes {
		return nil, 0, fmt.Errorf("overlay: %d entity types, at most %d", len(types), maxEntityTypes)
	}
	cats := make([]Category, len(types))
	for i, t := range types {
		cats[i] = CategoryOf(t)
	}

	layers := make(map[Category]*image.Paletted, len(Categories))
	for _, c := range Categories {
		layers[c] = image.NewPaletted(image.Rect(0, 0, s.Width, s.Height), layerPalette(c))
	}

	n := 0
	var writes int64
	for sc.Scan() {
		n++
		if n > maxEntities {
			return nil, 0, fmt.Errorf("overlay: more than %d entities", maxEntities)
		}
		ti, x1, y1, x2, y2, err := parseBox(sc.Bytes())
		if err != nil {
			return nil, 0, fmt.Errorf("overlay: entity %d: %w", n, err)
		}
		if ti >= int64(len(cats)) {
			return nil, 0, fmt.Errorf("overlay: entity %d: type index %d, only %d types", n, ti, len(cats))
		}
		if x2 <= x1 || y2 <= y1 {
			return nil, 0, fmt.Errorf("overlay: entity %d: empty box", n)
		}
		if x2-x1 > maxBoxTiles || y2-y1 > maxBoxTiles {
			return nil, 0, fmt.Errorf("overlay: entity %d: box over %d tiles a side", n, maxBoxTiles)
		}
		writes += drawBox(layers[cats[ti]], x1, y1, x2, y2, int64(s.TilesPerPixel))
	}
	if err := sc.Err(); err != nil {
		return nil, 0, fmt.Errorf("overlay: read entities: %w", err)
	}
	return layers, writes, nil
}

// layerPalette is a category's palette: clear, solid, translucent.
func layerPalette(c Category) color.Palette {
	col := CategoryColor(c)
	fill := col
	fill.A = fillAlpha
	return color.Palette{color.NRGBA{}, col, fill}
}

// drawBox draws a box given in tiles onto a layer. A box of one to two pixels
// on a side is filled solid; a larger one is an outline with a translucent
// fill that never covers an outline already drawn. It returns the number of
// pixels it wrote.
func drawBox(p *image.Paletted, x1, y1, x2, y2, tpp int64) int64 {
	w, h := int64(p.Rect.Dx()), int64(p.Rect.Dy())
	// Round out to whole pixels, then clip.
	px1, py1 := x1/tpp, y1/tpp
	px2, py2 := (x2+tpp-1)/tpp, (y2+tpp-1)/tpp
	if px2 > w {
		px2 = w
	}
	if py2 > h {
		py2 = h
	}
	if px1 >= px2 || py1 >= py2 {
		return 0
	}
	small := px2-px1 < 3 || py2-py1 < 3
	for y := py1; y < py2; y++ {
		row := p.Pix[y*int64(p.Stride):]
		for x := px1; x < px2; x++ {
			switch {
			case small || x == px1 || x == px2-1 || y == py1 || y == py2-1:
				row[x] = pixelOutline
			case row[x] == pixelClear:
				row[x] = pixelFill
			}
		}
	}
	return (px2 - px1) * (py2 - py1)
}

// parseBox reads one line, [a,b,c,d,e], of five whole numbers from 0 to 2^53
// with only spaces between the tokens. It is written by hand so that a
// fraction, an exponent, a sign or a quoted number is refused, and so that two
// million lines parse quickly.
func parseBox(line []byte) (ti, x1, y1, x2, y2 int64, err error) {
	i := 0
	skip := func() {
		for i < len(line) && line[i] == ' ' {
			i++
		}
	}
	expect := func(b byte) bool {
		skip()
		if i < len(line) && line[i] == b {
			i++
			return true
		}
		return false
	}
	num := func() (int64, bool) {
		skip()
		start := i
		var v int64
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			v = v*10 + int64(line[i]-'0')
			if v > maxCoordinate {
				return 0, false
			}
			i++
		}
		if i == start || (line[start] == '0' && i-start > 1) {
			return 0, false
		}
		return v, true
	}
	vals := [5]int64{}
	if !expect('[') {
		return 0, 0, 0, 0, 0, errors.New("not an array of five numbers")
	}
	for k := range vals {
		if k > 0 && !expect(',') {
			return 0, 0, 0, 0, 0, errors.New("not an array of five numbers")
		}
		v, ok := num()
		if !ok {
			return 0, 0, 0, 0, 0, errors.New("not a whole number from 0 to 2^53")
		}
		vals[k] = v
	}
	if !expect(']') {
		return 0, 0, 0, 0, 0, errors.New("not an array of five numbers")
	}
	skip()
	if i != len(line) {
		return 0, 0, 0, 0, 0, errors.New("text after the array")
	}
	return vals[0], vals[1], vals[2], vals[3], vals[4], nil
}
