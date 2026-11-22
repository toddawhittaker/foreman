package factorymap

import (
	"errors"
	"image"
	"io"
)

// DrawOverlay draws one transparent layer per category from the surface's
// entity file (its first line the entity types, each later line
// [type_index, x1, y1, x2, y2]), on the pixel grid of s (SPEC 5.16). Task F3
// builds it.
func DrawOverlay(entities io.Reader, s Surface) (map[Category]*image.Paletted, error) {
	return nil, errors.New("not built")
}
