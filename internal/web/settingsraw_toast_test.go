package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// DESIGN 6, "Toasts": a save the raw editor refuses as a whole is
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

// A save refused because the world is read-only is told once, by the page's
// own read-only note, not by a toast as well.
func TestSettingsRawReadOnlyRefusalIsNotToastedTwice(t *testing.T) {
	why := i18n.M("settings.raw.title", "file", "a", "world", "b")
	if got := (SettingsRawPage{ReadOnly: why, Error: why}).toasts(); got != nil {
		t.Errorf("a read-only refusal made toasts: %+v", got)
	}
}

// A save posted to a read-only world is refused with 409, and the refusal is
// told once: by the page's read-only note, with no toast over it.
func TestSettingsRawReadOnlyPostIsToldOnce(t *testing.T) {
	r := newSettingsRig(t)
	if err := os.WriteFile(filepath.Join(r.dir, "world.json"), []byte(`{"format_version": 99, "name": "Base", "game_version": "2.1.24"}`), 0o600); err != nil { //nolint:gosec // a test temp folder
		t.Fatal(err)
	}
	rec := r.postAs(r.cookie, r.path("/raw/server-settings.json"), url.Values{"content": {"{}"}})
	body := rec.Body.String()
	why := "A newer version of Foreman wrote this world, so this version cannot change it."
	if rec.Code != http.StatusConflict || strings.Count(body, why) != 1 || strings.Contains(body, "data-toast>") {
		t.Errorf("status %d, %d times told, toast %v:\n%s", rec.Code, strings.Count(body, why), strings.Contains(body, "data-toast>"), body)
	}
}
