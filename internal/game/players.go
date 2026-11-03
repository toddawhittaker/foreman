package game

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrPlayersReply means the /players online reply was not in a known form.
var ErrPlayersReply = errors.New("unrecognised /players online reply")

var onlineHeader = regexp.MustCompile(`^Online players \((\d+)\):$`)

// ParseOnline reads the reply to RCON "/players online" and returns the
// names (SPEC 8.1). It fails rather than guess when the count in the header
// does not match the names listed.
func ParseOnline(reply string) ([]string, error) {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(reply, "\r\n", "\n")), "\n")
	m := onlineHeader.FindStringSubmatch(strings.TrimSpace(lines[0]))
	if m == nil {
		return nil, ErrPlayersReply
	}
	count, err := strconv.Atoi(m[1])
	if err != nil {
		return nil, ErrPlayersReply
	}
	names := []string{}
	for _, l := range lines[1:] {
		name := strings.TrimSuffix(strings.TrimSpace(l), " (online)")
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) != count {
		return nil, fmt.Errorf("%w: header says %d, found %d", ErrPlayersReply, count, len(names))
	}
	return names, nil
}

// Player is one person online now.
type Player struct {
	Name string
	// Joined is the last [JOIN] seen; zero when the log never showed it.
	Joined time.Time
	// Duration is how long they have been on; zero when Joined is unknown.
	Duration time.Duration
}

// JoinTimes remembers each player's last [JOIN] line. It only supplies "how
// long"; RCON decides who is online (SPEC 8.1). It is safe for concurrent use.
type JoinTimes struct {
	mu     sync.Mutex
	joined map[string]time.Time
}

// NewJoinTimes returns an empty record.
func NewJoinTimes() *JoinTimes { return &JoinTimes{joined: map[string]time.Time{}} }

// Observe records a join or forgets a player who left. Other events and
// joins without a time are ignored.
func (j *JoinTimes) Observe(ev Event) {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch ev.Kind {
	case KindPlayerJoined:
		if !ev.Time.IsZero() {
			j.joined[ev.Player] = ev.Time
		}
	case KindPlayerLeft:
		delete(j.joined, ev.Player)
	}
}

// Reset forgets everything, for when the game container starts again.
func (j *JoinTimes) Reset() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.joined = map[string]time.Time{}
}

// Online pairs the names RCON reports with their join times, in RCON's order.
func (j *JoinTimes) Online(names []string, now time.Time) []Player {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Player, 0, len(names))
	for _, n := range names {
		p := Player{Name: n}
		if t, ok := j.joined[n]; ok {
			p.Joined = t
			p.Duration = max(now.Sub(t), 0)
		}
		out = append(out, p)
	}
	return out
}
