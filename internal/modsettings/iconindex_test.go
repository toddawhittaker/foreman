package modsettings

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func iconDoc(mods, entries string) []byte {
	return []byte(`{"mods":{` + mods + `},"icons":[` + entries + `]}`)
}

func entry(kind, name, path string, size any) string {
	b, err := json.Marshal(map[string]any{"kind": kind, "name": name, "path": path, "size": size})
	if err != nil {
		panic(err)
	}
	return string(b)
}

const fakeMods = `"fake-planet":"1.0.0","other":"2.1"`

func TestDecodeIconIndexHappyPath(t *testing.T) {
	in := iconDoc(fakeMods, strings.Join([]string{
		entry("item", "fake-widget", "__fake-planet__/graphics/widget.png", 64),
		entry("starmap", "fake-planet", "__fake-planet__/graphics/planet.png", 512),
		entry("item", "other-thing", "__other__/a/b/c.png", 32.0),
	}, ","))
	x, err := DecodeIconIndex(in)
	if err != nil {
		t.Fatal(err)
	}
	if x.Len() != 3 || x.Dropped != 0 {
		t.Fatalf("len %d, dropped %d", x.Len(), x.Dropped)
	}
	got, ok := x.Lookup("item", "fake-widget")
	want := IconRef{Mod: "fake-planet", Version: "1.0.0", Path: "graphics/widget.png", Size: 64}
	if !ok || got != want {
		t.Errorf("Lookup = %+v, %v; want %+v", got, ok, want)
	}
	if r, ok := x.Lookup("starmap", "fake-planet"); !ok || r.Size != 512 {
		t.Errorf("starmap = %+v, %v", r, ok)
	}
	if r, ok := x.Lookup("item", "other-thing"); !ok || r.Mod != "other" || r.Version != "2.1" || r.Path != "a/b/c.png" {
		t.Errorf("other = %+v, %v", r, ok)
	}
	// Kind matters: the same name under another kind finds nothing.
	if _, ok := x.Lookup("entity", "fake-widget"); ok {
		t.Error("an entity matched an item")
	}
	if _, ok := x.Lookup("item", "missing"); ok {
		t.Error("a missing name matched")
	}
}

func TestIconIndexZeroValueFindsNothing(t *testing.T) {
	var x IconIndex
	if _, ok := x.Lookup("item", "a"); ok || x.Len() != 0 {
		t.Error("the zero index found something")
	}
}

func TestDecodeIconIndexEmptyForms(t *testing.T) {
	for _, in := range []string{`{}`, `{"mods":{},"icons":{}}`, `{"icons":null}`, `{"icons":[]}`} {
		x, err := DecodeIconIndex([]byte(in))
		if err != nil || x.Len() != 0 {
			t.Errorf("%s: %d, %v", in, x.Len(), err)
		}
	}
}

