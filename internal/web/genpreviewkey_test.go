package web

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/resource"
)

// SPEC 5.6: after a change to Nauvis's own controls only Nauvis is drawn
// again, and it stays first in the zip and the planet list.
func TestPreviewStoreKeepsThePlanetsInOrder(t *testing.T) {
	st := newPreviewStore(time.Now)
	order := []string{"nauvis", "vulcanus", "gleba"}
	before := previewKey{all: "a", planets: map[string]string{"nauvis": "n1", "vulcanus": "v", "gleba": "g"}}
	after := previewKey{all: "b", planets: map[string]string{"nauvis": "n2", "vulcanus": "v", "gleba": "g"}}
	_, _ = st.add(1, before, "World", "7", []game.PNG{{Planet: "nauvis", Data: pngBytes}, {Planet: "vulcanus", Data: pngBytes}, {Planet: "gleba", Data: pngBytes}}, order)
	_, _ = st.add(1, after, "World", "7", onePNG("nauvis"), order)
	if got := st.byUser[1].planets; !slices.Equal(got, order) {
		t.Errorf("planets %v, want %v", got, order)
	}
}

// testKey is a key whose planets all share its one value, as before
// planets had keys of their own.
func testKey(s string) previewKey { return previewKey{all: s} }

// spaceAgeReq is a Space Age preview whose autoplace controls are set as
// given, beside a shared field the form also writes.
func spaceAgeReq(t *testing.T, controls map[string]any) game.PreviewRequest {
	t.Helper()
	mg, err := json.Marshal(map[string]any{"starting_area": 1, "autoplace_controls": controls})
	if err != nil {
		t.Fatal(err)
	}
	return game.PreviewRequest{GameVersion: "2.1.24", SpaceAge: true, Seed: 7, Size: 1280, MapGenSettings: mg}
}

var bigControl = map[string]any{"frequency": 6, "size": 6, "richness": 6}

// SPEC 5.6: another planet's controls cannot change a planet's map (checked
// on the real 2.1.24 game), so they leave its key alone; its own controls,
// and every shared setting, change it.
func TestSettingsKeyIsPerPlanet(t *testing.T) {
	base := settingsKey(spaceAgeReq(t, map[string]any{"iron-ore": map[string]any{"size": 1}, "scrap": map[string]any{"size": 1}}))
	fulgora := settingsKey(spaceAgeReq(t, map[string]any{"iron-ore": map[string]any{"size": 1}, "scrap": bigControl}))
	if fulgora.all == base.all {
		t.Error("a change left the whole key the same")
	}
	if fulgora.planet("fulgora") == base.planet("fulgora") {
		t.Error("Fulgora's own control left its key the same")
	}
	for _, p := range []string{"nauvis", "vulcanus", "gleba", "aquilo"} {
		if fulgora.planet(p) != base.planet(p) {
			t.Errorf("Fulgora's control changed %s's key", p)
		}
	}
	// The order of the JSON's members is not a change.
	reordered := spaceAgeReq(t, nil)
	reordered.MapGenSettings = []byte(`{"autoplace_controls":{"scrap":{"size":1},"iron-ore":{"size":1}},"starting_area":1}`)
	if got := settingsKey(reordered); got.planet("nauvis") != base.planet("nauvis") {
		t.Error("the members' order changed Nauvis's key")
	}

	shared := map[string]func(*game.PreviewRequest){
		"seed":         func(r *game.PreviewRequest) { r.Seed = 8 },
		"size":         func(r *game.PreviewRequest) { r.Size = 2048 },
		"preset":       func(r *game.PreviewRequest) { r.Preset = "rich-resources" },
		"map settings": func(r *game.PreviewRequest) { r.MapSettings = []byte(`{"pollution":{"enabled":false}}`) },
		"map-gen field": func(r *game.PreviewRequest) {
			r.MapGenSettings = []byte(`{"starting_area":2,"autoplace_controls":{"iron-ore":{"size":1},"scrap":{"size":1}}}`)
		},
		"control of no planet": func(r *game.PreviewRequest) {
			r.MapGenSettings = []byte(`{"starting_area":1,"autoplace_controls":{"iron-ore":{"size":1},"scrap":{"size":1},"modded":{"size":2}}}`)
		},
	}
	for name, change := range shared {
		req := spaceAgeReq(t, map[string]any{"iron-ore": map[string]any{"size": 1}, "scrap": map[string]any{"size": 1}})
		change(&req)
		got := settingsKey(req)
		for _, p := range []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo"} {
			if got.planet(p) == base.planet(p) {
				t.Errorf("the %s left %s's key the same", name, p)
			}
		}
	}
}

// SPEC 5.6: settings that are not a JSON object have no planet shares, so
// every planet's key is the whole key.
func TestSettingsKeyOfUnreadableSettings(t *testing.T) {
	req := spaceAgeReq(t, nil)
	req.MapGenSettings = []byte(`[1]`)
	k := settingsKey(req)
	if k.planets != nil || k.planet("nauvis") != k.all {
		t.Errorf("key %+v", k)
	}
}

