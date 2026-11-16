package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	reLinkTag  = regexp.MustCompile(`<a\b[^>]*>`)
	reLinkHref = regexp.MustCompile(`\bhref="([^"]*)"`)
)

// offSite says a link's href points off this site: a fixed https address,
// or the mod portal's address from portalPath.
func offSite(file, href string) bool {
	if file == filepath.Join("templates", "versions.html") && href == "{{.}}" {
		// The release notice's changelog link; changelogURL's address is checked below.
		return strings.HasPrefix(changelogURL("2.0.77"), "https://")
	}
	return strings.HasPrefix(href, "https://") || strings.HasPrefix(href, "{{portalPath ")
}

// DESIGN 7.1: one style rule puts a diagonal arrow after every link that
// opens in a new tab, so a new-tab link must be a link out of Foreman, and a
// link out of Foreman must open in a new tab. Otherwise a link inside
// Foreman would wear the arrow, or a link out would lack it.
func TestNewTabLinksAreLinksOutAndLinksOutOpenInANewTab(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("templates", "*.html"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no templates: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // a fixed template folder
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range reLinkTag.FindAllString(string(b), -1) {
			m := reLinkHref.FindStringSubmatch(tag)
			if m == nil {
				continue
			}
			newTab := strings.Contains(tag, `target="_blank"`)
			switch {
			case newTab && !offSite(f, m[1]):
				t.Errorf("%s: %s opens in a new tab but does not point off the site", f, tag)
			case !newTab && strings.HasPrefix(m[1], "https://"):
				t.Errorf("%s: %s leaves the site but does not open in a new tab", f, tag)
			case newTab && !strings.Contains(tag, `rel="noopener noreferrer"`):
				t.Errorf("%s: %s opens in a new tab without rel=noopener noreferrer", f, tag)
			}
		}
	}
}
