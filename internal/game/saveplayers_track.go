package game

import (
	"errors"

	"github.com/toddawhittaker/foreman/internal/saveplayers"
)

// recordState is the players records' mutexes, one per world, and the last
// /time reading. Task A5 fills it.
type recordState struct{} //nolint:unused // built by A5

// updatePlayersRecord reads the world's players record, calls f on it, and
// writes it back, under one mutex per world (SPEC 8.14). Task A5 builds it.
func (c *Controller) updatePlayersRecord(id string, f func(*saveplayers.Record) error) error { //nolint:unused // built by A5
	return errors.New("not built")
}
