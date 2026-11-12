package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
)

// PathSchedule is the restart schedule page.
const PathSchedule = "/settings/schedule"

// SchedulePage is the restart schedule page.
type SchedulePage struct{}

func (SchedulePage) templateName() string { return "schedule" }
func (SchedulePage) navItem() string      { return PathSchedule }

func (s *server) scheduleRoutes() {
	s.mux.HandleFunc("GET "+PathSchedule, s.requireUser(s.handleSchedule))
}

func (s *server) handleSchedule(w http.ResponseWriter, r *http.Request, u store.User) {
	s.page(w, r, http.StatusOK, u.Username, SchedulePage{})
}
