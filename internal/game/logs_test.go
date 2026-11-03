package game

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/secret"
)

var versions = []string{"1.1.110", "2.0.77", "2.1.24"}

func transcript(t *testing.T, version, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", version, name)) //nolint:gosec // fixed test data paths
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// kinds returns the non-Other event kinds of a transcript, in order.
func kinds(lines []string) []Kind {
	var out []Kind
	for _, l := range lines {
		if ev := ParseLine(l); ev.Kind != KindOther {
			out = append(out, ev.Kind)
		}
	}
	return out
}

func TestParseLineSigtermTranscripts(t *testing.T) {
	// Start, RCON up, /server-save, one autosave, then SIGTERM.
	want := []Kind{KindLoadStarted, KindInGame, KindRCONReady, KindSaveFinished, KindSaveFinished}
	for _, v := range versions {
		t.Run(v, func(t *testing.T) {
			got := kinds(transcript(t, v, "run-sigterm.log"))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("kinds = %v, want %v", got, want)
			}
		})
	}
}

func TestParseLineFailedLoadTranscripts(t *testing.T) {
	cases := []struct{ file, reason string }{
		{"run-corrupt.log", "Opening zip /factorio/saves/corrupt.zip failed: "},
		{"run-truncated.log", "Opening zip /factorio/saves/truncated.zip failed: "},
		{"run-unreadable.log", "Opening zip /factorio/saves/unreadable.zip failed: "},
	}
	for _, v := range versions {
		for _, c := range cases {
			t.Run(v+"/"+c.file, func(t *testing.T) {
				lines := transcript(t, v, c.file)
				var failed, errs, inGame int
				for _, l := range lines {
					ev := ParseLine(l)
					switch ev.Kind {
					case KindLoadFailed:
						failed++
					case KindLoadError:
						errs++
						if !strings.HasPrefix(ev.Detail, c.reason) {
							t.Errorf("detail = %q, want prefix %q", ev.Detail, c.reason)
						}
					case KindInGame, KindRCONReady:
						inGame++
					}
				}
				if failed == 0 || failed != errs {
					t.Errorf("failed loads = %d, load errors = %d; want equal and non-zero", failed, errs)
				}
				if inGame != 0 {
					t.Errorf("a failed load reported InGame or RCON ready %d times", inGame)
				}
			})
		}
	}
}

func TestParseLineOtherTranscriptsHaveNoPlayerEvents(t *testing.T) {
	// No [JOIN] or [LEAVE] line was captured (no game client), so none may be invented.
	for _, v := range versions {
		for _, name := range []string{"create.log", "run-sigterm.log", "run-sigint.log", "run-sigkill.log", "run-no-stdin.log"} {
			for _, l := range transcript(t, v, name) {
				if k := ParseLine(l).Kind; k == KindPlayerJoined || k == KindPlayerLeft {
					t.Errorf("%s/%s: %q parsed as a player event", v, name, l)
				}
			}
		}
	}
}

func TestParseLineRealLines(t *testing.T) {
	docker := time.Date(2026, 11, 3, 22, 26, 40, 787585323, time.UTC)
	cases := []struct {
		name string
		line string
		want Event
	}{
		{"1.1 load", "2026-11-03T22:26:05.129049581Z    0.394 Loading map /factorio/saves/spike.zip: 729344 bytes.", Event{Kind: KindLoadStarted, Time: time.Date(2026, 11, 3, 22, 26, 5, 129049581, time.UTC)}},
		{"1.1 in game", "   0.431 Info ServerMultiplayerManager.cpp:814: updateTick(0) changing state from(CreatingGame) to(InGame)", Event{Kind: KindInGame}},
		{"in game, save tick not zero", "   9.000 Info ServerMultiplayerManager.cpp:808: updateTick(3814) changing state from(CreatingGame) to(InGame)", Event{Kind: KindInGame}},
		{"2.1 rcon", "2026-11-03T22:26:40.787585323Z    1.112 Info RemoteCommandProcessor.cpp:119: Starting RCON interface at IP ADDR:({0.0.0.0:27015})", Event{Kind: KindRCONReady, Time: docker}},
		{"server-save", "   3.279 Info AppManagerStates.cpp:2131: Saving finished", Event{Kind: KindSaveFinished}},
		{"hosting line is not in game", "   1.112 Hosting game at IP ADDR:({0.0.0.0:34197})", Event{}},
		{"save start is not finished", "   3.229 Info AppManager.cpp:424: Saving game as /factorio/saves/spike.zip", Event{}},
		{"loading a mod", "   0.033 Loading mod base 2.1.24 (data.lua)", Event{}},
		{"closing state", "  64.867 Info ServerMultiplayerManager.cpp:809: updateTick(3814) changing state from(Disconnected) to(Closed)", Event{}},
		{"failed state", "   0.361 Info ServerMultiplayerManager.cpp:814: updateTick(4294967295) changing state from(CreatingGame) to(InitializationFailed)", Event{Kind: KindLoadFailed}},
		{"empty", "", Event{}},
		{"windows line ending", "   3.279 Info AppManagerStates.cpp:2131: Saving finished\r", Event{Kind: KindSaveFinished}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseLine(c.line); got != c.want {
				t.Errorf("ParseLine = %+v, want %+v", got, c.want)
			}
		})
	}
}

