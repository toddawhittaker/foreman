package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/version"
)

// PathPublicModsSettings is where an admin turns the public mods page on
// or off (SPEC 9.16).
const PathPublicModsSettings = "/settings/public-mods"

// AuditPublicModsSet records the choice, with the detail "on" or "off".
const AuditPublicModsSet = "public-mods-set"

// publicModsGuide is the operator guide's section on the proxy rule.
const publicModsGuide = version.Repository + "/blob/main/docs/OPERATOR.md#let-players-reach-the-public-mods-page"

// PublicModsSettingsPage shows whether the public page is on and switches it.
type PublicModsSettingsPage struct {
	On bool
	// Done is the setting just saved, "on" or "off", else empty.
	Done string
}

func (PublicModsSettingsPage) templateName() string { return "publicmodssettings" }
func (PublicModsSettingsPage) navItem() string      { return PathSchedule }

// Path is where the form posts.
func (PublicModsSettingsPage) Path() string { return PathPublicModsSettings }

// PagePath is the public page's address.
func (PublicModsSettingsPage) PagePath() string { return PathPublicMods }

// GuidePath is the operator guide's proxy rule.
func (PublicModsSettingsPage) GuidePath() string { return publicModsGuide }

func (s *server) publicModsSettingsRoutes() {
	s.mux.HandleFunc("GET "+PathPublicModsSettings, s.requireUser(s.handlePublicModsSettings))
	s.mux.HandleFunc("POST "+PathPublicModsSettings, s.requireUser(s.handlePublicModsSet))
}

func (s *server) handlePublicModsSettings(w http.ResponseWriter, r *http.Request, u store.User) {
	p := PublicModsSettingsPage{On: s.publicModsOn(r.Context())}
	if d := r.URL.Query().Get("done"); d == PublicModsOn || d == PublicModsOff {
		p.Done = d
	}
	s.page(w, r, http.StatusOK, u.Username, p)
}

// handlePublicModsSet stores the posted choice, audits it and comes back
// to the page with a Done message.
func (s *server) handlePublicModsSet(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	v := r.PostFormValue("public_mods")
	if v != PublicModsOn && v != PublicModsOff {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	if err := s.setPublicMods(r.Context(), v, u.Username); err != nil {
		s.fail(w, r, "save the public mods setting", err)
		return
	}
	s.audit(r.Context(), u, AuditPublicModsSet, "", v)
	http.Redirect(w, r, PathPublicModsSettings+"?done="+v, http.StatusSeeOther)
}