func TestDecodeIconIndexRefusesWholeFile(t *testing.T) {
	many := strings.TrimSuffix(strings.Repeat(entry("item", "a", "__fake-planet__/a.png", 1)+",", MaxIconIndexEntries+1), ",")
	mods := make([]string, MaxIconIndexMods+1)
	for i := range mods {
		mods[i] = fmt.Sprintf(`"m%d":"1.0"`, i)
	}
	tests := []struct {
		name string
		in   []byte
	}{
		{"not json", []byte(`nope`)},
		{"wrong shape", []byte(`{"icons":5}`)},
		{"icons as a keyed object", []byte(`{"icons":{"a":{}}}`)},
		{"mods not strings", []byte(`{"mods":{"a":1}}`)},
		{"size not a number", iconDoc(fakeMods, `{"kind":"item","name":"a","path":"x","size":"big"}`)},
		{"over the byte limit", []byte(`{"x":"` + strings.Repeat("a", MaxIconIndexBytes) + `"}`)},
		{"too many entries", iconDoc(fakeMods, many)},
		{"too many mods", []byte(`{"mods":{` + strings.Join(mods, ",") + `}}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeIconIndex(tt.in); err == nil {
				t.Error("accepted")
			}
		})
	}
}

func TestDecodeIconIndexAtTheEntryLimit(t *testing.T) {
	var es []string
	for i := range MaxIconIndexEntries {
		es = append(es, entry("item", fmt.Sprintf("i%d", i), "__fake-planet__/a.png", 1))
	}
	x, err := DecodeIconIndex(iconDoc(fakeMods, strings.Join(es, ",")))
	if err != nil || x.Len() != MaxIconIndexEntries {
		t.Fatalf("%d, %v", x.Len(), err)
	}
}

func TestDecodeIconIndexErrorsHoldNoFileText(t *testing.T) {
	_, err := DecodeIconIndex([]byte(`{"icons":[{"kind":"secret-text","size":"secret-text"}]}`))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("error = %v", err)
	}
}

func TestDecodeIconIndexDropsBadEntries(t *testing.T) {
	good := entry("item", "keep", "__keepmod__/ok.png", 64)
	long := "__fake-planet__/" + strings.Repeat("a/", MaxIconPathLen) + "x.png"
	tests := []struct {
		name  string
		entry string
		mods  string
	}{
		{"unknown kind", entry("bogus", "a", "__fake-planet__/a.png", 64), fakeMods},
		{"empty kind", entry("", "a", "__fake-planet__/a.png", 64), fakeMods},
		{"upper-case name", entry("item", "Widget", "__fake-planet__/a.png", 64), fakeMods},
		{"empty name", entry("item", "", "__fake-planet__/a.png", 64), fakeMods},
		{"name with a slash", entry("item", "a/b", "__fake-planet__/a.png", 64), fakeMods},
		{"name too long", entry("item", strings.Repeat("a", 101), "__fake-planet__/a.png", 64), fakeMods},
		{"size zero", entry("item", "a", "__fake-planet__/a.png", 0), fakeMods},
		{"size negative", entry("item", "a", "__fake-planet__/a.png", -4), fakeMods},
		{"size over the limit", entry("item", "a", "__fake-planet__/a.png", MaxIconSize+1), fakeMods},
		{"size fractional", entry("item", "a", "__fake-planet__/a.png", 64.5), fakeMods},
		{"no mod prefix", entry("item", "a", "graphics/a.png", 64), fakeMods},
		{"absolute path", entry("item", "a", "/etc/passwd.png", 64), fakeMods},
		{"absolute after the mod", entry("item", "a", "__fake-planet__//etc/a.png", 64), fakeMods},
		{"dot dot", entry("item", "a", "__fake-planet__/../other/a.png", 64), fakeMods},
		{"dot dot deep", entry("item", "a", "__fake-planet__/a/../../a.png", 64), fakeMods},
		{"dot segment", entry("item", "a", "__fake-planet__/./a.png", 64), fakeMods},
		{"empty segment", entry("item", "a", "__fake-planet__/a//b.png", 64), fakeMods},
		{"backslash", entry("item", "a", `__fake-planet__/a\b.png`, 64), fakeMods},
		{"backslash dot dot", entry("item", "a", `__fake-planet__/..\a.png`, 64), fakeMods},
		{"control character", entry("item", "a", "__fake-planet__/a\x01.png", 64), fakeMods},
		{"newline", entry("item", "a", "__fake-planet__/a\n.png", 64), fakeMods},
		{"not png", entry("item", "a", "__fake-planet__/a.jpg", 64), fakeMods},
		{"upper-case png", entry("item", "a", "__fake-planet__/a.PNG", 64), fakeMods},
		{"png then more", entry("item", "a", "__fake-planet__/a.png/x", 64), fakeMods},
		{"path too long", entry("item", "a", long, 64), fakeMods},
		{"no closing marker", entry("item", "a", "__fake-planet/a.png", 64), fakeMods},
		{"no slash after the mod", entry("item", "a", "__fake-planet__a.png", 64), fakeMods},
		{"empty mod", entry("item", "a", "____/a.png", 64), fakeMods},
		{"mod with a dot dot", entry("item", "a", "__..__/a.png", 64), `"..":"1.0"`},
		{"mod with a slash", entry("item", "a", "__a/b__/a.png", 64), `"a/b":"1.0"`},
		{"mod starting with a dot", entry("item", "a", "__.hid__/a.png", 64), `".hid":"1.0"`},
		{"mod name too long", entry("item", "a", "__"+strings.Repeat("m", 201)+"__/a.png", 64), `"` + strings.Repeat("m", 201) + `":"1.0"`},
		{"mod not active", entry("item", "a", "__ghost__/a.png", 64), fakeMods},
		{"mod version bad", entry("item", "a", "__fake-planet__/a.png", 64), `"fake-planet":"1.0.0/../x"`},
		{"mod version empty", entry("item", "a", "__fake-planet__/a.png", 64), `"fake-planet":""`},
		{"mod version one number", entry("item", "a", "__fake-planet__/a.png", 64), `"fake-planet":"1"`},
	}
	for _, b := range []string{"base", "core", "space-age", "quality", "elevated-rails"} {
		tests = append(tests, struct{ name, entry, mods string }{
			"built-in " + b, entry("item", "a", "__"+b+"__/graphics/a.png", 64), `"` + b + `":"2.0.0"`,
		})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, err := DecodeIconIndex(iconDoc(tt.mods+`,"keepmod":"1.0.0"`, good+","+tt.entry))
			if err != nil {
				t.Fatal(err)
			}
			if x.Len() != 1 || x.Dropped != 1 {
				t.Errorf("len %d, dropped %d", x.Len(), x.Dropped)
			}
			if _, ok := x.Lookup("item", "keep"); !ok {
				t.Error("the good entry was lost")
			}
		})
	}
}

func TestDecodeIconIndexFirstEntryWins(t *testing.T) {
	x, err := DecodeIconIndex(iconDoc(fakeMods, entry("item", "a", "__fake-planet__/one.png", 8)+","+entry("item", "a", "__fake-planet__/two.png", 16)))
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := x.Lookup("item", "a"); r.Path != "one.png" || x.Dropped != 1 {
		t.Errorf("%+v, dropped %d", r, x.Dropped)
	}
}

func TestValidIconKind(t *testing.T) {
	for _, k := range []string{"item", "fluid", "recipe", "technology", "entity", "tile", "virtual-signal", "space-location", "quality", "starmap"} {
		if !ValidIconKind(k) {
			t.Errorf("%q refused", k)
		}
	}
	for _, k := range []string{"", "planet", "Item", "item/", "../item"} {
		if ValidIconKind(k) {
			t.Errorf("%q accepted", k)
		}
	}
}

func TestDecodeIconIndexModNameEndingInUnderscore(t *testing.T) {
	in := iconDoc(`"mod_":"1.0.0"`, entry("item", "w", "__mod___/graphics/w.png", 64))
	x, err := DecodeIconIndex(in)
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := x.Lookup("item", "w"); !ok || r.Mod != "mod_" || r.Path != "graphics/w.png" {
		t.Errorf("Lookup = %+v, %v", r, ok)
	}
}
