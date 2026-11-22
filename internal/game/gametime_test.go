package game

import (
	"testing"
	"time"
)

// SPEC 8.15: the reply of plain /time is read as a duration. Only the
// English forms the game prints are accepted; anything else fails safe.
func TestParseTimeReplyReadsTheGamesForms(t *testing.T) {
	cases := []struct {
		reply string
		want  time.Duration
	}{
		{"276 hours and 40 minutes", 276*time.Hour + 40*time.Minute},
		{"38 hours and 40 minutes", 38*time.Hour + 40*time.Minute},
		{"1 hour and 1 minute", time.Hour + time.Minute},
		{"1 hour and 0 minutes", time.Hour},
		{"2 hours and 1 minute", 2*time.Hour + time.Minute},
		{"5 minutes", 5 * time.Minute},
		{"1 minute", time.Minute},
		{"0 minutes", 0},
		{"276 hours and 40 minutes\n", 276*time.Hour + 40*time.Minute},
		{"  38 hours and 40 minutes  ", 38*time.Hour + 40*time.Minute},
	}
	for _, c := range cases {
		got, err := ParseTimeReply(c.reply)
		if err != nil {
			t.Errorf("ParseTimeReply(%q) error %v, want %v", c.reply, err, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("ParseTimeReply(%q) = %v, want %v", c.reply, got, c.want)
		}
	}
}

// Any other reply, including a reply the game might print in another form,
// is refused rather than guessed at.
func TestParseTimeReplyRefusesAnythingElse(t *testing.T) {
	for _, reply := range []string{
		"",
		"   ",
		"garbage",
		"276 hours",
		"276 hours and",
		"59760005 ticks",
		"276 hours and 40 minutes and 3 seconds",
		"-1 minutes",
		"+5 minutes",
		"1.5 hours and 2 minutes",
		"12 hours and 75 minutes",
		"12 hours and 60 minutes",
		"90 minutes",
		"5 Minutes",
		"5 MINUTES",
		"5 min",
		"5 minutes ago",
		"276 heures et 40 minutes",
		"٣ minutes",
		"99999999999999999999 hours and 0 minutes",
		"99999999999999999999 minutes",
		"276 hours and 40 minutes\n\n5 minutes",
		"Prints info about how old the map is",
	} {
		if got, err := ParseTimeReply(reply); err == nil {
			t.Errorf("ParseTimeReply(%q) = %v, want an error", reply, got)
		}
	}
}
