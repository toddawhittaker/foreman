package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// MapGenForm is the map-gen form shown inside Generate (SPEC 5.6, 11.5).
// This is a placeholder with the shape Generate builds on; the full form
// replaces this file.
type MapGenForm struct{}

// NewMapGenForm is the form filled with the game line's defaults.
func NewMapGenForm(line string, spaceAge bool) MapGenForm { return MapGenForm{} }

// ParseMapGenForm reads the form's fields into the map-gen-settings.json to
// write, or the problems to show.
func ParseMapGenForm(r *http.Request, line string, spaceAge bool) (MapGenForm, []byte, []Problem) {
	return MapGenForm{}, mapgen.DefaultMapGen(line), nil
}
