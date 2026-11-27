package web

import (
	"net/http"

	"github.com/toddawhittaker/foreman/internal/store"
)

// previewCarrier is step 3's page holding a preview's answer, for the
// fragments that answer the page's requests and for its whole page without
// script (SPEC 5.6).
func previewCarrier(in previewInput, view *PreviewView) NewMapPage {
	p := NewMapPage{World: in.sum, Form: newMapForm(in.sum, in.f.MapForm), CanPreview: true, Preview: view}
	if in.catErr != nil {
		p.Builtin, _ = builtinFailure(nil, in.sum.ID, in.catErr)
	}
	return p
}

// previewPage is a preview's whole page, for a browser without script: step
// 3 with the pictures under its form (SPEC 5.6).
func (s *server) previewPage(w http.ResponseWriter, r *http.Request, u store.User, status int, in previewInput, view *PreviewView) {
	s.page(w, r, status, u.Username, previewCarrier(in, view))
}
