// Package game runs each world's game container: start, stop, restart,
// forced stop and restore, the monitor that watches the containers, the
// game's log and RCON (SPEC 4.3 to 4.6, 7, 8.1, 8.4, 8.6, 8.7, 8.9).
package game

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/secret"
)

// LogBufferLines is how many log lines the page keeps (SPEC 8.6).
const LogBufferLines = 1000

// Mask replaces a secret in a relayed log line.
const Mask = "••••"

// Kind says what a log line means to the controller.
type Kind int

// The kinds of log line the controller reacts to.
const (
	KindOther        Kind = iota
	KindLoadStarted       // the game began loading the save
	KindInGame            // the map is loaded and the server is hosting
	KindSaveFinished      // an autosave or /server-save completed
	KindLoadFailed        // the game gave up loading the save
	KindLoadError         // the reason for a failed load, in Detail
	KindPlayerJoined
	KindPlayerLeft
)

// Event is one classified log line.
type Event struct {
	Kind Kind
	// Time is the Docker timestamp when the line carried one, else the
	// game's own timestamp on a join or leave line (read as UTC), else zero.
	Time   time.Time
	Player string // set for KindPlayerJoined and KindPlayerLeft
	Detail string // set for KindLoadError: the game's reason
}

var (
	// Game lines start with seconds since start; Docker may add a timestamp before it.
	secondsPrefix = regexp.MustCompile(`^\s*\d+\.\d+ `)
	// "Info File.cpp:123: message"; the file and line change between versions.
	levelLine = regexp.MustCompile(`^(?:Info|Warning|Error) [A-Za-z]+\.cpp:\d+: (.*)$`)
	// Anchored at both ends so a chat line cannot pass as a join (SPEC 8.1).
	playerLine = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \[(JOIN|LEAVE)\] (\S+) (?:joined|left) the game$`)
)

// ParseLine classifies one log line. It matches on message text only, since
// source line numbers change between game versions.
func ParseLine(line string) Event {
	var ev Event
	var rest string
	ev.Time, rest = splitDockerTime(line)
	rest = strings.TrimSuffix(secondsPrefix.ReplaceAllString(rest, ""), "\r")
	rest = strings.TrimLeft(rest, " ")

	if m := playerLine.FindStringSubmatch(rest); m != nil {
		if ev.Time.IsZero() {
			if t, err := time.Parse("2006-01-02 15:04:05", m[1]); err == nil {
				ev.Time = t
			}
		}
		ev.Player = m[3]
		ev.Kind = KindPlayerJoined
		if m[2] == "LEAVE" {
			ev.Kind = KindPlayerLeft
		}
		return ev
	}

	if strings.HasPrefix(rest, "Loading map ") && strings.HasSuffix(rest, " bytes.") {
		ev.Kind = KindLoadStarted
		return ev
	}
	m := levelLine.FindStringSubmatch(rest)
	if m == nil {
		return ev
	}
	msg := m[1]
	switch {
	case strings.Contains(msg, "changing state from(CreatingGame) to(InGame)"):
		ev.Kind = KindInGame
	case strings.Contains(msg, "changing state from(CreatingGame) to(InitializationFailed)"):
		ev.Kind = KindLoadFailed
	case strings.HasPrefix(msg, "MultiplayerManager failed: "):
		ev.Kind = KindLoadError
		ev.Detail = strings.Trim(strings.TrimPrefix(msg, "MultiplayerManager failed: "), `"`)
	case msg == "Saving finished":
		ev.Kind = KindSaveFinished
	}
	return ev
}

// splitDockerTime takes the timestamp Docker puts before a line when asked
// for timestamps. It returns the zero time and the line unchanged when
// there is none.
func splitDockerTime(line string) (time.Time, string) {
	first, after, ok := strings.Cut(line, " ")
	if !ok {
		return time.Time{}, line
	}
	t, err := time.Parse(time.RFC3339Nano, first)
	if err != nil {
		return time.Time{}, line
	}
	return t, after
}

// Redact removes every secret from a log line before it is relayed or
// stored (SPEC 8.6, 14.8). Longer secrets go first so one that contains
// another is still removed whole.
func Redact(line string, secrets ...secret.Secret) string {
	values := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if v := s.Reveal(); v != "" {
			values = append(values, v)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, v := range values {
		line = strings.ReplaceAll(line, v, Mask)
	}
	return line
}

// LogBuffer keeps the most recent lines. It is safe for concurrent use.
type LogBuffer struct {
	mu    sync.Mutex
	lines []string // ring storage
	next  int      // where the next line goes
	full  bool
	added uint64 // lines ever added, so a caller can mark a point in it
}

// NewLogBuffer returns a buffer holding LogBufferLines lines.
func NewLogBuffer() *LogBuffer { return newLogBuffer(LogBufferLines) }

func newLogBuffer(n int) *LogBuffer { return &LogBuffer{lines: make([]string, n)} }

// Add appends a line, dropping the oldest when the buffer is full.
func (b *LogBuffer) Add(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines[b.next] = line
	b.added++
	b.next++
	if b.next == len(b.lines) {
		b.next, b.full = 0, true
	}
}

// Mark returns a point in the buffer; LinesSince(mark) returns only the
// lines added after it.
func (b *LogBuffer) Mark() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.added
}

// LinesSince returns the held lines added after mark, oldest first.
func (b *LogBuffer) LinesSince(mark uint64) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	lines, n := b.linesLocked(), b.added-mark
	if n < uint64(len(lines)) {
		lines = lines[uint64(len(lines))-n:]
	}
	return lines
}

// Lines returns a copy of the held lines, oldest first.
func (b *LogBuffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.linesLocked()
}

func (b *LogBuffer) linesLocked() []string {
	if !b.full {
		return append([]string(nil), b.lines[:b.next]...)
	}
	out := make([]string, 0, len(b.lines))
	out = append(out, b.lines[b.next:]...)
	return append(out, b.lines[:b.next]...)
}
