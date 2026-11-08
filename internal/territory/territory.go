// Package territory reads Vulcanus's demolisher territories from the JSON
// the game writes for a map preview, works out the overlay drawn over the
// picture, and draws it into a PNG (SPEC 5.6). The JSON comes from a game
// container, so it is checked as untrusted input (SPEC 14.1).
package territory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// The size of a territory, from its demolisher. A territory whose demolisher
// is gone has no size the game can tell, so it is Unknown.
const (
	Small   = "small"
	Medium  = "medium"
	Big     = "big"
	Unknown = "unknown"
)

// Sizes lists every size in the order a list of counts shows them.
var Sizes = []string{Small, Medium, Big, Unknown}

// units maps the game's demolisher names to sizes; "" is a territory with no
// demolisher left.
var units = map[string]string{
	"small-demolisher":  Small,
	"medium-demolisher": Medium,
	"big-demolisher":    Big,
	"":                  Unknown,
}

// Limits on what Parse accepts from the game.
const (
	// MaxJSON is the most bytes Parse reads. The game wrote about 150 KB for
	// a 4096 picture.
	MaxJSON = 2 << 20
	// MaxTerritories is the most territories Parse accepts. A 4096 picture
	// held 142.
	MaxTerritories = 1000
	// MaxPicture is the largest picture size Parse accepts: MaxSize, the
	// largest preview there is (SPEC 5.6).
	MaxPicture = MaxSize
	// MaxSize is the largest preview size territories are offered for, and
	// also the largest preview size there is (game.PreviewSizeMax), kept
	// low to fail safe (SPEC 5.6). At 3584, 2.0 took 32 s on a fast desktop
	// and 2.1 used 88% of the container's 1 GiB, too little room for a slow
	// home server; 3072 used at most 750 MiB (73%) and 27 s. 4096 ran out of
	// memory on 2.1.
	MaxSize = 3072
)

// ChunkTiles is the width of a chunk in tiles, and so in picture pixels.
const ChunkTiles = 32

// Chunk is a chunk's position, in chunks from the map's centre.
type Chunk struct{ X, Y int }

// Territory is one demolisher territory: its size and its chunks inside the
// picture.
type Territory struct {
	Size   string
	Chunks []Chunk
}

// Result is the territories that reach into one picture.
type Result struct {
	Territories []Territory
}

// gameJSON is what the territory script writes.
type gameJSON struct {
	Territories []struct {
		Unit   string  `json:"unit"`
		Chunks [][]int `json:"chunks"`
	} `json:"territories"`
}

// ErrInvalid is JSON from the game that Parse refuses.
var ErrInvalid = errors.New("the game's territory data is not valid")

// Parse checks the game's JSON for a picture of size pixels and returns the
// territories in it. Chunks outside the picture are dropped, and so is a
// territory left with none. It refuses JSON over MaxJSON, more than
// MaxTerritories territories, an unknown demolisher, and a chunk named twice.
func Parse(b []byte, size int) (Result, error) {
	if err := checkSize(size); err != nil {
		return Result{}, err
	}
	if len(b) > MaxJSON {
		return Result{}, fmt.Errorf("%w: more than %d bytes", ErrInvalid, MaxJSON)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var g gameJSON
	if err := dec.Decode(&g); err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Result{}, fmt.Errorf("%w: data after the JSON", ErrInvalid)
	}
	if len(g.Territories) > MaxTerritories {
		return Result{}, fmt.Errorf("%w: more than %d territories", ErrInvalid, MaxTerritories)
	}
	r := chunkRadius(size)
	seen := map[Chunk]bool{}
	var out Result
	for _, t := range g.Territories {
		kind, ok := units[t.Unit]
		if !ok {
			return Result{}, fmt.Errorf("%w: unknown demolisher %q", ErrInvalid, t.Unit)
		}
		var chunks []Chunk
		for _, c := range t.Chunks {
			if len(c) != 2 {
				return Result{}, fmt.Errorf("%w: a chunk is not a pair", ErrInvalid)
			}
			ch := Chunk{c[0], c[1]}
			if seen[ch] {
				return Result{}, fmt.Errorf("%w: chunk %d,%d is named twice", ErrInvalid, ch.X, ch.Y)
			}
			seen[ch] = true
			if inside(ch, r) {
				chunks = append(chunks, ch)
			}
		}
		if len(chunks) > 0 {
			out.Territories = append(out.Territories, Territory{Size: kind, Chunks: chunks})
		}
	}
	return out, nil
}

// checkSize refuses a picture size whose half is not a whole number of
// chunks, so every chunk is either wholly in the picture or out of it.
func checkSize(size int) error {
	if size <= 0 || size > MaxPicture || size%(2*ChunkTiles) != 0 {
		return fmt.Errorf("%w: picture size %d", ErrInvalid, size)
	}
	return nil
}

// chunkRadius is how many chunks lie between the picture's centre and its
// edge. The picture's pixel x is tile x + size/2, so it shows chunks -r to
// r-1 on each axis.
func chunkRadius(size int) int { return size / (2 * ChunkTiles) }

func inside(c Chunk, r int) bool {
	return c.X >= -r && c.X < r && c.Y >= -r && c.Y < r
}

// Label is the word for a size in l's language, as the PNG and the page
// show it.
func Label(l i18n.Locale, size string) string {
	switch size {
	case Small:
		return l.T("mapgen.territory.small")
	case Medium:
		return l.T("mapgen.territory.medium")
	case Big:
		return l.T("mapgen.territory.big")
	}
	return l.T("mapgen.territory.unknown")
}

// ShortLabel is the size as the small picture shows it, so it fits there
// (DESIGN 8.8).
func ShortLabel(l i18n.Locale, size string) string {
	switch size {
	case Small:
		return l.T("mapgen.territory.short.small")
	case Medium:
		return l.T("mapgen.territory.short.medium")
	case Big:
		return l.T("mapgen.territory.short.big")
	}
	return l.T("mapgen.territory.short.unknown")
}
