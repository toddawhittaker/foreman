package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// SPEC 5.6: without script, Preview map on step 3 answers with step 3
// itself, the pictures under its form; only step 3 has a map form, so a
// preview of no world is refused.
func TestPreviewPageWithoutScriptIsStepThree(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	hn.withActive("2.0.77")
	id := hn.previewWorld(generateForm(), c)
	body := hn.postLong(PathGeneratePreview, generateForm(previewWorldField, id, "seed", "9"), c).Body.String()
	for _, want := range []string{`<form method="post" action="` + newMapPath(id) + `"`, `id="newmap-save"`, `<img src="` + PathGeneratePreview + "/"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	req := httptest.NewRequest(http.MethodPost, PathGeneratePreview, strings.NewReader(generateForm().Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(c)
	r := hn.serveLong(req)
	if r.Code != http.StatusBadRequest || strings.Contains(r.Body.String(), `id="generate-form"`) {
		t.Errorf("a preview of no world: status %d", r.Code)
	}
}
