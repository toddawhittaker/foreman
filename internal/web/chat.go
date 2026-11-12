package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
)

// PathChat is the Chat tab of the server view.
const PathChat = "/chat"

// ChatPage is the Chat tab.
type ChatPage struct{}

func (ChatPage) templateName() string { return "chat" }
func (ChatPage) navItem() string      { return PathServer }

func (s *server) chatRoutes() {
	if s.game == nil {
		s.mux.HandleFunc("GET "+PathChat, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, PathWorlds, http.StatusSeeOther)
		})
		return
	}
	s.mux.HandleFunc("GET "+PathChat, s.requireUser(s.handleChat))
}

func (s *server) handleChat(w http.ResponseWriter, r *http.Request, u store.User) {
	s.page(w, r, http.StatusOK, u.Username, ChatPage{})
}
