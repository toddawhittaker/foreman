package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
)

// worldExportRoutes registers the world file export (SPEC 5.13). Task T3
// builds the handler; until then the route answers 501.
func (s *server) worldExportRoutes() {
	s.handleLong(patternWorldExport, s.requireUser(func(w http.ResponseWriter, _ *http.Request, _ store.User) {
		http.Error(w, "not built", http.StatusNotImplemented)
	}))
}
