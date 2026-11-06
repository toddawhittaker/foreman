package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The Console tab (SPEC 8.4, 8.6): the game's log and an RCON console.
const (
	PathConsole       = "/console"
	PathConsoleSend   = "/worlds/{id}/console"
	PathConsoleEvents = "/worlds/{id}/console/events"
	PathConsoleLog    = "/worlds/{id}/console/log"
)

func consolePath(id, tail string) string { return worldPath(id) + "/console" + tail }

const (
	// maxCommandLength is the most characters a console command may have.
	maxCommandLength = 1000
	// historyEntries is how many commands a world's console remembers, and
	// maxReplyBytes how much of one reply is kept.
	historyEntries = 20
	maxReplyBytes  = 100_000
	// announceRunes bounds the reply read out to a screen reader.
	announceRunes = 200
)

// Commands that carry a secret (SPEC 8.4, 14.8). A command in
// game.SetsSecret has its argument replaced in the audit log and on the
// page; one in showsSecret has its reply hidden. The game has no other
// command that prints a password or token. The Lua commands (/c, /sc,
// /silent-command) can hold anything, so they cannot be told apart and are
// recorded as typed.
var showsSecret = []string{"config get password"}

var (
	// secretArgument finds the typed command up to its secret.
	secretArgument = regexp.MustCompile(`(?is)^(\s*/?\s*` + game.CommandPatterns(game.SetsSecret) + `)\s+\S.*$`)
	secretReply    = regexp.MustCompile(`(?i)^\s*/?\s*` + game.CommandPatterns(showsSecret) + `(?:\s|$)`)
)

// auditText is the command as the audit log and the page record it: with
// the surrounding space gone and a secret argument replaced by the mask.
// The game is sent the command unchanged.
func auditText(command string) string {
	command = strings.TrimSpace(command)
	return secretArgument.ReplaceAllString(command, "${1} "+game.Mask)
}

// hidesReply reports whether the game's reply to command prints a secret.
func hidesReply(command string) bool { return secretReply.MatchString(command) }

// checkCommand returns the words for the command field's error, or zero.
func checkCommand(command string) i18n.Msg {
	switch {
	case strings.TrimSpace(command) == "":
		return i18n.M("server.console.error.empty")
	case utf8.RuneCountInString(command) > maxCommandLength:
		return i18n.M("server.console.error.long", "max", maxCommandLength)
	case strings.ContainsAny(command, "\x00"):
		return i18n.M("server.console.error.nul")
	case strings.ContainsAny(command, "\r\n"):
		return i18n.M("server.console.error.lines")
	}
	return i18n.Msg{}
}

// consoleEntry is one command and the game's reply, as shown.
type consoleEntry struct {
	Command string // already masked
	Reply   string
	Cut     bool // the reply was longer than maxReplyBytes
	Last    bool // the newest entry; the page links to it
}

// consoleHistory keeps each world's recent commands and replies in memory,
// so a page without JavaScript can show the reply after its post. It is
// lost at restart, like the log (SPEC 8.6).
type consoleHistory struct {
	mu      sync.Mutex
	byWorld map[string][]consoleEntry
}

func newConsoleHistory() *consoleHistory {
	return &consoleHistory{byWorld: map[string][]consoleEntry{}}
}

func (h *consoleHistory) add(id string, e consoleEntry) {
	if len(e.Reply) > maxReplyBytes {
		cut := maxReplyBytes
		for cut > 0 && !utf8.RuneStart(e.Reply[cut]) {
			cut--
		}
		e.Reply, e.Cut = e.Reply[:cut], true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	list := append(h.byWorld[id], e)
	if len(list) > historyEntries {
		list = list[len(list)-historyEntries:]
	}
	h.byWorld[id] = list
}

// list returns a copy, oldest first, with the newest marked.
func (h *consoleHistory) list(id string) []consoleEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := append([]consoleEntry(nil), h.byWorld[id]...)
	if len(out) > 0 {
		out[len(out)-1].Last = true
	}
	return out
}

// announceReply is what a screen reader hears for the admin's own command
// (DESIGN 8.5): the reply on one line, cut short.
func announceReply(command, reply string) i18n.Msg {
	reply = strings.Join(strings.Fields(reply), " ")
	if reply == "" {
		return i18n.M("server.console.announce.no_reply", "command", command)
	}
	if r := []rune(reply); len(r) > announceRunes {
		reply = string(r[:announceRunes]) + "…"
	}
	return i18n.M("server.console.announce.reply", "command", command, "reply", reply)
}

