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
		"moment": Moment(bad, bad), "nowrap": NoWrap(bad), "strong": Strong(bad), "codes": Codes([]string{bad, "b"}),
	} {
		if strings.Contains(f.html, "<b>") {
			t.Errorf("%s: %s", name, f.html)
		}
	}
	if got := Codes([]string{"a", "b"}).html; got != "<code>a</code>, <code>b</code>" {
		t.Errorf("codes: %s", got)
	}
	if got := Moment("2026-11-03T14:25:00Z", "2026-11-03 14:25 UTC").html; got != `<time datetime="2026-11-03T14:25:00Z">2026-11-03 14:25 UTC</time>` {
		t.Errorf("moment: %s", got)
	}
}
