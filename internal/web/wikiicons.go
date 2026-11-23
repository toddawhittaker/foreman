package web

import (
	"context"
	"net/http"

	"github.com/toddawhittaker/foreman/internal/modsettings"
)

// Game icons from the Factorio wiki are opt-in (SPEC 13.1, 14.10). The
// setting is read once per page and carried in the request's context, so
// the page's headers and its object tags agree.

type wikiIconsKey struct{}

// withWikiIcons records in r whether the wiki icons setting is on.
func withWikiIcons(r *http.Request, on bool) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), wikiIconsKey{}, on))
}

// wikiIconsOf reports whether r's page shows wiki icons; off when unset.
func wikiIconsOf(r *http.Request) bool {
	on, _ := r.Context().Value(wikiIconsKey{}).(bool)
	return on
}

// pageCSP is a signed-in page's Content-Security-Policy: no other site may
// frame it, and it loads images from itself and data: URLs, plus the wiki
// while its icons are on. The wiki images carry an inline onerror handler
// (modsettings.ShowWikiIcons); if a script-src is ever added, it must allow
// that handler with 'unsafe-hashes' and its hash, or the fallback breaks.
func pageCSP(wiki bool) string {
	csp := "frame-ancestors 'none'; img-src 'self' data:"
	if wiki {
		csp += " " + modsettings.WikiHost
	}
	return csp
}

// showWikiIcons turns the object markers in a fragment into wiki images when
// the setting is on, as a whole page does.
func (s *server) showWikiIcons(ctx context.Context, fragment []byte) []byte {
	if !s.wikiIconsOn(ctx) {
		return fragment
	}
	return modsettings.ShowWikiIcons(fragment)
}

// wikiIconsOn reads the setting for a request. A store that cannot be read
// counts as off, so a fault never lets a page contact the wiki.
func (s *server) wikiIconsOn(ctx context.Context) bool {
	if s.store == nil {
		return false
	}
	on, ok := readBoolSetting(ctx, s.store, settingWikiIcons)
	return on && ok
}
