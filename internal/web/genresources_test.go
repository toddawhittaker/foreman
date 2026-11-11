package web

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/resource"
	"github.com/toddawhittaker/foreman/internal/territory"
)

// The previewer most tests use counts no resources; they never ask.
func (f *fakePreviewer) Resources(context.Context, game.PreviewRequest, string) (resource.Result, error) {
	return resource.Result{}, errors.New("the fake previewer counts no resources")
}

// fakeResources draws pictures and answers resource counts as set.
type fakeResources struct {
	fakePreviewer
	runs    []game.PreviewRequest
	planets []string
	res     resource.Result
	err     error
}

func (f *fakeResources) Resources(_ context.Context, req game.PreviewRequest, planet string) (resource.Result, error) {
	f.runs = append(f.runs, req)
	f.planets = append(f.planets, planet)
	return f.res, f.err
}

// nauvisCount is a big iron patch, a small iron patch, a copper patch and an
// oil field of three wells, largest box first as resource.Parse sorts them.
var nauvisCount = resource.Result{
	Planet: "nauvis",
	Resources: map[string]resource.Resource{
		"iron-ore":   {Total: 12_595_678},
		"copper-ore": {Total: 1_500_000},
		"crude-oil":  {Total: 900_000, Infinite: true, Normal: 300_000},
	},
	Patches: []resource.Patch{
		{Name: "iron-ore", X: -50, Y: -30, Amount: 12_345_678, Entities: 900, Box: [4]int{-70, -55, -45, -5}},
		{Name: "crude-oil", X: 50, Y: -50, Amount: 900_000, Entities: 3, Box: [4]int{46, -54, 56, -46}, Normal: 300_000},
		{Name: "copper-ore", X: 30, Y: 40, Amount: 1_500_000, Entities: 80, Box: [4]int{26, 36, 34, 44}},
		// 65 tiles below the large patch, so its label is not hidden beside it (SPEC 5.6).
		{Name: "iron-ore", X: -40, Y: 63, Amount: 250_000, Entities: 30, Box: [4]int{-43, 60, -37, 66}},
	},
}

func newResourceHarness(t *testing.T) (*harness, *fakeResources, *http.Cookie) {
	t.Helper()
	prev := &fakeResources{res: nauvisCount}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.1.24")
	return hn, prev, c
}

// planetForm is the large view's request for one planet's amounts on 2.1
// with Space Age.
func planetForm(planet string, extra ...string) url.Values {
	return generateForm(append([]string{"game_version_typed", "2.1.24", "map_key", "2.1|1", "seed", "123456", "planet", planet}, extra...)...)
}

