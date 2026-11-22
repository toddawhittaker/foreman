package web

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// worldPreview posts a preview of a new 2.1 world whose catalogue is cat.
func worldPreview(t *testing.T, prev *fakePreviewer, cat mapgen.Catalogue, catErr error, extra ...string) (*harness, string, string) {
	t.Helper()
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	withWorldCatalogue(t, cat, catErr)
	id, err := hn.worlds.Create(t.Context(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	rec := hn.postLong(PathGeneratePreview, worldPreviewForm(id, extra...), c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	return hn, id, rec.Body.String()
}

// SPEC 5.6, 14.6, 22: a modded planet's picture is named with its mod's
// label, marked translate="no" and escaped; a built-in one keeps Foreman's.
func TestPreviewNamesAModdedPlanet(t *testing.T) {
	cat := cerysCatalogue()
	cat.Planets[len(cat.Planets)-1].Label = `<i>Cerys</i>`
	_, _, body := worldPreview(t, &fakePreviewer{}, cat, nil, "planet", "cerys")
	if !strings.Contains(body, `<figcaption><span translate="no">&lt;i&gt;Cerys&lt;/i&gt;</span></figcaption>`) {
		t.Errorf("no foreign caption in:\n%s", body)
	}
	if !strings.Contains(body, `<option value="cerys" translate="no">&lt;i&gt;Cerys&lt;/i&gt;</option>`) {
		t.Error("the planet list does not mark Cerys translate=\"no\"")
	}
	if !strings.Contains(body, `<summary><span translate="no">&lt;i&gt;Cerys&lt;/i&gt;</span></summary>`) {
		t.Error("the settings row of Cerys is not marked translate=\"no\"")
	}
	if strings.Contains(body, "<i>Cerys") {
		t.Error("the mod's label is not escaped")
	}
}

// SPEC 5.6: a world's preview says under its heading what the fast preview
// cannot show, with a toggletip.
func TestPreviewSaysWhatItCannotShow(t *testing.T) {
	_, _, body := worldPreview(t, &fakePreviewer{}, cerysCatalogue(), nil, "planet", "nauvis")
	line := html.EscapeString(i18n.English.T("mapgen.preview.limit"))
	if !strings.Contains(body, `id="preview-limit-line"`) || !strings.Contains(body, line) || !strings.Contains(body, `id="preview-limit-tip"`) {
		t.Errorf("no limit line and toggletip in:\n%s", body)
	}
}

// SPEC 5.6: when the world's planet list cannot be read, the preview says
// it shows the game's own planets.
func TestPreviewSaysWhenThePlanetListFailed(t *testing.T) {
	_, _, body := worldPreview(t, &fakePreviewer{}, mapgen.Builtin("2.1", true), errors.New("planet run failed"), "planet", "nauvis")
	if !strings.Contains(body, `id="preview-fallback"`) || !strings.Contains(body, html.EscapeString(i18n.English.T("mapgen.preview.fallback"))) {
		t.Errorf("no fallback note in:\n%s", body)
	}
	_, _, body = worldPreview(t, &fakePreviewer{}, mapgen.Builtin("2.1", true), nil, "planet", "nauvis")
	if strings.Contains(body, `id="preview-fallback"`) {
		t.Error("the fallback note shows with the world's own planets")
	}
}

// SPEC 5.6: a picture of about one color says the start is outside the map.
func TestPreviewNotesAOneColorPicture(t *testing.T) {
	_, _, body := worldPreview(t, &fakePreviewer{png: solidPNG(t, 64, false)}, cerysCatalogue(), nil, "planet", "nauvis")
	if !strings.Contains(body, `class="hint preview-one-color"`) {
		t.Errorf("no one-color note in:\n%s", body)
	}
	_, _, body = worldPreview(t, &fakePreviewer{png: solidPNG(t, 64, true)}, cerysCatalogue(), nil, "planet", "nauvis")
	if strings.Contains(body, `preview-one-color`) {
		t.Error("a two-color picture has the note")
	}
}

// SPEC 5.6, ADR 0033: a planet list unread because a world mod is named
// like Foreman's helper says so in the fallback note.
func TestPreviewFallbackNamesTheHelperModClash(t *testing.T) {
	_, _, body := worldPreview(t, &fakePreviewer{}, mapgen.Builtin("2.1", true), fmt.Errorf("%w: %w", game.ErrModSettingsBadOutput, game.ErrControlModTaken), "planet", "nauvis")
	if !strings.Contains(body, `id="preview-fallback"`) || !strings.Contains(body, html.EscapeString(i18n.English.T("mapgen.preview.control_mod_taken"))) {
		t.Errorf("no clash note in:\n%s", body)
	}
}

// SPEC 5.6, ADR 0033: a world mod named like Foreman's helper mod has its
// own message.
func TestPreviewSaysTheHelperModNameIsTaken(t *testing.T) {
	_, _, body := worldPreview(t, &fakePreviewer{err: game.ErrControlModTaken}, cerysCatalogue(), nil, "planet", "nauvis")
	if !strings.Contains(body, html.EscapeString(i18n.English.T("mapgen.preview.control_mod_taken"))) {
		t.Errorf("no helper-mod message in:\n%s", body)
	}
}

// SPEC 5.6: Space Age comes from the world's catalogue, which says whether
// its mods enable it.
func TestWorldPreviewTakesSpaceAgeFromTheCatalogue(t *testing.T) {
	prev := &fakePreviewer{}
	cat := mapgen.Builtin("2.1", false)
	worldPreview(t, prev, cat, nil, "map_key", "2.1|0")
	if len(prev.got) != 1 || prev.got[0].SpaceAge {
		t.Errorf("runs %+v: want one without Space Age", prev.got)
	}
}

// SPEC 5.6: a preview of step 3's settings that has a problem answers the
// preview slot alone, never Generate's form in place of step 3's.
func TestStep3PreviewProblemStaysInThePreview(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	withWorldCatalogue(t, cerysCatalogue(), nil)
	id, err := hn.worlds.Create(t.Context(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	rec := hn.postLong(PathGeneratePreview, worldPreviewForm(id, "seed", "not a number"), c, withHeader("HX-Request", "true"))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Retarget") != "" || !strings.Contains(body, `data-preview="invalid"`) {
		t.Errorf("status %d, retarget %q:\n%s", rec.Code, rec.Header().Get("HX-Retarget"), body)
	}
	if strings.Contains(body, `id="generate-form"`) || len(prev.got) != 0 {
		t.Errorf("a form came back, or %d runs", len(prev.got))
	}
}
