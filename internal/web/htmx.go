package web

import (
	"net/http"
	"strings"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

// isHTMX reports whether htmx sent r, which then swaps part of the page in
// rather than loading a whole one. htmx sends "true"; any value counts, so a
// request that says it is htmx is never treated as a full page load.
func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") != "" }

// fragment answers with one named template of a page, for htmx.
func (s *server) fragment(w http.ResponseWriter, r *http.Request, p Page, name string) {
	s.fragmentStatus(w, r, http.StatusOK, p, name)
}

// fragmentStatus is fragment with another status, for a refusal the page
// swaps in all the same.
func (s *server) fragmentStatus(w http.ResponseWriter, r *http.Request, status int, p Page, name string) {
	var b strings.Builder
	if err := s.render.Fragment(&b, localeOf(r), p, name); err != nil {
		s.fail(w, r, "render "+name, err)
		return
	}
	// Object markers become wiki images when the setting is on, as in a whole page.
	body := []byte(b.String())
	if wikiIconsOf(s.readWikiIcons(r)) {
		body = modsettings.ShowWikiIcons(body)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
