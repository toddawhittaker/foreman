package i18n

import (
	"strings"
	"testing"
	"time"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		cookie, header string
		want           Locale
	}{
		{"", "", English},
		{"", "de-DE,de;q=0.9,en;q=0.5", "de"},
		{"fr", "de-DE,de;q=0.9", "fr"},    // the cookie beats the header
		{"zh-CN", "ru", "ru"},             // only an exact tag counts in the cookie
		{"xx", "pl", "pl"},                // a bad cookie is ignored
		{"", "zh-CN,zh;q=0.9", "zh-Hans"}, // browsers send zh-CN
		{"", "zh", "zh-Hans"},
		{"", "ja-JP,ko;q=0.8", English},  // nothing supported
		{"", "not a header;;;", English}, // unreadable
		{"", "cs-CZ", "cs"},
		{"", "es-419", "es"},
		{"", "en-GB,de;q=0.5", English},
		{"", "fr-CA, fr;q=0.9, en;q=0.8", "fr"},
		{"", "pt-BR,ru;q=0.5", "ru"}, // the first supported one wins
		{"zh-Hans", "", "zh-Hans"},
		{"", "zh-TW", "zh-Hans"}, // the matcher rates Simplified above English
		{"", "*", English},
		{"", "de;q=0", English}, // q=0 means "not this"
		{"", strings.Repeat("x-", 2000), English},
		{"<script>", "", English},
		{"", "sr-Latn", English},
		{"", "ru-RU,ru;q=0.9,en-US;q=0.8", "ru"},
		{"", "pl-PL,pl;q=0.9,en-US;q=0.8", "pl"},
		{"", "en-US,en;q=0.9,de;q=0.8", English},
		{"", "es-ES,es;q=0.9,en;q=0.8", "es"},
		{"", "de-AT", "de"},
		{"", "fr-BE", "fr"},
		{"", "zh-Hans-CN", "zh-Hans"},
		{"", "zh-SG", "zh-Hans"},
		{"", "cs", "cs"},
		{"", "sk", English},                             // Slovak is close to Czech, but not offered
		{"", "xx, de;q=0.5", "de"},                      // an unknown tag does not hide a later match
		{"", "tlh, de;q=0.5", "de"},                     // nor does a valid tag with no catalogue
		{"", "ru, !!!", "ru"},                           // a malformed entry does not hide a valid one
		{"", "ru, x", "ru"},                             // a too-short tag is dropped on its own
		{"", "ru;q=abc", English},                       // a bad q-value drops only its entry
		{"", "ru;q=abc, pl;q=0.5", "pl"},                // and the rest still count
		{"", "!!!, de_DE", "de"},                        // underscore form after a bad entry
		{"", "de;q=0.3, xx, fr;q=0.8", "fr"},            // q-values order the survivors
		{"", strings.Repeat("xx,", 40) + "de", English}, // entries past the cap are not read
		{"", strings.Repeat("xx,", 31) + "de", "de"},    // the last one under the cap is
	}
	for _, c := range cases {
		if got := Match(c.cookie, c.header); got != c.want {
			t.Errorf("Match(%q, %q) = %q, want %q", c.cookie, c.header, got, c.want)
		}
	}
}

func TestPluralsSlavic(t *testing.T) {
	want := map[Locale][4]string{ // at 1, 2, 5, 21
		"ru": {"one", "few", "many", "one"},
		"pl": {"one", "few", "many", "many"},
		"cs": {"one", "few", "other", "other"},
	}
	for l, w := range want {
		for i, n := range []int{1, 2, 5, 21} {
			if got := Category(l, n); got != w[i] {
				t.Errorf("%s %d: %s, want %s", l, n, got, w[i])
			}
		}
	}
	got := []string{}
	for _, n := range []int{1, 2, 5, 21} {
		got = append(got, Locale("ru").T("common.duration.minutes", "count", n))
	}
	if strings.Join(got, "|") != "1 минута|2 минуты|5 минут|21 минута" {
		t.Errorf("ru minutes: %v", got)
	}
	if s := Locale("pl").T("common.duration.minutes", "count", 22); s != "22 minuty" {
		t.Errorf("pl 22: %q", s)
	}
	if s := Locale("cs").T("common.duration.minutes", "count", 5); s != "5 minut" {
		t.Errorf("cs 5: %q", s)
	}
}