// ConsolePage is the log and console of the active world (SPEC 8.4, 8.6,
// DESIGN 8.5).
type ConsolePage struct {
	World    world.Summary
	HasWorld bool
	// Status is the game's state for the heading's badge, which follows the
	// Server page's live facts (DESIGN 7.5).
	Status  game.Status
	Lines   []string
	History []consoleEntry
	// Command refills the field after a refusal; it is empty when the
	// command carried a secret.
	Command      string
	CommandError i18n.Msg
	// Failure says why a command was not sent or answered.
	Failure i18n.Msg
	// Announce is read out by #announcer when a script swaps in the panel.
	Announce i18n.Msg
}

func (ConsolePage) templateName() string { return "console" }
func (ConsolePage) navItem() string      { return PathServer }
func (p ConsolePage) hasErrors() bool    { return !p.CommandError.IsZero() }

func (p ConsolePage) Problems() []Problem {
	return problems(Problem{"command", p.CommandError})
}

func (p ConsolePage) WorldPath() string  { return worldPath(p.World.ID) }
func (p ConsolePage) SendPath() string   { return consolePath(p.World.ID, "") }
func (p ConsolePage) EventsPath() string { return consolePath(p.World.ID, "/events") }
func (p ConsolePage) LogPath() string    { return consolePath(p.World.ID, "/log") }

// The heading names the world and its state, as on the Server page.
func (p ConsolePage) ActiveName() string  { return p.World.Name }
func (p ConsolePage) StateWord() i18n.Msg { return stateWord(p.Status.State) }

// liveURL is the Server page's stream: its facts carry the heading's badge.
func (p ConsolePage) liveURL() string {
	if !p.HasWorld {
		return ""
	}
	return gamePath(p.World.ID, "events")
}

// gameLineKind finds the game's own Error and Warning lines, such as
// "  12.345 Error ServerMultiplayerManager.cpp:...". log.js holds the same
// pattern for lines that arrive live.
var gameLineKind = regexp.MustCompile(`^\s*\d+\.\d+ (Error|Warning) `)

// LineClass is the class that colours a game Error or Warning line, or "".
// The line keeps its own word, so the colour is never the only sign (SPEC 17.4).
func (ConsolePage) LineClass(line string) string {
	if m := gameLineKind.FindStringSubmatch(line); m != nil {
		return "log-" + strings.ToLower(m[1])
	}
	return ""
}

// CommandLimit is the longest command, for the hint.
func (ConsolePage) CommandLimit() int { return maxCommandLength }

// MaxLines is how many log lines the page keeps (SPEC 8.6), for the script.
func (ConsolePage) MaxLines() int { return game.LogBufferLines }

func (s *server) consoleRoutes() {
	if s.game == nil {
		s.mux.HandleFunc("GET "+PathConsole, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, PathWorlds, http.StatusSeeOther)
		})
		return
	}
	s.mux.HandleFunc("GET "+PathConsole, s.requireUser(s.handleConsole))
	s.mux.HandleFunc("POST "+PathConsoleSend, s.requireUser(s.handleConsoleSend))
	s.handleLong("GET "+PathConsoleEvents, s.requireUser(s.handleConsoleEvents))
	s.mux.HandleFunc("GET "+PathConsoleLog, s.requireUser(s.handleConsoleLog))
}