// SPEC 5.6: the amounts of a planet are counted once for a set of settings
// and answered from the store after; another planet or other settings count
// again. The fragment holds the labels, largest box first, and the text list.
func TestResourcesAnswerFromTheStore(t *testing.T) {
	hn, prev, c := newResourceHarness(t)
	hx := withHeader("HX-Request", "true")
	rec := hn.postLong(pathResources, planetForm("nauvis"), c, hx)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || len(prev.runs) != 1 || prev.planets[0] != "nauvis" || prev.runs[0].Seed != 123456 || prev.runs[0].Size != game.PreviewSizeDefault {
		t.Fatalf("status %d, runs %+v %v", rec.Code, prev.runs, prev.planets)
	}
	for _, want := range []string{
		`<div id="resource-reply">`,
		`<span class="resources" aria-hidden="true">`,
		// The middle of tile -50 of a 1280 picture, tiles -640 to 639, is 590.5 px in.
		`<span class="resources__label" style="left: 46.133%; top: 47.695%; --x: 46.133; --y: 47.695" data-amount="12345678" data-kind="amount"></span>`,
		`data-amount="3" data-kind="yield"`,
		`<summary>Resource amounts as text (4 patches)</summary>`,
		"Positions are in tiles from where players start, as (x, y).",
		`<h4>Iron ore: 12,595,678 in total</h4>`,
		`<li>12,345,678 at (-50, -30)</li><li>250,000 at (-40, 63)</li>`,
		`<h4>Copper ore: 1,500,000 in total</h4>`,
		`<h4>Crude oil: 300% yield in total</h4>`,
		`<li>300% yield at (50, -50)</li>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the fragment lacks %q:\n%s", want, body)
		}
	}
	// The labels keep the server's order: the largest box first.
	if a, b := strings.Index(body, `data-amount="12345678"`), strings.Index(body, `data-amount="3"`); a < 0 || b < a ||
		strings.Index(body, `data-amount="1500000"`) < b || strings.Index(body, `data-amount="250000"`) < strings.Index(body, `data-amount="1500000"`) {
		t.Errorf("labels out of the server's order:\n%s", body)
	}
	// The lists go largest total first.
	if i, o := strings.Index(body, "Iron ore:"), strings.Index(body, "Crude oil:"); o < 0 || strings.Index(body, "Copper ore:") < i || o < strings.Index(body, "Copper ore:") {
		t.Errorf("lists out of order:\n%s", body)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "<form") || strings.Contains(body, "message--error") {
		t.Error("the fragment is a page, or an error")
	}
	hn.postLong(pathResources, planetForm("nauvis"), c, hx)
	if len(prev.runs) != 1 {
		t.Errorf("the same settings counted again: %d runs", len(prev.runs))
	}
	hn.postLong(pathResources, planetForm("vulcanus"), c, hx)
	hn.postLong(pathResources, planetForm("nauvis", "seed", "7"), c, hx)
	if len(prev.runs) != 3 || prev.planets[1] != "vulcanus" || prev.runs[2].Seed != 7 {
		t.Errorf("another planet or seed did not count: %v %+v", prev.planets, prev.runs)
	}
	// The count of other settings did not replace the user's preview: the
	// first settings still answer from it, and seed 7 counts again.
	hn.postLong(pathResources, planetForm("nauvis"), c, hx)
	hn.postLong(pathResources, planetForm("nauvis", "seed", "7"), c, hx)
	if len(prev.runs) != 4 {
		t.Errorf("a count of other settings replaced the preview: %d runs", len(prev.runs))
	}
}

// SPEC 5.6: a resource is named as the planet's map generator control is
// (Vulcanus's coal is "Coal"); one with no control shows its game name; a
// count with no patches says so.
func TestResourceNamesAndAnEmptyCount(t *testing.T) {
	hn, prev, c := newResourceHarness(t)
	prev.res = resource.Result{
		Planet: "vulcanus",
		Resources: map[string]resource.Resource{
			"coal":                 {Total: 10},
			"sulfuric-acid-geyser": {Total: 600_000, Infinite: true, Normal: 100_000},
			"mystery-ore":          {Total: 5},
		},
		Patches: []resource.Patch{
			{Name: "coal", Amount: 10, Entities: 1},
			{Name: "sulfuric-acid-geyser", X: 3, Amount: 600_000, Entities: 2, Normal: 100_000},
			{Name: "mystery-ore", Amount: 5, Entities: 1},
		},
	}
	body := hn.postLong(pathResources, planetForm("vulcanus"), c, withHeader("HX-Request", "true")).Body.String()
	for _, want := range []string{"<h4>Coal: 10 in total</h4>", "<h4>Sulfuric acid geyser: 600% yield in total</h4>", "<h4>mystery-ore: 5 in total</h4>", "(3 patches)"} {
		if !strings.Contains(body, want) {
			t.Errorf("lacks %q:\n%s", want, body)
		}
	}
	prev.res = resource.Result{Planet: "nauvis"}
	body = hn.postLong(pathResources, planetForm("nauvis"), c, withHeader("HX-Request", "true")).Body.String()
	if !strings.Contains(body, "Resource amounts as text (0 patches)") || !strings.Contains(body, "This picture has no resource patches.") ||
		strings.Contains(body, "resources__label") {
		t.Errorf("an empty count:\n%s", body)
	}
}

// SPEC 5.6: each failure says what happened and what to do, with Try again
// where asking again can work; settings that are not valid count nothing.
func TestResourceFailuresAreWorded(t *testing.T) {
	cases := []struct {
		name  string
		form  url.Values
		err   error
		want  string
		runs  int
		retry bool
	}{
		{"busy", planetForm("nauvis"), game.ErrPreviewBusy, "Another map preview is being made, so the resources were not counted.", 1, true},
		{"timeout", planetForm("nauvis"), game.ErrPreviewTimeout, "Counting the resources took too long and was stopped.", 1, true},
		{"no image", planetForm("nauvis"), game.ErrPreviewImageMissing, "the game image for Factorio 2.1.24 is not downloaded here", 1, false},
		{"memory", planetForm("nauvis"), &game.PreviewError{ExitCode: 137, OOM: true}, "The game ran out of memory counting the resources.", 1, true},
		{"game failed", planetForm("nauvis"), &game.PreviewError{ExitCode: 1}, "The game could not count the resources.", 1, true},
		{"invalid file", planetForm("nauvis"), fmt.Errorf("patch 3: %w", resource.ErrInvalid), "The game could not count the resources.", 1, false},
		{"bad settings", planetForm("nauvis", "preview_size", "999"), nil, "The settings have problems, so the resources were not counted.", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hn, prev, c := newResourceHarness(t)
			prev.err = tc.err
			rec := hn.postLong(pathResources, tc.form, c, withHeader("HX-Request", "true"))
			body := rec.Body.String()
			if rec.Code != http.StatusOK || len(prev.runs) != tc.runs {
				t.Fatalf("status %d, %d runs", rec.Code, len(prev.runs))
			}
			if !strings.Contains(body, tc.want) || !strings.Contains(body, "message--error") || strings.Contains(body, `class="resources"`) {
				t.Errorf("want %q:\n%s", tc.want, body)
			}
			if strings.Contains(body, "data-resource-retry") != tc.retry {
				t.Errorf("retry offered: %v, want %v", !tc.retry, tc.retry)
			}
		})
	}
}

// SPEC 14.1: the planet is one of the world's own; anything else is refused
// before a game runs.
func TestResourcesRefuseAPlanetTheWorldLacks(t *testing.T) {
	hn, prev, c := newResourceHarness(t)
	for _, form := range []url.Values{
		planetForm(""),
		planetForm("../nauvis"),
		planetForm("mars"),
		// Without Space Age the world has Nauvis only.
		planetForm("vulcanus", "space_age", "", "map_key", "2.1|0"),
	} {
		if rec := hn.postLong(pathResources, form, c, withHeader("HX-Request", "true")); rec.Code != http.StatusBadRequest {
			t.Errorf("planet %q: status %d", form.Get("planet"), rec.Code)
		}
	}
	if len(prev.runs) != 0 {
		t.Errorf("%d runs", len(prev.runs))
	}
}

// SPEC 14.7: the route is for a signed-in user's own page only.
func TestResourcesNeedSignInAndTheSameOrigin(t *testing.T) {
	hn, prev, c := newResourceHarness(t)
	hn.wantRedirect(hn.postLong(pathResources, planetForm("nauvis"), nil), PathSignIn)
	if rec := hn.postLong(pathResources, planetForm("nauvis"), c, withHeader("Sec-Fetch-Site", "cross-site")); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
	if len(prev.runs) != 0 {
		t.Errorf("%d runs", len(prev.runs))
	}
	none := newGenerateHarness(t, nil)
	nc := none.signIn()
	if rec := none.postLong(pathResources, planetForm("nauvis"), nc); rec.Code != http.StatusNotFound {
		t.Errorf("no previewer: %d", rec.Code)
	}
}

// SPEC 5.6: counts are kept per planet in the user's preview and count in
// its bytes; a copy handed out earlier never sees a later count; new
// settings drop them; a count over the whole limit is refused.
func TestPreviewStoreKeepsResourcesPerPlanet(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	tok, _ := st.add(1, testKey("k1"), "World", "7", onePNG("nauvis"))
	before := st.byUser[1].bytes
	held, _ := st.entry(tok, 1)
	if tok2, err := st.addResources(1, testKey("k1"), "World", "7", "nauvis", nauvisCount); err != nil || tok2 != tok {
		t.Fatalf("token %q, %v", tok2, err)
	}
	if held.resources != nil {
		t.Error("a copy taken before the count was written in place")
	}
	held, _ = st.entry(tok, 1)
	_, _ = st.addResources(1, testKey("k1"), "World", "7", "vulcanus", nauvisCount)
	if _, ok := held.resources["vulcanus"]; ok {
		t.Error("a later planet's count reached a copy handed out before it")
	}
	if want := before + 2*resourceBytes(&nauvisCount); st.byUser[1].bytes != want || resourceBytes(&nauvisCount) == 0 {
		t.Errorf("%d bytes, want %d", st.byUser[1].bytes, want)
	}
	// Counted again, a planet replaces its count rather than adding to the total.
	_, _ = st.addResources(1, testKey("k1"), "World", "7", "nauvis", nauvisCount)
	if want := before + 2*resourceBytes(&nauvisCount); st.byUser[1].bytes != want {
		t.Errorf("counted twice: %d bytes, want %d", st.byUser[1].bytes, want)
	}
	if _, ok := st.resourcesOf(1, testKey("k1"), "nauvis"); !ok {
		t.Error("the count is not found")
	}
	if _, ok := st.resourcesOf(1, testKey("k1"), "gleba"); ok {
		t.Error("a planet not counted is found")
	}
	if _, ok := st.resourcesOf(2, testKey("k1"), "nauvis"); ok {
		t.Error("another user found it")
	}
	// More pictures of the same settings keep the counts; other settings drop them.
	_, _ = st.add(1, testKey("k1"), "World", "7", onePNG("gleba"))
	if _, ok := st.resourcesOf(1, testKey("k1"), "nauvis"); !ok {
		t.Error("another picture lost the count")
	}
	_, _ = st.add(1, testKey("k2"), "World", "8", onePNG("nauvis"))
	if _, ok := st.resourcesOf(1, testKey("k2"), "nauvis"); ok {
		t.Error("new settings kept the old count")
	}
	// Kept 15 minutes after the last look, like the pictures.
	_, _ = st.addResources(1, testKey("k2"), "World", "8", "nauvis", nauvisCount)
	now = now.Add(previewTTL)
	if _, ok := st.resourcesOf(1, testKey("k2"), "nauvis"); ok {
		t.Error("an expired preview's count is found")
	}
	tooBig := resource.Result{Patches: make([]resource.Patch, maxPreviewBytes/96+1)}
	if _, err := st.addResources(4, testKey("k"), "", "1", "nauvis", tooBig); err == nil {
		t.Error("a count over the whole limit was kept")
	}
}

// SPEC 5.6, 17: the large view has the box, off and with no name so the
// form never sends it, and the counting line hidden until it counts.
func TestLargeViewHasTheResourceBox(t *testing.T) {
	hn, _, c := newResourceHarness(t)
	body := hn.get(PathGenerate, withCookie(c)).Body.String()
	for _, want := range []string{
		`<input type="checkbox" id="large-amounts"> Show resource amounts</label>`,
		`<div class="wait resource-amounts__counting" id="large-counting" hidden>`,
		`<p class="hint" id="large-counting-text">Counting resources in the map… This can take up to a minute.</p>`,
		// The sentence is said as a count starts; the bar is not named again (DESIGN 8.8).
		`<progress aria-hidden="true"></progress>`,
		`data-ready="Resource amounts ready."`,
		`<template id="resource-failed">`,
		`<p class="visually-hidden" id="large-amounts-news" role="status"></p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}

// SPEC 5.6: a count or territories that arrive after the person moved on to
// other settings are answered but not kept, so they never replace the newer
// preview and its pictures.
func TestLateResultsDoNotReplaceANewerPreview(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	tok, _ := st.add(1, testKey("new"), "World", "8", onePNG("nauvis"))
	if got, err := st.addResources(1, testKey("old"), "World", "7", "nauvis", nauvisCount); err != nil || got != "" {
		t.Errorf("a late count: token %q, %v", got, err)
	}
	if got, err := st.addTerritories(1, testKey("old"), "World", "7", 1280, threeTerritories); err != nil || got != "" {
		t.Errorf("late territories: token %q, %v", got, err)
	}
	e := st.byUser[1]
	if e.token != tok || e.key.all != "new" || len(e.images) != 1 || e.resources != nil || e.territories != nil || e.bytes != len(pngBytes) {
		t.Errorf("the newer preview changed: %+v", e)
	}
	if _, ok := st.resourcesOf(1, testKey("old"), "nauvis"); ok {
		t.Error("the late count is found")
	}
	// With no preview at all, a result starts one, as before.
	if got, _ := st.addResources(2, testKey("k"), "", "1", "nauvis", nauvisCount); got == "" {
		t.Error("a user with no preview kept no count")
	}
	if got, _ := st.addTerritories(3, testKey("k"), "", "1", 1280, threeTerritories); got == "" {
		t.Error("a user with no preview kept no territories")
	}
}

// A resource entity is shown under the name of the map generator control
// that sets it, on every planet; one with no control keeps its game name.
func TestResourceNameFindsTheControl(t *testing.T) {
	for _, c := range []struct{ line, planet, entity string }{
		{"2.1", "nauvis", "iron-ore"},
		{"2.1", "nauvis", "crude-oil"},
		{"2.1", "vulcanus", "coal"},
		{"2.1", "vulcanus", "tungsten-ore"},
		{"2.1", "vulcanus", "sulfuric-acid-geyser"},
		{"2.1", "gleba", "stone"},
		{"2.1", "aquilo", "crude-oil"},
		{"2.1", "aquilo", "lithium-brine"},
	} {
		got := resourceName(i18n.English, c.line, c.planet, c.entity)
		if got == c.entity || strings.Contains(got, "_") {
			t.Errorf("%s %s: %q is not the control's name (%q)", c.planet, c.entity, c.entity, got)
		}
	}
	if got := resourceName(i18n.English, "2.1", "vulcanus", "no-such-ore"); got != "no-such-ore" {
		t.Errorf("a resource with no control is shown as %q", got)
	}
}

// SPEC 5.6: with amountsQuery a download draws in the amounts the preview
// already holds for its planet, and a planet without them is served plain:
// no download, zip or page ever counts (the fake records every count).
func TestDownloadsDrawHeldAmountsAndNeverCount(t *testing.T) {
	hn, prev, c := newResourceHarness(t)
	prev.png = realPNG(t, game.PreviewSizeDefault)
	hx := withHeader("HX-Request", "true")
	all := planetForm("")
	all.Del("planet")
	page := func() string { return hn.postLong(PathGeneratePreview, all, c, hx).Body.String() }
	body := page()
	token := strings.Split(pictureOf(t, body), "/")[3]
	base := PathGeneratePreview + "/" + token
	if strings.Contains(body, "data-counted") {
		t.Error("a planet is marked counted before any count")
	}
	name := func(rec *httptest.ResponseRecorder) string { return rec.Header().Get("Content-Disposition") }

	// Nothing held: asked for, the amounts are left out, and nothing is counted.
	rec := hn.getLong(base+"/nauvis/download?amounts=1", c)
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), prev.png) || strings.Contains(name(rec), "amounts") {
		t.Errorf("uncounted: status %d, drawn %v, named %q", rec.Code, !bytes.Equal(rec.Body.Bytes(), prev.png), name(rec))
	}
	hn.getLong(base+"/all.zip?amounts=1", c)
	if len(prev.runs) != 0 {
		t.Fatalf("a download counted: %v", prev.planets)
	}

	hn.postLong(pathResources, planetForm("nauvis"), c, hx)
	if len(prev.runs) != 1 {
		t.Fatalf("%d counts", len(prev.runs))
	}
	body = page()
	if strings.Count(body, "data-counted") != 2 || !regexp.MustCompile(`/nauvis/download"[^>]*data-counted`).MatchString(body) ||
		!regexp.MustCompile(`/all\.zip"[^>]*data-counted`).MatchString(body) {
		t.Errorf("only Nauvis and the zip are marked counted:\n%s", body)
	}

	rec = hn.getLong(base+"/nauvis/download?amounts=1", c)
	if rec.Code != http.StatusOK || bytes.Equal(rec.Body.Bytes(), prev.png) || !strings.Contains(name(rec), `filename="Fresh_start-123456-nauvis-amounts.png"`) {
		t.Fatalf("counted: status %d, named %q", rec.Code, name(rec))
	}
	if _, err := png.Decode(bytes.NewReader(rec.Body.Bytes())); err != nil {
		t.Errorf("the drawn picture is not a PNG: %v", err)
	}
	if rec := hn.getLong(base+"/nauvis/download", c); !bytes.Equal(rec.Body.Bytes(), prev.png) {
		t.Error("a download not asking for the amounts drew them")
	}
	if rec := hn.getLong(base+"/vulcanus/download?amounts=1", c); !bytes.Equal(rec.Body.Bytes(), prev.png) || strings.Contains(name(rec), "amounts") {
		t.Error("Vulcanus, not counted, was drawn on")
	}

	zrec := hn.getLong(base+"/all.zip?amounts=1", c)
	zr, err := zip.NewReader(bytes.NewReader(zrec.Body.Bytes()), int64(zrec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	drawn := 0
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		isNauvis := strings.Contains(f.Name, "nauvis")
		if isNauvis != !bytes.Equal(b, prev.png) || isNauvis != (f.Name == "Fresh_start-123456-nauvis-amounts.png") {
			t.Errorf("%s: drawn on is %v", f.Name, !bytes.Equal(b, prev.png))
		}
		if isNauvis {
			drawn++
		}
	}
	if len(zr.File) != 5 || drawn != 1 {
		t.Errorf("%d files, %d drawn", len(zr.File), drawn)
	}
	if len(prev.runs) != 1 {
		t.Errorf("a download counted: %v", prev.planets)
	}
}

