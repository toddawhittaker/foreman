package territory

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// The overlay's look, as the page draws it: diagonal red stripes lighter
// than the game's own, and a solid border.
const (
	stripePeriod = 56
	stripeWidth  = 28
	stripeAlpha  = 85
)

var (
	stripeColour  = color.RGBA{200, 20, 20, 255}
	borderColour  = color.RGBA{255, 30, 30, 255}
	outlineColour = color.RGBA{16, 16, 16, 255}
	textColour    = color.RGBA{255, 255, 255, 255}
)

// outline holds the offsets the dark copies of a label are drawn at, a ring
// about 2 px wide around the white text.
var outline = []image.Point{{-2, 0}, {2, 0}, {0, -2}, {0, 2}, {-1, -1}, {1, -1}, {-1, 1}, {1, 1}}

// outlineWidth is how far the outline reaches past the text, in pixels.
const outlineWidth = 2

// notoLabels is Noto Sans CJK SC Bold cut down to the Simplified Chinese
// labels' glyphs (fonts/NOTICE); Go Bold has no CJK.
//
//go:embed fonts/NotoSansCJKsc-Bold-labels.otf
var notoLabels []byte

// fonts are parsed once: Go Bold first, then the CJK subset.
var fonts = sync.OnceValues(func() ([]*sfnt.Font, error) {
	var out []*sfnt.Font
	for _, b := range [][]byte{gobold.TTF, notoLabels} {
		f, err := opentype.Parse(b)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
})

// renderSlot lets one render run at a time. The peak is the decoded picture
// plus its RGBA copy: the game's PNG is untrusted and may be 16-bit, which
// decodes at 8 bytes a pixel, so at MaxSize (3072 square) that is about
// 75 MB plus 38 MB, about 115 MB with the encoder's buffers on top. Raising
// MaxSize, or the number of renders at once, needs this checked again.
var renderSlot = make(chan struct{}, 1)

// maxWaiting is how many callers may queue for renderSlot. Each holds its
// source picture (up to 16 MB) while it waits.
const maxWaiting = 4

// waiting counts the callers queued for renderSlot.
var waiting atomic.Int32

// ErrBusy means too many callers are already waiting for the render slot.
var ErrBusy = errors.New("too many pictures are being drawn")

// acquire takes renderSlot, waiting until ctx ends. It refuses with ErrBusy
// when maxWaiting callers are already waiting.
func acquire(ctx context.Context) error {
	select {
	case renderSlot <- struct{}{}:
		return nil
	default:
	}
	if waiting.Add(1) > maxWaiting {
		waiting.Add(-1)
		return ErrBusy
	}
	defer waiting.Add(-1)
	select {
	case renderSlot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DrawPNG draws ov over the PNG src, which must be ov.Size pixels square,
// and returns the new PNG. label gives the word for each size. It waits for
// the render slot until ctx ends, and is ErrBusy when the queue is full.
func DrawPNG(ctx context.Context, src []byte, ov Overlay, label func(size string) string) ([]byte, error) {
	if err := acquire(ctx); err != nil {
		return nil, err
	}
	defer func() { <-renderSlot }()

	cfg, err := png.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	if cfg.Width != ov.Size || cfg.Height != ov.Size {
		return nil, fmt.Errorf("the picture is %d by %d, not %d square", cfg.Width, cfg.Height, ov.Size)
	}
	pic, err := png.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, err
	}
	img := image.NewRGBA(pic.Bounds())
	draw.Draw(img, img.Bounds(), pic, pic.Bounds().Min, draw.Src)

	for _, s := range ov.Shapes {
		for _, r := range s.Runs {
			stripe(img, r)
		}
	}
	for _, seg := range ov.Borders {
		border(img, seg)
	}
	fs, err := fonts()
	if err != nil {
		return nil, err
	}
	px := float64(max(16, ov.Size/64))
	faces := make([]font.Face, len(fs))
	for i, f := range fs {
		if faces[i], err = opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull}); err != nil {
			return nil, err
		}
		defer func() { _ = faces[i].Close() }()
	}
	var drawn []image.Rectangle
	for _, s := range ov.Shapes {
		drawn = append(drawn, drawLabel(img, fs, faces, label(s.Size), s.LabelX, s.LabelY))
	}
	for _, l := range ov.Captions {
		text := strings.ReplaceAll(l.Text, "\u202f", "\u00a0")
		w, h := measure(fs, faces, text)
		// The box's top left, pulled in as the page's style sheet does.
		x0 := min(max(0, l.U*float64(ov.Size-w)), float64(max(0, ov.Size-w)))
		y0 := min(max(0, l.V*float64(ov.Size-h)), float64(max(0, ov.Size-h)))
		box := image.Rect(int(x0), int(y0), int(x0)+w, int(y0)+h).Inset(-labelGap)
		if slices.ContainsFunc(drawn, box.Overlaps) {
			continue
		}
		drawn = append(drawn, drawLabel(img, fs, faces, text, x0+float64(w)/2, y0+float64(h)/2))
	}
	var out bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// stripe blends the stripe colour into the pixels of r on the stripes, bars
// running from top left to bottom right as in the game.
func stripe(img *image.RGBA, r Rect) {
	area := image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H).Intersect(img.Bounds())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			if ((x-y)%stripePeriod+stripePeriod)%stripePeriod >= stripeWidth {
				continue
			}
			i := img.PixOffset(x, y)
			p := img.Pix[i : i+3 : i+3]
			p[0] = blend(p[0], stripeColour.R)
			p[1] = blend(p[1], stripeColour.G)
			p[2] = blend(p[2], stripeColour.B)
		}
	}
}

