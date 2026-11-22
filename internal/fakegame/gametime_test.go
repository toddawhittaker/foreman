package fakegame

import (
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
)

// The fake's /time reply must read back, through the parser the game uses
// (SPEC 8.15), as the duration it was printed from.
func TestPlayTimeRoundTripsThroughTheParser(t *testing.T) {
	for _, d := range []time.Duration{
		0,
		time.Second,
		59 * time.Second,
		time.Minute,
		time.Hour,
		time.Hour + time.Minute + time.Second,
		276*time.Hour + 40*time.Minute,
	} {
		reply := formatPlayTime(int(d / time.Second))
		got, err := game.ParseTimeReply(reply)
		if err != nil {
			t.Errorf("reply %q for %v: ParseTimeReply error %v", strings.TrimSpace(reply), d, err)
			continue
		}
		if got != d {
			t.Errorf("reply %q for %v: parsed as %v", strings.TrimSpace(reply), d, got)
		}
	}
}
