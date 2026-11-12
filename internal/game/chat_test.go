package game

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// SPEC 8.5: the server's line is the captured one (testdata README, "Chat");
// the client's line was not captured, so it is read in the same way.
func TestParseChat(t *testing.T) {
	cases := []struct {
		name string
		line string
		want ChatLine
		ok   bool
	}{
		{"server", "2026-11-12 14:59:13 [CHAT] <server>: hello from rcon",
			ChatLine{Time: time.Date(2026, 11, 12, 14, 59, 13, 0, time.UTC), Text: "hello from rcon", Server: true}, true},
		{"client", "2026-11-12 15:00:00 [CHAT] alice: is anyone near the oil field?",
			ChatLine{Time: time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC), Name: "alice", Text: "is anyone near the oil field?"}, true},
		{"docker time wins", "2026-11-12T14:59:13.765716503Z 2026-11-12 14:59:13 [CHAT] bob: hi",
			ChatLine{Time: time.Date(2026, 11, 12, 14, 59, 13, 765716503, time.UTC), Name: "bob", Text: "hi"}, true},
		{"text with a colon", "2026-11-12 15:00:00 [CHAT] alice: ratio: 3: 1", ChatLine{Time: time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC), Name: "alice", Text: "ratio: 3: 1"}, true},
		{"empty text", "2026-11-12 15:00:00 [CHAT] alice: ", ChatLine{Time: time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC), Name: "alice"}, true},
		{"carriage return", "2026-11-12 15:00:00 [CHAT] alice: hi\r", ChatLine{Time: time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC), Name: "alice", Text: "hi"}, true},
		{"a join is not chat", "2026-11-12 15:00:00 [JOIN] alice joined the game", ChatLine{}, false},
		{"no speaker", "2026-11-12 15:00:00 [CHAT] just words", ChatLine{}, false},
		{"tag inside a log line", "  12.345 Info Foo.cpp:1: 2026-11-12 15:00:00 [CHAT] alice: x", ChatLine{}, false},
		{"command log", "2026-11-12 14:59:13 [COMMAND] <server> (command): /c 1", ChatLine{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseChat(c.line)
			if ok != c.ok || got != c.want {
				t.Errorf("ParseChat(%q) = %+v, %v; want %+v, %v", c.line, got, ok, c.want, c.ok)
			}
		})
	}
}

// The captured log holds the server's chat lines among everything else.
func TestParseChatLinesFromTheCapturedLog(t *testing.T) {
	got := ParseChatLines(transcript(t, "2.1.24", "run-players.log"))
	if len(got) == 0 {
		t.Fatal("no chat in the captured log")
	}
	for _, c := range got {
		if !c.Server || c.Text == "" {
			t.Errorf("chat line %+v: want a server line with text", c)
		}
	}
	if !slices.ContainsFunc(got, func(c ChatLine) bool { return c.Text == "hello from rcon" }) {
		t.Errorf("the RCON line is missing: %+v", got)
	}
}

func TestParseChatLinesKeepsTheLast200(t *testing.T) {
	var lines []string
	for i := range ChatHistoryLines + 25 {
		lines = append(lines, "2026-11-12 15:00:00 [CHAT] alice: m"+string(rune('a'+i%26))+strings.Repeat("x", i))
		lines = append(lines, "2026-11-12 15:00:00 [JOIN] bob joined the game")
	}
	got := ParseChatLines(lines)
	if len(got) != ChatHistoryLines {
		t.Fatalf("%d lines, want %d", len(got), ChatHistoryLines)
	}
	if want := strings.Repeat("x", 25); !strings.HasSuffix(got[0].Text, want) || len(got[0].Text) != 25+2 {
		t.Errorf("the oldest kept line is %q; want the 26th", got[0].Text)
	}
}

