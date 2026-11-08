package i18n

import (
	"strings"
	"testing"
)

// A Size argument is shown in the language's units, as Bytes does.
func TestSizeArgument(t *testing.T) {
	m := M("worlds.upload.too_large", "limit", Size(1_500_000))
	if got := English.Format(m); got != "The file is larger than the limit of 1.5 MB." {
		t.Errorf("en: %q", got)
	}
	if got := Locale("de").Format(m); !strings.Contains(got, "1,5 MB") {
		t.Errorf("de: %q", got)
	}
}

// The fragments escape what they wrap, so a name cannot add markup.
func TestFragmentsEscape(t *testing.T) {
	bad := `<b>"x"</b>`
	for name, f := range map[string]Fragment{
		"moment": Moment(bad, bad), "nowrap": NoWrap(bad), "strong": Strong(bad), "codes": English.Codes([]string{bad, "b"}),
	} {
		if strings.Contains(f.html, "<b>") {
			t.Errorf("%s: %s", name, f.html)
		}
	}
	if got := English.Codes([]string{"a", "b"}).html; got != "<code>a</code>, <code>b</code>" {
		t.Errorf("codes: %s", got)
	}
	// The separator is the language's own (common.list.sep).
	if got := Locale("zh-Hans").Codes([]string{"a", "b"}).text; got != "a、b" {
		t.Errorf("zh-Hans codes: %q", got)
	}
	if got := Moment("2026-11-03T14:25:00Z", "2026-11-03 14:25 UTC").html; got != `<time datetime="2026-11-03T14:25:00Z">2026-11-03 14:25 UTC</time>` {
		t.Errorf("moment: %s", got)
	}
}

// Link makes a link only to one of Foreman's own paths; anything else is
// refused, which panics under test so a template cannot ship one.
func TestLinkRefusesForeignHref(t *testing.T) {
	if got := English.Link("/worlds", "common.nav.worlds").html; got != `<a href="/worlds">Worlds</a>` {
		t.Errorf("own path: %s", got)
	}
	for _, href := range []string{"", "worlds", "//evil.example/x", `/\evil.example`, "https://evil.example/", "javascript:alert(1)",
		"/\t/evil.example", "/\n/evil.example", "/a\rb", "/a\x00b", "/a\x7fb"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Link(%q) did not refuse", href)
				}
			}()
			English.Link(href, "common.nav.worlds")
		}()
	}
}

// An ident never hyphenates and is left alone by a page translator; it
// escapes what it wraps (DESIGN 8.3).
func TestIdent(t *testing.T) {
	if got := Ident(`a<b>`).html; got != `<span class="ident" translate="no">a&lt;b&gt;</span>` {
		t.Errorf("ident: %s", got)
	}
	if got := Locale("zh-Hans").Idents([]string{"base", "quality"}); got.text != "base、quality" || strings.Count(got.html, `class="ident"`) != 2 {
		t.Errorf("idents: %+v", got)
	}
}

// An external link opens in a new tab and says so, and only an https
// address is a link.
func TestExternalLink(t *testing.T) {
	got := English.ExternalLink("https://factorio.com/profile", "account.token.profile").html
	want := `<a href="https://factorio.com/profile" target="_blank" rel="noopener noreferrer">your factorio.com profile page<span class="visually-hidden"> (opens in a new tab)</span></a>`
	if got != want {
		t.Errorf("link: %s", got)
	}
	// The address is escaped, so it cannot leave its attribute; the plain
	// form is the link text alone.
	f := English.ExternalLink(`https://x.example/"><b>`, "account.token.profile")
	if strings.Contains(f.html, "<b>") || !strings.Contains(f.html, `href="https://x.example/&#34;&gt;&lt;b&gt;"`) {
		t.Errorf("unescaped href: %s", f.html)
	}
	if f.text != "your factorio.com profile page" {
		t.Errorf("text: %q", f.text)
	}
}
