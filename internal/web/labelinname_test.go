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

var (
	reTable = regexp.MustCompile(`(?s)<table\b([^>]*)>(.*?)</table>`)
	reCell  = regexp.MustCompile(`<(td|th|tr|thead|tbody)\b([^>]*)>`)
)

// Every table keeps its roles when the stylesheet stacks it on a narrow
// screen, and each value cell past the first column carries its column's
// name, in every language (SPEC 17.12).
func TestTablesKeepRolesAndLabels(t *testing.T) {
	want := map[string]string{"tr": `role="row"`, "thead": `role="rowgroup"`, "tbody": `role="rowgroup"`, "th": `role="columnheader"`}
	r := newTestRenderer(t)
	for _, l := range i18n.Languages {
		for _, c := range pageCases() {
			_, body := renderIn(t, r, c, l.Tag)
			for _, tm := range reTable.FindAllStringSubmatch(body, -1) {
				if !strings.Contains(tm[1], `role="table"`) {
					t.Errorf("%s/%s: table without role=table", l.Tag, c.name)
				}
				for _, rm := range strings.Split(tm[2], "<tr")[1:] {
					col := 0
					for _, m := range reCell.FindAllStringSubmatch("<tr"+rm, -1) {
						if w, ok := want[m[1]]; ok && !strings.Contains(m[2], w) {
							t.Errorf("%s/%s: <%s> lacks %s", l.Tag, c.name, m[1], w)
						}
						if m[1] == "td" {
							col++
							if !strings.Contains(m[2], `role="cell"`) || (col > 1 && !strings.Contains(m[2], "data-label=")) {
								t.Errorf("%s/%s: cell %d lacks role or data-label: %s", l.Tag, c.name, col, m[0])
							}
						}
					}
				}
			}
		}
	}
}
