package game

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
)

// The chat limits (SPEC 8.5).
const (
	// ChatHistoryLines is how many chat lines the Chat tab shows.
	ChatHistoryLines = 200
	// MaxChatRunes is the longest message an admin may send.
	MaxChatRunes = 500
)

// AuditChat is the audit action of a chat message (SPEC 12.9).
const AuditChat = "chat"

// Why a chat message was refused, before anything was recorded or sent.
var (
	ErrChatEmpty   = errors.New("a chat message cannot be empty")
	ErrChatTooLong = errors.New("a chat message is too long")
	ErrChatNUL     = errors.New("a chat message cannot contain a NUL character")
	ErrChatLines   = errors.New("a chat message must be on one line")
	ErrChatControl = errors.New("a chat message cannot contain a control character")
	// ErrChatCommand: the game strips leading spaces from what RCON sends,
	// so a message that starts with "/" after any blank is a command.
	ErrChatCommand = errors.New("a chat message cannot start with /")
)

// CheckChat says why text cannot be sent as a chat message, or returns nil
// (SPEC 8.5). A refused message is never changed to make it pass.
func CheckChat(text string) error {
	switch {
	case strings.TrimSpace(text) == "":
		return ErrChatEmpty
	case utf8.RuneCountInString(text) > MaxChatRunes:
		return ErrChatTooLong
	case strings.ContainsRune(text, 0):
		return ErrChatNUL
	case strings.ContainsFunc(text, isLineBreak):
		return ErrChatLines
	case strings.ContainsFunc(text, gamesettings.EndsCommand):
		return ErrChatControl
	case startsWithSlash(text):
		return ErrChatCommand
	}
	return nil
}

// isLineBreak covers CR and LF, and the Unicode separators that also end a line.
func isLineBreak(r rune) bool {
	switch r {
	case '\r', '\n', '\u0085', ' ', ' ':
		return true
	}
	return false
}

// startsWithSlash looks past every character that is blank or invisible, not
// only the spaces the game is known to strip, so that no spelling of a leading
// gap hides a command.
func startsWithSlash(text string) bool {
	rest := strings.TrimLeftFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	})
	return strings.HasPrefix(rest, "/")
}

// ChatLine is one line of chat as the game logged it.
type ChatLine struct {
	// Time is the Docker timestamp when the line carried one, else the
	// game's own (UTC), else zero.
	Time time.Time
	// Name is who spoke; it is empty for the server.
	Name string
	Text string
	// Server is set for a message the server said, such as one sent from
	// Foreman or a stop warning.
	Server bool
	// Seq numbers the log line it came from, counting every line since the
	// log began, so no two chat lines share one and a reader can ask for those
	// after the last it saw. It is zero for a line read from a plain slice.
	Seq uint64
}

// serverSpeaker is how the game names the server in a chat line.
const serverSpeaker = "<server>"

// chatLine is "2026-11-12 14:59:13 [CHAT] name: text". The server's own lines
// were captured from a real game; the client's, "name: text", were not (a
// headless server cannot join as a client), so they are read in the same way.
var chatLine = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \[CHAT\] (.*)$`)

// ParseChat reads one log line as chat.
func ParseChat(line string) (ChatLine, bool) {
	var c ChatLine
	var rest string
	c.Time, rest = splitDockerTime(line)
	m := chatLine.FindStringSubmatch(strings.TrimSuffix(rest, "\r"))
	if m == nil {
		return ChatLine{}, false
	}
	name, text, ok := strings.Cut(m[2], ": ")
	if !ok || name == "" {
		return ChatLine{}, false
	}
	if c.Time.IsZero() {
		if t, err := time.Parse("2006-01-02 15:04:05", m[1]); err == nil {
			c.Time = t
		}
	}
	c.Text = text
	if name == serverSpeaker {
		c.Server = true
	} else {
		c.Name = name
	}
	return c, true
}

// parseChatLines returns the chat in lines, oldest first, at most the last
// ChatHistoryLines; the first of lines is log line number first.
func parseChatLines(lines []string, first uint64) []ChatLine {
	var out []ChatLine
	for i, l := range lines {
		if c, ok := ParseChat(l); ok {
			c.Seq = first + uint64(i)
			out = append(out, c)
		}
	}
	if len(out) > ChatHistoryLines {
		out = out[len(out)-ChatHistoryLines:]
	}
	return out
}

// ChatLines returns the world's chat since its log began (SPEC 8.5).
func (c *Controller) ChatLines(id string) []ChatLine {
	lines, _, stop := c.WatchChat(id)
	stop()
	return lines
}

// ChatFeed follows a world's log and numbers the chat lines in it.
type ChatFeed struct {
	// Updates are the log's changes after the history WatchChat returned.
	Updates <-chan Update
	next    uint64
}

// WatchChat returns the world's chat so far and a feed of the log after it.
// The two are taken together, so a page sees every line once (see Watch).
// Call the function to stop watching.
func (c *Controller) WatchChat(id string) ([]ChatLine, *ChatFeed, func()) {
	w := c.world(id)
	w.mu.Lock()
	defer w.mu.Unlock()
	lines, added := w.logs.Lines(), w.logs.Mark()
	ch, stop := w.hub.Subscribe()
	return parseChatLines(lines, added-uint64(len(lines))), &ChatFeed{Updates: ch, next: added}, stop
}

// Line reads one update from Updates. It must be given every update, in order,
// since each log line takes the next number.
func (f *ChatFeed) Line(up Update) (ChatLine, bool) {
	if up.Kind != UpdateLine {
		return ChatLine{}, false
	}
	seq := f.next
	f.next++
	c, ok := ParseChat(up.Line)
	c.Seq = seq
	return c, ok
}

// Next is the number the next log line will take.
func (f *ChatFeed) Next() uint64 { return f.next }

// SendChat says text in the game's chat as the server (SPEC 8.5). It is
// recorded in the audit log before it is sent, and not sent if it cannot be
// recorded (SPEC 12.9). The game is sent the text as given.
func (c *Controller) SendChat(ctx context.Context, id string, by Actor, text string) error {
	if err := CheckChat(text); err != nil {
		return err
	}
	rc, secrets, err := c.liveRCON(c.world(id))
	if err != nil {
		return err
	}
	if err := c.audit(ctx, by, AuditChat, id, Redact(text, secrets...)); err != nil {
		return err
	}
	return c.exec(ctx, rc, text)
}