// SPEC 5.6: Vulcanus's download with both draws the territories and the
// amounts in one picture, the amounts worded as the page words them in the
// page's language.
func TestDownloadDrawsTerritoriesAndAmountsTogether(t *testing.T) {
	src := realPNG(t, 256)
	res := nauvisCount
	e := previewEntry{territories: &threeTerritories, size: 256, resources: map[string]*resource.Result{"vulcanus": &res}}
	orig := drawPNG
	t.Cleanup(func() { drawPNG = orig })
	var got territory.Overlay
	drawPNG = func(_ context.Context, b []byte, ov territory.Overlay, _ func(string) string) ([]byte, error) {
		got = ov
		return b, nil
	}
	r := httptest.NewRequest(http.MethodGet, "/x?territories=1&amounts=1", nil)
	r = r.WithContext(context.WithValue(r.Context(), localeKey{}, i18n.Locale("de")))
	_, suffix, ok, err := drawnDownload(r, e, "vulcanus", src)
	if err != nil || !ok || suffix != "-territories-amounts" {
		t.Fatalf("suffix %q, ok %v, %v", suffix, ok, err)
	}
	if got.Size != 256 || len(got.Shapes) == 0 {
		t.Errorf("the territories are not drawn: %+v", got)
	}
	var texts []string
	for _, cap := range got.Captions {
		texts = append(texts, cap.Text)
	}
	if want := []string{"12,3 Mio.", "300 %", "1,5 Mio.", "250.000"}; !slices.Equal(texts, want) {
		t.Errorf("captions %q, want %q", texts, want)
	}
	// The largest patch's label sits where the page puts it: tile -50 of 256 is 78.5 px in.
	if c := got.Captions[0]; math.Abs(c.U-78.5/256) > 1e-4 {
		t.Errorf("the first caption is at %v", c.U)
	}
	// A picture of another size than its territories is refused, not drawn wrongly.
	e.size = 128
	if _, _, _, err := drawnDownload(httptest.NewRequest(http.MethodGet, "/x?territories=1&amounts=1", nil), e, "vulcanus", src); err == nil {
		t.Error("a 256 picture drew 128 territories")
	}
	e.size = 256
	// Only the territories: the amounts are not drawn, even though held.
	r = httptest.NewRequest(http.MethodGet, "/x?territories=1", nil)
	if _, suffix, _, _ = drawnDownload(r, e, "vulcanus", src); suffix != "-territories" || len(got.Captions) != 0 {
		t.Errorf("suffix %q, %d captions", suffix, len(got.Captions))
	}
}

