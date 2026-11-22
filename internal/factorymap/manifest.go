package factorymap

import (
	"errors"
	"io"
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

// ParseManifest reads the manifest under the limits of SPEC 14.6. Task F4
// builds it.
func ParseManifest(r io.Reader) (Manifest, error) {
	return Manifest{}, errors.New("not built")
}
