package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
)

func (s *server) worldRoutes() {
	s.mux.HandleFunc("GET "+PathWorlds, s.requireUser(s.handleWorlds))
}

// handleWorlds shows the world list (SPEC 5.4).
func (s *server) handleWorlds(w http.ResponseWriter, r *http.Request, u store.User) {
	list, err := s.worlds.List(r.Context())
	if err != nil {
		s.fail(w, r, "list worlds", err)
		return
	}
	s.page(w, r, http.StatusOK, u.Username, WorldsPage{Worlds: list})
}