// logFileName keeps a world id to the characters safe in a header and a file name.
var logFileName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// handleConsoleLog sends the log lines the Console shows as a text file
// (SPEC 8.6). They are the lines Foreman keeps, already masked, so nothing
// leaves that the page does not show (SPEC 14.8).
func (s *server) handleConsoleLog(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	var b strings.Builder
	for _, line := range s.game.Lines(sum.ID) {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	name := "foreman-" + logFileName.ReplaceAllString(sum.ID, "_") + "-log.txt"
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

// consolePage fills the page for a world with the log so far and the
// recent commands.
func (s *server) consolePage(sum world.Summary) ConsolePage {
	return ConsolePage{World: sum, HasWorld: true, Status: s.game.Status(sum.ID), Lines: s.game.Lines(sum.ID), History: s.console.list(sum.ID)}
}

func (s *server) handleConsole(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, found, err := s.activeWorld(r.Context())
	if err != nil {
		s.fail(w, r, "find the active world", err)
		return
	}
	if !found {
		s.page(w, r, http.StatusOK, u.Username, ConsolePage{})
		return
	}
	s.page(w, r, http.StatusOK, u.Username, s.consolePage(sum))
}

// refill is the text to put back in the command field after a refusal:
// never a command that carries a secret (SPEC 14.8).
func refill(command string) string {
	if auditText(command) != strings.TrimSpace(command) {
		return ""
	}
	return command
}

// respond answers a console post. A script (htmx) gets the panel alone, and
// always with 200 so that it swaps a refusal in as it does a reply.
func (s *server) respond(w http.ResponseWriter, r *http.Request, status int, user string, p ConsolePage) {
	if r.Header.Get("HX-Request") != "true" {
		s.page(w, r, status, user, p)
		return
	}
	var b strings.Builder
	if err := s.render.Fragment(&b, localeOf(r), p, "console-response"); err != nil {
		s.fail(w, r, "render console panel", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

// handleConsoleSend sends one command and records it (SPEC 8.4, 12.9).
func (s *server) handleConsoleSend(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	command := r.PostFormValue("command")
	p := s.consolePage(sum)
	p.Command = refill(command)
	if msg := checkCommand(command); !msg.IsZero() {
		p.CommandError = msg
		s.respond(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	shown, reply, err := s.game.Console(r.Context(), sum.ID, actor(u), command, auditText(command))
	switch {
	case err == nil:
	case errors.Is(err, game.ErrNotRunning):
		p.Failure = i18n.M("server.console.not_running")
	case errors.Is(err, game.ErrRCONUnreachable):
		p.Failure = i18n.M("server.console.unreachable")
	default:
		s.fail(w, r, "send console command", err)
		return
	}
	if err != nil {
		p.Announce = labelled(i18n.M("common.error.label"), p.Failure)
		s.respond(w, r, http.StatusConflict, u.Username, p)
		return
	}
	if hidesReply(command) {
		reply = game.Mask
	}
	s.console.add(sum.ID, consoleEntry{Command: shown, Reply: reply})
	if r.Header.Get("HX-Request") != "true" {
		// The fragment makes a browser without JavaScript scroll the
		// history box to the reply.
		http.Redirect(w, r, PathConsole+"#last-reply", http.StatusSeeOther)
		return
	}
	p.Command, p.History = "", s.console.list(sum.ID)
	p.Announce = announceReply(shown, reply)
	s.respond(w, r, http.StatusOK, u.Username, p)
}

// handleConsoleEvents streams a world's log lines as server-sent events
// (SPEC 8.6): "reset" with every line so far, then "lines" with each
// batch of new ones, so a page that reconnects misses and repeats nothing.
// The lines are JSON, so the page can only ever set them as text. The
// stream ends when its user is no longer signed in, when the page falls
// too far behind, and first of all at shutdown.
func (s *server) handleConsoleEvents(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	if !s.runs.track() {
		http.Error(w, localeOf(r).T("server.stream.shutting_down"), http.StatusServiceUnavailable)
		return
	}
	defer s.runs.wg.Done()
	opened := s.now()
	out, err := s.openStream(w, localeOf(r))
	if err != nil {
		return
	}

	view, updates, stop := s.game.Watch(sum.ID)
	defer stop()
	if out.lines("reset", view.Lines) != nil {
		return
	}
	beat := time.NewTicker(heartbeatEvery)
	defer beat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.runs.ctx.Done():
			return
		case up, open := <-updates:
			if !open {
				return
			}
			var batch []string
			if up.Kind == game.UpdateLine {
				batch = append(batch, up.Line)
			}
			// Whatever else is waiting goes in the same event.
			for drained := false; !drained; {
				select {
				case up, open := <-updates:
					if !open {
						return
					}
					if up.Kind == game.UpdateLine {
						batch = append(batch, up.Line)
					}
				default:
					drained = true
				}
			}
			if len(batch) > 0 && out.lines("lines", batch) != nil {
				return
			}
		case <-beat.C:
			if !s.stillSignedIn(r, opened) {
				return
			}
			if out.comment() != nil {
				return
			}
		}
	}
}

// lines sends log lines as one event holding a JSON array.
func (l *liveStream) lines(event string, lines []string) error {
	data, err := json.Marshal(lines)
	if err != nil {
		return err
	}
	if err := l.event(event, string(data)); err != nil {
		return err
	}
	return l.rc.Flush()
}
