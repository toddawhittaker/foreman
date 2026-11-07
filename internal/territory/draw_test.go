package territory

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

var grey = color.RGBA{100, 100, 100, 255}

// greyPNG is a size-square picture of one grey.
func greyPNG(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []byte{grey.R, grey.G, grey.B, grey.A})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func at(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

func noLabel(string) string { return "" }

// Stripes blend rgb(200,20,20) at alpha 85 into the pixels of a territory
// where (x+y) mod 56 < 28; borders are a 2 px rgb(255,30,30) line astride
// the chunk edge; the free area is untouched.
func TestDrawPNGPixels(t *testing.T) {
	ov := Geometry(Result{Territories: []Territory{{Small, []Chunk{{-2, -2}}}}}, 128)
	out, err := DrawPNG(greyPNG(t, 128), ov, noLabel)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	striped := color.RGBA{133, 73, 73, 255}
	for _, c := range []struct {
		x, y int
		want color.RGBA
	}{
		{0, 0, striped},        // x+y = 0, on a stripe
		{27, 0, striped},       // x+y = 27, the stripe's last pixel
		{28, 0, grey},          // x+y = 28, between stripes
		{29, 26, grey},         // x+y = 55, the gap's last pixel
		{30, 26, striped},      // x+y = 56, the next stripe
		{31, 10, borderColour}, // the border's left pixel, inside the territory
		{32, 10, borderColour}, // its right pixel, in the free area
		{10, 31, borderColour}, // the border below
		{10, 32, borderColour},
		{33, 10, grey}, // free area
		{100, 100, grey},
	} {
		if got := at(img, c.x, c.y); got != c.want {
			t.Errorf("pixel %d,%d is %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

// A label is white text with a dark outline, centred on the label point.
func TestDrawPNGLabel(t *testing.T) {
	// A 4 by 4 chunk territory in the middle of a 256 picture; its label
	// point is the picture's centre, 128,128.
	var chunks []Chunk
	for y := -2; y < 2; y++ {
		for x := -2; x < 2; x++ {
			chunks = append(chunks, Chunk{x, y})
		}
	}
	ov := Geometry(Result{Territories: []Territory{{Big, chunks}}}, 256)
	if s := ov.Shapes[0]; s.LabelX != 128 || s.LabelY != 128 {
		t.Fatalf("label point %v,%v", s.LabelX, s.LabelY)
	}
	out, err := DrawPNG(greyPNG(t, 256), ov, func(size string) string { return "WWWW" })
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	var white, dark int
	left, right := 256, 0
	for y := 108; y < 148; y++ {
		for x := 0; x < 256; x++ {
			switch at(img, x, y) {
			case textColour:
				white++
				left, right = min(left, x), max(right, x)
			case outlineColour:
				dark++
			}
		}
	}
	if white == 0 || dark == 0 {
		t.Fatalf("label drew %d white and %d outline pixels", white, dark)
	}
	if mid := (left + right) / 2; mid < 124 || mid > 132 {
		t.Errorf("label centred at x %d, want about 128", mid)
	}
	// Far from the label, the territory is only striped.
	if got := at(img, 70, 70); got != (color.RGBA{133, 73, 73, 255}) && got != grey {
		t.Errorf("pixel 70,70 is %v", got)
	}
}

func TestDrawPNGRefusesWrongSize(t *testing.T) {
	ov := Geometry(Result{}, 128)
	if _, err := DrawPNG(greyPNG(t, 64), ov, noLabel); err == nil {
		t.Error("a 64 picture drew a 128 overlay")
	}
	if _, err := DrawPNG([]byte("not a png"), ov, noLabel); err == nil {
		t.Error("a non-PNG drew")
	}
}

// Drawing in Chinese uses the subset font and still draws text.
func TestDrawPNGChinese(t *testing.T) {
	ov := Geometry(Result{Territories: []Territory{{Small, []Chunk{{-1, -1}, {0, -1}, {-1, 0}, {0, 0}}}}}, 128)
	out, err := DrawPNG(greyPNG(t, 128), ov, func(string) string { return "小型" })
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	white := 0
	for y := 48; y < 80; y++ {
		for x := 32; x < 96; x++ {
			if at(img, x, y) == textColour {
				white++
			}
		}
	}
	if white < 20 {
		t.Errorf("the Chinese label drew %d white pixels", white)
	}
}
