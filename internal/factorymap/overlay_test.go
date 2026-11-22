package factorymap

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// entityFile builds an entity file: the type line, then one line per box.
func entityFile(types string, boxes ...string) string {
	if len(boxes) == 0 {
		return types + "\n"
	}
	return types + "\n" + strings.Join(boxes, "\n") + "\n"
}

const typesLine = `["transport-belt","assembling-machine","no-such-type"]`

func surf(w, h, tpp int) Surface {
	return Surface{Index: 1, Name: "nauvis", Kind: "planet", Width: w, Height: h, TilesPerPixel: tpp}
}

func drawTestOverlay(t *testing.T, file string, s Surface) map[Category]*image.Paletted {
	t.Helper()
	layers, err := DrawOverlay(strings.NewReader(file), s)
	if err != nil {
		t.Fatalf("DrawOverlay: %v", err)
	}
	return layers
}

// countSet is how many pixels of a layer are not transparent.
func countSet(p *image.Paletted) int {
	n := 0
	for _, i := range p.Pix {
		if i != 0 {
			n++
		}
	}
	return n
}

// Every category has a layer on the same grid as the base picture, even
// with no entities, so the viewer's grids always line up.
func TestOverlayHasEveryLayerOnTheBaseGrid(t *testing.T) {
	layers := drawTestOverlay(t, entityFile(typesLine), surf(40, 30, 1))
	if len(layers) != len(Categories) {
		t.Fatalf("%d layers, want %d", len(layers), len(Categories))
	}
	for _, c := range Categories {
		l := layers[c]
		if l == nil {
			t.Fatalf("no layer for %s", c)
		}
		if l.Rect != image.Rect(0, 0, 40, 30) {
			t.Errorf("%s: rect %v", c, l.Rect)
		}
		if countSet(l) != 0 {
			t.Errorf("%s: drew with no entities", c)
		}
		if _, _, _, a := l.Palette[0].RGBA(); a != 0 {
			t.Errorf("%s: index 0 is not transparent", c)
		}
	}
}

func TestOverlayOneTileEntityFillsOnePixelInItsCategoryOnly(t *testing.T) {
	layers := drawTestOverlay(t, entityFile(typesLine, "[0,2,3,3,4]"), surf(10, 10, 1))
	for _, c := range Categories {
		want := 0
		if c == CategoryBelts {
			want = 1
		}
		if got := countSet(layers[c]); got != want {
			t.Errorf("%s: %d pixels, want %d", c, got, want)
		}
	}
	if layers[CategoryBelts].ColorIndexAt(2, 3) != 1 {
		t.Error("belt pixel (2,3) is not the solid colour")
	}
}

