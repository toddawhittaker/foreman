package web

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/world"
)

// chartPNG is what the fake draws from a real game.
var chartPNG = []byte("\x89PNG\r\n\x1a\nreal-game picture")

// fakeCharter is a previewer that can also draw from a real game.
type fakeCharter struct {
	fakePreviewer
	charts   []game.PreviewRequest
	planets  []string
	chartErr error
}

func (f *fakeCharter) Chart(_ context.Context, req game.PreviewRequest, planet string) (game.PNG, error) {
	f.charts = append(f.charts, req)
	f.planets = append(f.planets, planet)
	if f.chartErr != nil {
		return game.PNG{}, f.chartErr
	}
	return game.PNG{Planet: planet, Data: chartPNG}, nil
}

// unfinishedN numbers the worlds unfinishedWorld makes, as names are unique.
var unfinishedN int

// unfinishedWorld makes an Unfinished world of version: new_map and no save.
func unfinishedWorld(t *testing.T, hn *harness, version string) string {
	unfinishedN++
	t.Helper()
	id, err := hn.worlds.Generate(t.Context(), world.NewWorld{Name: fmt.Sprintf("Charted %d", unfinishedN), GameVersion: version, SpaceAge: true})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

var chartSrc = regexp.MustCompile(`<img src="(` + regexp.QuoteMeta(PathGeneratePreview+"-chart") + `/[^"]+)"`)

// SPEC 5.6, 14.3, 14.6: a 2.1 world's planet is drawn from a real game on
// request, with the world and its catalogue's planets in the request; the
// picture is captioned, has its way back, is served to its user, and is kept
// by what makes its map, so asking again runs nothing.
func TestChartDrawsAndKeepsThePicture(t *testing.T) {
	prev := &fakeCharter{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	withWorldCatalogue(t, cerysCatalogue(), nil)
	id := unfinishedWorld(t, hn, "2.1.24")

	form := worldPreviewForm(id, "planet", "cerys", "known_planets", "mars")
	rec := hn.postLong(pathChart, form, c)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || len(prev.charts) != 1 {
		t.Fatalf("status %d, runs %d:\n%s", rec.Code, len(prev.charts), body)
	}
	got := prev.charts[0]
	if got.World != id || !slices.Equal(got.KnownPlanets, cerysCatalogue().PlanetNames()) || prev.planets[0] != "cerys" || got.Seed != 42 {
		t.Errorf("request %+v planet %s: not the world's", got, prev.planets[0])
	}
	for _, want := range []string{`id="chart-reply"`, "From a real game, 1024 tiles", `id="preview-chart-back"`, "Back to the fast preview", `<span translate="no">Cerys</span>`} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in the reply:\n%s", want, body)
		}
	}
	m := chartSrc.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no picture in the reply:\n%s", body)
	}
	pic := httptest.NewRequest(http.MethodGet, html.UnescapeString(m[1]), nil)
	pic.AddCookie(c)
	if r := hn.serve(pic); r.Code != http.StatusOK || r.Body.String() != string(chartPNG) || r.Header().Get("Content-Type") != "image/png" {
		t.Errorf("picture: status %d, type %q", r.Code, r.Header().Get("Content-Type"))
	}
	// Another user cannot read it.
	hn.addUser("bob", testPassword)
	other := hn.signInAgain("bob")
	pic = httptest.NewRequest(http.MethodGet, html.UnescapeString(m[1]), nil)
	pic.AddCookie(other)
	if r := hn.serve(pic); r.Code != http.StatusNotFound {
		t.Errorf("another user's picture: status %d", r.Code)
	}

	// Kept, even past the fast preview's time limit; another preview size
	// makes no other picture.
	hn.clock.Advance(2 * previewTTL)
	c = hn.signInAgain("alice")
	form.Set("preview_size", "2048")
	if rec := hn.postLong(pathChart, form, c); rec.Code != http.StatusOK || len(prev.charts) != 1 || !chartSrc.MatchString(rec.Body.String()) {
		t.Errorf("asked again: status %d, runs %d:\n%s", rec.Code, len(prev.charts), rec.Body.String())
	}
	// Another seed is another map.
	form.Set("seed", "43")
	if hn.postLong(pathChart, form, c); len(prev.charts) != 2 {
		t.Errorf("another seed: runs %d, want 2", len(prev.charts))
	}
}

