package territory

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"
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
// where (x-y) mod 56 < 28, bars from top left to bottom right; borders are a 2 px rgb(255,30,30) line astride
// the chunk edge; the free area is untouched.
func TestDrawPNGPixels(t *testing.T) {
	ov := Geometry(Result{Territories: []Territory{{Small, []Chunk{{-2, -2}}}}}, 128)
	out, err := DrawPNG(context.Background(), greyPNG(t, 128), ov, noLabel)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out)
	striped := color.RGBA{133, 73, 73, 255}
	for _, c := range []struct {
		x, y int
		want color.RGBA
	}{
		{0, 0, striped},        // x-y = 0, on a stripe
		{27, 0, striped},       // x-y = 27, the stripe's last pixel
		{28, 0, grey},          // x-y = 28, between stripes
		{0, 1, grey},           // x-y = -1, so 55: the gap's last pixel
		{0, 28, grey},          // x-y = -28, so 28: between stripes
		{0, 29, striped},       // x-y = -29, so 27: the next bar down
		{26, 26, striped},      // x-y = 0, down the same bar
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
	out, err := DrawPNG(context.Background(), greyPNG(t, 256), ov, func(size string) string { return "WWWW" })
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
	if _, err := DrawPNG(context.Background(), greyPNG(t, 64), ov, noLabel); err == nil {
		t.Error("a 64 picture drew a 128 overlay")
	}
	if _, err := DrawPNG(context.Background(), []byte("not a png"), ov, noLabel); err == nil {
		t.Error("a non-PNG drew")
	}
}

// Drawing in Chinese uses the subset font and still draws text.
func TestDrawPNGChinese(t *testing.T) {
	ov := Geometry(Result{Territories: []Territory{{Small, []Chunk{{-1, -1}, {0, -1}, {-1, 0}, {0, 0}}}}}, 128)
	out, err := DrawPNG(context.Background(), greyPNG(t, 128), ov, func(string) string { return "小型" })
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

// holdSlot takes the render slot for the test, as a render in progress does.
func holdSlot(t *testing.T) (release func()) {
	t.Helper()
	renderSlot <- struct{}{}
	var once sync.Once
	release = func() { once.Do(func() { <-renderSlot }) }
	t.Cleanup(release)
	return release
}

// waitFor polls until cond holds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); !cond(); {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// SPEC 14.1: a request that gives up stops waiting for the render slot and
// leaves no place taken in the queue.
func TestDrawPNGStopsWaitingWhenCancelled(t *testing.T) {
	holdSlot(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := DrawPNG(ctx, greyPNG(t, 128), Geometry(Result{}, 128), noLabel)
		done <- err
	}()
	waitFor(t, "the caller to queue", func() bool { return waiting.Load() == 1 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a cancelled caller is still waiting for the slot")
	}
	if n := waiting.Load(); n != 0 {
		t.Errorf("%d callers still counted as waiting", n)
	}
}

// SPEC 14.1: a fifth caller waiting for the slot is refused at once, so the
// queue (and the pictures held with it) stays small.
func TestDrawPNGRefusesTooManyWaiters(t *testing.T) {
	holdSlot(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := greyPNG(t, 128)
	ov := Geometry(Result{}, 128)
	var wg sync.WaitGroup
	for range maxWaiting {
		wg.Go(func() { _, _ = DrawPNG(ctx, src, ov, noLabel) })
	}
	waitFor(t, "the queue to fill", func() bool { return waiting.Load() == maxWaiting })

	start := time.Now()
	if _, err := DrawPNG(context.Background(), src, ov, noLabel); !errors.Is(err, ErrBusy) {
		t.Errorf("got %v, want ErrBusy", err)
	}
	if time.Since(start) > time.Second {
		t.Error("the extra caller waited instead of being refused at once")
	}
	cancel()
	wg.Wait()
	if n := waiting.Load(); n != 0 {
		t.Errorf("%d callers still counted as waiting", n)
	}
}

// With the slot free a caller draws at once, and the slot is free again after.
func TestDrawPNGFreesTheSlot(t *testing.T) {
	if _, err := DrawPNG(context.Background(), greyPNG(t, 128), Geometry(Result{}, 128), noLabel); err != nil {
		t.Fatal(err)
	}
	if len(renderSlot) != 0 || waiting.Load() != 0 {
		t.Errorf("slot holds %d, %d waiting", len(renderSlot), waiting.Load())
	}
}

// whiteBounds is the box of the label text's white pixels in img.
func whiteBounds(img image.Image) (image.Rectangle, int) {
	var r image.Rectangle
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if at(img, x, y) == textColour {
				r = r.Union(image.Rect(x, y, x+1, y+1))
				n++
			}
		}
	}
	return r, n
}

// Captions are drawn with the territory labels' look, and pulled in at an
// edge as the page places them, so one at a corner stays whole inside.
func TestDrawPNGCaptionsPulledIn(t *testing.T) {
	ov := Overlay{Size: 256, Captions: []Caption{{Text: "12.3M", U: 1, V: 1}}}
	out, err := DrawPNG(context.Background(), greyPNG(t, 256), ov, noLabel)
	if err != nil {
		t.Fatal(err)
	}
	r, n := whiteBounds(decode(t, out))
	if n == 0 {
		t.Fatal("the caption drew nothing")
	}
	if r.Max.X > 254 || r.Max.Y > 254 || r.Max.X < 230 || r.Max.Y < 230 {
		t.Errorf("caption at %v, want inside the bottom right corner", r)
	}
	if countOutline(decode(t, out)) == 0 {
		t.Error("the caption has no outline")
	}
}

func countOutline(img image.Image) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if at(img, x, y) == outlineColour {
				n++
			}
		}
	}
	return n
}

