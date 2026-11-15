package web

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

const (
	favLine    = "2.0"
	favVersion = "2.0.77"
	// favModControl is a mod's control the form has no field for.
	favModControl = "my-mod-ore"
)

// leaves flattens a JSON object to its dotted paths.
func leaves(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%v:\n%s", err, b)
	}
	return flatPaths(m, "")
}

// favoriteMapGen is a map-gen-settings.json with every select of the form
// moved off the preset's step, every other resource turned off, and a mod's
// control the form does not know.
func favoriteMapGen(t *testing.T) []byte {
	t.Helper()
	form := NewMapGenFormForPreset(favLine, true, defaultPreset)
	vals := url.Values{mgSeed: {"12345"}, mgWidth: {"2000"}, mgHeight: {"1500"}, mgPeaceful: {"on"}}
	for _, s := range form.selects() {
		for _, o := range s.Options {
			if v, ok := stepValue(o.Value); ok && v > 0 && o.Value != s.Value {
				vals.Set(s.Name, o.Value)
				break
			}
		}
	}
	for i, c := range form.toggles() {
		vals.Add(c.Name, "off")
		if i%2 == 0 {
			vals.Add(c.Name, "on")
		}
	}
	raw := `{"autoplace_controls":{"` + favModControl + `":{"frequency":2,"size":1,"richness":1}}}`
	vals.Set(mgRaw, raw)
	_, out, problems := parseMapGenValues(vals, favLine, true)
	if len(problems) > 0 || out == nil {
		t.Fatalf("problems %v", problems)
	}
	return out
}

func TestFavoriteMapGenRoundTripsThroughTheForm(t *testing.T) {
	file := favoriteMapGen(t)
	form := NewMapGenFormFromFile(favLine, true, file)
	back, ok := buildMapGen(form, rawOrEmpty(form.Raw.Value))
	if !ok {
		t.Fatal("build failed")
	}
	want, got := leaves(t, file), leaves(t, back)
	if !maps.EqualFunc(want, got, func(a, b any) bool { return reflect.DeepEqual(a, b) }) {
		for k, v := range want {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("%s: %v, want %v", k, got[k], v)
			}
		}
		for k, v := range got {
			if _, ok := want[k]; !ok {
				t.Errorf("%s: %v, not in the favorite", k, v)
			}
		}
	}
	// The mod's control is not a field, and is kept.
	if _, ok := got["autoplace_controls."+favModControl+".frequency"]; !ok {
		t.Error("the mod's control was dropped")
	}
}

func TestFavoriteMapGenOneEditChangesOneValue(t *testing.T) {
	file := favoriteMapGen(t)
	form := NewMapGenFormFromFile(favLine, true, file)
	vals := fileValues(favLine, true, file, form.Seed.Value, form.Raw.Value, true)
	s := form.selects()[0]
	for _, o := range s.Options {
		if v, ok := stepValue(o.Value); ok && v > 0 && o.Value != s.Value {
			vals.Set(s.Name, o.Value)
			break
		}
	}
	_, edited, problems := parseMapGenValues(vals, favLine, true)
	if len(problems) > 0 {
		t.Fatalf("problems %v", problems)
	}
	want, got := leaves(t, file), leaves(t, edited)
	var changed []string
	for k := range want {
		if !reflect.DeepEqual(want[k], got[k]) {
			changed = append(changed, k)
		}
	}
	if len(changed) != 1 || len(got) != len(want) {
		t.Errorf("changed %v (%d keys, want %d), want one value", changed, len(got), len(want))
	}
}

// favoriteMapSettings is a map-settings.json with fields moved off the
// preset and a value the form has no field for.
func favoriteMapSettings(t *testing.T) []byte {
	t.Helper()
	b, err := mapgen.MapSettings(favVersion, defaultPreset, map[string]any{
		"enemy_expansion.enabled": false, "pollution.enabled": false, "enemy_evolution.time_factor": 0.00001,
	})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["foreman_test_unknown"] = map[string]any{"kept": true}
	out, _ := json.Marshal(m)
	return out
}

