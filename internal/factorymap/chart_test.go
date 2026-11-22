package factorymap

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"testing"
)

// chunkData returns a chunk's data field: pixels as RGB565 little endian,
// compressed and base64, as the helper mod writes it.
func chunkData(t *testing.T, raw []byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// solid is a chunk of one RGB565 value.
func solid(v uint16) []byte {
	raw := make([]byte, chunkDataBytes)
	for i := range chunkDataBytes / 2 {
		binary.LittleEndian.PutUint16(raw[2*i:], v)
	}
	return raw
}

func chartLineText(t *testing.T, x, y int, raw []byte) string {
	t.Helper()
	return fmt.Sprintf(`{"x":%d,"y":%d,"data":%q}`, x, y, chunkData(t, raw)) + "\n"
}

func ms(chunks, minX, minY, maxX, maxY int) ManifestSurface {
	return ManifestSurface{Index: 3, Name: "nauvis", Kind: "planet", Chunks: chunks, MinX: minX, MinY: minY, MaxX: maxX, MaxY: maxY}
}

func TestRGB565Expansion(t *testing.T) {
	cases := []struct {
		v       uint16
		r, g, b uint8
	}{
		{0x0000, 0, 0, 0},
		{0xFFFF, 255, 255, 255},
		{0xF800, 255, 0, 0},
		{0x07E0, 0, 255, 0},
		{0x001F, 0, 0, 255},
	}
	for _, c := range cases {
		r, g, b := rgb565(c.v)
		if r != c.r || g != c.g || b != c.b {
			t.Errorf("rgb565(%#04x) = %d,%d,%d, want %d,%d,%d", c.v, r, g, b, c.r, c.g, c.b)
		}
	}
}

func TestDrawChartPlacesChunks(t *testing.T) {
	// Two chunks, one left of and one below the origin chunk's corner.
	red, blue := solid(0xF800), solid(0x001F)
	in := chartLineText(t, -2, -1, red) + chartLineText(t, -1, 0, blue)
	img, s, err := drawChart(ms(2, -2, -1, -1, 0), strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if s.Width != 64 || s.Height != 64 || s.TilesPerPixel != 1 {
		t.Fatalf("surface = %+v, want 64x64 at 1 tile a pixel", s)
	}
	if s.Index != 3 || s.Name != "nauvis" || s.Kind != "planet" {
		t.Fatalf("surface identity = %+v", s)
	}
	b := img.Bounds()
	if b.Dx() != 64 || b.Dy() != 64 {
		t.Fatalf("image is %v", b)
	}
	want := map[[2]int]color.NRGBA{
		{0, 0}:   {255, 0, 0, 255}, // first pixel of the red chunk
		{31, 31}: {255, 0, 0, 255}, // last pixel of the red chunk
		{32, 32}: {0, 0, 255, 255}, // first pixel of the blue chunk
		{63, 63}: {0, 0, 255, 255}, // last pixel of the blue chunk
		{32, 0}:  {0, 0, 0, 0},     // uncharted: transparent
		{0, 32}:  {0, 0, 0, 0},     // uncharted: transparent
	}
	for p, c := range want {
		if got := img.NRGBAAt(p[0], p[1]); got != c {
			t.Errorf("pixel %v = %v, want %v", p, got, c)
		}
	}
}

func TestDrawChartPixelOrderInChunk(t *testing.T) {
	// Row by row: pixel 1 is to the right of pixel 0, pixel 32 is below it.
	raw := make([]byte, chunkDataBytes)
	raw[2], raw[3] = 0xE0, 0x07   // pixel 1: green
	raw[64], raw[65] = 0x1F, 0x00 // pixel 32: blue
	img, _, err := drawChart(ms(1, 0, 0, 0, 0), strings.NewReader(chartLineText(t, 0, 0, raw)))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.NRGBAAt(1, 0); got != (color.NRGBA{0, 255, 0, 255}) {
		t.Errorf("pixel (1,0) = %v, want green", got)
	}
	if got := img.NRGBAAt(0, 1); got != (color.NRGBA{0, 0, 255, 255}) {
		t.Errorf("pixel (0,1) = %v, want blue", got)
	}
}

func TestDrawChartScalesDownPast4096(t *testing.T) {
	// 129 chunks across is 4128 pixels: two tiles a pixel, 2064 pixels.
	// Two neighbouring chunks of different grey average inside one output
	// pixel only when they share it; here they sit in different pixels.
	in := chartLineText(t, 0, 0, solid(0xFFFF)) + chartLineText(t, 128, 0, solid(0xF800))
	img, s, err := drawChart(ms(2, 0, 0, 128, 0), strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if s.TilesPerPixel != 2 || s.Width != 2064 || s.Height != 16 {
		t.Fatalf("surface = %+v, want 2064x16 at 2 tiles a pixel", s)
	}
	if got := img.NRGBAAt(0, 0); got != (color.NRGBA{255, 255, 255, 255}) {
		t.Errorf("pixel (0,0) = %v", got)
	}
	if got := img.NRGBAAt(2063, 15); got != (color.NRGBA{255, 0, 0, 255}) {
		t.Errorf("pixel (2063,15) = %v", got)
	}
	if got := img.NRGBAAt(1000, 0); got.A != 0 {
		t.Errorf("uncharted pixel is %v, want transparent", got)
	}
}

func TestDrawChartScaleAveragesChartedPixels(t *testing.T) {
	// Black and white pixels alternate along each row; at 2 tiles a pixel
	// the average is mid grey. Chunk 128 forces the scale.
	raw := make([]byte, chunkDataBytes)
	for i := 0; i < ChunkPixels*ChunkPixels; i++ {
		if i%2 == 1 {
			raw[2*i], raw[2*i+1] = 0xFF, 0xFF
		}
	}
	in := chartLineText(t, 0, 0, raw) + chartLineText(t, 128, 0, solid(0))
	img, s, err := drawChart(ms(2, 0, 0, 128, 0), strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if s.TilesPerPixel != 2 {
		t.Fatalf("tiles per pixel = %d", s.TilesPerPixel)
	}
	if got := img.NRGBAAt(0, 0); got != (color.NRGBA{127, 127, 127, 255}) {
		t.Errorf("average = %v, want 127 grey", got)
	}
}

func TestDrawChartExactly4096IsNotScaled(t *testing.T) {
	in := chartLineText(t, 0, 0, solid(0xFFFF)) + chartLineText(t, 127, 127, solid(0xFFFF))
	img, s, err := drawChart(ms(2, 0, 0, 127, 127), strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if s.TilesPerPixel != 1 || s.Width != 4096 || s.Height != 4096 || img.Bounds().Dx() != 4096 {
		t.Fatalf("surface = %+v", s)
	}
}

func TestDrawChartHugeSparseBoundsStayBounded(t *testing.T) {
	// Two chunks a million chunks apart: scaled to fit, never 32 million
	// pixels a side.
	in := chartLineText(t, -500000, 0, solid(0xF800)) + chartLineText(t, 500000, 0, solid(0xF800))
	img, s, err := drawChart(ms(2, -500000, 0, 500000, 0), strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() > MaxSidePixels || img.Bounds().Dy() > MaxSidePixels {
		t.Fatalf("picture is %v", img.Bounds())
	}
	if s.TilesPerPixel < 7000 {
		t.Fatalf("tiles per pixel = %d", s.TilesPerPixel)
	}
}

func TestDrawChartEmptySurface(t *testing.T) {
	_, _, err := drawChart(ms(0, 0, 0, 0, 0), strings.NewReader(""))
	if !errors.Is(err, ErrNoChart) {
		t.Fatalf("err = %v, want ErrNoChart", err)
	}
}

func TestDrawChartRefusesBadInput(t *testing.T) {
	good := chartLineText(t, 0, 0, solid(0xF800))
	short := chartLineText(t, 0, 0, solid(0xF800)[:2046])
	long := chartLineText(t, 0, 0, append(solid(0xF800), 0, 0))
	bomb := chartLineText(t, 0, 0, make([]byte, 64<<20)) // inflates far past a chunk
	notZlib := fmt.Sprintf(`{"x":0,"y":0,"data":%q}`, base64.StdEncoding.EncodeToString([]byte("hello world, not zlib"))) + "\n"
	cases := []struct {
		name string
		in   string
		surf ManifestSurface
	}{
		{"chunk too small", short, ms(1, 0, 0, 0, 0)},
		{"chunk too large", long, ms(1, 0, 0, 0, 0)},
		{"inflate bomb", bomb, ms(1, 0, 0, 0, 0)},
		{"not base64", `{"x":0,"y":0,"data":"!!!"}` + "\n", ms(1, 0, 0, 0, 0)},
		{"not zlib", notZlib, ms(1, 0, 0, 0, 0)},
		{"data not a string", `{"x":0,"y":0,"data":5}` + "\n", ms(1, 0, 0, 0, 0)},
		{"unknown field", strings.Replace(good, `"y":0`, `"y":0,"z":1`, 1), ms(1, 0, 0, 0, 0)},
		{"missing data", `{"x":0,"y":0}` + "\n", ms(1, 0, 0, 0, 0)},
		{"missing x", strings.Replace(good, `"x":0,`, "", 1), ms(1, 0, 0, 0, 0)},
		{"fractional x", strings.Replace(good, `"x":0`, `"x":0.5`, 1), ms(1, 0, 0, 0, 0)},
		{"text after object", strings.TrimSuffix(good, "\n") + ` {"x":1}` + "\n", ms(1, 0, 0, 0, 0)},
		{"not json", "garbage\n", ms(1, 0, 0, 0, 0)},
		{"array line", "[]\n", ms(1, 0, 0, 0, 0)},
		{"blank line", "\n" + good, ms(1, 0, 0, 0, 0)},
		{"chunk outside bounds", chartLineText(t, 5, 0, solid(0)), ms(1, 0, 0, 0, 0)},
		{"chunk left of bounds", chartLineText(t, -1, 0, solid(0)), ms(1, 0, 0, 3, 3)},
		{"coordinate beyond range", chartLineText(t, 1<<40, 0, solid(0)), ms(1, 0, 0, 0, 0)},
		{"repeated chunk", good + good, ms(2, 0, 0, 0, 0)},
		{"more chunks than declared", good + chartLineText(t, 1, 0, solid(0)), ms(1, 0, 1, 0, 0)},
		{"fewer chunks than declared", good, ms(2, 0, 0, 1, 0)},
		{"count over the limit", good, ms(MaxChunksPerSurface+1, 0, 0, 0, 0)},
		{"negative count", good, ms(-1, 0, 0, 0, 0)},
		{"reversed bounds", good, ms(1, 1, 0, 0, 0)},
		{"bounds beyond range", good, ms(1, 0, 0, 1<<40, 0)},
		{"line over the limit", `{"x":0,"y":0,"data":"` + strings.Repeat("A", maxChartLine) + `"}` + "\n", ms(1, 0, 0, 0, 0)},
		{"empty file", "", ms(1, 0, 0, 0, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			img, _, err := drawChart(c.surf, strings.NewReader(c.in))
			if err == nil {
				t.Fatalf("accepted, picture %v", img.Bounds())
			}
		})
	}
}

func TestDrawChartNoTrailingNewline(t *testing.T) {
	in := strings.TrimSuffix(chartLineText(t, 0, 0, solid(0xF800)), "\n")
	if _, _, err := drawChart(ms(1, 0, 0, 0, 0), strings.NewReader(in)); err != nil {
		t.Fatal(err)
	}
}

// DrawChart goes through the manifest first. It needs F4's ParseManifest.
func TestDrawChartFindsTheSurface(t *testing.T) {
	manifest := `{"format":1,"game_version":"2.1.24","tick":1,"surfaces":[` +
		`{"index":1,"name":"nauvis","kind":"planet","chunks":1,"min_x":0,"min_y":0,"max_x":0,"max_y":0,"chart":"surface-1.jsonl","entities":"surface-1-entities.jsonl"},` +
		`{"index":4,"name":"platform-1","kind":"platform","chunks":1,"min_x":-1,"min_y":-1,"max_x":-1,"max_y":-1,"chart":"surface-4.jsonl","entities":"surface-4-entities.jsonl"}]}`
	if _, err := ParseManifest(strings.NewReader(manifest)); err != nil && err.Error() == "not built" {
		t.Skip("ParseManifest is task F4's, not merged yet")
	}
	img, s, err := DrawChart(strings.NewReader(manifest), strings.NewReader(chartLineText(t, -1, -1, solid(0x001F))), 4)
	if err != nil {
		t.Fatal(err)
	}
	if s.Index != 4 || s.Name != "platform-1" || s.Kind != "platform" || img.NRGBAAt(0, 0) != (color.NRGBA{0, 0, 255, 255}) {
		t.Fatalf("surface %+v pixel %v", s, img.NRGBAAt(0, 0))
	}
	if _, _, err := DrawChart(strings.NewReader(manifest), strings.NewReader(""), 9); err == nil {
		t.Fatal("drew a surface the manifest does not have")
	}
}
