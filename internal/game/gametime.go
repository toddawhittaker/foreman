package game

import (
	"errors"
	"time"
)

// ParseTimeReply reads the reply of the game's plain /time command, such as
// "276 hours and 40 minutes", as a duration (SPEC 8.15). It refuses any form
// it does not know rather than guess.
func ParseTimeReply(reply string) (time.Duration, error) {
	return 0, errors.New("not built")
}
