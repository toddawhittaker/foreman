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

// SPEC 5.6: a preview, resource count or territory request that
// names no world is refused before any game runs, for htmx too.
func TestAPreviewOfNoWorldRunsNoGame(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.0.77")
	for _, p := range []string{PathGeneratePreview, pathResources, pathTerritories} {
		for _, htmx := range []bool{false, true} {
			req := httptest.NewRequest(http.MethodPost, p, strings.NewReader(generateForm("planet", "nauvis", "auto", "1").Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if htmx {
				req.Header.Set("HX-Request", "true")
			}
			req.AddCookie(c)
			rec := hn.serveLong(req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "map preview") {
				t.Errorf("%s htmx %t: status %d\n%s", p, htmx, rec.Code, rec.Body.String())
			}
		}
	}
	if len(prev.got) != 0 {
		t.Errorf("%d previews ran", len(prev.got))
	}
}
