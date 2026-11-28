package web

import "github.com/toddawhittaker/foreman/internal/i18n"

// Step 4's and Mod settings' result notes are toasts, so the form comes back
// with its fields where they were (DESIGN 6, "Toasts"; DESIGN 8.6). Each is a
// save the page did not make, so it is an error toast that stays until closed.

// toasts: the map options were set back to the new version's defaults, or
// filled from a preset the page had not shown yet; either way they are to be
// checked before the save is sent again.
func (p NewMapPage) toasts() []Toast {
	switch {
	case p.Form.Reset:
		return []Toast{{Error: true, Text: i18n.M("worlds.generate.reset_note")}}
	case p.Form.Refilled:
		return []Toast{{Error: true, Text: i18n.M("worlds.generate.refilled", "preset", p.Form.PresetMsg(p.Form.Preset))}}
	}
	return nil
}

// toasts: a save that changed no setting wrote nothing. Step 3 of a new
// world shares it.
func (p ModSettingsPage) toasts() []Toast {
	if p.NoChange {
		return []Toast{{Error: true, Text: i18n.M("mods.settings.edit.no_change")}}
	}
	return nil
}
