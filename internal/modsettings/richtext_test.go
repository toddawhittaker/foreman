package modsettings

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStripRichText(t *testing.T) {
	cases := []struct{ in, want string }{
		{"[entity=iron-ore] Iron ore", "Iron ore"},
		{"[img=item/coal][item=coal]Coal", "Coal"},
		{"[color=red]Hot[/color] rocks", "Hot rocks"},
		{"[font=default-bold]Bold[/font]", "Bold"},
		{"Plain text", "Plain text"},
		{"Keep [not a tag] text", "Keep [not a tag] text"},
		{"  spaced   out  ", "spaced out"},
		{"[entity=x]", ""},
		{"Ünïcode [virtual-signal=signal-A] ok", "Ünïcode ok"},
	}
	for _, c := range cases {
		if got := StripRichText(c.in); got != c.want {
			t.Errorf("StripRichText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStripRichTextCutsToLimit(t *testing.T) {
	got := StripRichText("[entity=a]" + strings.Repeat("é", 500))
	if n := utf8.RuneCountInString(got); n != MaxLabelChars {
		t.Fatalf("got %d characters, want %d", n, MaxLabelChars)
	}
	if !utf8.ValidString(got) {
		t.Fatal("cut left invalid UTF-8")
	}
}
