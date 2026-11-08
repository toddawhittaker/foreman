package i18n

import (
	"html/template"
	"strings"
	"testing"
)

// Size is a number of bytes as a Msg argument. It is shown in the
// language's units, like Locale.Bytes.
type Size uint64

// Moment is a <time> element for a placeholder of HTML, so a sentence can
// hold a time that the page's script shows in the browser's zone. iso is
// the datetime attribute and text what shows without script (SPEC 17.11).
func Moment(iso, text string) Fragment {
	esc := template.HTMLEscapeString
	return Fragment{`<time datetime="` + esc(iso) + `">` + esc(text) + `</time>`, text}
}

// NoWrap keeps text, such as a size, on one line.
func NoWrap(text string) Fragment {
	return Fragment{`<span class="nowrap">` + template.HTMLEscapeString(text) + `</span>`, text}
}

// Strong emphasises text, such as a name the person must type.
func Strong(text string) Fragment {
	return Fragment{"<strong>" + template.HTMLEscapeString(text) + "</strong>", text}
}

// Codes is a run of code elements, joined by the language's list
// separator, for a placeholder of HTML, such as a list of version numbers.
func (l Locale) Codes(items []string) Fragment {
	sep := l.T("common.list.sep")
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = Code(it).html
	}
	return Fragment{strings.Join(parts, template.HTMLEscapeString(sep)), strings.Join(items, sep)}
}

// Ident is a name a program reads, such as a mod's: it never breaks with a
// hyphen, and a page translator leaves it as it is.
func Ident(s string) Fragment {
	return Fragment{`<span class="ident" translate="no">` + template.HTMLEscapeString(s) + `</span>`, s}
}

// Idents is a run of Idents, joined by the language's list separator.
func (l Locale) Idents(items []string) Fragment {
	sep := l.T("common.list.sep")
	parts := make([]string, len(items))
	for i, it := range items {
		parts[i] = Ident(it).html
	}
	return Fragment{strings.Join(parts, template.HTMLEscapeString(sep)), strings.Join(items, sep)}
}

// ExternalLink is a link to an https address on another site, such as
// factorio.com, whose text is linkKey's translation. It opens in a new tab
// and says so to a screen reader. Any other href is shown as text only, and
// under test it panics, so no page ships one.
func (l Locale) ExternalLink(href, linkKey string) Fragment {
	esc := template.HTMLEscapeString
	text := l.T(linkKey)
	if !strings.HasPrefix(href, "https://") || strings.IndexFunc(href, func(r rune) bool { return r <= 0x20 || r == 0x7f }) >= 0 {
		if testing.Testing() {
			panic("i18n: ExternalLink to an address that is not https: " + href)
		}
		return Fragment{esc(text), text}
	}
	return Fragment{`<a href="` + esc(href) + `" target="_blank" rel="noopener noreferrer">` + esc(text) +
		`<span class="visually-hidden"> ` + esc(l.T("common.link.new_tab")) + `</span></a>`, text}
}
