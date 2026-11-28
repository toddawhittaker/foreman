package web

import (
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// DESIGN 6, "Toasts" (ruling 6): a save the raw editor refuses as a whole is
// an error toast; the editor's own text errors stay at the field, so a page
// with only those has none.
func TestSettingsRawRefusalIsAToast(t *testing.T) {
	if got := (SettingsRawPage{Errors: []Problem{{}}}).toasts(); got != nil {
		t.Errorf("field errors alone made toasts: %v", got)
	}
	why := i18n.M("settings.raw.title")
	got := (SettingsRawPage{Error: why}).toasts()
	if len(got) != 1 || !got[0].Error || got[0].Text.IsZero() {
		t.Fatalf("a refused save: %+v, want one error toast", got)
	}
}
