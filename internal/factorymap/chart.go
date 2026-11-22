package factorymap

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
)

// ErrNoChart is returned for a surface that has no charted chunk, so there is
// nothing to draw.
var ErrNoChart = errors.New("factorymap: surface has no charted chunk")

const (
	// chunkDataBytes is a decoded chunk: 32 by 32 pixels of two bytes each
	// (RGB565, little endian), row by row.
	chunkDataBytes = ChunkPixels * ChunkPixels * 2
	// maxChartLine bounds one line of the chart file. A chunk's data is at most
	// about 2.2 KB compressed, so a line is well under 4 KB.
	maxChartLine = 16 << 10
)

// chartLine is one line of the chart file.
type chartLine struct {
	X    *int64  `json:"x"`
	Y    *int64  `json:"y"`
	Data *string `json:"data"`
}

// DrawChart draws one surface's picture from the game's own chart: manifest
// is the helper's manifest.json, chunks is the surface's chart file (one
// {"x","y","data"} line per charted chunk), and surface is the manifest's
// index of the surface to draw. A picture over MaxSidePixels a side is scaled
// down, and Surface says by how much (SPEC 5.16, 14.6). The output is
// untrusted: every limit is checked while reading, and nothing the file
// claims decides how much memory is used.
func DrawChart(manifest, chunks io.Reader, surface int) (*image.NRGBA, Surface, error) {
	m, err := ParseManifest(manifest)
	if err != nil {
		return nil, Surface{}, err
	}
	for _, ms := range m.Surfaces {
		if ms.Index == surface {
			return drawChart(ms, chunks)
		}
	}
	return nil, Surface{}, fmt.Errorf("factorymap: manifest has no surface %d", surface)
}

