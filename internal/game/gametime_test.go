package game

import (
	"testing"
	"time"
)

// SPEC 8.15: the reply of plain /time is read as a duration. Only the forms
// the game prints are accepted; anything else fails safe.

// The real replies, from a 2.0.77 server (feasibility.md section 5 and the
// fix review). They are facts, so each must parse to the duration it names.
func TestParseTimeReplyReadsTheRealReplies(t *testing.T) {
	cases := []struct {
		reply string
		want  time.Duration
	}{
		{"8 seconds", 8 * time.Second},
		{"1 minute and 10 seconds", time.Minute + 10*time.Second},
		{"2 minutes", 2 * time.Minute},
		{"5 minutes and 1 second", 5*time.Minute + time.Second},
		{"1 hour and 13 seconds", time.Hour + 13*time.Second},
		{"1 hour, 1 minute and 10 seconds", time.Hour + time.Minute + 10*time.Second},
		{"1 hour and 10 minutes", time.Hour + 10*time.Minute},
		{"2 hours and 9 seconds", 2*time.Hour + 9*time.Second},
		{"2 hours, 44 minutes and 1 second", 2*time.Hour + 44*time.Minute + time.Second},
		{"3 hours, 36 minutes and 58 seconds", 3*time.Hour + 36*time.Minute + 58*time.Second},
		{"276 hours and 40 minutes", 276*time.Hour + 40*time.Minute},
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

// Surrounding whitespace is not part of the reply and is trimmed.
func TestParseTimeReplyTrimsWhitespace(t *testing.T) {
	for _, reply := range []string{"38 hours and 40 minutes\n", "  38 hours and 40 minutes  "} {
		got, err := ParseTimeReply(reply)
		if err != nil || got != 38*time.Hour+40*time.Minute {
			t.Errorf("ParseTimeReply(%q) = %v, %v; want 38h40m", reply, got, err)
		}
	}
}

// Any reply not in the game's form is refused: wrong order, repeated parts,
// "and" in the wrong place, bad joiners, bad plurals, out-of-range values,
// other units, other languages, and text that is not a /time reply.
func TestParseTimeReplyRefusesAnythingElse(t *testing.T) {
	for _, reply := range []string{
		"",
		"   ",
		"garbage",
		"276 hours and",
		"276 hours and 40 minutes and 3 seconds", // "and" twice
		"1 minute and 1 hour",                    // wrong order
		"10 seconds and 1 minute",                // wrong order
		"1 hour and 2 hours",                     // repeated unit
		"1 hour, 2 hours and 3 seconds",          // repeated unit
		"1 hour, 1 minute, 10 seconds",           // no "and" before the last part
		"1 hour and 1 minute, 10 seconds",        // wrong joiner before the last part
		"1 hour,  1 minute and 10 seconds",       // doubled space
		"1 hour and 1 minute and 10 seconds",     // "and" twice
		"and 1 hour",                             // "and" with nothing before it
		"1 hour and",                             // nothing after the "and"
		"1 hours and 1 minute",                   // plural on 1
		"2 hour and 1 minute",                    // singular on 2
		"5 minutes and 1 seconds",                // plural on 1
		"2 hours 40 minutes",                     // no "and"
		"276 hours and 40 minutes, 3 seconds",    // joiner before the last part
		"12 hours and 75 minutes",                // minutes over 59
		"12 hours and 60 minutes",                // minutes over 59
		"1 minute and 75 seconds",                // seconds over 59
		"90 minutes",                             // minutes over 59 alone
		"-1 minutes",
		"+5 minutes",
		"1.5 hours and 2 minutes",
		"5 Minutes",
		"5 MINUTES",
		"5 min",
		"5 minutes ago",
		"276 heures et 40 minutes",
		"٣ minutes",
		"59760005 ticks",
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
