package web

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// designReadme describes the bundle ExportDesign writes.
const designReadme = `# Foreman design bundle

Written by ` + "`make design-export`" + `. Do not edit these files: they are
rendered from the same templates as Foreman's component page (GET /design),
so they change when the templates or the stylesheet change.

- One HTML file per component group. Its first line is a
  ` + "`<!-- @dsCard group=\"...\" -->`" + ` marker naming the card group. Each
  file shows its group twice, once in the light theme and once in the dark.
- ` + "`foreman.css`" + ` is Foreman's stylesheet, unchanged. ` + "`vendor/`" + ` holds the
  IBM Plex fonts it loads and their licences (SIL Open Font License 1.1).
  The line icons are data URIs inside the stylesheet, from Lucide
  (` + "`vendor/LUCIDE-LICENSE.txt`" + `, ISC licence). Every link is
  relative, so the folder works opened straight from disk.

The design tokens are the custom properties on ` + "`:root`" + ` at the top of
` + "`foreman.css`" + `. Each colour holds both themes as ` + "`light-dark(light, dark)`" + `,
so ` + "`color-scheme`" + ` alone picks the theme; ` + "`data-theme=\"light\"`" + ` or
` + "`\"dark\"`" + ` on an element forces one. docs/DESIGN.md (sections 2 to 4)
lists every token, and a test fails when the two disagree.
`

// ExportDesign writes the design bundle to dir: each component group as a
// standalone page, with the stylesheet and fonts beside it (DESIGN 7). It
// renders in-process, so no server needs to run.
func ExportDesign(dir string) error {
	r, err := NewRenderer()
	if err != nil {
		return err
	}
	p, err := newDesignPage()
	if err != nil {
		return err
	}
	t, err := r.template(i18n.English, p)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "vendor"), 0o755); err != nil { //nolint:gosec // an output folder for people to open
		return err
	}
	for _, g := range p.Groups {
		var b bytes.Buffer
		// html/template drops comments, so the marker is written here.
		fmt.Fprintf(&b, "<!-- @dsCard group=%q -->\n", g.Card)
		if err := t.ExecuteTemplate(&b, "design-file", p.Section(g)); err != nil {
			return fmt.Errorf("render %s: %w", g.Key, err)
		}
		b.WriteByte('\n')
		if err := writeExportFile(dir, g.Key+".html", b.Bytes()); err != nil {
			return err
		}
	}
	files, err := fs.Glob(staticFS, "static/vendor/*")
	if err != nil {
		return err
	}
	files = append(files, "static/foreman.css")
	for _, f := range files {
		// The fonts, the stylesheet and the licences; not the scripts or SHA256SUMS.
		if ext := path.Ext(f); ext != ".woff2" && ext != ".txt" && ext != ".css" {
			continue
		}
		body, err := staticFS.ReadFile(f)
		if err != nil {
			return err
		}
		if err := writeExportFile(dir, strings.TrimPrefix(f, "static/"), body); err != nil {
			return err
		}
	}
	return writeExportFile(dir, "README.md", []byte(designReadme))
}

func writeExportFile(dir, name string, body []byte) error {
	return os.WriteFile(filepath.Join(dir, filepath.FromSlash(path.Clean(name))), body, 0o644) //nolint:gosec // an output file for people to open
}