// SPEC 5.6: a planet's count and picture survive a change to another
// planet's settings, and only those of the planet changed are dropped.
func TestPreviewStoreKeepsPlanetsWhoseSettingsDidNotChange(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	before := settingsKey(spaceAgeReq(t, nil))
	after := settingsKey(spaceAgeReq(t, map[string]any{"scrap": bigControl}))
	fulgoraCount := resource.Result{Planet: "fulgora", Resources: map[string]resource.Resource{"scrap": {Total: 5}}}

	_, _ = st.add(1, before, "World", "7", []game.PNG{{Planet: "nauvis", Data: pngBytes}, {Planet: "fulgora", Data: pngBytes}}, nil)
	_, _ = st.addResources(1, before, "World", "7", "nauvis", nauvisCount)
	_, _ = st.addResources(1, before, "World", "7", "fulgora", fulgoraCount)

	// Before anything of the new settings is drawn, Nauvis's count is found.
	if _, ok := st.resourcesOf(1, after, "nauvis"); !ok {
		t.Error("Fulgora's change hid Nauvis's count")
	}
	if _, ok := st.resourcesOf(1, after, "fulgora"); ok {
		t.Error("Fulgora's count survived its own change")
	}
	// Nauvis's picture is there for the new settings, under a new token.
	tok, miss := st.missing(1, after, "World", []string{"nauvis"})
	if tok == "" || miss != nil {
		t.Fatalf("Nauvis's picture: token %q, missing %v", tok, miss)
	}
	if _, ok := st.get(tok, 1, "fulgora"); ok {
		t.Error("Fulgora's old picture is served for the new settings")
	}
	if _, miss := st.missing(1, after, "World", []string{"fulgora"}); len(miss) != 1 {
		t.Errorf("Fulgora is not missing: %v", miss)
	}
	e := st.byUser[1]
	if want := len(pngBytes) + resourceBytes(&nauvisCount); e.bytes != want {
		t.Errorf("%d bytes, want %d", e.bytes, want)
	}
	if _, ok := st.resourcesOf(1, after, "nauvis"); !ok {
		t.Error("Nauvis's count was lost on the move")
	}
	// A seed change drops them all.
	seed := spaceAgeReq(t, map[string]any{"scrap": bigControl})
	seed.Seed = 8
	_, _ = st.add(1, settingsKey(seed), "World", "8", onePNG("fulgora"), nil)
	if _, ok := st.resourcesOf(1, settingsKey(seed), "nauvis"); ok {
		t.Error("a new seed kept Nauvis's count")
	}
	if _, miss := st.missing(1, settingsKey(seed), "World", []string{"nauvis"}); len(miss) != 1 {
		t.Error("a new seed kept Nauvis's picture")
	}
}

// SPEC 5.6: through the handler, a change to Fulgora's slider leaves
// Nauvis's count in the store, and Fulgora is counted again.
func TestResourcesSurviveAnotherPlanetsChange(t *testing.T) {
	hn, prev, c := newResourceHarness(t)
	hx := withHeader("HX-Request", "true")
	// The page shows the settings, so it posts the map settings' boxes too, ticked as the preset has them.
	changed := []string{fieldName("fulgora", "scrap", "frequency"), "50",
		"ms.enemy_expansion.enabled", "on", "ms.enemy_evolution.enabled", "on", "ms.pollution.enabled", "on"}
	hn.postLong(pathResources, planetForm("nauvis"), c, hx)
	hn.postLong(pathResources, planetForm("fulgora"), c, hx)
	hn.postLong(pathResources, planetForm("fulgora", changed...), c, hx)
	hn.postLong(pathResources, planetForm("nauvis", changed...), c, hx)
	if len(prev.runs) != 3 || prev.planets[2] != "fulgora" {
		t.Errorf("want Nauvis, Fulgora and Fulgora again counted, got %v", prev.planets)
	}
}

// SPEC 5.6: a count that ends after the person changed another planet's
// settings still fits the newer preview, so it is kept there; one whose own
// planet changed is not.
func TestLateCountJoinsANewerPreviewOfTheSamePlanet(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	before := settingsKey(spaceAgeReq(t, nil))
	after := settingsKey(spaceAgeReq(t, map[string]any{"scrap": bigControl}))
	tok, _ := st.add(1, after, "World", "7", onePNG("fulgora"), nil)
	if got, err := st.addResources(1, before, "World", "7", "nauvis", nauvisCount); err != nil || got != tok {
		t.Errorf("token %q, want %q, %v", got, tok, err)
	}
	if st.byUser[1].key.all != after.all {
		t.Error("the late count moved the preview back to the older settings")
	}
	if _, ok := st.resourcesOf(1, after, "nauvis"); !ok {
		t.Error("the late count is not found")
	}
	if got, _ := st.addResources(1, before, "World", "7", "fulgora", nauvisCount); got != "" {
		t.Error("a late count of a changed planet was kept")
	}
}
