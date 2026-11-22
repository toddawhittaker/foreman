package game

import (
	"context"
	"errors"
	"time"
)

// The "players in this save" the world page shows (SPEC 8.14). Task A4 builds
// the reading, A5 the tracking; the types below are the contract.

// The states of a SavePlayersView.
const (
	SavePlayersReady        = "ready"         // a record matches the save that loads
	SavePlayersReading      = "reading"       // a read is running
	SavePlayersTrackedOnly  = "tracked-only"  // a read failed; see Failure
	SavePlayersNone         = "none"          // no save to read
	SavePlayersNotSupported = "not-supported" // the world's game cannot be read
)

// seedState is the reads of players records in flight and the bases whose read
// failed, by world id. Task A4 fills it.
type seedState struct{} //nolint:unused // built by A4

// SavePlayersView is what the web layer shows of a world's players record.
type SavePlayersView struct {
	// State is one of the SavePlayers* constants.
	State string
	// Failure is the failure id when State is "tracked-only".
	Failure      string
	Save         string
	TrackedSince time.Time
	// GameTime is the save's total play time, true when GameTimeKnown.
	GameTime      time.Duration
	GameTimeKnown bool
	Players       []SavePlayer
}

// SavePlayer is one player of the save.
type SavePlayer struct {
	Name     string
	PlayTime time.Duration
	Online   bool
	LastSeen time.Time
	// BeforeForeman is true when the play time is from before Foreman
	// tracked this world.
	BeforeForeman bool
}

// SavePlayers returns the view and starts a read if one is due and allowed
// (SPEC 8.16). It never waits for a run: a read's result arrives as an
// UpdateSavePlayers.
func (c *Controller) SavePlayers(ctx context.Context, id string) (SavePlayersView, error) {
	return SavePlayersView{}, errors.New("not built")
}

// ReadSavePlayersAgain reads the newest save again, on an admin's click. It
// is allowed beside a running game (SPEC 8.16).
func (c *Controller) ReadSavePlayersAgain(ctx context.Context, id string, by Actor) error {
	return errors.New("not built")
}
