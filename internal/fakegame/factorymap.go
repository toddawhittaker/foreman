package fakegame

import (
	"errors"

	"github.com/toddawhittaker/foreman/internal/docker/dockertest"
)

// fakeFactoryMap is a save read for the factory map: it writes the
// gameenv.FactoryMapDir folder to the work folder as the helper mod does (SPEC
// 5.16). Task F9 builds it.
func fakeFactoryMap(c dockertest.Container) error {
	return errors.New("the fake game's factory map run is not built")
}
