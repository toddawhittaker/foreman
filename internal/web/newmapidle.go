package web

import (
	"errors"

	"github.com/toddawhittaker/foreman/internal/game"
)

// errNewMapServerUp refuses saving map settings while the world's server is up.
var errNewMapServerUp = errors.New("this world's server is running or starting, so its map settings cannot be changed now; stop it first")

// newMapIdle is the check that a world's server is down before its map
// settings are saved (SPEC 5.6). It refuses a game that is starting, running,
// stopping, crashed (a restart is coming) or crash-looping. Pass it as
// world.NewMapFiles.Idle, so it runs under the world's lock.
func (s *server) newMapIdle(id string) error {
	st := s.game.Status(id).State
	if st == game.StateStarting || st == game.StateStopping || st == game.StateCrashLooping || game.IsRunning(st) {
		return errNewMapServerUp
	}
	return nil
}
