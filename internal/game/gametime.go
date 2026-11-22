package game

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrTimeReply is returned for a /time reply that is not in the form the
// game prints.
var ErrTimeReply = errors.New("unrecognised /time reply")

// The game prints the play time as a list of parts in the order hours,
// minutes, seconds, each at most once, with zero parts left out (SPEC 8.15).
// The parts are joined with ", " and the last with " and", as in "1 hour,
// 1 minute and 10 seconds"; a count of one takes the singular. The hour count
// is capped at seven digits so the duration cannot overflow.
var timePart = regexp.MustCompile(`^([0-9]{1,7}) (hour|minute|second)(s?)$`)

// timeUnits is the order the parts must come in.
var timeUnits = []string{"hour", "minute", "second"}

// ParseTimeReply reads the reply of the game's plain /time command, such as
// "276 hours and 40 minutes", as a duration (SPEC 8.15). It refuses any form
// it does not know rather than guess.
func ParseTimeReply(reply string) (time.Duration, error) {
	text := strings.TrimSpace(reply)
	if strings.Count(text, " and ") > 1 {
		return 0, ErrTimeReply
	}
	parts := []string{text}
	if head, last, found := strings.Cut(text, " and "); found {
		parts = append(strings.Split(head, ", "), last)
	}
	var total time.Duration
	next := 0 // the earliest unit still allowed, as an index into timeUnits
	for _, part := range parts {
		m := timePart.FindStringSubmatch(part)
		if m == nil {
			return 0, ErrTimeReply
		}
		value, err := strconv.Atoi(m[1])
		if err != nil {
			return 0, ErrTimeReply
		}
		unit := indexFrom(timeUnits, m[2], next)
		if unit < 0 {
			return 0, ErrTimeReply // out of order, or repeated
		}
		if (value == 1) != (m[3] == "") {
			return 0, ErrTimeReply // singular for one, plural otherwise
		}
		switch unit {
		case 0:
			total += time.Duration(value) * time.Hour
		case 1:
			if value >= 60 {
				return 0, ErrTimeReply
			}
			total += time.Duration(value) * time.Minute
		case 2:
			if value >= 60 {
				return 0, ErrTimeReply
			}
			total += time.Duration(value) * time.Second
		}
		next = unit + 1
	}
	return total, nil
}

// indexFrom returns the index of word in units at or after from, or -1.
func indexFrom(units []string, word string, from int) int {
	for i := from; i < len(units); i++ {
		if units[i] == word {
			return i
		}
	}
	return -1
}
