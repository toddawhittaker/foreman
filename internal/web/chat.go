package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The Chat tab of the server view (SPEC 8.5, DESIGN 8.14).
const (
	PathChat       = "/chat"
	PathChatSend   = "/worlds/{id}/chat"
	PathChatEvents = "/worlds/{id}/chat/events"
)

func chatPath(id, tail string) string { return worldPath(id) + "/chat" + tail }

// ChatPage is the chat of the active world and the field to answer it.
type ChatPage struct {
	World    world.Summary
	HasWorld bool
	// Status is the game's state for the heading's badge, which follows the
	// Server page's live facts (DESIGN 7.5).
	Status game.Status
	// Rows are the chat lines, oldest first, drawn by the "chat-rows"
	// template both in the page and in the stream (DESIGN 8.14).
	Rows []game.ChatLine
	// After numbers the last line in Rows, so the stream sends only what is
	// newer; zero when there is none.
	After uint64
	// Message refills the field after a refusal.
	Message      string
	MessageError i18n.Msg
	// Failure says why a message was not sent.
	Failure i18n.Msg
	// GoConsole and GoServer add the link that leads out of the refusal.
	GoConsole, GoServer bool
	// Announce is read out by #announcer when a script swaps in the panel.
	Announce i18n.Msg
}

func (ChatPage) templateName() string { return "chat" }
func (ChatPage) navItem() string      { return PathServer }
func (p ChatPage) hasErrors() bool    { return !p.MessageError.IsZero() }

func (p ChatPage) Problems() []Problem {
	return problems(Problem{"chat-message", p.MessageError})
}

func (p ChatPage) SendPath() string { return chatPath(p.World.ID, "") }
func (p ChatPage) EventsPath() string {
	return chatPath(p.World.ID, "/events") + "?" + afterParam + "=" + strconv.FormatUint(p.After, 10)
}

// MaxLines is how many chat lines the page keeps, for the script.
func (ChatPage) MaxLines() int { return game.ChatHistoryLines }

// afterParam on the stream's address names the last line the page was drawn
// with.
const afterParam = "after"

// chatEventID numbers a line for the browser's Last-Event-ID: one more than
// its place in the log, so that zero means no line.
func chatEventID(c game.ChatLine) uint64 { return c.Seq + 1 }

// ChatLimit is the longest message, for the field.
func (ChatPage) ChatLimit() int { return game.MaxChatRunes }

// The heading names the world and its state, as on the Server page.
func (p ChatPage) ActiveName() string  { return p.World.Name }
func (p ChatPage) StateWord() i18n.Msg { return stateWord(p.Status.State) }

// liveURL is the Server page's stream: its facts carry the heading's badge.
// The chat lines have a stream of their own.
func (p ChatPage) liveURL() string {
	if !p.HasWorld {
		return ""
	}
	return gamePath(p.World.ID, "events")
}

func (s *server) chatRoutes() {
	if s.game == nil {
		s.mux.HandleFunc("GET "+PathChat, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, PathWorlds, http.StatusSeeOther)
		})
		return
	}
	s.mux.HandleFunc("GET "+PathChat, s.requireUser(s.handleChat))
	s.mux.HandleFunc("POST "+PathChatSend, s.requireUser(s.handleChatSend))
	s.handleLong("GET "+PathChatEvents, s.requireUser(s.handleChatEvents))
}

func (s *server) chatPage(sum world.Summary) ChatPage {
	p := ChatPage{World: sum, HasWorld: true, Status: s.game.Status(sum.ID)}
	p.Rows = s.game.ChatLines(sum.ID)
	for _, c := range p.Rows {
		p.After = max(p.After, chatEventID(c))
	}
	return p
}

func (s *server) handleChat(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, found, err := s.activeWorld(r.Context())
	if err != nil {
		s.fail(w, r, "find the active world", err)
		return
	}
	if !found {
		s.page(w, r, http.StatusOK, u.Username, ChatPage{})
		return
	}
	s.page(w, r, http.StatusOK, u.Username, s.chatPage(sum))
}

// chatRefusal returns the words for the message field's error when err is
// why a message cannot be sent.
func chatRefusal(err error) i18n.Msg {
	switch {
	case errors.Is(err, game.ErrChatEmpty):
		return i18n.M("chat.error.empty")
	case errors.Is(err, game.ErrChatTooLong):
		return i18n.M("chat.error.long", "max", game.MaxChatRunes)
	case errors.Is(err, game.ErrChatNUL):
		return i18n.M("chat.error.nul")
	case errors.Is(err, game.ErrChatLines):
		return i18n.M("chat.error.lines")
	case errors.Is(err, game.ErrChatControl):
		return i18n.M("chat.error.control")
	case errors.Is(err, game.ErrChatCommand):
		return i18n.M("chat.error.slash")
	}
	return i18n.Msg{}
}

