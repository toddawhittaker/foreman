package fakegame

import (
	"errors"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

// fakeSavePlayers is a save read for the players of a save: it writes
// gameenv.SavePlayersFile to the work folder as the helper mod does (SPEC
// 8.16). Task A8 builds it.
func fakeSavePlayers(c dockertest.Container) error {
	return errors.New("the fake game's save players run is not built")
}
