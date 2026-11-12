package web

import (
	"errors"
	"net/http"
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
	Lines  []game.ChatLine
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

func (p ChatPage) SendPath() string   { return chatPath(p.World.ID, "") }
func (p ChatPage) EventsPath() string { return chatPath(p.World.ID, "/events") }

// ChatLimit is the longest message, for the field.
func (ChatPage) ChatLimit() int { return game.MaxChatRunes }

// Running is true while the game can take a chat message.
func (p ChatPage) Running() bool { return p.Status.State == game.StateRunning }

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
	return ChatPage{World: sum, HasWorld: true, Status: s.game.Status(sum.ID), Lines: s.game.ChatLines(sum.ID)}
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

// handleChatEvents streams a world's chat as server-sent events (SPEC 8.5):
// "chat" holds an out-of-band swap that sets the whole list on connecting and
// appends the lines that follow, so a page that reconnects misses and repeats
// nothing. The HTML is rendered by the template, which escapes every name and
// message. The stream ends like the Console's does.
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
	opened := s.now()
	out, err := s.openStream(w, localeOf(r))
	if err != nil {
		return
	}

	view, updates, stop := s.game.Watch(sum.ID)
	defer stop()
	if out.chat("chat-reset", game.ParseChatLines(view.Lines)) != nil {
		return
	}
	beat := time.NewTicker(heartbeatEvery)
	defer beat.Stop()
	for {
		var batch []game.ChatLine
		take := func(up game.Update) {
			if up.Kind != game.UpdateLine {
				return
			}
			if c, ok := game.ParseChat(up.Line); ok {
				batch = append(batch, c)
			}
		}
		select {
		case <-r.Context().Done():
			return
		case <-s.runs.ctx.Done():
			return
		case up, open := <-updates:
			if !open {
				return
			}
			take(up)
			// Whatever else is waiting goes in the same event.
			for drained := false; !drained; {
				select {
				case up, open := <-updates:
					if !open {
						return
					}
					take(up)
				default:
					drained = true
				}
			}
			if len(batch) > 0 && out.chat("chat-add", batch) != nil {
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

// chat sends chat lines as one event holding a swap of the list.
func (l *liveStream) chat(fragment string, lines []game.ChatLine) error {
	var b strings.Builder
	if err := l.render.Fragment(&b, l.loc, ChatPage{Lines: lines}, fragment); err != nil {
		return err
	}
	if err := l.event("chat", b.String()); err != nil {
		return err
	}
	return l.rc.Flush()
}
