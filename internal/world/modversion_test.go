package world

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// A mod's title comes from its info.json, cleaned and cut short, since it
// is untrusted text (SPEC 9.15).
func TestParseInfoJSONTitle(t *testing.T) {
	long := strings.Repeat("Ü", maxModTitle+20)
	cases := []struct{ name, doc, want string }{
		{"plain", `{"name":"a","title":"Belt Direction Indicator"}`, "Belt Direction Indicator"},
		{"missing", `{"name":"a"}`, ""},
		{"blank", `{"name":"a","title":"   "}`, ""},
		{"control characters", `{"name":"a","title":"\tTwo\nlines\u0000 "}`, "Two lines"},
		{"bidirectional override", `{"name":"a","title":"Helper\u202eexe.gnp"}`, "Helper exe.gnp"},
		{"isolates and zero-width", `{"name":"a","title":"\u2066A\u2069\u200bB\ufeff"}`, "A  B"},
		{"markup kept as text", `{"name":"a","title":"<b>x</b>"}`, "<b>x</b>"},
		{"long", `{"name":"a","title":"` + long + `"}`, strings.Repeat("Ü", maxModTitle) + "…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			info, ok := parseInfoJSON([]byte(c.doc))
			if !ok {
				t.Fatal("not read")
			}
			if info.Title != c.want {
				t.Errorf("title = %q, want %q", info.Title, c.want)
			}
			if n := utf8.RuneCountInString(info.Title); n > maxModTitle+1 {
				t.Errorf("title has %d characters, over the cap", n)
			}
		})
	}
}

// The mod list carries each zip's and folder's title; one without a title
// has none, so the page falls back to the name.
func TestModsCarryTitles(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModZip(t, dir, "a_1.0.0.zip", "a_1.0.0", `{"name":"a","version":"1.0.0","title":"Alpha Mod"}`)
	putModZip(t, dir, "b_1.0.0.zip", "b_1.0.0", `{"name":"b","version":"1.0.0"}`)
	folder := filepath.Join(dir, ModsDir, "local")
	if err := os.MkdirAll(folder, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "info.json"), []byte(`{"name":"local","version":"0.1.0","title":"My Local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	list, err := m.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, mod := range list {
		got[mod.Name] = mod.Title
	}
	for name, want := range map[string]string{"a": "Alpha Mod", "b": "", "local": "My Local", "base": ""} {
		if got[name] != want {
			t.Errorf("%s title = %q, want %q", name, got[name], want)
		}
	}
}

// CleanText turns control and format characters into spaces without
// cutting; CutText cuts by characters and marks the cut.
func TestCleanTextAndCutText(t *testing.T) {
	if got := CleanText(" a\u202Eb\u200Bc\n "); got != "a b c" {
		t.Errorf("CleanText = %q", got)
	}
	if got := CutText("ééééé", 3); got != "ééé…" {
		t.Errorf("CutText = %q", got)
	}
	if got := CutText("abc", 3); got != "abc" {
		t.Errorf("CutText of a short text = %q", got)
	}
}
