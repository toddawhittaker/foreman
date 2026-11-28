package web

import (
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// DESIGN 6, "Toasts": step 4's and Mod settings' result notes are
// toasts that take no room. Each is a save the page did not make, so it is an
// error toast that stays until closed.
func TestGenerateResultNotesAreToasts(t *testing.T) {
	one := func(name string, got []Toast, key string) {
		t.Helper()
		if len(got) != 1 || !got[0].Error || got[0].Text.Key != key {
			t.Errorf("%s: %+v, want one error toast %s", name, got, key)
		}
	}
	if got := (NewMapPage{}).toasts(); got != nil {
		t.Errorf("a plain step 4 made toasts: %+v", got)
	}
	one("reset", NewMapPage{Form: NewMapForm{MapForm: MapForm{Reset: true}}}.toasts(), "worlds.generate.reset_note")
	refilled := NewMapPage{Form: NewMapForm{MapForm: MapForm{Refilled: true, Preset: "death-world"}}}.toasts()
	one("refilled", refilled, "worlds.generate.refilled")
	if len(refilled) == 1 && i18n.English.Format(refilled[0].Text) != "The settings below were filled from the preset “Death world”. Check them, then choose Generate world again." {
		t.Errorf("refilled says %q", i18n.English.Format(refilled[0].Text))
	}
	if got := (ModSettingsPage{}).toasts(); got != nil {
		t.Errorf("a plain Mod settings page made toasts: %+v", got)
	}
	one("no change", ModSettingsPage{NoChange: true}.toasts(), "mods.settings.edit.no_change")
	one("no change in the flow", FlowModSettingsPage{ModSettingsPage: ModSettingsPage{NoChange: true}}.toasts(), "mods.settings.edit.no_change")
}

// DESIGN 6, "Toasts", "Shown once": the Mods step's "just made" toast comes
// from the generated parameter, so it names it and toast.js drops it; a reload
// then shows nothing (SPEC 5.6). The favorite's review does the same (SPEC 5.12).
func TestGeneratedToastsDropTheirParameter(t *testing.T) {
	sum := world.Summary{ID: "fresh", Name: "Fresh", GameVersion: "2.0.1", Unfinished: true}
	for _, c := range []struct {
		name string
		page Page
	}{
		{"mods step", ModsPage{World: sum, Generated: true}},
		{"favorite review", ModPlanPage{World: sum, Generated: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			region := toastRegion(pageHTML(t, c.page))
			if !strings.Contains(region, `data-toast data-drop="`+generatedQuery+`"`) {
				t.Errorf("toast does not drop %q: %q", generatedQuery, region)
			}
		})
	}
}
