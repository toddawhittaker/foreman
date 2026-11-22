package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
)

// factoryMapRoutes registers the factory map page and its pictures (SPEC
// 5.16). Task F7 builds the handlers; until then the page, which the Factory
// map tab links to, answers 501 "not built".
func (s *server) factoryMapRoutes() {
	s.mux.HandleFunc("GET "+PathWorlds+"/{id}/map", s.requireUser(func(w http.ResponseWriter, _ *http.Request, _ store.User) {
		http.Error(w, "not built", http.StatusNotImplemented)
	}))
}