func TestOverlayLargeEntityIsAnOutline(t *testing.T) {
	// A 5 by 5 machine at tiles 10..15.
	layers := drawTestOverlay(t, entityFile(typesLine, "[1,10,10,15,15]"), surf(30, 30, 1))
	p := layers[CategoryProduction]
	for x := 10; x < 15; x++ {
		for y := 10; y < 15; y++ {
			edge := x == 10 || x == 14 || y == 10 || y == 14
			want := uint8(2)
			if edge {
				want = 1
			}
			if got := p.ColorIndexAt(x, y); got != want {
				t.Errorf("(%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}
	if got := countSet(p); got != 25 {
		t.Errorf("%d pixels set, want 25", got)
	}
	if p.ColorIndexAt(9, 12) != 0 || p.ColorIndexAt(15, 12) != 0 {
		t.Error("drew outside the box")
	}
}

// An outline never hides what is under it: a later translucent interior
// does not overwrite an earlier outline.
func TestOverlayInteriorDoesNotCoverEarlierOutline(t *testing.T) {
	layers := drawTestOverlay(t, entityFile(typesLine,
		"[1,10,10,15,15]", // outline
		"[1,8,8,19,19]",   // bigger box over it
	), surf(30, 30, 1))
	if got := layers[CategoryProduction].ColorIndexAt(10, 12); got != 1 {
		t.Errorf("earlier outline pixel is %d, want 1", got)
	}
}

func TestOverlayScaledDownPictureRoundsOut(t *testing.T) {
	// 4 tiles a pixel: tiles 8..12 are exactly pixel 2; tiles 9..10 touch pixel 2.
	layers := drawTestOverlay(t, entityFile(typesLine, "[0,8,8,12,12]", "[0,17,17,18,18]"), surf(10, 10, 4))
	b := layers[CategoryBelts]
	if b.ColorIndexAt(2, 2) != 1 || b.ColorIndexAt(4, 4) != 1 {
		t.Error("expected pixels (2,2) and (4,4)")
	}
	if got := countSet(b); got != 2 {
		t.Errorf("%d pixels, want 2", got)
	}
}

func TestOverlayClipsAndSkipsBoxesOutsideThePicture(t *testing.T) {
	layers := drawTestOverlay(t, entityFile(typesLine,
		"[0,8,8,20,9]",    // one row, runs off the right edge
		"[0,50,50,60,60]", // wholly outside
		"[0,0,12,1,13]",   // below the picture
		"[0,9007199254740991,0,9007199254740992,1]", // far right, at the number limit
	), surf(10, 10, 1))
	if got := countSet(layers[CategoryBelts]); got != 2 {
		t.Errorf("%d pixels, want 2 (the clipped row)", got)
	}
}

func TestOverlayUnknownTypeIsOther(t *testing.T) {
	layers := drawTestOverlay(t, entityFile(typesLine, "[2,1,1,2,2]"), surf(5, 5, 1))
	if countSet(layers[CategoryOther]) != 1 {
		t.Error("unknown type did not land in other")
	}
}

func TestOverlayEncodesToPNG(t *testing.T) {
	layers := drawTestOverlay(t, entityFile(typesLine, "[0,1,1,2,2]"), surf(5, 5, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, layers[CategoryBelts]); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Error("transparent pixel decoded opaque")
	}
	if _, _, _, a := img.At(1, 1).RGBA(); a == 0 {
		t.Error("drawn pixel decoded transparent")
	}
}

func TestOverlayRefusesBadOutput(t *testing.T) {
	big := 4097
	cases := map[string]struct {
		file string
		s    Surface
	}{
		"empty file":          {"", surf(5, 5, 1)},
		"type line not array": {entityFile(`{"a":1}`), surf(5, 5, 1)},
		"type not string":     {entityFile(`["a",1]`), surf(5, 5, 1)},
		"type line trailing":  {entityFile(`["a"] x`), surf(5, 5, 1)},
		"too many types":      {entityFile("[" + strings.Repeat(`"a",`, maxEntityTypes) + `"a"]`), surf(5, 5, 1)},
		"four numbers":        {entityFile(typesLine, "[0,1,1,2]"), surf(5, 5, 1)},
		"six numbers":         {entityFile(typesLine, "[0,1,1,2,2,3]"), surf(5, 5, 1)},
		"not an array":        {entityFile(typesLine, `{"t":0}`), surf(5, 5, 1)},
		"blank line":          {entityFile(typesLine, "[0,1,1,2,2]", "", "[0,1,1,2,2]"), surf(5, 5, 1)},
		"negative":            {entityFile(typesLine, "[0,-1,1,2,2]"), surf(5, 5, 1)},
		"fraction":            {entityFile(typesLine, "[0,1.5,1,2,2]"), surf(5, 5, 1)},
		"exponent":            {entityFile(typesLine, "[0,1e1,1,20,2]"), surf(5, 5, 1)},
		"string number":       {entityFile(typesLine, `[0,"1",1,2,2]`), surf(5, 5, 1)},
		"over 2^53":           {entityFile(typesLine, "[0,1,1,9007199254740993,2]"), surf(5, 5, 1)},
		"index past the list": {entityFile(typesLine, "[3,1,1,2,2]"), surf(5, 5, 1)},
		"box 65 wide":         {entityFile(typesLine, "[0,0,0,65,1]"), surf(5, 5, 1)},
		"box 65 tall":         {entityFile(typesLine, "[0,0,0,1,65]"), surf(5, 5, 1)},
		"whole picture box":   {entityFile(typesLine, "[0,0,0,4096,4096]"), surf(MaxSidePixels, MaxSidePixels, 1)},
		"empty box":           {entityFile(typesLine, "[0,1,1,1,2]"), surf(5, 5, 1)},
		"backwards box":       {entityFile(typesLine, "[0,3,1,2,2]"), surf(5, 5, 1)},
		"line too long":       {entityFile(typesLine, "[0,1,1,2,2"+strings.Repeat(" ", maxLineBytes)+"]"), surf(5, 5, 1)},
		"zero width":          {entityFile(typesLine), surf(0, 5, 1)},
		"zero height":         {entityFile(typesLine), surf(5, 0, 1)},
		"too wide":            {entityFile(typesLine), surf(big, 5, 1)},
		"too tall":            {entityFile(typesLine), surf(5, big, 1)},
		"zero scale":          {entityFile(typesLine), surf(5, 5, 0)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if layers, err := DrawOverlay(strings.NewReader(c.file), c.s); err == nil {
				t.Errorf("no error (layers %v)", layers != nil)
			}
		})
	}
}

func TestOverlayRefusesMoreEntitiesThanTheLimit(t *testing.T) {
	file := entityFile(typesLine, "[0,0,0,1,1]", "[0,1,0,2,1]", "[0,2,0,3,1]")
	if _, _, err := drawOverlay(strings.NewReader(file), surf(5, 5, 1), 3); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
	if _, _, err := drawOverlay(strings.NewReader(file), surf(5, 5, 1), 2); err == nil {
		t.Fatal("over the limit was accepted")
	}
}

func TestOverlayPassesOnAReadError(t *testing.T) {
	boom := errors.New("boom")
	r := iotest.ErrReader(boom)
	if _, err := DrawOverlay(r, surf(5, 5, 1)); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
}

func TestOverlayMaxSizedPicture(t *testing.T) {
	layers := drawTestOverlay(t, entityFile(typesLine, "[1,4032,4032,4096,4096]"), surf(MaxSidePixels, MaxSidePixels, 1))
	if got := layers[CategoryProduction].ColorIndexAt(4095, 4095); got != 1 {
		t.Errorf("corner = %d, want 1", got)
	}
}

func TestOverlayManyEntitiesAllDrawn(t *testing.T) {
	var b strings.Builder
	b.WriteString(typesLine + "\n")
	for i := 0; i < 200000; i++ {
		fmt.Fprintf(&b, "[0,%d,%d,%d,%d]\n", i%1000, i/1000, i%1000+1, i/1000+1)
	}
	layers := drawTestOverlay(t, b.String(), surf(1000, 200, 1))
	if got := countSet(layers[CategoryBelts]); got != 200000 {
		t.Errorf("%d pixels, want 200000", got)
	}
}

// The cost of drawing is the pixels written, and the side limit bounds it per
// box: two million boxes are at most 2e6 x 64 x 64 writes, not 10^13. The test
// counts the writes, which does not depend on how busy the machine is. The
// clock only catches a hang.
func TestOverlayLargestBoxesCostABoundedNumberOfWrites(t *testing.T) {
	const boxes = 2000
	var b strings.Builder
	b.WriteString(typesLine + "\n")
	for i := 0; i < boxes; i++ {
		fmt.Fprintf(&b, "[1,%d,%d,%d,%d]\n", i%3000, i%3000, i%3000+MaxBoxTiles, i%3000+MaxBoxTiles)
	}
	start := time.Now()
	_, writes, err := drawOverlay(strings.NewReader(b.String()), surf(MaxSidePixels, MaxSidePixels, 1), MaxEntities)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(boxes) * MaxBoxTiles * MaxBoxTiles; writes != want {
		t.Errorf("%d pixel writes, want %d (every box whole on the picture)", writes, want)
	}
	if maxWrites := int64(MaxEntities) * MaxBoxTiles * MaxBoxTiles; maxWrites > 1<<33 {
		t.Errorf("a full file costs %d writes, over the bound", maxWrites)
	}
	if d := time.Since(start); d > 5*time.Minute {
		t.Errorf("took %v: hung?", d)
	}
}

// Clipped boxes cost only what lands on the picture.
func TestOverlayWritesAreClipped(t *testing.T) {
	_, writes, err := drawOverlay(strings.NewReader(entityFile(typesLine, "[0,8,8,40,40]", "[0,50,50,60,60]")), surf(10, 10, 1), MaxEntities)
	if err != nil {
		t.Fatal(err)
	}
	if writes != 4 {
		t.Errorf("%d writes, want 4 (a 2 by 2 corner; the outside box is skipped)", writes)
	}
}
