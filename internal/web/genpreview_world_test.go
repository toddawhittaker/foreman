package web

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/resource"
)

// cerysCatalogue is the 2.1 Space Age planets plus a modded planet, Cerys,
// with one modded resource control.
func cerysCatalogue() mapgen.Catalogue {
	cat := mapgen.Builtin("2.1", true)
	ctl := mapgen.CatalogueControl{
		Control: mapgen.Control{Name: "cerys_ore", Category: mapgen.CategoryResource, Richness: true},
		Label:   "Cerys ore", Planets: []string{"cerys"},
	}
	controls := map[string]mapgen.CatalogueControl{"cerys_ore": ctl}
	for k, v := range cat.Controls {
		controls[k] = v
	}
	cat.Controls = controls
	cat.Planets = append(slices.Clone(cat.Planets), mapgen.Planet{Name: "cerys", Label: "Cerys", Controls: []string{"cerys_ore"}})
	cat.Resources = append(slices.Clone(cat.Resources), mapgen.Resource{Name: "cerys-ore-entity", Control: "cerys_ore", Label: "Cerys ore"})
	return cat
}

// withWorldCatalogue makes every world's catalogue cat.
func withWorldCatalogue(t *testing.T, cat mapgen.Catalogue, err error) {
	t.Helper()
	old := worldCatalogue
	worldCatalogue = func(*server, context.Context, string, i18n.Locale) (mapgen.Catalogue, error) { return cat, err }
	t.Cleanup(func() { worldCatalogue = old })
}

// worldPreviewForm is a preview form for world id: no version or name
// fields, as step 3 posts them.
func worldPreviewForm(id string, extra ...string) url.Values {
	f := url.Values{"world": {id}, "mode": {"settings"}, "preset": {"default"}, "filled_from": {"default"}, "seed": {"42"}, "map_key": {"2.1|1"}}
	for i := 0; i+1 < len(extra); i += 2 {
		f.Set(extra[i], extra[i+1])
	}
	return f
}

// SPEC 5.6, 14.3, 14.6, ADR 0033: with a world the planets come from its
// catalogue alone; the request names the world and its planets, a planet
// outside them is refused before anything runs, and a planet list the form
// posts is never trusted.
func TestWorldPreviewTakesPlanetsFromTheWorld(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	withWorldCatalogue(t, cerysCatalogue(), nil)
	id, err := hn.worlds.Create(t.Context(), "Modded", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	form := worldPreviewForm(id, "planet", "cerys", "known_planets", "mars", "planets", "mars")
	rec := hn.postLong(PathGeneratePreview, form, c)
	if rec.Code != http.StatusOK || len(prev.got) != 1 {
		t.Fatalf("status %d, runs %d:\n%s", rec.Code, len(prev.got), rec.Body.String())
	}
	got := prev.got[0]
	want := cerysCatalogue().PlanetNames()
	if got.World != id || !slices.Equal(got.KnownPlanets, want) || !slices.Equal(got.Planets, []string{"cerys"}) {
		t.Errorf("request %+v, want world %s, known %v, planets [cerys]", got, id, want)
	}
	if got.GameVersion != "2.1.24" || !got.SpaceAge || got.Seed != 42 {
		t.Errorf("request version %q, Space Age %t, seed %d: not the world's", got.GameVersion, got.SpaceAge, got.Seed)
	}
	for _, p := range []string{"mars", "scrap-world"} {
		form.Set("planet", p)
		if rec := hn.postLong(PathGeneratePreview, form, c); rec.Code != http.StatusBadRequest || len(prev.got) != 1 {
			t.Errorf("planet %q outside the catalogue: status %d, %d runs", p, rec.Code, len(prev.got))
		}
	}
	// The resource count refuses it too.
	form.Set("planet", "mars")
	if rec := hn.postLong(pathResources, form, c); rec.Code != http.StatusBadRequest {
		t.Errorf("resources of a planet outside the catalogue: status %d", rec.Code)
	}
}

// SPEC 5.6: a world that does not exist is not found, and nothing runs.
func TestWorldPreviewOfAnUnknownWorld(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	withWorldCatalogue(t, cerysCatalogue(), nil)
	for _, p := range []string{PathGeneratePreview, pathResources, pathTerritories} {
		rec := hn.postLong(p, worldPreviewForm("00000000000000000000000000000000", "planet", "nauvis"), c)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, rec.Code)
		}
	}
	if len(prev.got) != 0 {
		t.Errorf("%d runs for an unknown world", len(prev.got))
	}
}

