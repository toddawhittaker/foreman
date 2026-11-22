package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
)

// previewPage is a preview's whole page, for a browser without script: step
// 3 with the pictures under its form (SPEC 5.6). Only step 3 has a map form,
// so a preview of no world is refused.
func (s *server) previewPage(w http.ResponseWriter, r *http.Request, u store.User, status int, in previewInput, view *PreviewView) {
	if !in.world {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest, Detail: i18n.M("worlds.generate.preview.failed")})
		return
	}
	p := NewMapPage{World: in.sum, Form: newMapForm(in.sum, in.f.MapForm), CanPreview: true, Preview: view}
	if in.catErr != nil {
		p.Builtin = i18n.M("newmap.builtin.failed")
	}
	s.page(w, r, status, u.Username, p)
}
