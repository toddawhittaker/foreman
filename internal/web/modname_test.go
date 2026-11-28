package web

import (
	"bytes"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// renderModName renders the mod-name partial for one input.
func renderModName(t *testing.T, m ModName) string {
	t.Helper()
	r := newTestRenderer(t)
	var b bytes.Buffer
	if err := r.pages[i18n.English]["mods"].ExecuteTemplate(&b, "mod-name", m); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// A mod is shown by its title, linked to its portal page in a new tab, and
// never translated (SPEC 9.15).
func TestModNameShowsTheLinkedTitle(t *testing.T) {
	got := renderModName(t, modName("belt-direction", "Belt Direction", true))
	for _, want := range []string{
		`<a href="https://mods.factorio.com/mod/belt-direction" class="mod-title" target="_blank" rel="noopener noreferrer">`,
		`<span translate="no">Belt Direction</span>`,
		`<span class="visually-hidden"> on the mod portal, opens in a new tab</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, ">belt-direction<") {
		t.Errorf("shows the slug: %s", got)
	}
}

// With no title the slug shows, kept whole as a name.
func TestModNameFallsBackToTheSlug(t *testing.T) {
	for _, title := range []string{"", "  "} {
		got := renderModName(t, modName("aai-loaders", title, true))
		if !strings.Contains(got, `<span class="ident" translate="no">aai-loaders</span>`) {
			t.Errorf("title %q: %s", title, got)
		}
	}
}

// Built-in mods, folder mods and mods the portal lacks get no link.
func TestModNameExceptionsAreNotLinked(t *testing.T) {
	cases := []struct {
		what string
		m    ModName
	}{
		{"built-in base", modName("base", "", true)},
		{"built-in Space Age", modName("space-age", "Space Age", true)},
		{"built-in quality", modName("quality", "", true)},
		{"built-in elevated rails", modName("elevated-rails", "", true)},
		{"folder mod", modName("local", "My Local", false)},
		{"not on the portal", modName("gone", "", false)},
	}
	for _, c := range cases {
		got := renderModName(t, c.m)
		if strings.Contains(got, "<a ") {
			t.Errorf("%s is linked: %s", c.what, got)
		}
		if !strings.Contains(got, `translate="no"`) || !strings.Contains(got, c.m.Title) {
			t.Errorf("%s: %s", c.what, got)
		}
	}
}

// A title is untrusted: markup in it shows as text.
func TestModNameEscapesTheTitle(t *testing.T) {
	got := renderModName(t, modName("x", `<img src=x onerror=alert(1)>`, true))
	if strings.Contains(got, "<img") || !strings.Contains(got, "&lt;img") {
		t.Errorf("not escaped: %s", got)
	}
}