// SPEC 8.5 and the plan's invariants: what a message may be.
func TestCheckChat(t *testing.T) {
	cases := []struct {
		name string
		text string
		want error
	}{
		{"plain", "hello", nil},
		{"one character", "x", nil},
		{"500 characters", strings.Repeat("x", MaxChatRunes), nil},
		{"500 multibyte characters", strings.Repeat("é", MaxChatRunes), nil},
		{"slash inside", "see a/b and /c", nil},
		{"501 characters", strings.Repeat("x", MaxChatRunes+1), ErrChatTooLong},
		{"empty", "", ErrChatEmpty},
		{"blank", "  \t ", ErrChatEmpty},
		{"NUL", "a\x00b", ErrChatNUL},
		{"LF", "a\nb", ErrChatLines},
		{"CR", "a\rb", ErrChatLines},
		{"CRLF at the end", "a\r\n", ErrChatLines},
		{"line separator", "a b", ErrChatLines},
		{"slash", "/c game.print(1)", ErrChatCommand},
		{"slash alone", "/", ErrChatCommand},
		{"one space then slash (captured: the game runs it)", " /c rcon.print(1)", ErrChatCommand},
		{"three spaces then slash", "   /c rcon.print(1)", ErrChatCommand},
		{"tab then slash", "\t/c 1", ErrChatCommand},
		{"no-break space then slash", " /c 1", ErrChatCommand},
		{"zero-width space then slash", "\u200b /c 1", ErrChatCommand},
		{"control character then slash", "\x07/c 1", ErrChatCommand},
		{"too long and a slash", "/" + strings.Repeat("x", MaxChatRunes), ErrChatTooLong},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CheckChat(c.text); !errors.Is(got, c.want) {
				t.Errorf("CheckChat(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

func chatAudit(r *rig) []string {
	var out []string
	for _, e := range r.audit.list() {
		if e.Action == AuditChat {
			out = append(out, e.Detail)
		}
	}
	return out
}

// SPEC 8.5: a message is audited, then said by the server.
func TestSendChatIsAuditedAndSaid(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Chatty")
	r.start(id)
	if err := r.c.SendChat(context.Background(), id, admin, "welcome, all"); err != nil {
		t.Fatal(err)
	}
	if got := chatAudit(r); !slices.Equal(got, []string{"welcome, all"}) {
		t.Errorf("audit = %q", got)
	}
	if !slices.Contains(r.commands(id), "welcome, all") {
		t.Errorf("the game was sent %q", r.commands(id))
	}
	eventually(t, func() bool {
		return slices.ContainsFunc(r.c.ChatLines(id), func(c ChatLine) bool { return c.Server && c.Text == "welcome, all" })
	}, "the server's chat line in the history")
}

// The plan's invariant: a message that starts with "/" is never sent, never
// escaped, and not recorded as sent. The fake strips leading spaces and runs
// the command, as the real game did.
func TestSendChatNeverRunsACommand(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Commands")
	r.start(id)
	before := len(r.commands(id))
	for _, msg := range []string{"/c rcon.print('ran')", " /c rcon.print('ran')", "   /c rcon.print('ran')", "\t/c rcon.print('ran')", "\u200b/c rcon.print('ran')"} {
		if err := r.c.SendChat(context.Background(), id, admin, msg); !errors.Is(err, ErrChatCommand) {
			t.Errorf("SendChat(%q) = %v, want ErrChatCommand", msg, err)
		}
	}
	if got := r.commands(id)[before:]; len(got) != 0 {
		t.Errorf("the game was sent %q", got)
	}
	if got := chatAudit(r); len(got) != 0 {
		t.Errorf("a refused message was audited as sent: %q", got)
	}
}

func TestSendChatRefusesOtherBadMessagesWithoutSending(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Bad")
	r.start(id)
	before := len(r.commands(id))
	for _, msg := range []string{"", "  ", "a\nb", "a\rb", "a\x00b", strings.Repeat("x", MaxChatRunes+1)} {
		if err := r.c.SendChat(context.Background(), id, admin, msg); err == nil {
			t.Errorf("SendChat(%.20q) was accepted", msg)
		}
	}
	if len(r.commands(id)) != before || len(chatAudit(r)) != 0 {
		t.Errorf("something was sent or audited: %q, %q", r.commands(id)[before:], chatAudit(r))
	}
}

// SPEC 12.9, the plan's invariant: if the audit fails, nothing is sent.
func TestSendChatIsNotSentWhenTheAuditFails(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Audit")
	r.start(id)
	before := len(r.commands(id))
	r.audit.setFail(true)
	if err := r.c.SendChat(context.Background(), id, admin, "hello"); err == nil {
		t.Fatal("SendChat succeeded without an audit entry")
	}
	if got := r.commands(id)[before:]; len(got) != 0 {
		t.Errorf("the game was sent %q", got)
	}
}

func TestSendChatNeedsARunningGame(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Stopped")
	if err := r.c.SendChat(context.Background(), id, admin, "hello"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("SendChat = %v, want ErrNotRunning", err)
	}
	if got := chatAudit(r); len(got) != 0 {
		t.Errorf("audited: %q", got)
	}
}

// SPEC 14.8: a password typed into a message does not reach the audit log.
func TestSendChatRedactsKnownPasswordsInTheAudit(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Chat secret")
	r.writeFile(id, "config/server-settings.json", []byte(`{"game_password":"hunter2-game"}`))
	r.start(id)
	if err := r.c.SendChat(context.Background(), id, admin, "the password is hunter2-game"); err != nil {
		t.Fatal(err)
	}
	if got := chatAudit(r); len(got) != 1 || strings.Contains(got[0], "hunter2-game") {
		t.Errorf("audit = %q", got)
	}
}

// A client's line, injected through the fake, is in the history, and a
// watcher sees it as a log line to parse.
func TestChatLinesFromAClient(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Clients")
	r.start(id)
	if err := r.game.Join(ContainerName(id), "alice"); err != nil {
		t.Fatal(err)
	}
	if err := r.game.Chat(ContainerName(id), "alice", "anyone near the oil?"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return slices.ContainsFunc(r.c.ChatLines(id), func(c ChatLine) bool { return c.Name == "alice" && c.Text == "anyone near the oil?" && !c.Server })
	}, "alice's chat line in the history")
}
