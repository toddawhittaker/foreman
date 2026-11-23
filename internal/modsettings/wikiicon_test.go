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

// testIconIndex is an index with an item, a space location and a starmap
// picture in one mod.
func testIconIndex(t *testing.T) IconIndex {
	t.Helper()
	idx, err := DecodeIconIndex([]byte(`{"mods":{"fake-planet":"1.0.0"},"icons":[
		{"kind":"item","name":"fake-widget","path":"__fake-planet__/graphics/widget.png","size":64},
		{"kind":"space-location","name":"fake-planet","path":"__fake-planet__/graphics/planet.png","size":64},
		{"kind":"starmap","name":"fake-planet","path":"__fake-planet__/graphics/map.png","size":512}]}`))
	if err != nil || idx.Len() != 3 {
		t.Fatalf("index: %d icons, %v", idx.Len(), err)
	}
	return idx
}

// SPEC 14.6: an object tag's marker names the object by kind, and the tag
// "planet" is a space location. Kinds the index is not keyed by, and
// "starmap", leave the marker with no object.
func TestObjectMarkerKinds(t *testing.T) {
	cases := []struct{ tag, want string }{
		{"item", `data-obj="item/fake-widget"`},
		{"fluid", `data-obj="fluid/fake-widget"`},
		{"entity", `data-obj="entity/fake-widget"`},
		{"recipe", `data-obj="recipe/fake-widget"`},
		{"technology", `data-obj="technology/fake-widget"`},
		{"tile", `data-obj="tile/fake-widget"`},
		{"virtual-signal", `data-obj="virtual-signal/fake-widget"`},
		{"space-location", `data-obj="space-location/fake-widget"`},
		{"planet", `data-obj="space-location/fake-widget"`},
	}
	for _, c := range cases {
		got := string(RichTextHTML("["+c.tag+"=fake-widget]", false, RichTextIcons{}))
		if !strings.Contains(got, c.want) {
			t.Errorf("[%s=fake-widget] = %s, want %s", c.tag, got, c.want)
		}
	}
	for _, in := range []string{"[img=starmap/fake-widget]", "[img=bogus/fake-widget]", "[img=Item/fake-widget]"} {
		got := string(RichTextHTML(in, false, RichTextIcons{}))
		if strings.Contains(got, "data-obj") || !strings.Contains(got, `data-wiki="Fake_widget"`) {
			t.Errorf("%s = %s, want a wiki-only marker", in, got)
		}
	}
	if got := string(RichTextHTML("[img=quality/rare]", false, RichTextIcons{})); !strings.Contains(got, `data-obj="quality/rare"`) {
		t.Errorf("[img=quality/rare] = %s", got)
	}
}

// SPEC 14.6, 14.10: the mod's own icon comes first; an object the mods give no
// icon is left for the wiki pass, which leaves the plain name when the
// wiki is off.
func TestShowModIconsPrecedence(t *testing.T) {
	idx := testIconIndex(t)
	in := []byte(RichTextHTML("[item=fake-widget] [planet=fake-planet] [item=iron-plate] [item=Bad Name]", false, RichTextIcons{}))
	got := string(ShowModIcons(in, "w1", idx))
	for _, want := range []string{
		`<img class="rt-obj-icon" src="/worlds/w1/icons/item/fake-widget.png" alt="Fake widget" loading="lazy" onerror="this.replaceWith(this.alt)">`,
		`<img class="rt-obj-icon" src="/worlds/w1/icons/space-location/fake-planet.png" alt="Fake planet" loading="lazy" onerror="this.replaceWith(this.alt)">`,
		`<span class="rt-obj" data-wiki="Iron_plate" data-obj="item/iron-plate">Iron plate</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "wiki.factorio.com") || strings.Contains(got, `referrerpolicy`) {
		t.Errorf("a mod icon image reaches the wiki: %s", got)
	}
	// The wiki pass then handles the rest and never touches the mod icons.
	both := string(ShowWikiIcons([]byte(got)))
	if n := strings.Count(both, "<img"); n != 3 {
		t.Errorf("%d images after both passes, want 3: %s", n, both)
	}
	if !strings.Contains(both, `src="https://wiki.factorio.com/images/Iron_plate.png"`) || !strings.Contains(both, `src="/worlds/w1/icons/item/fake-widget.png"`) {
		t.Errorf("both passes: %s", both)
	}
	if !strings.HasSuffix(both, "Bad Name") {
		t.Errorf("a name outside protoNameRE stays text: %s", both)
	}
}

// ShowModIcons with no icons, or on a page with none, changes nothing; the
// world id is a path segment, so it cannot break out of the address.
func TestShowModIconsEdges(t *testing.T) {
	in := []byte(RichTextHTML("[item=fake-widget]", false, RichTextIcons{}))
	if got := ShowModIcons(in, "w1", IconIndex{}); string(got) != string(in) {
		t.Errorf("an empty index changed the page: %s", got)
	}
	if got := ShowModIcons([]byte("<p>x</p>"), "w1", testIconIndex(t)); string(got) != "<p>x</p>" {
		t.Errorf("a page with no marker changed: %s", got)
	}
	got := string(ShowModIcons(in, `a"b/c`, testIconIndex(t)))
	if !strings.Contains(got, `src="/worlds/a%22b%2Fc/icons/item/fake-widget.png"`) {
		t.Errorf("the world id is not escaped: %s", got)
	}
	forged := RichTextHTML(`<span class="rt-obj" data-wiki="Evil" data-obj="item/fake-widget">x</span>`, false, RichTextIcons{})
	if got := ShowModIcons([]byte(forged), "w1", testIconIndex(t)); strings.Contains(string(got), "<img") {
		t.Errorf("a forged marker became an image: %s", got)
	}
}