// SPEC 5.6, ADR 0033: the same settings with a world's mods make another
// map, so its pictures are not the base game's; a modded planet's own
// control leaves the other planets' keys alone.
func TestWorldPreviewKeyHasTheMods(t *testing.T) {
	cat := cerysCatalogue()
	req := spaceAgeReq(t, map[string]any{"iron-ore": map[string]any{"size": 1}})
	base := builtinKey(req)
	req.World = "w1"
	one := settingsKey(req, cat, "mods-a")
	other := settingsKey(req, cat, "mods-b")
	if one.all == base.all || one.planet("nauvis") == base.planet("nauvis") {
		t.Error("a world's preview shares the base game's key")
	}
	if one.planet("nauvis") == other.planet("nauvis") {
		t.Error("other mods share the key")
	}
	if _, ok := one.planets["cerys"]; !ok {
		t.Fatal("the modded planet has no key of its own")
	}
	bigger := spaceAgeReq(t, map[string]any{"iron-ore": map[string]any{"size": 1}, "cerys_ore": bigControl})
	bigger.World = "w1"
	changed := settingsKey(bigger, cat, "mods-a")
	if changed.planet("cerys") == one.planet("cerys") {
		t.Error("Cerys's own control leaves its key alone")
	}
	if changed.planet("nauvis") != one.planet("nauvis") {
		t.Error("Cerys's control changes Nauvis's key")
	}
}

// SPEC 5.6, 14.6: a modded resource is labelled with the name its mod
// gives it, resolved in the catalogue; a built-in one keeps Foreman's own.
func TestResourceNameOfAModdedResource(t *testing.T) {
	cat := cerysCatalogue()
	if got := resourceName(i18n.English, cat, "cerys", "cerys-ore-entity"); got != "Cerys ore" {
		t.Errorf("modded resource named %q, want Cerys ore", got)
	}
	if got, want := resourceName(i18n.English, cat, "nauvis", "iron-ore"), resourceName(i18n.English, mapgen.Builtin("2.1", true), "nauvis", "iron-ore"); got != want {
		t.Errorf("iron ore named %q with mods, %q without", got, want)
	}
	v := resourceView(i18n.English, cat, "cerys", resource.Result{
		Patches:   []resource.Patch{{Name: "cerys-ore-entity", Amount: 5}},
		Resources: map[string]resource.Resource{"cerys-ore-entity": {Total: 5}},
	}, 1280)
	if len(v.Lists) != 1 || !strings.Contains(v.Lists[0].Total, "Cerys ore") {
		t.Errorf("lists %+v", v.Lists)
	}
}

// SPEC 5.6, ADR 0033: a world mod named like Foreman's helper, or a request
// the world's game refuses, gives the failed message and is logged.
func TestWorldPreviewRefusalsAreWorded(t *testing.T) {
	for _, e := range []error{game.ErrControlModTaken, game.ErrPreviewInvalid} {
		prev := &fakePreviewer{err: fmt.Errorf("%w: x", e)}
		hn := newGenerateHarness(t, prev)
		c := hn.signIn()
		withWorldCatalogue(t, cerysCatalogue(), nil)
		id, err := hn.worlds.Create(t.Context(), "Modded", "2.1.24", true)
		if err != nil {
			t.Fatal(err)
		}
		rec := hn.postLong(PathGeneratePreview, worldPreviewForm(id, "planet", "cerys"), c)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), html.EscapeString(i18n.English.T("worlds.generate.preview.failed"))) {
			t.Errorf("%v: status %d, no failed message", e, rec.Code)
		}
		if !strings.Contains(hn.logs.String(), "map preview refused") {
			t.Errorf("%v: not logged", e)
		}
	}
}

// SPEC 5.6, 14.3: the preview store holds 160 MiB.
func TestPreviewStoreHolds160MiB(t *testing.T) {
	if maxPreviewBytes != 160<<20 {
		t.Errorf("store holds %d bytes", maxPreviewBytes)
	}
}
