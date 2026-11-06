package web

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

var (
	reControl   = regexp.MustCompile(`(?s)<(a|button)\b([^>]*)>(.*?)</(?:a|button)>`)
	reAriaLabel = regexp.MustCompile(`aria-label="([^"]*)"`)
	reTag       = regexp.MustCompile(`<[^>]*>`)
)

// dropSpan removes every span that opens with open, with what it holds,
// counting nested spans so a fragment inside it does not end it early.
func dropSpan(s, open string) string {
	for {
		i := strings.Index(s, open)
		if i < 0 {
			return s
		}
		depth, j := 0, i
		for j < len(s) {
			switch {
			case strings.HasPrefix(s[j:], "<span"):
				depth++
			case strings.HasPrefix(s[j:], "</span>"):
				depth--
				if depth == 0 {
					s = s[:i] + " " + s[j+len("</span>"):]
					j = -1
				}
			}
			if j < 0 {
				break
			}
			j++
		}
		if j >= len(s) {
			return s
		}
	}
}

func plainText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(html.UnescapeString(reTag.ReplaceAllString(s, " "))), " "))
}

// Every link and button's accessible name holds its visible text unbroken,
// in every language, so a person who says what they see reaches the
// control (WCAG 2.5.3, label in name; SPEC 22.4).
func TestControlNamesContainTheirVisibleText(t *testing.T) {
	r := newTestRenderer(t)
	for _, l := range i18n.Languages {
		for _, c := range pageCases() {
			_, body := renderIn(t, r, c, l.Tag)
			for _, m := range reControl.FindAllStringSubmatch(body, -1) {
				visible := plainText(dropSpan(m[3], `<span class="visually-hidden">`))
				name := plainText(dropSpan(m[3], `<span aria-hidden="true">`))
				if a := reAriaLabel.FindStringSubmatch(m[2]); a != nil {
					name = plainText(a[1])
				}
				if visible != "" && !strings.Contains(name, visible) {
					t.Errorf("%s/%s: %s shows %q but is named %q", l.Tag, c.name, m[1], visible, name)
				}
			}
		}
	}
}
