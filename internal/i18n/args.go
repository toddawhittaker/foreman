package i18n

import (
	"html/template"
	"strings"
)

// Size is a number of bytes as a Msg argument. It is shown in the
// language's units, like Locale.Bytes.
type Size uint64

// Moment is a <time> element for a placeholder of HTML, so a sentence can
// hold a time that the page's script shows in the browser's zone. iso is
// the datetime attribute and text what shows without script (SPEC 17.11).
func Moment(iso, text string) Fragment {
	esc := template.HTMLEscapeString
	return Fragment{`<time datetime="` + esc(iso) + `">` + esc(text) + `</time>`}
}

// NoWrap keeps text, such as a size, on one line.
func NoWrap(text string) Fragment {
	return Fragment{`<span class="nowrap">` + template.HTMLEscapeString(text) + `</span>`}
}

// Strong emphasises text, such as a name the person must type.
func Strong(text string) Fragment {
	return Fragment{"<strong>" + template.HTMLEscapeString(text) + "</strong>"}
}

// Codes is a comma-separated run of code elements for a placeholder of
// HTML, such as a list of version numbers.
func Codes(items []string) Fragment {
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = Code(it).html
	}
	return Fragment{strings.Join(parts, ", ")}
}
