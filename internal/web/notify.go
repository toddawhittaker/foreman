package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
)

// PathNotifications is the page that sets where Foreman sends its events.
const PathNotifications = "/settings/notifications"

// NotificationsPage is the notifications page.
type NotificationsPage struct{}

func (NotificationsPage) templateName() string { return "notifications" }
func (NotificationsPage) navItem() string      { return PathNotifications }

func (s *server) notifyRoutes() {
	s.mux.HandleFunc("GET "+PathNotifications, s.requireUser(s.handleNotifications))
}

func (s *server) handleNotifications(w http.ResponseWriter, r *http.Request, u store.User) {
	s.page(w, r, http.StatusOK, u.Username, NotificationsPage{})
}