func TestFavoriteMapSettingsRoundTripAndKeepUnknown(t *testing.T) {
	file := favoriteMapSettings(t)
	form, overrides, problems := readMapSettings(favVersion, defaultPreset, mapSettingsValues(favVersion, defaultPreset, file))
	if len(problems) > 0 || len(form.Groups) == 0 {
		t.Fatalf("problems %v", problems)
	}
	formFile, err := mapgen.MapSettings(favVersion, defaultPreset, overrides)
	if err != nil {
		t.Fatal(err)
	}
	back, err := overlayFields(file, formFile)
	if err != nil {
		t.Fatal(err)
	}
	want, got := leaves(t, file), leaves(t, back)
	if !reflect.DeepEqual(want, got) {
		for k, v := range want {
			if !reflect.DeepEqual(got[k], v) {
				t.Errorf("%s: %v, want %v", k, got[k], v)
			}
		}
	}

	// One edit changes only that value.
	vals := mapSettingsValues(favVersion, defaultPreset, file)
	vals.Set(msPrefix+"enemy_expansion.enabled", "on")
	_, overrides, _ = readMapSettings(favVersion, defaultPreset, vals)
	formFile, _ = mapgen.MapSettings(favVersion, defaultPreset, overrides)
	edited, _ := overlayFields(file, formFile)
	got = leaves(t, edited)
	for k, v := range want {
		if k != "enemy_expansion.enabled" && !reflect.DeepEqual(got[k], v) {
			t.Errorf("%s changed: %v, want %v", k, got[k], v)
		}
	}
	if got["enemy_expansion.enabled"] != true {
		t.Error("the edit was lost")
	}
}

