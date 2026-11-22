package factorymap

import (
	"errors"
	"image"
	"os"
)

// Store is a world's kept pictures, in factorymaps/<world-id>/ under the data
// directory: never in a world or a snapshot, one drawing per world (SPEC
// 5.16). Task F4 builds it.
type Store struct{}

// Open opens the world's store, which may be empty.
func Open(dataDir, id string) (*Store, error) { return nil, errors.New("not built") }

// Replace keeps d and its PNGs, by file name, in place of the previous
// drawing, so a reader sees the old drawing or the new one, never a mix.
func (s *Store) Replace(d Drawing, pngs map[string][]byte) error { return errors.New("not built") }

// Read returns the kept drawing's facts; ok is false when there is none.
func (s *Store) Read() (d Drawing, ok bool, err error) {
	return Drawing{}, false, errors.New("not built")
}

// OpenPNG opens one picture of a surface: the base picture when layer is "",
// else the layer's. The caller closes it.
func (s *Store) OpenPNG(surface int, layer Category) (*os.File, error) {
	return nil, errors.New("not built")
}

// Composite returns the base picture of a surface with the layers stacked,
// for the combined download.
func (s *Store) Composite(surface int, layers []Category) (image.Image, error) {
	return nil, errors.New("not built")
}

// Remove deletes a world's kept pictures; none is not an error.
func Remove(dataDir, id string) error { return errors.New("not built") }