// SPEC 5.6: the One planet picture has its own box, unticked and without a
// name, script only; All planets has none.
func TestOnePlanetHasTheResourceBox(t *testing.T) {
	hn, _, c := newResourceHarness(t)
	hx := withHeader("HX-Request", "true")
	one := planetForm("nauvis", "auto", "1")
	body := hn.postLong(PathGeneratePreview, one, c, hx).Body.String()
	for _, want := range []string{
		`<div class="js-only"><div class="resource-box"><label class="check"><input type="checkbox" id="preview-amounts"> Show resource amounts</label>`,
		`<span class="hint resource-box__busy" id="preview-amounts-busy" aria-hidden="true">Counting…</span></div></div>`,
		`<div class="js-only"><div class="resource-amounts" id="preview-amounts-bar" data-ready="Resource amounts ready.">`,
		`<div class="wait resource-amounts__counting" id="preview-counting" hidden>`,
		`<progress aria-hidden="true"></progress>`,
		`<p class="visually-hidden" id="preview-amounts-news" role="status"></p>`,
		`data-amounts="Download Nauvis with resource amounts (PNG)"`,
		`data-amounts="Download all drawn planets (zip), with resource amounts where counted"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("One planet lacks %q:\n%s", want, body)
		}
	}
	// The box sits above the picture; the wait, the failure and the list under it and its downloads.
	if b, p, slot := strings.Index(body, `id="preview-amounts"`), strings.Index(body, `class="pictures"`), strings.Index(body, `id="preview-amounts-bar"`); b > p || slot < strings.Index(body, "all.zip") {
		t.Errorf("box at %d, pictures at %d, slot at %d", b, p, slot)
	}
	all := planetForm("", "auto", "1", "preview_view", "all")
	all.Del("planet")
	if body := hn.postLong(PathGeneratePreview, all, c, hx).Body.String(); strings.Contains(body, `id="preview-amounts"`) {
		t.Error("All planets has the box")
	}
}

// SPEC 5.6: a label hidden beside a richer patch keeps its list entry, and
// the download draws the page's labels.
func TestHiddenLabelsStayListed(t *testing.T) {
	res := resource.Result{
		Planet:    "nauvis",
		Resources: map[string]resource.Resource{"iron-ore": {Total: 5000}},
		Patches: []resource.Patch{
			{Name: "iron-ore", X: 0, Y: 0, Amount: 4000, Box: [4]int{0, 0, 10, 10}},
			{Name: "iron-ore", X: 20, Y: 0, Amount: 1000, Box: [4]int{20, 0, 22, 2}},
		},
	}
	v := resourceView(i18n.English, "2.0", "nauvis", res, 256)
	if len(v.Labels) != 1 || v.Labels[0].Value != "4000" {
		t.Errorf("labels %+v, want only the 4000 patch", v.Labels)
	}
	if len(v.Lists) != 1 || len(v.Lists[0].Patches) != 2 {
		t.Errorf("lists %+v, want both patches", v.Lists)
	}
	caps := amountCaptions(i18n.English, res, 256)
	if len(caps) != len(v.Labels) || caps[0].U != v.Labels[0].Left/100 || caps[0].V != v.Labels[0].Top/100 {
		t.Errorf("captions %+v differ from labels %+v", caps, v.Labels)
	}
}
