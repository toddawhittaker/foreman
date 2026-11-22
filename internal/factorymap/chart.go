package factorymap

import (
	"errors"
	"image"
	"io"
)

// DrawChart draws one surface's picture from the game's own chart: manifest
// is the helper's manifest.json, chunks is the surface's chart file (one
// {"x","y","data"} line per charted chunk), and surface is the manifest's
// index of the surface to draw. A picture over MaxSidePixels a side is scaled
// down, and Surface says by how much (SPEC 5.16). Task F2 builds it.
func DrawChart(manifest, chunks io.Reader, surface int) (*image.NRGBA, Surface, error) {
	return nil, Surface{}, errors.New("not built")
}
