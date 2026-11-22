package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
)

// worldImportRoutes registers the world file import (SPEC 5.14): the upload
// and the confirmed preview. Task T4 builds the handlers (the upload with
// handleUpload and guardUpload); until then the routes answer 501.
func (s *server) worldImportRoutes() {
	notBuilt := func(w http.ResponseWriter, _ *http.Request, _ store.User) {
		http.Error(w, "not built", http.StatusNotImplemented)
	}
	s.handleLong("POST "+PathWorldFileImport, s.requireUser(notBuilt))
	s.handleLong("POST "+PathWorldFileConfirm, s.requireUser(notBuilt))
}
