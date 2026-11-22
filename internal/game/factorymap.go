package game

import (
	"context"
	"errors"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The factory map (SPEC 5.16). Task F5 builds the runs and the loop; the
// types below are the contract.

// factoryMapState is the factory map runs in flight and the loop's memory, by
// world id. Task F5 fills it.
type factoryMapState struct{} //nolint:unused // built by F5

// FactoryMapView is what the map page shows of a world's factory map.
type FactoryMapView struct {
	// State and Failure say what the map is doing and why a drawing failed.
	State, Failure string
	// Mode is the world's setting.
	Mode world.FactoryMapMode
	// Drawing is the kept picture's facts; its DrawnAt is zero when none.
	Drawing factorymap.Drawing
	// Running is whether a game runs on this Foreman, and PlayersOnline
	// whether this world's game has players (SPEC 5.16, rule 20).
	Running, PlayersOnline bool
}

// FactoryMap returns the view. It never starts a run.
func (c *Controller) FactoryMap(id string) (FactoryMapView, error) {
	return FactoryMapView{}, errors.New("not built")
}

// DrawFactoryMap draws the newest save's factory map now. Beside a running
// game it refuses while players are online when the world says so (SPEC
// 5.16).
func (c *Controller) DrawFactoryMap(ctx context.Context, id string) error {
	return errors.New("not built")
}

// FactoryMapStore opens the world's kept pictures, for the map page to serve.
// It is the web layer's way to the factorymap.Store.
func (c *Controller) FactoryMapStore(id string) (*factorymap.Store, error) {
	return nil, errors.New("not built")
}
