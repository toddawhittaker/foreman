package web

import (
	"net/url"
	"strings"
	"testing"
)

// SPEC 11.2, 17: a raw edit refused for lines hands the page those lines in
// the order of the summary links, so a script can put the caret on the named
// one; the field is described by the summary heading, not the whole summary.
func TestSettingsRawEditorNamesTheLinesForTheCaret(t *testing.T) {
	r := newSettingsRig(t)
	path := r.path("/raw/server-settings.json")

	form := r.getAs(r.cookie, path).Body.String()
	if !strings.Contains(form, "settingsraw.js") {
		t.Errorf("the raw editor does not load its script:\n%s", form)
	}
	if strings.Contains(form, "data-error-lines") {
		t.Error("an editor with no error names lines")
	}
	if strings.Contains(r.getAs(r.cookie, r.path("")).Body.String(), "settingsraw.js") {
		t.Error("the form page loads the raw editor's script")
	}

	bad := "{\n  \"name\": \"x\",\n  \"max_players\": \"ten\",\n  \"auto_pause\": 3,\n  \"description\": \"\",\n  \"visibility\": {\"public\": false, \"lan\": true}\n}\n"
	body := r.postAs(r.cookie, path, url.Values{"content": {bad}}).Body.String()
	for _, want := range []string{`data-error-lines="3,4"`, `aria-describedby="content-hint content-secrets error-summary-heading"`} {
		if !strings.Contains(body, want) {
			t.Errorf("refused editor lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `content-secrets error-summary"`) {
		t.Error("the field is described by the whole summary")
	}

	// A missing key has no line of its own.
	body = r.postAs(r.cookie, path, url.Values{"content": {"{\n  \"name\": \"x\"\n}\n"}}).Body.String()
	if !strings.Contains(body, `data-error-lines="0`) {
		t.Errorf("a missing key was given a line:\n%s", body)
	}
}
