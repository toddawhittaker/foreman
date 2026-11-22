package factorymap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// ManifestFormat is the helper mod's output format this Foreman reads.
const ManifestFormat = 1

// Limits of the helper mod's output (SPEC 14.6).
const (
	MaxOutputBytes      = 512 << 20
	MaxSurfaces         = 300
	MaxChunksPerSurface = 65536
	MaxEntities         = 2_000_000
	// ChunkPixels is the side, in pixels, of each decoded chart chunk.
	ChunkPixels = 32
	// MaxSidePixels is the most pixels a side of a picture has.
	MaxSidePixels = 4096
)

// Manifest is factorymap/manifest.json, which the helper mod writes into the
// work folder.
type Manifest struct {
	Format      int               `json:"format"`
	GameVersion string            `json:"game_version"`
	Tick        int64             `json:"tick"`
	Surfaces    []ManifestSurface `json:"surfaces"`
}

// ManifestSurface is one surface of the manifest.
type ManifestSurface struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Chunks int    `json:"chunks"`
	MinX   int    `json:"min_x"`
	MinY   int    `json:"min_y"`
	MaxX   int    `json:"max_x"`
	MaxY   int    `json:"max_y"`
	// Chart and Entities name the surface's two files in the work folder.
	Chart             string `json:"chart"`
	Entities          string `json:"entities"`
	EntitiesTotal     int    `json:"entities_total"`
	EntitiesTruncated bool   `json:"entities_truncated"`
}

// maxManifestBytes is the most a manifest may hold: 300 surfaces take about
// 100 KiB, so 1 MiB leaves room and still stops a hostile file early.
const maxManifestBytes = 1 << 20

// Bounds on the manifest's numbers. SPEC 14.6 says every number is a whole
// number from 0 to 2^53; chunk bounds are the one exception, since chunk
// coordinates are negative west and north of the start. They are whole
// numbers within +/- maxChunkCoord.
const (
	maxWholeNumber = 1 << 53
	maxChunkCoord  = 1 << 20
	maxNameBytes   = 200
	maxVersionLen  = 32
)

// ErrManifest wraps every refusal of the manifest.
var ErrManifest = errors.New("factory map manifest refused")

func refuse(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrManifest, fmt.Sprintf(format, a...))
}

// ChartFile is the file name of surface i's chart in the work folder. The
// manifest must name exactly this, so a name never leaves the folder.
func ChartFile(i int) string { return "surface-" + strconv.Itoa(i) + ".jsonl" }

// EntitiesFile is the file name of surface i's building list.
func EntitiesFile(i int) string { return "surface-" + strconv.Itoa(i) + "-entities.jsonl" }

// ParseManifest reads the manifest under the limits of SPEC 14.6: at most 1
// MiB, format 1, unknown fields refused, at most MaxSurfaces surfaces with
// distinct indexes and the file names ChartFile and EntitiesFile give, whole
// numbers only, and counts that fit the surface's bounds and the limits. The
// helper mod's output is untrusted, so every field is checked.
func ParseManifest(r io.Reader) (Manifest, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return Manifest{}, err
	}
	if len(data) > maxManifestBytes {
		return Manifest{}, refuse("larger than %d bytes", maxManifestBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, refuse("%v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Manifest{}, refuse("data after the manifest")
	}
	if err := m.check(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func (m Manifest) check() error {
	if m.Format != ManifestFormat {
		return refuse("format %d, want %d", m.Format, ManifestFormat)
	}
	if !plainText(m.GameVersion, maxVersionLen) {
		return refuse("bad game version")
	}
	if m.Tick < 0 || m.Tick > maxWholeNumber {
		return refuse("tick out of range")
	}
	if len(m.Surfaces) > MaxSurfaces {
		return refuse("%d surfaces, at most %d", len(m.Surfaces), MaxSurfaces)
	}
	seen := make(map[int]bool, len(m.Surfaces))
	entities := 0
	for _, s := range m.Surfaces {
		if err := s.check(); err != nil {
			return err
		}
		if seen[s.Index] {
			return refuse("surface %d listed twice", s.Index)
		}
		seen[s.Index] = true
		entities += s.EntitiesTotal
		if entities > MaxEntities {
			return refuse("more than %d buildings in all", MaxEntities)
		}
	}
	return nil
}

func (s ManifestSurface) check() error {
	if s.Index < 0 || s.Index > maxWholeNumber {
		return refuse("surface index out of range")
	}
	if !plainText(s.Name, maxNameBytes) {
		return refuse("surface %d has a bad name", s.Index)
	}
	switch s.Kind {
	case "planet", "platform", "surface":
	default:
		return refuse("surface %d has kind %q", s.Index, s.Kind)
	}
	if s.Chunks < 0 || s.Chunks > MaxChunksPerSurface {
		return refuse("surface %d has %d chunks, at most %d", s.Index, s.Chunks, MaxChunksPerSurface)
	}
	for _, v := range []int{s.MinX, s.MinY, s.MaxX, s.MaxY} {
		if v < -maxChunkCoord || v > maxChunkCoord {
			return refuse("surface %d has bounds out of range", s.Index)
		}
	}
	if s.MinX > s.MaxX || s.MinY > s.MaxY {
		return refuse("surface %d has inverted bounds", s.Index)
	}
	area := int64(s.MaxX-s.MinX+1) * int64(s.MaxY-s.MinY+1)
	if int64(s.Chunks) > area {
		return refuse("surface %d has more chunks than its bounds hold", s.Index)
	}
	if s.EntitiesTotal < 0 || s.EntitiesTotal > MaxEntities {
		return refuse("surface %d has %d buildings, at most %d", s.Index, s.EntitiesTotal, MaxEntities)
	}
	if s.Chart != ChartFile(s.Index) || s.Entities != EntitiesFile(s.Index) {
		return refuse("surface %d names a file the helper does not write", s.Index)
	}
	return nil
}

// plainText reports whether s is 1 to max bytes of valid UTF-8 with no
// control characters. Surface names and versions are untrusted text.
func plainText(s string, max int) bool {
	if s == "" || len(s) > max || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return false
		}
	}
	return true
}
