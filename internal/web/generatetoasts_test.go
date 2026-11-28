package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// DESIGN 6, "Toasts" (ruling 6): step 4's and Mod settings' result notes are
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
