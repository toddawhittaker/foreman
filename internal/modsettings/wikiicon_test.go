package modsettings

import (
	"strings"
	"testing"
)

// TestWikiIconFile pins how a prototype name becomes the wiki's file name,
// and that only a plain prototype name ever does (SPEC 14.10).
func TestWikiIconFile(t *testing.T) {
	good := map[string]string{
		"space-science-pack": "Space_science_pack",
		"iron-ore":           "Iron_ore",
		"a_b":                "A_b",
		"nauvis":             "Nauvis",
		"2x-thing":           "2x_thing",
	}
	for in, want := range good {
		if got, ok := WikiIconFile(in); !ok || got != want {
			t.Errorf("WikiIconFile(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	bad := []string{
		"", "Signal-A", "a b", "../etc", "a/b", `a"onerror="x`, "<b>", "a.png", "é", "a?b", "a#b", "a%2fb",
		strings.Repeat("a", 101),
	}
	for _, in := range bad {
		if got, ok := WikiIconFile(in); ok {
			t.Errorf("WikiIconFile(%q) = %q, true; want false", in, got)
		}
	}
}

// TestShowWikiIcons: with the setting on, a marker becomes an image whose
// address is built from the validated file only; text a mod supplies stays
// escaped text.
func TestShowWikiIcons(t *testing.T) {
	in := RichTextHTML("needs [item=space-science-pack] and [item=Bad Name] and [entity=<b>]", false, RichTextIcons{})
	got := string(ShowWikiIcons([]byte(in)))
	want := `needs <img class="rt-obj-icon" src="https://wiki.factorio.com/images/Space_science_pack.png" alt="Space science pack" referrerpolicy="no-referrer" loading="lazy" onerror="this.replaceWith(this.alt)"> and Bad Name and &lt;b&gt;`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if n := strings.Count(got, "<img"); n != 1 {
		t.Errorf("%d images, want 1", n)
	}
}

// TestShowWikiIconsLeavesOtherText: a page without markers is unchanged, and
// a mod's own text cannot forge a marker, because its "<" is escaped.
func TestShowWikiIconsLeavesOtherText(t *testing.T) {
	plain := []byte(`<p>hello <span class="x">a</span></p>`)
	if got := ShowWikiIcons(plain); string(got) != string(plain) {
		t.Errorf("changed %s to %s", plain, got)
	}
	forged := RichTextHTML(`<span class="rt-obj" data-wiki="Evil">x</span>`, false, RichTextIcons{})
	if got := ShowWikiIcons([]byte(forged)); strings.Contains(string(got), "<img") {
		t.Errorf("a forged marker became an image: %s", got)
	}
}
