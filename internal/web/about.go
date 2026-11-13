package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/version"
)

// PathAbout is the About page: the product, its version and commit, the Go
// version, the licence and the project's address (DESIGN 6, "User menu").
// With script the user menu's About item opens the same content in a dialog;
// this page is what the item is without script.
const PathAbout = "/about"

// AboutPage is the About page's view model. The layout draws the dialog from
// the same values (see the "about" template function).
type AboutPage struct {
	Info version.Info
}

func (AboutPage) templateName() string { return "about" }

// navItem lets the layout know it is on this page, which needs no dialog.
func (AboutPage) navItem() string { return PathAbout }

func (s *server) aboutRoutes() {
	s.mux.HandleFunc("GET "+PathAbout, s.requireUser(s.handleAbout))
}

func (s *server) handleAbout(w http.ResponseWriter, r *http.Request, u store.User) {
	s.page(w, r, http.StatusOK, u.Username, AboutPage{Info: version.Get()})
}
