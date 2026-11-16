package web

import (
	"net/http"
)

// Public paths the page loads from and posts to (SPEC 9.16).
const (
	PathPublicStatic   = PathPublic + "static/"
	PathPublicLanguage = PathPublic + "language"
)

// PublicModsPage is the page players download the running world's mods from
// (SPEC 9.16). It shows only what PublicModsData holds.
type PublicModsPage struct {
	PublicModsData
}

func (PublicModsPage) templateName() string { return "publicmods" }
func (PublicModsPage) public()              {}

// publicModsPageRoutes registers the page's style, script and language
// routes; publicModsRoutes registers the page itself.
func (s *server) publicModsPageRoutes() {
	s.handlePublic("GET "+PathPublicStatic, s.render.PublicStatic())
	// The same handling and return check as the user menu's (SPEC 22.2).
	s.handlePublic("POST "+PathPublicLanguage, s.handleLanguage)
}

// handlePublicModsPage shows the running world's mods to anyone. It never
// reads a session (SPEC 14.18).
func (s *server) handlePublicModsPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.publicModsData(r.Context())
	if err != nil {
		s.fail(w, r, "list the mods for the public page", err)
		return
	}
	s.page(w, r, http.StatusOK, "", PublicModsPage{data})
}
