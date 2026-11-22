// Package factorymap draws a world's factory map from what the helper mod
// read out of a copy of its save: one picture per surface from the game's
// own chart, and a transparent layer per category of building (SPEC 5.16,
// 14.6). It is pure: it parses untrusted output under limits and keeps the
// pictures on disk, and it never runs a container.
package factorymap

import (
	"time"

	"github.com/toddawhittaker/foreman/internal/saveplayers"
)

// Category is a kind of building, from the engine's prototype type.
type Category string

// The categories, in the order the page lists them.
const (
	CategoryBelts      Category = "belts"
	CategoryProduction Category = "production"
	CategoryPower      Category = "power"
	CategoryFluids     Category = "fluids"
	CategoryRail       Category = "rail"
	CategoryDefence    Category = "defence"
	CategoryLogistics  Category = "logistics"
	CategoryOther      Category = "other"
)

// Categories are every category, in a fixed order: the page's order.
var Categories = []Category{
	CategoryBelts, CategoryProduction, CategoryPower, CategoryFluids,
	CategoryRail, CategoryDefence, CategoryLogistics, CategoryOther,
}

// Surface is one drawn surface: a planet, a space platform or a modded
// surface.
type Surface struct {
	Index int
	// Name is the game's name for the surface; it is untrusted text.
	Name string
	// Kind is "planet", "platform" or "surface".
	Kind          string
	Width, Height int
	// TilesPerPixel is 1 unless the charted area was scaled down to fit the
	// 4096-pixel limit (SPEC 5.16).
	TilesPerPixel int
}

// Drawing is the facts of the kept drawing of a world.
type Drawing struct {
	// Basis is the save the picture was drawn from.
	Basis       saveplayers.Basis
	DrawnAt     time.Time
	GameVersion string
	Surfaces    []Surface
	// Failure is the failure id when the last drawing failed; the previous
	// picture, if any, stays.
	Failure string
}
