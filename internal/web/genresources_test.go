package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/resource"
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
		{Name: "iron-ore", X: -40, Y: -30, Amount: 250_000, Entities: 30, Box: [4]int{-43, -33, -37, -27}},
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
		`<li>12,345,678 at (-50, -30)</li><li>250,000 at (-40, -30)</li>`,
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
	tok, _ := st.add(1, "k1", "World", "7", onePNG("nauvis"))
	before := st.byUser[1].bytes
	held, _ := st.entry(tok, 1)
	if tok2, err := st.addResources(1, "k1", "World", "7", "nauvis", nauvisCount); err != nil || tok2 != tok {
		t.Fatalf("token %q, %v", tok2, err)
	}
	if held.resources != nil {
		t.Error("a copy taken before the count was written in place")
	}
	held, _ = st.entry(tok, 1)
	_, _ = st.addResources(1, "k1", "World", "7", "vulcanus", nauvisCount)
	if _, ok := held.resources["vulcanus"]; ok {
		t.Error("a later planet's count reached a copy handed out before it")
	}
	if want := before + 2*resourceBytes(&nauvisCount); st.byUser[1].bytes != want || resourceBytes(&nauvisCount) == 0 {
		t.Errorf("%d bytes, want %d", st.byUser[1].bytes, want)
	}
	// Counted again, a planet replaces its count rather than adding to the total.
	_, _ = st.addResources(1, "k1", "World", "7", "nauvis", nauvisCount)
	if want := before + 2*resourceBytes(&nauvisCount); st.byUser[1].bytes != want {
		t.Errorf("counted twice: %d bytes, want %d", st.byUser[1].bytes, want)
	}
	if _, ok := st.resourcesOf(1, "k1", "nauvis"); !ok {
		t.Error("the count is not found")
	}
	if _, ok := st.resourcesOf(1, "k1", "gleba"); ok {
		t.Error("a planet not counted is found")
	}
	if _, ok := st.resourcesOf(2, "k1", "nauvis"); ok {
		t.Error("another user found it")
	}
	// More pictures of the same settings keep the counts; other settings drop them.
	_, _ = st.add(1, "k1", "World", "7", onePNG("gleba"))
	if _, ok := st.resourcesOf(1, "k1", "nauvis"); !ok {
		t.Error("another picture lost the count")
	}
	_, _ = st.add(1, "k2", "World", "8", onePNG("nauvis"))
	if _, ok := st.resourcesOf(1, "k2", "nauvis"); ok {
		t.Error("new settings kept the old count")
	}
	// Kept 15 minutes after the last look, like the pictures.
	_, _ = st.addResources(1, "k2", "World", "8", "nauvis", nauvisCount)
	now = now.Add(previewTTL)
	if _, ok := st.resourcesOf(1, "k2", "nauvis"); ok {
		t.Error("an expired preview's count is found")
	}
	tooBig := resource.Result{Patches: make([]resource.Patch, maxPreviewBytes/96+1)}
	if _, err := st.addResources(4, "k", "", "1", "nauvis", tooBig); err == nil {
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
		`<div class="wait large-view__counting" id="large-counting" hidden>`,
		`<p class="hint" id="large-counting-text">Counting resources in the map… This can take up to a minute.</p>`,
		`<progress aria-labelledby="large-counting-text"></progress>`,
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
	tok, _ := st.add(1, "new", "World", "8", onePNG("nauvis"))
	if got, err := st.addResources(1, "old", "World", "7", "nauvis", nauvisCount); err != nil || got != "" {
		t.Errorf("a late count: token %q, %v", got, err)
	}
	if got, err := st.addTerritories(1, "old", "World", "7", 1280, threeTerritories); err != nil || got != "" {
		t.Errorf("late territories: token %q, %v", got, err)
	}
	e := st.byUser[1]
	if e.token != tok || e.key != "new" || len(e.images) != 1 || e.resources != nil || e.territories != nil || e.bytes != len(pngBytes) {
		t.Errorf("the newer preview changed: %+v", e)
	}
	if _, ok := st.resourcesOf(1, "old", "nauvis"); ok {
		t.Error("the late count is found")
	}
	// With no preview at all, a result starts one, as before.
	if got, _ := st.addResources(2, "k", "", "1", "nauvis", nauvisCount); got == "" {
		t.Error("a user with no preview kept no count")
	}
	if got, _ := st.addTerritories(3, "k", "", "1", 1280, threeTerritories); got == "" {
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
