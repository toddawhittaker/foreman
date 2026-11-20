package web

import (
	"context"
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/updatecheck"
	"github.com/toddawhittaker/foreman/internal/version"
)

// PathAbout is the About page: the product, its version and commit, the Go
// version, the licence and the project's address (DESIGN 6, "User menu").
// With script the user menu's About item opens the same content in a dialog;
// this page is what the item is without script.
const PathAbout = "/about"

// UpdateCheck is the background check for a newer Foreman release
// (SPEC 14.10). *updatecheck.Checker implements it.
type UpdateCheck interface {
	// Newer is the plain version of a newer release, or empty; empty too
	// while the admin switch is off.
	Newer(context.Context) string
	// CheckSoon asks GitHub now, in the background.
	CheckSoon()
}

// AboutInfo is what About shows: this build, and a newer release when the
// update check found one (SPEC 17).
type AboutInfo struct {
	version.Info
	// Newer is the plain version of a newer release, else empty.
	Newer string
}

// NewerURL is the newer release's page, built from its checked version
// rather than any address GitHub sent.
func (a AboutInfo) NewerURL() string { return updatecheck.ReleaseTagURL + a.Newer }

// aboutOf is the "about" template function, which the layout's dialog uses.
func aboutOf(newer string) AboutInfo { return AboutInfo{Info: version.Get(), Newer: newer} }

// AboutPage is the About page's view model.
type AboutPage struct {
	Info AboutInfo
}

func (AboutPage) templateName() string { return "about" }

// navItem lets the layout know it is on this page, which needs no dialog.
func (AboutPage) navItem() string { return PathAbout }

func (s *server) aboutRoutes() {
	s.mux.HandleFunc("GET "+PathAbout, s.requireUser(s.handleAbout))
}

func (s *server) handleAbout(w http.ResponseWriter, r *http.Request, u store.User) {
	s.page(w, r, http.StatusOK, u.Username, AboutPage{Info: aboutOf(s.newerRelease(r, u.Username))})
}

// newerRelease is the newer Foreman release About names for a signed-in
// user, else empty. It reads memory and the switch, never GitHub.
func (s *server) newerRelease(r *http.Request, user string) string {
	if user == "" || s.updates == nil {
		return ""
	}
	return s.updates.Newer(r.Context())
}