func blend(under, over uint8) uint8 {
	return uint8((int(under)*(255-stripeAlpha) + int(over)*stripeAlpha + 127) / 255) //nolint:gosec // the sum of two weighted bytes over 255 is a byte
}

// border paints a 2 px line along seg, one pixel on each side of the
// boundary it lies on.
func border(img *image.RGBA, seg Segment) {
	var area image.Rectangle
	if seg.X0 == seg.X1 {
		area = image.Rect(seg.X0-1, seg.Y0, seg.X0+1, seg.Y1)
	} else {
		area = image.Rect(seg.X0, seg.Y0-1, seg.X1, seg.Y0+1)
	}
	draw.Draw(img, area.Intersect(img.Bounds()), image.NewUniform(borderColour), image.Point{}, draw.Src)
}

// labelGap is the room, in pixels, kept between two labels.
const labelGap = 2

// drawLabel writes text centred on x, y in white with a dark outline, in the
// first font that has all its glyphs, and returns the box it covers.
func drawLabel(img *image.RGBA, fs []*sfnt.Font, faces []font.Face, text string, x, y float64) image.Rectangle {
	face := faces[fontFor(fs, text)]
	m := face.Metrics()
	width := font.MeasureString(face, text)
	dot := fixed.Point26_6{
		X: fixed.Int26_6(x*64) - width/2,
		Y: fixed.Int26_6(y*64) + (m.Ascent-m.Descent)/2,
	}
	d := font.Drawer{Dst: img, Src: image.NewUniform(outlineColour), Face: face}
	for _, o := range outline {
		d.Dot = dot.Add(fixed.P(o.X, o.Y))
		d.DrawString(text)
	}
	d.Src = image.NewUniform(textColour)
	d.Dot = dot
	d.DrawString(text)
	w, h := measure(fs, faces, text)
	return image.Rect(0, 0, w, h).Add(image.Pt(int(x)-w/2, int(y)-h/2)).Inset(-labelGap)
}

// measure is the width and height of text as drawLabel draws it, its
// outline included.
func measure(fs []*sfnt.Font, faces []font.Face, text string) (w, h int) {
	face := faces[fontFor(fs, text)]
	m := face.Metrics()
	ring := 2 * outlineWidth
	return font.MeasureString(face, text).Ceil() + ring, (m.Ascent + m.Descent).Ceil() + ring
}

// fontFor picks the first font that has every glyph of text, or the first
// font if none has; the glyph tests keep the labels covered.
func fontFor(fs []*sfnt.Font, text string) int {
	for i, f := range fs {
		if hasGlyphs(f, text) {
			return i
		}
	}
	return 0
}

// Covered is true when one of the fonts the labels are drawn with has every
// glyph of text, so it draws without boxes. A narrow no-break space is drawn
// as a no-break space, which they have.
func Covered(text string) bool {
	fs, err := fonts()
	if err != nil {
		return false
	}
	text = strings.ReplaceAll(text, "\u202f", "\u00a0")
	return hasGlyphs(fs[fontFor(fs, text)], text)
}

func hasGlyphs(f *sfnt.Font, text string) bool {
	var buf sfnt.Buffer
	for _, r := range text {
		if g, err := f.GlyphIndex(&buf, r); err != nil || g == 0 {
			return false
		}
	}
	return true
}
