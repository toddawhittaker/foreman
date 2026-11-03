package game

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseOnlineEmptyIsInTheTranscripts(t *testing.T) {
	for _, v := range versions {
		t.Run(v, func(t *testing.T) {
			var reply string
			for _, l := range transcript(t, v, "rcon.log") {
				if _, body, ok := strings.Cut(l, "body='Online players (0):"); ok && strings.HasSuffix(body, `\n'`) {
					reply = "Online players (0):\n"
					break
				}
			}
			if reply == "" {
				t.Fatal("no /players online reply in rcon.log")
			}
			got, err := ParseOnline(reply)
			if err != nil || len(got) != 0 {
				t.Errorf("ParseOnline = %v, %v; want no names", got, err)
			}
		})
	}
}

// The populated reply is the documented form; no real server has shown it yet.
func TestParseOnlineFormatNotYetSeenOnARealServer(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  []string
	}{
		{"none", "Online players (0):\n", []string{}},
		{"none, no newline", "Online players (0):", []string{}},
		{"one", "Online players (1):\n  alice (online)\n", []string{"alice"}},
		{"two", "Online players (2):\n  alice (online)\n  bob (online)\n", []string{"alice", "bob"}},
		{"without the suffix", "Online players (2):\n  alice\n  bob\n", []string{"alice", "bob"}},
		{"carriage returns", "Online players (1):\r\n  alice (online)\r\n", []string{"alice"}},
		{"blank lines", "Online players (1):\n\n  alice (online)\n\n", []string{"alice"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseOnline(c.reply)
			if err != nil || !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseOnline = %v, %v; want %v", got, err, c.want)
			}
		})
	}
}

func TestParseOnlineRefusesWhatItDoesNotKnow(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"all players form":  "Players (1):\n  alice (online)\n",
		"error text":        "Unknown command",
		"count too high":    "Online players (3):\n  alice (online)\n",
		"count too low":     "Online players (0):\n  alice (online)\n",
		"header not first":  "  alice (online)\nOnline players (1):\n",
		"huge count":        "Online players (99999999999999999999):\n",
		"missing the colon": "Online players (0)\n",
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := ParseOnline(reply); !errors.Is(err, ErrPlayersReply) {
				t.Errorf("ParseOnline = %v, %v; want ErrPlayersReply", got, err)
			}
		})
	}
}

func TestJoinTimesOnline(t *testing.T) {
	at := time.Date(2026, 11, 3, 22, 0, 0, 0, time.UTC)
	now := at.Add(95 * time.Minute)
	join := func(name string, t time.Time) Event { return Event{Kind: KindPlayerJoined, Player: name, Time: t} }
	leave := func(name string) Event { return Event{Kind: KindPlayerLeft, Player: name} }

	cases := []struct {
		name   string
		events []Event
		online []string
		want   []Player
	}{
		{"nobody", nil, []string{}, []Player{}},
		{"one joined", []Event{join("alice", at)}, []string{"alice"},
			[]Player{{"alice", at, 95 * time.Minute}}},
		{"last join wins", []Event{join("alice", at), join("alice", at.Add(time.Hour))}, []string{"alice"},
			[]Player{{"alice", at.Add(time.Hour), 35 * time.Minute}}},
		{"left then rejoined", []Event{join("alice", at), leave("alice"), join("alice", at.Add(time.Hour))}, []string{"alice"},
			[]Player{{"alice", at.Add(time.Hour), 35 * time.Minute}}},
		{"RCON says online but the log never showed a join", nil, []string{"alice"},
			[]Player{{Name: "alice"}}},
		{"left and RCON still lists them: unknown, not stale", []Event{join("alice", at), leave("alice")}, []string{"alice"},
			[]Player{{Name: "alice"}}},
		{"logged in but RCON does not list them", []Event{join("ghost", at)}, []string{"alice"},
			[]Player{{Name: "alice"}}},
		{"RCON order is kept", []Event{join("bob", at), join("alice", at.Add(time.Hour))}, []string{"bob", "alice"},
			[]Player{{"bob", at, 95 * time.Minute}, {"alice", at.Add(time.Hour), 35 * time.Minute}}},
		{"join without a time is ignored", []Event{{Kind: KindPlayerJoined, Player: "alice"}}, []string{"alice"},
			[]Player{{Name: "alice"}}},
		{"other events are ignored", []Event{{Kind: KindSaveFinished}}, []string{"alice"},
			[]Player{{Name: "alice"}}},
		{"join in the future never gives a negative time", []Event{join("alice", now.Add(time.Minute))}, []string{"alice"},
			[]Player{{"alice", now.Add(time.Minute), 0}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			j := NewJoinTimes()
			for _, ev := range c.events {
				j.Observe(ev)
			}
			if got := j.Online(c.online, now); !reflect.DeepEqual(got, c.want) {
				t.Errorf("Online = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestJoinTimesFedFromParsedLines(t *testing.T) {
	// Format not yet seen on a real server.
	j := NewJoinTimes()
	for _, l := range []string{
		"2026-11-03 22:00:00 [JOIN] alice joined the game",
		"2026-11-03 22:10:00 [CHAT] bob: 2026-11-03 21:00:00 [JOIN] bob joined the game",
	} {
		j.Observe(ParseLine(l))
	}
	now := time.Date(2026, 11, 3, 22, 30, 0, 0, time.UTC)
	got := j.Online([]string{"alice", "bob"}, now)
	if got[0].Duration != 30*time.Minute || got[1].Duration != 0 || !got[1].Joined.IsZero() {
		t.Errorf("Online = %+v; chat must not fake bob's join", got)
	}
}

func TestJoinTimesReset(t *testing.T) {
	j := NewJoinTimes()
	j.Observe(Event{Kind: KindPlayerJoined, Player: "alice", Time: time.Now()})
	j.Reset()
	if got := j.Online([]string{"alice"}, time.Now()); !got[0].Joined.IsZero() {
		t.Errorf("Reset kept %+v", got)
	}
}

func TestJoinTimesConcurrent(t *testing.T) {
	j := NewJoinTimes()
	done := make(chan struct{})
	for g := 0; g < 4; g++ {
		go func() {
			for i := 0; i < 200; i++ {
				j.Observe(Event{Kind: KindPlayerJoined, Player: "a", Time: time.Now()})
				_ = j.Online([]string{"a"}, time.Now())
				j.Observe(Event{Kind: KindPlayerLeft, Player: "a"})
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < 4; g++ {
		<-done
	}
}