// A caption that would meet one drawn already, or a territory's label, is
// left out; the first, as the largest patch, wins.
func TestDrawPNGCaptionsNeverOverlap(t *testing.T) {
	one := Overlay{Size: 256, Captions: []Caption{{Text: "12.3M", U: 0.5, V: 0.2}}}
	two := Overlay{Size: 256, Captions: []Caption{{Text: "12.3M", U: 0.5, V: 0.2}, {Text: "4.5K", U: 0.51, V: 0.21}, {Text: "7K", U: 0.1, V: 0.9}}}
	count := func(ov Overlay) int {
		out, err := DrawPNG(context.Background(), greyPNG(t, 256), ov, noLabel)
		if err != nil {
			t.Fatal(err)
		}
		_, n := whiteBounds(decode(t, out))
		return n
	}
	alone, far := count(one), count(Overlay{Size: 256, Captions: two.Captions[2:]})
	if got := count(two); got != alone+far {
		t.Errorf("drew %d white pixels, want %d: the overlapping caption left out, the far one drawn", got, alone+far)
	}
	// A territory label in the middle keeps its place; a caption on it goes.
	var chunks []Chunk
	for y := -2; y < 2; y++ {
		for x := -2; x < 2; x++ {
			chunks = append(chunks, Chunk{x, y})
		}
	}
	terr := Geometry(Result{Territories: []Territory{{Big, chunks}}}, 256)
	label := func(string) string { return "Big" }
	draw := func(ov Overlay) int {
		out, err := DrawPNG(context.Background(), greyPNG(t, 256), ov, label)
		if err != nil {
			t.Fatal(err)
		}
		_, n := whiteBounds(decode(t, out))
		return n
	}
	bare := draw(terr)
	terr.Captions = []Caption{{Text: "300%", U: 0.5, V: 0.5}}
	if got := draw(terr); got != bare {
		t.Errorf("a caption over the territory label was drawn: %d white pixels, want %d", got, bare)
	}
}

// Chinese amounts use the subset's digits and 万 and 亿, and draw as text.
func TestDrawPNGChineseAmounts(t *testing.T) {
	fs, err := fonts()
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"123.5万", "1.2亿", "1.2万亿", "1,235%"} {
		if !hasGlyphs(fs[1], text) {
			t.Errorf("the Chinese subset lacks a glyph of %q", text)
		}
	}
	if fontFor(fs, "1.2万") != 1 {
		t.Error("a Chinese amount is not drawn with the subset")
	}
	// A narrow no-break space, French grouping, which no font has, is drawn as a no-break space.
	if l := drawnText("1\u202f235\u00a0%"); l != "1\u00a0235\u00a0%" || !hasGlyphs(fs[fontFor(fs, l)], l) {
		t.Errorf("French grouping is drawn as %q", l)
	}
	out, err := DrawPNG(context.Background(), greyPNG(t, 128), Overlay{Size: 128, Captions: []Caption{{Text: "1.2万", U: 0.5, V: 0.5}}}, noLabel)
	if err != nil {
		t.Fatal(err)
	}
	if _, n := whiteBounds(decode(t, out)); n < 20 {
		t.Errorf("the Chinese amount drew %d white pixels", n)
	}
}
