package game

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ErrTimeReply is returned for a /time reply that is not one of the forms
// the game prints.
var ErrTimeReply = errors.New("unrecognised /time reply")

// The forms the game prints (SPEC 8.15): "H hours and M minutes", with the
// singular where the count is one, and "M minutes" under an hour. Digits are
// ASCII; the hour count is capped so the duration cannot overflow.
var (
	timeHoursAndMinutes = regexp.MustCompile(`^([0-9]{1,7}) hours? and ([0-9]{1,2}) minutes?$`)
	timeMinutesOnly     = regexp.MustCompile(`^([0-9]{1,2}) minutes?$`)
)

// ParseTimeReply reads the reply of the game's plain /time command, such as
// "276 hours and 40 minutes", as a duration (SPEC 8.15). It refuses any form
// it does not know rather than guess.
func ParseTimeReply(reply string) (time.Duration, error) {
	text := strings.TrimSpace(reply)
	if m := timeHoursAndMinutes.FindStringSubmatch(text); m != nil {
		hours, errH := strconv.Atoi(m[1])
		minutes, errM := strconv.Atoi(m[2])
		if errH != nil || errM != nil || minutes >= 60 {
			return 0, ErrTimeReply
		}
		return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute, nil
	}
	if m := timeMinutesOnly.FindStringSubmatch(text); m != nil {
		minutes, err := strconv.Atoi(m[1])
		if err != nil || minutes >= 60 {
			return 0, ErrTimeReply
		}
		return time.Duration(minutes) * time.Minute, nil
	}
	return 0, ErrTimeReply
}