func TestNestedMsg(t *testing.T) {
	inner := M("common.duration.minutes", "count", 3)
	if got := English.T("common.signedin", "user", inner); got != "Signed in as 3 minutes" {
		t.Errorf("en: %q", got)
	}
	if got := Locale("de").T("common.signedin", "user", inner); got != "Angemeldet als 3 Minuten" {
		t.Errorf("de: %q", got)
	}
	if !(Msg{}).IsZero() || M("x").IsZero() || English.Format(Msg{}) != "" {
		t.Error("IsZero")
	}
}

func TestMissingKeyFallsBack(t *testing.T) {
	if got := Locale("de").T("no.such.key"); got != "no.such.key" {
		t.Errorf("missing everywhere: %q", got)
	}
	if got := Locale("qq").T("common.signout"); got != "Sign out" {
		t.Errorf("unknown locale: %q", got)
	}
}

func TestBytes(t *testing.T) {
	cases := []struct {
		l    Locale
		n    uint64
		want string
	}{
		{English, 512, "512 B"},
		{English, 1500, "1.5 kB"},
		{English, 2_100_000_000, "2.1 GB"},
		{English, 999_950, "1000.0 kB"},
		{"de", 1500, "1,5 kB"},
		{"de", 2_160_000, "2,2 MB"},
		{"de", 999, "999 B"},
		{"fr", 1500, "1,5 ko"},
		{"fr", 3_000_000_000, "3,0 Go"},
	}
	for _, c := range cases {
		if got := c.l.Bytes(c.n); got != c.want {
			t.Errorf("%s Bytes(%d) = %q, want %q", c.l, c.n, got, c.want)
		}
	}
}

func TestNumAndDuration(t *testing.T) {
	if got := Locale("de").Num(12345); got != "12.345" {
		t.Errorf("de Num: %q", got)
	}
	if got := English.Num(12345); got != "12,345" {
		t.Errorf("en Num: %q", got)
	}
	if got := English.Duration(90 * time.Second); got != "1 minute" {
		t.Errorf("Duration: %q", got)
	}
	if got := English.Duration(time.Second); got != "1 second" {
		t.Errorf("Duration: %q", got)
	}
	if got := Locale("zh-Hans").Duration(2 * time.Hour); got != "2 小时" {
		t.Errorf("Duration zh: %q", got)
	}
}

func TestHTML(t *testing.T) {
	cases := []struct {
		pattern string
		args    []any
		want    string
	}{
		// A hostile plain argument and a hostile path are escaped.
		{"Open {link} for <{name}>.", []any{"link", English.Link("/a?b=1&c=2", "common.signout"), "name", "<b>"},
			`Open <a href="/a?b=1&amp;c=2">Sign out</a> for &lt;&lt;b&gt;&gt;.`},
		{"In {file} & more", []any{"file", Code(`x"><script>alert(1)</script>`)},
			`In <code>x&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;</code> &amp; more`},
		// Fragments at the start and the end of the sentence.
		{"{file} is in {link}", []any{"file", Code("world.json"), "link", English.Link("/worlds", "common.nav.worlds")},
			`<code>world.json</code> is in <a href="/worlds">Worlds</a>`},
		// Two fragments, and a plain Msg argument translated then escaped.
		{"{a} and {b}, {m}", []any{"a", Code("a"), "b", Code("b"), "m", M("common.signout")},
			`<code>a</code> and <code>b</code>, Sign out`},
	}
	for _, c := range cases {
		catalogue["en"]["test.html"] = entry{text: c.pattern}
		got := English.HTML("test.html", c.args...)
		if string(got) != c.want {
			t.Errorf("%q:\n got %s\nwant %s", c.pattern, got, c.want)
		}
	}
	delete(catalogue["en"], "test.html")
}

func TestParse(t *testing.T) {
	for _, l := range Languages {
		if got, ok := Parse(string(l.Tag)); !ok || got != l.Tag {
			t.Errorf("Parse(%q)", l.Tag)
		}
	}
	for _, s := range []string{"", "EN", "de-DE", "zh", "zh-hans", "xx"} {
		if _, ok := Parse(s); ok {
			t.Errorf("Parse(%q) accepted", s)
		}
	}
}
