package web

import (
	"bytes"
	"fmt"
)

// stylesheetName is the one stylesheet every page links, under PathStatic.
const stylesheetName = "foreman.css"

// stylesheetPart is one file under static/css and the cascade layer it
// declares.
type stylesheetPart struct {
	File, Layer string
}

// stylesheetParts are the stylesheet's files, one per component or page
// area, in the order they are joined into foreman.css (DESIGN 11). Joining
// happens at startup, so there is no build step (STACK).
var stylesheetParts = []stylesheetPart{
	{"tokens.css", "tokens"},
	{"base.css", "base"},
	{"buttons.css", "components"},
	{"tables.css", "components"},
	{"plates.css", "components"},
	{"forms.css", "components"},
	{"messages.css", "components"},
	{"controls.css", "components"},
	{"shell.css", "components"},
	{"pages/server.css", "pages"},
	{"pages/settings.css", "pages"},
	{"pages/worlds.css", "pages"},
	{"pages/generate.css", "pages"},
	{"pages/mods.css", "pages"},
	{"pages/design.css", "pages"},
}

// stylesheet joins the parts in order. Each starts with a comment naming its
// file, so the browser's inspector leads back to the source.
func stylesheet() ([]byte, error) {
	var b bytes.Buffer
	for _, p := range stylesheetParts {
		body, err := staticFS.ReadFile("static/css/" + p.File)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "/* css/%s */\n", p.File)
		b.Write(body)
		if !bytes.HasSuffix(body, []byte("\n")) {
			b.WriteByte('\n')
		}
	}
	return b.Bytes(), nil
}