// favoriteWithMap makes a favorite from a world holding both test files.
func favoriteWithMap(t *testing.T, hn *harness, favs *world.Favorites) string {
	t.Helper()
	src, err := hn.worlds.Generate(context.Background(), world.NewWorld{Name: "Mapped", GameVersion: favVersion, SpaceAge: true,
		MapGenSettings: favoriteMapGen(t), MapSettings: favoriteMapSettings(t)})
	if err != nil {
		t.Fatal(err)
	}
	id, err := favs.Make(context.Background(), src, "Mapped start")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// favoritePost is Generate's post as the page sends it after starting from
// the favorite: its fields as filled in.
func favoritePost(t *testing.T, favID string, edit ...string) url.Values {
	t.Helper()
	file := favoriteMapGen(t)
	form := NewMapGenFormFromFile(favLine, true, file)
	vals := fileValues(favLine, true, file, form.Seed.Value, form.Raw.Value, true)
	for k, v := range mapSettingsValues(favVersion, defaultPreset, favoriteMapSettings(t)) {
		vals[k] = v
	}
	base := generateForm("favorite", favID)
	for k, v := range vals {
		base[k] = v
	}
	base.Set(settingsShown, "1")
	for i := 0; i+1 < len(edit); i += 2 {
		base.Set(edit[i], edit[i+1])
	}
	return base
}

func TestGenerateFromAFavoriteKeepsItsWholeMap(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	hn.withActive(favVersion)
	favID := favoriteWithMap(t, hn, favs)

	id := idFromLocation(t, hn.postLong(PathGenerate, favoritePost(t, favID, "name", "Same map"), c))
	mg, ms := leaves(t, hn.config(id, world.MapGenSettingsFile).Data), leaves(t, hn.config(id, world.MapSettingsFile).Data)
	for k, v := range leaves(t, favoriteMapGen(t)) {
		if !reflect.DeepEqual(mg[k], v) {
			t.Errorf("map-gen %s: %v, want %v", k, mg[k], v)
		}
	}
	for k, v := range leaves(t, favoriteMapSettings(t)) {
		if !reflect.DeepEqual(ms[k], v) {
			t.Errorf("map settings %s: %v, want %v", k, ms[k], v)
		}
	}
	if ms["foreman_test_unknown.kept"] != true {
		t.Error("a map setting the form has no field for was dropped")
	}

	// One edit: only that value differs from the favorite.
	id = idFromLocation(t, hn.postLong(PathGenerate, favoritePost(t, favID, "name", "Edited", msPrefix+"enemy_expansion.enabled", "on"), c))
	ms = leaves(t, hn.config(id, world.MapSettingsFile).Data)
	for k, v := range leaves(t, favoriteMapSettings(t)) {
		if k != "enemy_expansion.enabled" && !reflect.DeepEqual(ms[k], v) {
			t.Errorf("edited world: %s %v, want %v", k, ms[k], v)
		}
	}
	if ms["enemy_expansion.enabled"] != true {
		t.Error("the edit did not reach the world")
	}
}

func TestGenerateFormFillsTheFavoritesMapAndPreviewUsesIt(t *testing.T) {
	prev := &fakePreviewer{}
	hn, favs := newFavoritesHarness(t)
	h, err := NewHandler(Deps{Config: hn.cfg, Store: hn.store, Worlds: hn.worlds, Favorites: favs, Previewer: prev, Log: hn.slog, Now: hn.clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	hn.h = h
	c := hn.signIn()
	hn.withActive(favVersion)
	favID := favoriteWithMap(t, hn, favs)

	body := hn.get(generateFromPath(favID), withCookie(c)).Body.String()
	form := NewMapGenFormFromFile(favLine, true, favoriteMapGen(t))
	s := form.selects()[0]
	if want := `value="` + s.Value + `" selected`; !containsAfter(body, `id="`+s.ID+`"`, want) {
		t.Errorf("%s does not show the favorite's %s", s.ID, s.Value)
	}
	if !containsAfter(body, `id="generate-seed"`, `value="12345"`) {
		t.Error("the seed is not the favorite's")
	}

	hn.postLong(PathGeneratePreview, favoritePost(t, favID), c)
	if len(prev.got) != 1 {
		t.Fatalf("%d previews", len(prev.got))
	}
	got := leaves(t, prev.got[0].MapGenSettings)
	for k, v := range leaves(t, favoriteMapGen(t)) {
		if k != "seed" && !reflect.DeepEqual(got[k], v) {
			t.Errorf("preview %s: %v, want %v", k, got[k], v)
		}
	}
	if prev.got[0].Seed != 12345 {
		t.Errorf("preview seed %s", strconv.FormatUint(uint64(prev.got[0].Seed), 10))
	}
}

// containsAfter reports whether want appears within a few thousand bytes
// after the first marker.
func containsAfter(body, marker, want string) bool {
	i := strings.Index(body, marker)
	if i < 0 {
		return false
	}
	return strings.Contains(body[i:min(len(body), i+4000)], want)
}

func TestPresetFillStaysNauvisOnly(t *testing.T) {
	defaults := NewMapGenFormForPreset(favLine, true, defaultPreset).chosen()
	for _, preset := range mapgen.Presets(favLine) {
		vals := presetValues(favLine, true, preset, "")
		for k := range vals {
			if strings.HasPrefix(k, mgPrefix) && strings.Count(k, ".") == 3 && !strings.HasPrefix(k, mgPrefix+"nauvis.") {
				t.Errorf("preset %s sets %s on another planet", preset, k)
			}
		}
		if vals.Has(mgNoEnemies) {
			t.Errorf("preset %s sets no enemies", preset)
		}
		// Another planet's fields, such as its water, stay at the default.
		form := NewMapGenFormForPreset(favLine, true, preset)
		for _, s := range form.selects() {
			if strings.HasPrefix(s.Name, mgPrefix+"vulcanus.") && s.Value != defaults[s.Name] {
				t.Errorf("preset %s moved %s", preset, s.Name)
			}
		}
	}
}

func TestFileFillSetsNoEnemiesOnlyWhenTheFileDoes(t *testing.T) {
	if vals := fileValues(favLine, true, []byte(`{"no_enemies_mode":true}`), "", "{}", true); vals.Get(mgNoEnemies) != "on" {
		t.Error("a file with no enemies did not tick the box")
	}
	if vals := fileValues(favLine, true, []byte(`{"no_enemies_mode":false}`), "", "{}", true); vals.Has(mgNoEnemies) {
		t.Error("a file without no enemies ticked the box")
	}
}

func TestFavoriteMapFilesRefusesWhenMapSettingsCannotBeMade(t *testing.T) {
	f := GenerateForm{Version: "9.9.9", Preset: defaultPreset, Line: favLine, MapGen: NewMapGenFormForPreset(favLine, true, defaultPreset)}
	nw := world.NewWorld{}
	r := httptest.NewRequest(http.MethodPost, PathGenerate, nil)
	r.PostForm = url.Values{}
	if (&server{}).favoriteMapFiles(&f, &nw, world.FavoriteFiles{MapSettings: favoriteMapSettings(t)}, r) {
		t.Fatal("made map files without map settings")
	}
	if f.FormError.IsZero() || nw.MapSettings != nil {
		t.Errorf("error %v, map settings %s", f.FormError, nw.MapSettings)
	}
}

func TestDamagedFavoriteMapSettingsAreRefusedOnGenerate(t *testing.T) {
	hn, favs := newFavoritesHarness(t)
	c := hn.signIn()
	hn.withActive(favVersion)
	src, err := hn.worlds.Generate(context.Background(), world.NewWorld{Name: "Odd", GameVersion: favVersion, SpaceAge: true,
		MapSettings: []byte(`{"enemy_expansion":{"enabled":"yes"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	favID, err := favs.Make(context.Background(), src, "Odd start")
	if err != nil {
		t.Fatal(err)
	}
	rec := hn.get(generateFromPath(favID), withCookie(c))
	body := rec.Body.String()
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(body, `id="favorite-choice-error"`) ||
		!strings.Contains(body, `aria-describedby="favorite-choice-error"`) || !strings.Contains(body, `href="#favorite-choice"`) {
		t.Errorf("status %d, no error on the chooser", rec.Code)
	}
	n := hn.worldCount()
	if rec := hn.postLong(PathGenerate, generateForm("name", "Odd", "favorite", favID), c); rec.Code != http.StatusBadRequest {
		t.Errorf("generate: %d", rec.Code)
	}
	if hn.worldCount() != n {
		t.Error("a world was made from damaged map settings")
	}
}