// SPEC 5.6, 14.6: a planet not in the world's catalogue, a world on another
// game line, a world whose map is made, Generate's own form, and a previewer
// that cannot draw from a real game are all refused before anything runs.
func TestChartRefusals(t *testing.T) {
	prev := &fakeCharter{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	withWorldCatalogue(t, cerysCatalogue(), nil)
	id := unfinishedWorld(t, hn, "2.1.24")
	for _, p := range []string{"mars", "", "../nauvis"} {
		if rec := hn.postLong(pathChart, worldPreviewForm(id, "planet", p, "known_planets", p), c); rec.Code != http.StatusBadRequest {
			t.Errorf("planet %q: status %d", p, rec.Code)
		}
	}
	withWorldCatalogue(t, mapgen.Builtin("2.0", true), nil)
	old := unfinishedWorld(t, hn, "2.0.77")
	form := worldPreviewForm(old, "planet", "nauvis", "map_key", "2.0|1")
	if rec := hn.postLong(pathChart, form, c); rec.Code != http.StatusBadRequest {
		t.Errorf("2.0 world: status %d", rec.Code)
	}
	withWorldCatalogue(t, cerysCatalogue(), nil)
	made, err := hn.worlds.Create(t.Context(), "Made", "2.1.24", true)
	if err != nil {
		t.Fatal(err)
	}
	if rec := hn.postLong(pathChart, worldPreviewForm(made, "planet", "nauvis"), c); rec.Code != http.StatusConflict {
		t.Errorf("a world with its map made: status %d", rec.Code)
	}
	if rec := hn.postLong(pathChart, generateForm("planet", "nauvis"), c); rec.Code != http.StatusBadRequest {
		t.Errorf("Generate's form: status %d", rec.Code)
	}
	if len(prev.charts) != 0 {
		t.Errorf("%d runs for refused requests", len(prev.charts))
	}

	plain := newGenerateHarness(t, &fakePreviewer{})
	pc := plain.signIn()
	pid := unfinishedWorld(t, plain, "2.1.24")
	if rec := plain.postLong(pathChart, worldPreviewForm(pid, "planet", "nauvis"), pc); rec.Code != http.StatusNotFound {
		t.Errorf("no real-game previewer: status %d", rec.Code)
	}
}

// SPEC 5.6, 14.3, 14.6: busy, timeout and out of memory each have their own
// sentence; a helper that gives up, a crash and a refused chart share one,
// even when the refusal also matches the factory map's bad output.
func TestChartFailuresAreWorded(t *testing.T) {
	prev := &fakeCharter{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	withWorldCatalogue(t, cerysCatalogue(), nil)
	id := unfinishedWorld(t, hn, "2.1.24")
	failed := "Foreman could not draw this planet from a real game."
	for _, tc := range []struct {
		err  error
		want string
	}{
		{game.ErrPreviewBusy, "Another map preview is running."},
		{game.ErrPreviewTimeout, "took too long"},
		{&game.PreviewError{OOM: true}, "ran out of memory"},
		{&game.PreviewError{ExitCode: 1}, failed},
		{fmt.Errorf("%w: %w", game.ErrChartBadOutput, game.ErrSaveCopyBadOutput), failed},
		{errors.New("docker gone"), failed},
	} {
		prev.chartErr = tc.err
		rec := hn.postLong(pathChart, worldPreviewForm(id, "planet", "nauvis"), c)
		body := html.UnescapeString(rec.Body.String())
		if rec.Code != http.StatusOK || !strings.Contains(body, tc.want) || strings.Contains(body, "<img") {
			t.Errorf("%v: status %d, want %q in:\n%s", tc.err, rec.Code, tc.want, body)
		}
	}
	// Nothing failed is kept: the next ask runs again.
	prev.chartErr = nil
	n := len(prev.charts)
	if hn.postLong(pathChart, worldPreviewForm(id, "planet", "nauvis"), c); len(prev.charts) != n+1 {
		t.Error("a failure was kept")
	}
}

// SPEC 5.6: the offer is on a 2.1 world's one picture only; a 2.0 world and
// Generate's own preview get the fast preview alone.
func TestChartOfferedOnlyOn21Worlds(t *testing.T) {
	prev := &fakeCharter{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	withWorldCatalogue(t, cerysCatalogue(), nil)
	id := unfinishedWorld(t, hn, "2.1.24")
	body := hn.postLong(PathGeneratePreview, worldPreviewForm(id, "planet", "nauvis"), c).Body.String()
	for _, want := range []string{`id="preview-chart-button"`, "Draw from a real game (about 15 to 40 s)", `data-planet="nauvis"`, `id="preview-chart-tip"`} {
		if !strings.Contains(body, want) {
			t.Errorf("2.1 world: no %q", want)
		}
	}
	if b := hn.postLong(PathGeneratePreview, worldPreviewForm(id, "preview_view", "all", "auto", "1"), c).Body.String(); strings.Contains(b, `id="preview-chart-button"`) {
		t.Error("offered with every planet shown")
	}
	withWorldCatalogue(t, mapgen.Builtin("2.0", true), nil)
	old := unfinishedWorld(t, hn, "2.0.77")
	if b := hn.postLong(PathGeneratePreview, worldPreviewForm(old, "planet", "nauvis", "map_key", "2.0|1"), c).Body.String(); strings.Contains(b, `id="preview-chart-button"`) {
		t.Error("offered on a 2.0 world")
	}
	hn.withActive("2.1.24")
	if b := hn.postLong(PathGeneratePreview, generateForm(), c).Body.String(); strings.Contains(b, `id="preview-chart-button"`) {
		t.Error("offered on Generate's own preview")
	}
}

// SPEC 5.6: only the store's size limit evicts a real-game picture, oldest
// first; the newest stays. A world's pictures go when its map is made.
func TestChartStoreKeepsWithinItsLimit(t *testing.T) {
	clock := &fakeClock{}
	p := newPreviewStore(clock.Now)
	big := make([]byte, maxPreviewBytes/3+1)
	a, _ := p.addChart(1, "a", "w1", big)
	clock.Advance(previewTTL * 4)
	if _, ok := p.chart(a, 1); !ok {
		t.Fatal("dropped by time")
	}
	b, _ := p.addChart(2, "b", "w2", big)
	c, _ := p.addChart(1, "c", "w1", big)
	if _, ok := p.chart(a, 1); ok {
		t.Error("the oldest is kept past the limit")
	}
	if _, ok := p.chart(c, 1); !ok {
		t.Error("the newest went")
	}
	p.dropCharts("w1")
	if _, ok := p.chart(c, 1); ok {
		t.Error("a world's picture is kept after its map is made")
	}
	if _, ok := p.chart(b, 2); !ok {
		t.Error("another world's picture went")
	}
}