// respondChat answers a chat post. A script (htmx) gets the panel alone, and
// always with 200 so that it swaps a refusal in as it does a success.
func (s *server) respondChat(w http.ResponseWriter, r *http.Request, status int, user string, p ChatPage) {
	if r.Header.Get("HX-Request") != "true" {
		s.page(w, r, status, user, p)
		return
	}
	var b strings.Builder
	if err := s.render.Fragment(&b, localeOf(r), p, "chat-response"); err != nil {
		s.fail(w, r, "render chat panel", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(b.String()))
}

// handleChatSend says one message in the game and records it (SPEC 8.5, 12.9).
func (s *server) handleChatSend(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	message := r.PostFormValue("message")
	p := s.chatPage(sum)
	p.Message = message
	err := s.game.SendChat(r.Context(), sum.ID, actor(u), message)
	if msg := chatRefusal(err); !msg.IsZero() {
		p.MessageError, p.GoConsole = msg, errors.Is(err, game.ErrChatCommand)
		s.respondChat(w, r, http.StatusBadRequest, u.Username, p)
		return
	}
	switch {
	case err == nil:
	case errors.Is(err, game.ErrNotRunning):
		p.Failure, p.GoServer = i18n.M("chat.failure.not_running"), true
	case errors.Is(err, game.ErrRCONUnreachable):
		p.Failure = i18n.M("chat.failure.unreachable")
	default:
		s.fail(w, r, "send chat message", err)
		return
	}
	if err != nil {
		p.Announce = labelled(i18n.M("common.error.label"), p.Failure)
		s.respondChat(w, r, http.StatusConflict, u.Username, p)
		return
	}
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, PathChat, http.StatusSeeOther)
		return
	}
	p.Message, p.Announce = "", i18n.M("chat.announce.sent")
	s.respondChat(w, r, http.StatusOK, u.Username, p)
}

// handleChatEvents streams a world's chat as server-sent events the Console's
// log script reads (SPEC 8.5): "lines" holds a JSON array of new chat rows,
// each one list item of HTML drawn and escaped by the same template as the
// page's rows. Each event is numbered with its newest
// line, so a browser that reconnects names it in Last-Event-ID and is sent
// only what came after. Nothing is sent for lines the page already shows,
// because the log is a polite live region and would read them out again. The
// stream ends like the Console's does.
func (s *server) handleChatEvents(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return
	}
	if !s.runs.track() {
		http.Error(w, localeOf(r).T("server.stream.shutting_down"), http.StatusServiceUnavailable)
		return
	}
	defer s.runs.wg.Done()
	// What the browser last saw: its own count after a reconnect, else what
	// the page was drawn with.
	after, _ := strconv.ParseUint(r.URL.Query().Get(afterParam), 10, 64)
	if id, err := strconv.ParseUint(r.Header.Get("Last-Event-ID"), 10, 64); err == nil {
		after = id
	}
	opened := s.now()
	loc := localeOf(r)
	out, err := s.openStream(w, loc)
	if err != nil {
		return
	}
	// Nothing may be sent at first, but the browser must see the stream open.
	if out.rc.Flush() != nil {
		return
	}

	history, feed, stop := s.game.WatchChat(sum.ID)
	defer stop()
	// A number from before Foreman restarted is beyond the new log: start over.
	if after > feed.Next() {
		after = 0
	}
	var missed []game.ChatLine
	for _, c := range history {
		if chatEventID(c) > after {
			missed = append(missed, c)
		}
	}
	if len(missed) > 0 && out.chatLines(missed) != nil {
		return
	}
	beat := time.NewTicker(heartbeatEvery)
	defer beat.Stop()
	for {
		var batch []game.ChatLine
		take := func(up game.Update) {
			if c, ok := feed.Line(up); ok {
				batch = append(batch, c)
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.runs.ctx.Done():
			return
		case up, open := <-feed.Updates:
			if !open {
				return
			}
			take(up)
			// Whatever else is waiting goes in the same event.
			for drained := false; !drained; {
				select {
				case up, open := <-feed.Updates:
					if !open {
						return
					}
					take(up)
				default:
					drained = true
				}
			}
			if len(batch) > 0 && out.chatLines(batch) != nil {
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

// chatLines sends chat lines as one "lines" event numbered with the newest.
func (l *liveStream) chatLines(lines []game.ChatLine) error {
	rows := make([]string, len(lines))
	var id uint64
	for i, c := range lines {
		var b strings.Builder
		if err := l.render.Fragment(&b, l.loc, ChatPage{Rows: []game.ChatLine{c}}, "chat-rows"); err != nil {
			return err
		}
		rows[i] = b.String()
		id = max(id, chatEventID(c))
	}
	data, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	if id != 0 {
		if _, err := l.w.Write([]byte("id: " + strconv.FormatUint(id, 10) + "\n")); err != nil {
			return err
		}
	}
	if err := l.event("lines", string(data)); err != nil {
		return err
	}
	return l.rc.Flush()
}