// drawChart reads the chart file of one manifest surface. Any chunk outside
// the surface's bounds, repeated, of the wrong size, or not matching the
// declared count refuses the whole chart.
func drawChart(ms ManifestSurface, chunks io.Reader) (*image.NRGBA, Surface, error) {
	if ms.Chunks < 0 || ms.Chunks > MaxChunksPerSurface {
		return nil, Surface{}, fmt.Errorf("factorymap: surface %d declares %d chunks", ms.Index, ms.Chunks)
	}
	if ms.Chunks == 0 {
		return nil, Surface{}, ErrNoChart
	}
	for _, v := range []int{ms.MinX, ms.MinY, ms.MaxX, ms.MaxY} {
		if v < -maxChunkCoord || v > maxChunkCoord {
			return nil, Surface{}, fmt.Errorf("factorymap: surface %d bounds out of range", ms.Index)
		}
	}
	if ms.MinX > ms.MaxX || ms.MinY > ms.MaxY {
		return nil, Surface{}, fmt.Errorf("factorymap: surface %d bounds are reversed", ms.Index)
	}
	wTiles := (ms.MaxX - ms.MinX + 1) * ChunkPixels
	hTiles := (ms.MaxY - ms.MinY + 1) * ChunkPixels
	k := (max(wTiles, hTiles) + MaxSidePixels - 1) / MaxSidePixels
	w, h := (wTiles+k-1)/k, (hTiles+k-1)/k

	// The only large allocation is the picture itself. When it is scaled
	// down, a picture pixel keeps the colour of the first charted tile that
	// lands in it (point sampling): no sum buffers, and a charted chunk never
	// vanishes, however far apart the chunks are.
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	dec := newChunkDecoder()

	// The declared chunk count and the line limit already bound the read; the
	// size limit is a second wall.
	sc := bufio.NewScanner(io.LimitReader(chunks, MaxOutputBytes))
	sc.Buffer(make([]byte, 0, 4096), maxChartLine)
	seen := make(map[[2]int]struct{}, ms.Chunks)
	var read int
	for sc.Scan() {
		line := sc.Bytes()
		read++
		if read > ms.Chunks {
			return nil, Surface{}, fmt.Errorf("factorymap: chart has more chunks than the %d declared", ms.Chunks)
		}
		cx, cy, px, err := dec.parse(line)
		if err != nil {
			return nil, Surface{}, fmt.Errorf("factorymap: chart line %d: %w", read, err)
		}
		if cx < ms.MinX || cx > ms.MaxX || cy < ms.MinY || cy > ms.MaxY {
			return nil, Surface{}, fmt.Errorf("factorymap: chart line %d: chunk (%d, %d) is outside the surface's bounds", read, cx, cy)
		}
		key := [2]int{cx, cy}
		if _, dup := seen[key]; dup {
			return nil, Surface{}, fmt.Errorf("factorymap: chart line %d: chunk (%d, %d) is repeated", read, cx, cy)
		}
		seen[key] = struct{}{}
		ox, oy := (cx-ms.MinX)*ChunkPixels, (cy-ms.MinY)*ChunkPixels
		for i := 0; i < ChunkPixels*ChunkPixels; i++ {
			v := uint16(px[2*i]) | uint16(px[2*i+1])<<8
			r, g, b := rgb565(v)
			x, y := ox+i%ChunkPixels, oy+i/ChunkPixels
			o := img.PixOffset(x/k, y/k)
			if img.Pix[o+3] == 0 {
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = r, g, b, 255
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, Surface{}, fmt.Errorf("factorymap: reading chart: %w", err)
	}
	if read != ms.Chunks {
		return nil, Surface{}, fmt.Errorf("factorymap: chart has %d chunks, the manifest declares %d", read, ms.Chunks)
	}
	return img, Surface{
		Index: ms.Index, Name: ms.Name, Kind: ms.Kind,
		Width: w, Height: h, TilesPerPixel: k,
	}, nil
}

// chunkDecoder reads chart lines and reuses its buffers between them, so a
// chart of 65,536 chunks does not allocate 65,536 sets of buffers.
type chunkDecoder struct {
	rd  bytes.Reader
	raw []byte // base64-decoded data
	px  []byte // inflated data, one byte more than a chunk to catch excess
	zr  io.ReadCloser
}

func newChunkDecoder() *chunkDecoder {
	return &chunkDecoder{px: make([]byte, chunkDataBytes+1)}
}

// parse reads one {"x","y","data"} line into the chunk's coordinates and its
// 2048 bytes of pixels, which stay valid until the next call. Unknown or
// missing fields, text after the object, and data of any other size are
// errors.
func (d *chunkDecoder) parse(line []byte) (x, y int, px []byte, err error) {
	d.rd.Reset(line)
	dec := json.NewDecoder(&d.rd)
	dec.DisallowUnknownFields()
	var l chartLine
	if err := dec.Decode(&l); err != nil {
		return 0, 0, nil, fmt.Errorf("not a chunk: %w", err)
	}
	var extra json.RawMessage
	if dec.Decode(&extra) != io.EOF {
		return 0, 0, nil, errors.New("text after the chunk")
	}
	if l.X == nil || l.Y == nil || l.Data == nil {
		return 0, 0, nil, errors.New("a chunk needs x, y and data")
	}
	if *l.X < -maxChunkCoord || *l.X > maxChunkCoord || *l.Y < -maxChunkCoord || *l.Y > maxChunkCoord {
		return 0, 0, nil, errors.New("chunk coordinate out of range")
	}
	if n := base64.StdEncoding.DecodedLen(len(*l.Data)); cap(d.raw) < n {
		d.raw = make([]byte, n)
	}
	n, err := base64.StdEncoding.Decode(d.raw[:cap(d.raw)], []byte(*l.Data))
	if err != nil {
		return 0, 0, nil, fmt.Errorf("data is not base64: %w", err)
	}
	d.rd.Reset(d.raw[:n])
	if d.zr == nil {
		d.zr, err = zlib.NewReader(&d.rd)
	} else {
		err = d.zr.(zlib.Resetter).Reset(&d.rd, nil)
	}
	if err != nil {
		return 0, 0, nil, fmt.Errorf("data is not compressed: %w", err)
	}
	sizeErr := fmt.Errorf("a chunk must be %d by %d pixels (%d bytes)", ChunkPixels, ChunkPixels, chunkDataBytes)
	if _, err := io.ReadFull(d.zr, d.px[:chunkDataBytes]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return 0, 0, nil, sizeErr
		}
		return 0, 0, nil, fmt.Errorf("data is not compressed: %w", err)
	}
	// The stream must end here: one more byte means too much data, and a
	// stream cut short or with a bad checksum is an error, not an end.
	if n, err := d.zr.Read(d.px[chunkDataBytes:]); n != 0 {
		return 0, 0, nil, sizeErr
	} else if !errors.Is(err, io.EOF) {
		return 0, 0, nil, fmt.Errorf("data is not compressed: %w", err)
	}
	return int(*l.X), int(*l.Y), d.px[:chunkDataBytes], nil
}

// rgb565 expands a 16-bit 5-6-5 pixel to 8 bits a channel.
func rgb565(v uint16) (r, g, b uint8) {
	r5, g6, b5 := uint8(v>>11&31), uint8(v>>5&63), uint8(v&31)
	return r5<<3 | r5>>2, g6<<2 | g6>>4, b5<<3 | b5>>2
}
