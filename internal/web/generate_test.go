package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/config"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mapexchange"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// pngBytes stands for a picture. The game layer checks that a real one is a
// PNG; the store keeps whatever it is given.
var pngBytes = []byte("\x89PNG\r\n\x1a\nfake picture")

// fakePreviewer records what it was asked and answers as set.
type fakePreviewer struct {
	got []game.PreviewRequest
	err error
	png []byte
}

func (f *fakePreviewer) Preview(_ context.Context, req game.PreviewRequest) ([]game.PNG, error) {
	f.got = append(f.got, req)
	if f.err != nil {
		return nil, f.err
	}
	png := f.png
	if png == nil {
		png = pngBytes
	}
	var out []game.PNG
	for _, p := range req.Planets {
		out = append(out, game.PNG{Planet: p, Data: png})
	}
	return out, nil
}

// newGenerateHarness is a harness whose handler can make map previews.
func newGenerateHarness(t *testing.T, prev MapPreviewer) *harness {
	t.Helper()
	cfg, err := config.Load(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "foreman.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clock := &fakeClock{t: time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)}
	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	data := t.TempDir()
	worlds, err := world.NewManager(world.Dirs{
		Worlds:    filepath.Join(data, "worlds"),
		Snapshots: filepath.Join(data, "snapshots"),
		Imports:   filepath.Join(data, "import"),
	}, st, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewHandler(Deps{Config: cfg, Store: st, Worlds: worlds, Previewer: prev, Log: logger, Now: clock.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return &harness{t: t, h: h, store: st, clock: clock, logs: logs, cfg: cfg, worlds: worlds, slog: logger}
}

// postLong sends a form to a long route (Generate's, Clone's).
func (hn *harness) postLong(p string, form url.Values, c *http.Cookie, mods ...func(*http.Request)) *httptest.ResponseRecorder {
	hn.t.Helper()
	r := httptest.NewRequest(http.MethodPost, p, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if c != nil {
		r.AddCookie(c)
	}
	for _, m := range mods {
		m(r)
	}
	return hn.serveLong(r)
}

// fixedSeed makes the blank-seed fill predictable for the test.
func fixedSeed(t *testing.T, v uint32) {
	t.Helper()
	old := newSeed
	newSeed = func() uint32 { return v }
	t.Cleanup(func() { newSeed = old })
}

// generateForm is a valid preset-mode form for a 2.0.77 world.
func generateForm(extra ...string) url.Values {
	f := url.Values{
		"name": {"Fresh start"}, "game_version_typed": {"2.0.77"}, "space_age": {"on"},
		"mode": {"preset"}, "preset": {"default"}, "map_key": {"2.0|1"},
	}
	for i := 0; i+1 < len(extra); i += 2 {
		if extra[i+1] == "" {
			f.Del(extra[i])
		} else {
			f.Set(extra[i], extra[i+1])
		}
	}
	return f
}

// withActive makes an active world on version v, so Generate has a version to default to.
func (hn *harness) withActive(v string) string {
	hn.t.Helper()
	id, err := hn.worlds.Create(context.Background(), "Base", v, !strings.HasPrefix(v, "1."))
	if err != nil {
		hn.t.Fatal(err)
	}
	return id
}

func (hn *harness) meta(id string) world.Meta {
	hn.t.Helper()
	root, err := hn.worlds.OpenWorld(id)
	if err != nil {
		hn.t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	m, err := world.ReadMeta(root)
	if err != nil {
		hn.t.Fatal(err)
	}
	return m
}

func (hn *harness) config(id, file string) world.ConfigRead {
	hn.t.Helper()
	r, err := hn.worlds.ReadConfig(id, file)
	if err != nil {
		hn.t.Fatal(err)
	}
	return r
}

func (hn *harness) worldCount() int {
	hn.t.Helper()
	list, err := hn.worlds.List(context.Background())
	if err != nil {
		hn.t.Fatal(err)
	}
	return len(list)
}

// idFromLocation is the world id in a redirect to /worlds/{id}?...
func idFromLocation(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want 303:\n%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	rest, ok := strings.CutPrefix(loc, PathWorlds+"/")
	if !ok {
		t.Fatalf("redirect to %q", loc)
	}
	id, _, _ := strings.Cut(rest, "?")
	return id
}

func TestGenerateNeedsSignIn(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	hn.wantRedirect(hn.get(PathGenerate), PathSetup)
	hn.wantRedirect(hn.postLong(PathGenerate, generateForm(), nil), PathSetup)
	hn.wantRedirect(hn.postLong(PathGeneratePreview, generateForm(), nil), PathSetup)
	hn.wantRedirect(hn.postLong(PathGenerateRefresh, generateForm(), nil), PathSetup)
	hn.wantRedirect(hn.get(PathGeneratePreview+"/tok/nauvis"), PathSetup)
}

// SPEC 5.6, 10.7: the form opens on the active world's version, with that
// line's presets, and offers the world list a way in.
func TestGenerateFormOpensOnTheActiveWorldsVersion(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	hn.withActive("2.0.77")
	rec := hn.get(PathGenerate, withCookie(c))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"2.0.77, same as the active world", `name="map_key" value="2.0|1"`, `value="rich-resources"`, "Rich resources",
		`id="mode-preset" name="mode" value="preset" checked`, `id="preview-button"`, `id="generate-button"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if !strings.Contains(hn.get(PathWorlds, withCookie(c)).Body.String(), `href="`+PathGenerate+`"`) {
		t.Error("the world list does not link to Generate")
	}
}

func TestGenerateFormWithoutPreviewerHasNoPreviewButton(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	if body := hn.get(PathGenerate, withCookie(c)).Body.String(); strings.Contains(body, "preview-button") {
		t.Error("the preview button is offered with no previewer")
	}
	rec := hn.postLong(PathGeneratePreview, generateForm(), c)
	if rec.Code != http.StatusNotFound {
		t.Errorf("preview status %d, want 404", rec.Code)
	}
}

// SPEC 5.6: a preset world is made with no files, only new_map.
func TestGeneratePresetMakesAWorldWithoutFiles(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	rec := hn.postLong(PathGenerate, generateForm("preset", "rich-resources", "seed", "123"), c)
	id := idFromLocation(t, rec)
	m := hn.meta(id)
	if m.GameVersion != "2.0.77" || !m.SpaceAge || m.NewMap == nil || m.NewMap.Preset != "rich-resources" || m.NewMap.Seed == nil || *m.NewMap.Seed != 123 {
		t.Errorf("meta %+v new_map %+v", m, m.NewMap)
	}
	for _, f := range []string{world.MapGenSettingsFile, world.MapSettingsFile} {
		if hn.config(id, f).Found {
			t.Errorf("%s written for a preset world", f)
		}
	}
	if got := hn.audits(AuditGenerate); len(got) != 1 || !strings.Contains(got[0], "alice "+id+" preset, 2.0.77") {
		t.Errorf("audit %q", got)
	}
	page := hn.get(rec.Header().Get("Location"), withCookie(c)).Body.String()
	if !strings.Contains(page, "Generated Fresh start. Its map is made when it first starts.") {
		t.Errorf("no Done message:\n%s", page)
	}
}

// SPEC 5.6: the default preset is the game's own default, so no --preset.
func TestGenerateDefaultPresetPassesNone(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm("seed", "5"), c))
	if m := hn.meta(id); m.NewMap == nil || m.NewMap.Preset != "" {
		t.Errorf("new_map %+v", m.NewMap)
	}
}

// A blank seed is filled with a random one, so a later Generate matches the preview.
func TestGenerateBlankSeedIsFilled(t *testing.T) {
	fixedSeed(t, 987654)
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm(), c))
	if m := hn.meta(id); m.NewMap == nil || m.NewMap.Seed == nil || *m.NewMap.Seed != 987654 {
		t.Errorf("new_map %+v", m.NewMap)
	}
}

func TestGenerateRefusals(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"blank name", generateForm("name", " "), "Enter a name for the world."},
		{"taken name", generateForm("name", "base"), "Another world already has this name."},
		{"bad version", generateForm("game_version_typed", "9.9.9"), "Enter the game version as three numbers"},
		{"space age on 1.1", generateForm("game_version_typed", "1.1.110", "map_key", "1.1|1"), "Space Age needs Factorio 2.0 or later."},
		{"unknown preset", generateForm("preset", "../etc"), "Choose one of the presets in the list."},
		{"bad seed", generateForm("seed", "-4"), "Enter the seed as a whole number from 0 to 4294967295"},
		{"seed too large", generateForm("seed", "4294967296"), "Enter the seed as a whole number"},
		{"empty exchange", generateForm("mode", "exchange"), "Paste a map exchange string"},
		{"garbage exchange", generateForm("mode", "exchange", "exchange", ">>>not a string<<<"), "Foreman could not read this as a map exchange string."},
		{"huge exchange", generateForm("mode", "exchange", "exchange", strings.Repeat("A", mapexchange.MaxInputBytes+1)), "This text is too long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := hn.worldCount()
			rec := hn.postLong(PathGenerate, tc.form, c)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("page lacks %q", tc.want)
			}
			if !strings.Contains(body, `id="error-summary"`) {
				t.Error("no error summary")
			}
			if after := hn.worldCount(); after != before {
				t.Errorf("a world was made: %d worlds, was %d", after, before)
			}
		})
	}
}

// With no active world and no version chosen there is nothing to default to.
func TestGenerateNeedsAVersionWhenNoWorldIsActive(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	rec := hn.get(PathGenerate, withCookie(c))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Choose a version") {
		t.Fatalf("form: %d", rec.Code)
	}
	rec = hn.postLong(PathGenerate, generateForm("game_version_typed", ""), c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Choose a game version or type one.") {
		t.Errorf("status %d", rec.Code)
	}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(path.Join("..", "mapexchange", "unittestdata", name)) //nolint:gosec // a fixture of this repository
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// SPEC 5.6: an exchange string writes both decoded files and the seed in it.
func TestGenerateExchangeStringWritesBothFiles(t *testing.T) {
	fixedSeed(t, 31337)
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	rec := hn.postLong(PathGenerate, generateForm("mode", "exchange", "exchange", fixture(t, "2.0.77.txt")), c)
	id := idFromLocation(t, rec)
	want, err := mapexchange.Decode(fixture(t, "2.0.77.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := hn.config(id, world.MapGenSettingsFile); !got.Found || string(got.Data) != string(want.MapGenSettings) {
		t.Errorf("map-gen-settings.json %q", got.Data)
	}
	if got := hn.config(id, world.MapSettingsFile); !got.Found || string(got.Data) != string(want.MapSettings) {
		t.Errorf("map-settings.json %q", got.Data)
	}
	if m := hn.meta(id); m.NewMap == nil || m.NewMap.Seed == nil {
		t.Errorf("new_map %+v", m.NewMap)
	}
}

// SPEC 5.6: a string from another line, or newer than the world, is refused with both versions.
func TestGenerateExchangeStringMustFitTheWorld(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	other := hn.postLong(PathGenerate, generateForm("mode", "exchange", "exchange", fixture(t, "2.1.24.txt")), c)
	if other.Code != http.StatusBadRequest || !strings.Contains(other.Body.String(), "made by Factorio 2.1.24, but this world uses 2.0.77") {
		t.Errorf("other line: %d", other.Code)
	}
	// 2.0.77's string on a 2.0.66 world is newer than the world.
	newer := hn.postLong(PathGenerate, generateForm("mode", "exchange", "exchange", fixture(t, "2.0.77.txt"), "game_version_typed", "2.0.66"), c)
	if newer.Code != http.StatusBadRequest || !strings.Contains(newer.Body.String(), "game version 2.0.66") {
		t.Errorf("newer: %d", newer.Code)
	}
	if n := hn.worldCount(); n != 1 {
		t.Errorf("%d worlds, want only the active one", n)
	}
}

// DESIGN 8.7: custom mode writes only map-gen-settings.json, never a map-settings.json.
func TestGenerateCustomWritesOnlyMapGenSettings(t *testing.T) {
	fixedSeed(t, 55)
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm("mode", "custom", "mg.width", "512"), c))
	got := hn.config(id, world.MapGenSettingsFile)
	if !got.Found || !strings.Contains(string(got.Data), `"width": 512`) {
		t.Errorf("map-gen-settings.json %q", got.Data)
	}
	if hn.config(id, world.MapSettingsFile).Found {
		t.Error("map-settings.json written for a custom world")
	}
	if m := hn.meta(id); m.NewMap == nil || m.NewMap.Seed == nil || *m.NewMap.Seed != 55 {
		t.Errorf("new_map %+v", m.NewMap)
	}
}

// A seed in the custom form is the world's seed.
func TestGenerateCustomSeedIsTheWorldsSeed(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm("mode", "custom", "mg.seed", "4242"), c))
	if m := hn.meta(id); m.NewMap == nil || m.NewMap.Seed == nil || *m.NewMap.Seed != 4242 {
		t.Errorf("new_map %+v", m.NewMap)
	}
}

func TestGenerateCustomProblemsAreShownAndNothingIsMade(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	rec := hn.postLong(PathGenerate, generateForm("mode", "custom", "mg.width", "wide"), c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `href="#mg-width"`) || hn.worldCount() != 1 {
		t.Errorf("status %d, %d worlds\n%s", rec.Code, hn.worldCount(), rec.Body.String())
	}
	// The same problem in another mode's hidden fields is not shown.
	rec = hn.postLong(PathGenerate, generateForm("mode", "preset", "seed", "1", "mg.width", "wide"), c)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("preset mode refused for a custom problem: %d", rec.Code)
	}
}

// DESIGN 8.6: when the version or Space Age moved on, custom options made for
// the old line are set back and nothing is made; the other ways to make a map
// do not depend on them, so typing a version and pressing Generate just works.
func TestGenerateResetsCustomOptionsWhenTheVersionChanged(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	hn.withActive("2.0.77")
	form := generateForm("mode", "custom", "game_version_typed", "2.1.24", "seed", "7") // the options were made for 2.0|1
	for _, p := range []string{PathGenerate, PathGeneratePreview, PathGenerateRefresh} {
		before := hn.worldCount()
		rec := hn.postLong(p, form, c)
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.Contains(body, "set back to that version") ||
			!strings.Contains(body, `name="map_key" value="2.1|1"`) || !strings.Contains(body, `value="7"`) {
			t.Errorf("%s: status %d\n%s", p, rec.Code, body)
		}
		if hn.worldCount() != before {
			t.Errorf("%s made a world", p)
		}
	}
	// A preset map is made on the new version at once.
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm("game_version_typed", "2.1.24", "preset", "lakes", "seed", "7"), c))
	if m := hn.meta(id); m.GameVersion != "2.1.24" || m.NewMap == nil || m.NewMap.Preset != "lakes" {
		t.Errorf("meta %+v new_map %+v", m, m.NewMap)
	}
	// A preset the new line lacks is refused, not swapped for another.
	rec := hn.postLong(PathGenerate, generateForm("game_version_typed", "1.1.110", "space_age", "", "preset", "lakes"), c)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Choose one of the presets in the list.") {
		t.Errorf("1.1 lakes: %d", rec.Code)
	}
	// A refresh redraws the options for 1.1: no Space Age, and the presets of that line.
	rec = hn.postLong(PathGenerateRefresh, generateForm("game_version_typed", "1.1.110", "space_age", "", "preset", "lakes"), c)
	body := rec.Body.String()
	if !strings.Contains(body, `name="map_key" value="1.1|0"`) || !strings.Contains(body, `value="ribbon-world"`) || strings.Contains(body, `value="lakes"`) {
		t.Errorf("1.1 refresh:\n%s", body)
	}
}

// A refresh with an unfinished version keeps the options as they were.
func TestGenerateRefreshKeepsOptionsForAnUnfinishedVersion(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.0.77")
	rec := hn.postLong(PathGenerateRefresh, generateForm("game_version_typed", "2.1.", "preset", "lakes"), c)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || strings.Contains(body, "set back to that version") || !strings.Contains(body, `name="map_key" value="2.0|1"`) {
		t.Errorf("status %d\n%s", rec.Code, body)
	}
	if strings.Contains(body, `id="error-summary"`) {
		t.Error("a refresh shows the error summary")
	}
}

// SPEC 5.6, DESIGN 8.8: the preview asks for every planet of the line and Space Age, with the filled seed.
func TestPreviewAsksForEveryPlanetWithTheFilledSeed(t *testing.T) {
	fixedSeed(t, 424242)
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.0.77")
	rec := hn.postLong(PathGeneratePreview, generateForm("preset", "island", "name", ""), c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d\n%s", rec.Code, rec.Body.String())
	}
	if len(prev.got) != 1 {
		t.Fatalf("%d previews made", len(prev.got))
	}
	got := prev.got[0]
	if got.GameVersion != "2.0.77" || got.Preset != "island" || got.Seed != 424242 ||
		!slices.Equal(got.Planets, []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo"}) || got.MapGenSettings != nil || got.MapSettings != nil {
		t.Errorf("request %+v", got)
	}
	body := rec.Body.String()
	// The form keeps the seed, so Generate makes the same map.
	for _, want := range []string{`type="text" value="424242"`, "seed 424242", "Map preview of Nauvis, seed 424242", "Map preview of Aquilo, seed 424242"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if n := strings.Count(body, "<img "); n != 5 {
		t.Errorf("%d pictures, want 5", n)
	}
	// Generate with those very values makes that map.
	fixedSeed(t, 1)
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm("preset", "island", "seed", "424242"), c))
	if m := hn.meta(id); m.NewMap == nil || *m.NewMap.Seed != 424242 || m.NewMap.Preset != "island" {
		t.Errorf("new_map %+v", m.NewMap)
	}
}

func TestPreviewPlanetsFollowTheLineAndSpaceAge(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.0.77")
	hn.postLong(PathGeneratePreview, generateForm("space_age", "", "map_key", "2.0|0"), c)
	hn.postLong(PathGeneratePreview, generateForm("game_version_typed", "1.1.110", "map_key", "1.1|0", "space_age", ""), c)
	if len(prev.got) != 2 || !slices.Equal(prev.got[0].Planets, []string{"nauvis"}) || !slices.Equal(prev.got[1].Planets, []string{"nauvis"}) {
		t.Fatalf("requests %+v", prev.got)
	}
	if prev.got[0].GameVersion != "2.0.77" || prev.got[1].GameVersion != "1.1.110" {
		t.Errorf("versions %q, %q", prev.got[0].GameVersion, prev.got[1].GameVersion)
	}
	// Custom options made with Space Age are set back when it is turned off: no preview from stale options.
	hn.postLong(PathGeneratePreview, generateForm("mode", "custom", "space_age", "", "map_key", "2.0|1"), c)
	if len(prev.got) != 2 {
		t.Errorf("a preview was made from stale options: %d", len(prev.got))
	}
}

// A custom preview puts its random seed in the custom form, so Generate makes that map.
func TestPreviewKeepsTheFilledSeedForCustomMode(t *testing.T) {
	fixedSeed(t, 777)
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.0.77")
	body := hn.postLong(PathGeneratePreview, generateForm("mode", "custom", "mg.width", "512"), c).Body.String()
	if !strings.Contains(body, `id="mg-seed" name="mg.seed" type="text" value="777"`) || !strings.Contains(string(prev.got[0].MapGenSettings), `"width": 512`) || prev.got[0].Seed != 777 {
		t.Errorf("request %+v\n%s", prev.got, body)
	}
	fixedSeed(t, 1)
	id := idFromLocation(t, hn.postLong(PathGenerate, generateForm("mode", "custom", "mg.seed", "777"), c))
	if m := hn.meta(id); *m.NewMap.Seed != 777 {
		t.Errorf("seed %d, want the previewed 777", *m.NewMap.Seed)
	}
}

// An exchange string carries its own seed; preview and world both use it.
func TestExchangeStringSeedIsUsedByPreviewAndWorld(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.0.77")
	form := generateForm("mode", "exchange", "exchange", fixture(t, "2.0.77.txt"))
	hn.postLong(PathGeneratePreview, form, c)
	id := idFromLocation(t, hn.postLong(PathGenerate, form, c))
	if m := hn.meta(id); m.NewMap == nil || m.NewMap.Seed == nil || *m.NewMap.Seed != prev.got[0].Seed {
		t.Errorf("seed %+v, previewed %d", m.NewMap, prev.got[0].Seed)
	}
}

func TestPreviewOfAnExchangeStringSendsBothFiles(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.0.77")
	hn.postLong(PathGeneratePreview, generateForm("mode", "exchange", "exchange", fixture(t, "2.0.77.txt")), c)
	if len(prev.got) != 1 || len(prev.got[0].MapGenSettings) == 0 || len(prev.got[0].MapSettings) == 0 {
		t.Fatalf("requests %+v", prev.got)
	}
}

// A bad form shows its errors in the whole form for htmx, and makes no preview.
func TestPreviewRefusesABadFormWithoutCallingTheGame(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.0.77")
	form := generateForm("mode", "exchange", "exchange", "nonsense")
	rec := hn.postLong(PathGeneratePreview, form, c)
	if rec.Code != http.StatusBadRequest || len(prev.got) != 0 {
		t.Errorf("plain: status %d, %d previews", rec.Code, len(prev.got))
	}
	rec = hn.postLong(PathGeneratePreview, form, c, withHeader("HX-Request", "true"))
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Retarget") != "#generate-form" || rec.Header().Get("HX-Reswap") != "outerHTML" {
		t.Errorf("htmx: status %d, headers %v", rec.Code, rec.Header())
	}
	if !strings.Contains(rec.Body.String(), "Foreman could not read this as a map exchange string.") {
		t.Error("htmx answer lacks the error")
	}
}

func TestPreviewFailuresSayWhatToDo(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"no image", game.ErrPreviewImageMissing, "game image for Factorio 2.0.77 is not downloaded"},
		{"busy", game.ErrPreviewBusy, "Another map preview is being made."},
		{"timeout", game.ErrPreviewTimeout, "took too long and was stopped"},
		{"out of memory", &game.PreviewError{OOM: true}, "ran out of memory"},
		{"game failed", &game.PreviewError{Log: "line one\nsecret game output"}, "The game could not make the map preview. You can still generate the world."},
		{"unknown", errors.New("docker is gone"), "The game could not make the map preview."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hn := newGenerateHarness(t, &fakePreviewer{err: tc.err})
			c := hn.signIn()
			hn.withActive("2.0.77")
			rec := hn.postLong(PathGeneratePreview, generateForm(), c)
			body := rec.Body.String()
			if rec.Code != http.StatusOK || !strings.Contains(body, tc.want) || !strings.Contains(body, `id="generate-button"`) {
				t.Errorf("status %d\n%s", rec.Code, body)
			}
			// The game's output goes to Foreman's log, never to the page.
			for _, leak := range []string{"docker is gone", "secret game output"} {
				if strings.Contains(body, leak) {
					t.Errorf("%q reached the page", leak)
				}
			}
		})
	}
}

// SPEC 5.6: a string whose settings layout the world's version cannot read is refused on the form, with the versions that would work.
func TestGenerateRefusesAnExchangeStringWithAnotherLayout(t *testing.T) {
	hn := newGenerateHarness(t, nil)
	c := hn.signIn()
	hn.withActive("2.1.24")
	rec := hn.postLong(PathGenerate, generateForm("mode", "exchange", "exchange", fixture(t, "2.1.9.txt"), "game_version_typed", "2.1.24", "map_key", "2.1|1"), c)
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "map settings do not fit Factorio 2.1.24") || !strings.Contains(body, "up to, but not including,") || hn.worldCount() != 1 {
		t.Errorf("status %d, %d worlds\n%s", rec.Code, hn.worldCount(), body)
	}
	// A string made by the world's own version is fine.
	rec = hn.postLong(PathGenerate, generateForm("mode", "exchange", "exchange", fixture(t, "2.1.24.txt"), "game_version_typed", "2.1.24", "map_key", "2.1|1"), c)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("own version: %d", rec.Code)
	}
}

func pictureOf(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `src="`+PathGeneratePreview+"/")
	if i < 0 {
		t.Fatalf("no picture in the page:\n%s", body)
	}
	rest := body[i+len(`src="`):]
	return rest[:strings.IndexByte(rest, '"')]
}

// SPEC 5.6: a picture is served as a PNG that is never cached, only to the user who made it, and only for 15 minutes.
func TestPreviewPicturesAreServedOnlyToTheirUser(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	hn.withActive("2.0.77")
	pic := pictureOf(t, hn.postLong(PathGeneratePreview, generateForm(), c).Body.String())

	rec := hn.get(pic, withCookie(c))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Header().Get("Cache-Control") != "no-store" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(rec.Body.String(), "\x89PNG") {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}

	hn.addUser("bob", testPassword)
	bob := sessionCookie(hn.post(PathSignIn, signInForm("bob", testPassword)))
	if rec := hn.get(pic, withCookie(bob)); rec.Code != http.StatusNotFound {
		t.Errorf("another user got status %d, want 404", rec.Code)
	}
	if rec := hn.get(pic); rec.Code != http.StatusSeeOther {
		t.Errorf("signed out got status %d, want a redirect to sign in", rec.Code)
	}

	// Only the planets of the preview, and only its token.
	parts := strings.Split(pic, "/")
	token := parts[len(parts)-2]
	for _, p := range []string{
		PathGeneratePreview + "/" + token + "/zzz", PathGeneratePreview + "/" + token + "/..%2Fetc",
		PathGeneratePreview + "/wrong/nauvis", PathGeneratePreview + "/" + token[1:] + "/nauvis",
	} {
		if rec := hn.get(p, withCookie(c)); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, rec.Code)
		}
	}

	hn.clock.Advance(previewTTL + time.Second)
	if rec := hn.get(pic, withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("after 15 minutes: status %d, want 404", rec.Code)
	}
}

// A user keeps one preview: a new one replaces the last.
func TestANewPreviewReplacesTheUsersLastOne(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	hn.withActive("2.0.77")
	first := pictureOf(t, hn.postLong(PathGeneratePreview, generateForm(), c).Body.String())
	second := pictureOf(t, hn.postLong(PathGeneratePreview, generateForm(), c).Body.String())
	if first == second {
		t.Fatal("the same token twice")
	}
	if rec := hn.get(first, withCookie(c)); rec.Code != http.StatusNotFound {
		t.Errorf("the old preview is still served: %d", rec.Code)
	}
	if rec := hn.get(second, withCookie(c)); rec.Code != http.StatusOK {
		t.Errorf("the new preview: %d", rec.Code)
	}
}

func TestPreviewStoreCapsWhatItHolds(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	one := []game.PNG{{Planet: "nauvis", Data: pngBytes}}

	var tokens []string
	for user := range int64(maxPreviews + 3) {
		tok, err := st.put(user, one)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, tok)
	}
	if _, ok := st.get(tokens[0], 0, "nauvis"); ok {
		t.Error("the oldest preview survived the cap")
	}
	if _, ok := st.get(tokens[len(tokens)-1], int64(maxPreviews+2), "nauvis"); !ok {
		t.Error("the newest preview is gone")
	}
	if _, ok := st.get(tokens[len(tokens)-1], 0, "nauvis"); ok {
		t.Error("another user read the preview")
	}

	big := []game.PNG{{Planet: "nauvis", Data: make([]byte, maxPreviewBytes+1)}}
	if _, err := st.put(1, big); err == nil {
		t.Error("pictures over the total cap were kept")
	}
	tokenA, _ := st.put(2, one)
	now = now.Add(previewTTL)
	if _, ok := st.get(tokenA, 2, "nauvis"); ok {
		t.Error("an expired preview was served")
	}
	if len(st.items) != 0 || len(st.order) != 0 {
		t.Errorf("expired previews kept: %d, %d", len(st.items), len(st.order))
	}
}