// The [JOIN] and [LEAVE] form is the documented one; no real server has
// shown it yet.
func TestParseLinePlayersFormatNotYetSeenOnARealServer(t *testing.T) {
	at := time.Date(2026, 11, 3, 22, 30, 1, 0, time.UTC)
	cases := []struct {
		name string
		line string
		want Event
	}{
		{"join", "2026-11-03 22:30:01 [JOIN] alice joined the game", Event{Kind: KindPlayerJoined, Time: at, Player: "alice"}},
		{"leave", "2026-11-03 22:30:01 [LEAVE] alice left the game", Event{Kind: KindPlayerLeft, Time: at, Player: "alice"}},
		{"with seconds prefix", "  75.100 2026-11-03 22:30:01 [JOIN] bob_1 joined the game", Event{Kind: KindPlayerJoined, Time: at, Player: "bob_1"}},
		{"docker time wins", "2026-11-03T22:30:02Z 2026-11-03 22:30:01 [JOIN] alice joined the game", Event{Kind: KindPlayerJoined, Time: at.Add(time.Second), Player: "alice"}},
		{"chat cannot fake a join", "2026-11-03 22:30:01 [CHAT] mallory: 2026-11-03 22:30:01 [JOIN] alice joined the game", Event{}},
		{"text before the date", "x 2026-11-03 22:30:01 [JOIN] alice joined the game", Event{}},
		{"text after", "2026-11-03 22:30:01 [JOIN] alice joined the game, really", Event{}},
		{"command line", "2026-11-03 22:30:01 [COMMAND] alice: /c game.print(1)", Event{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ParseLine(c.line); got != c.want {
				t.Errorf("ParseLine = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestRedact(t *testing.T) {
	rcon, game := secret.New("hunter2xyz"), secret.New("gamepass")
	cases := []struct {
		name    string
		line    string
		secrets []secret.Secret
		want    string
	}{
		{"startup line", `Program arguments: "factorio" "--rcon-password" hunter2xyz `, []secret.Secret{rcon, game}, `Program arguments: "factorio" "--rcon-password" •••• `},
		{"both, several times", "hunter2xyz gamepass hunter2xyz", []secret.Secret{rcon, game}, "•••• •••• ••••"},
		{"game prints private", `"--rcon-password" <private> `, []secret.Secret{rcon}, `"--rcon-password" <private> `},
		{"nothing to remove", "Saving finished", []secret.Secret{rcon, game}, "Saving finished"},
		{"empty secret is skipped", "Saving finished", []secret.Secret{secret.New("")}, "Saving finished"},
		{"no secrets", "Saving finished", nil, "Saving finished"},
		{"longer secret first", "pass-word-2", []secret.Secret{secret.New("pass"), secret.New("pass-word-2")}, "••••"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Redact(c.line, c.secrets...); got != c.want {
				t.Errorf("Redact = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRedactRealStartupLineWithThePasswordInIt(t *testing.T) {
	pw := secret.New("Zq8-real-looking-password")
	for _, v := range versions {
		for _, l := range transcript(t, v, "run-sigterm.log") {
			if !strings.Contains(l, "--rcon-password") {
				continue
			}
			leaky := strings.Replace(l, "<private>", pw.Reveal(), 1)
			if !strings.Contains(leaky, pw.Reveal()) {
				t.Fatalf("%s: test setup did not insert the password", v)
			}
			if got := Redact(leaky, pw); strings.Contains(got, pw.Reveal()) || !strings.Contains(got, Mask) {
				t.Errorf("%s: password survived or mask missing: %q", v, got)
			}
		}
	}
}

func TestLogBuffer(t *testing.T) {
	cases := []struct {
		name string
		cap  int
		add  int
		want []string
	}{
		{"empty", 3, 0, []string{}},
		{"partly full", 3, 2, []string{"l1", "l2"}},
		{"exactly full", 3, 3, []string{"l1", "l2", "l3"}},
		{"wrapped once", 3, 4, []string{"l2", "l3", "l4"}},
		{"wrapped twice and a bit", 3, 8, []string{"l6", "l7", "l8"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newLogBuffer(c.cap)
			for i := 1; i <= c.add; i++ {
				b.Add(fmt.Sprintf("l%d", i))
			}
			got := b.Lines()
			if len(got) != len(c.want) || (len(got) > 0 && !reflect.DeepEqual(got, c.want)) {
				t.Errorf("Lines = %v, want %v", got, c.want)
			}
		})
	}
}

func TestLogBufferKeeps1000Lines(t *testing.T) {
	b := NewLogBuffer()
	for i := 0; i < 2500; i++ {
		b.Add(fmt.Sprintf("line %d", i))
	}
	got := b.Lines()
	if len(got) != 1000 || got[0] != "line 1500" || got[999] != "line 2499" {
		t.Errorf("got %d lines, first %q, last %q", len(got), got[0], got[len(got)-1])
	}
}

func TestLogBufferLinesIsACopy(t *testing.T) {
	b := newLogBuffer(2)
	b.Add("a")
	got := b.Lines()
	got[0] = "changed"
	if b.Lines()[0] != "a" {
		t.Error("changing the returned slice changed the buffer")
	}
}

func TestLogBufferConcurrent(t *testing.T) {
	b := NewLogBuffer()
	done := make(chan struct{})
	for g := 0; g < 4; g++ {
		go func() {
			for i := 0; i < 500; i++ {
				b.Add("x")
				_ = b.Lines()
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < 4; g++ {
		<-done
	}
	if n := len(b.Lines()); n != 1000 {
		t.Errorf("lines = %d, want 1000", n)
	}
}
