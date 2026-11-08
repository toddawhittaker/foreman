package web

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
)

func onePNG(planet string) []game.PNG { return []game.PNG{{Planet: planet, Data: pngBytes}} }

// SPEC 5.6: the same settings find the pictures already drawn, planet by
// planet; other settings replace the user's preview.
func TestPreviewStoreFindsTheSameSettings(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	tok, err := st.add(1, "k1", "World", "7", onePNG("nauvis"))
	if err != nil {
		t.Fatal(err)
	}
	if got, miss := st.missing(1, "k1", "World", []string{"nauvis"}); got != tok || miss != nil {
		t.Errorf("same settings: token %q, missing %v", got, miss)
	}
	if got, miss := st.missing(1, "k1", "World", []string{"nauvis", "gleba"}); got != "" || !slices.Equal(miss, []string{"gleba"}) {
		t.Errorf("one planet more: token %q, missing %v", got, miss)
	}
	if got, miss := st.missing(2, "k1", "World", []string{"nauvis"}); got != "" || len(miss) != 1 {
		t.Errorf("another user found the preview: %q, %v", got, miss)
	}
	tok2, _ := st.add(1, "k1", "World", "7", onePNG("gleba"))
	if tok2 != tok {
		t.Error("a second planet of the same settings changed the token")
	}
	if _, ok := st.get(tok, 1, "nauvis"); !ok {
		t.Error("the first planet was lost when the second was drawn")
	}
	tok3, _ := st.add(1, "k2", "World", "8", onePNG("nauvis"))
	if tok3 == tok {
		t.Error("new settings kept the old token")
	}
	if _, ok := st.get(tok, 1, "nauvis"); ok {
		t.Error("the old settings' pictures are still served")
	}
}

// SPEC 5.6: at most 8 users' previews, 96 MiB in all, 16 MiB a picture, and
// 15 minutes.
func TestPreviewStoreKeepsTheLimits(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	var tokens []string
	for user := range int64(maxPreviews + 3) {
		tok, err := st.add(user, "k", "", "1", onePNG("nauvis"))
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, tok)
	}
	if len(st.byUser) != maxPreviews {
		t.Errorf("%d previews held, want %d", len(st.byUser), maxPreviews)
	}
	if _, ok := st.get(tokens[0], 0, "nauvis"); ok {
		t.Error("the oldest preview survived the cap")
	}
	if _, ok := st.get(tokens[len(tokens)-1], int64(maxPreviews+2), "nauvis"); !ok {
		t.Error("the newest preview is gone")
	}

	if _, err := st.add(1, "k", "", "1", []game.PNG{{Planet: "nauvis", Data: make([]byte, game.MaxPreviewPNG+1)}}); err == nil {
		t.Error("a picture over the cap was kept")
	}
	// Six pictures at the cap fill the 96 MiB; the users before go to make room.
	pic := make([]byte, game.MaxPreviewPNG)
	for user := range int64(6) {
		if _, err := st.add(100+user, "k", "", "1", []game.PNG{{Planet: "nauvis", Data: pic}}); err != nil {
			t.Fatal(err)
		}
	}
	if total := st.totalLocked(); total > maxPreviewBytes {
		t.Errorf("%d bytes held, over %d", total, maxPreviewBytes)
	}
	if _, err := st.add(100, "k", "", "1", []game.PNG{{Planet: "gleba", Data: pic}, {Planet: "fulgora", Data: pic}, {Planet: "aquilo", Data: pic}, {Planet: "vulcanus", Data: pic}, {Planet: "x", Data: pic}, {Planet: "y", Data: pic}}); err == nil {
		t.Error("one user's preview over 96 MiB was kept")
	}

	tok, _ := st.add(200, "k", "", "1", onePNG("nauvis"))
	now = now.Add(previewTTL)
	if _, ok := st.get(tok, 200, "nauvis"); ok {
		t.Error("an expired preview was served")
	}
	if len(st.byUser) != 0 || len(st.order) != 0 {
		t.Errorf("expired previews kept: %d, %d", len(st.byUser), len(st.order))
	}
}

// SPEC 5.6: asking again for the same settings and planet shows the stored
// picture without a run; planet= draws only that planet, and only it is run.
func TestPreviewRunsOnlyWhatIsNotDrawnYet(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.1.24")
	form := generateForm("game_version_typed", "2.1.24", "map_key", "2.1|1", "seed", "42", "planet", "vulcanus")

	first := hn.postLong(PathGeneratePreview, form, c)
	if first.Code != http.StatusOK || len(prev.got) != 1 || !slices.Equal(prev.got[0].Planets, []string{"vulcanus"}) {
		t.Fatalf("status %d, requests %+v", first.Code, prev.got)
	}
	pic := pictureOf(t, first.Body.String())
	if !strings.HasSuffix(pic, "/vulcanus") || strings.Contains(first.Body.String(), "/nauvis\"") {
		t.Errorf("the page shows more than vulcanus:\n%s", first.Body.String())
	}
	again := hn.postLong(PathGeneratePreview, form, c)
	if len(prev.got) != 1 || pictureOf(t, again.Body.String()) != pic {
		t.Errorf("the same request ran again: %d runs", len(prev.got))
	}
	// Without planet= every planet is shown; only those not drawn are run.
	form.Del("planet")
	all := hn.postLong(PathGeneratePreview, form, c)
	if len(prev.got) != 2 || slices.Contains(prev.got[1].Planets, "vulcanus") || len(prev.got[1].Planets) != 4 {
		t.Fatalf("requests %+v", prev.got)
	}
	if n := strings.Count(all.Body.String(), `<img src="`+PathGeneratePreview); n != 5 {
		t.Errorf("%d pictures shown, want 5", n)
	}
	// A planet the world will not have is refused, and nothing runs.
	form.Set("planet", "mars")
	if rec := hn.postLong(PathGeneratePreview, form, c); rec.Code != http.StatusBadRequest || len(prev.got) != 2 {
		t.Errorf("unknown planet: status %d, %d runs", rec.Code, len(prev.got))
	}
}

// An automatic request with settings that are not valid gets a short
// fragment for the preview's slot: never a full page, never the form, and
// no focus moved.
func TestAnInvalidAutomaticPreviewGetsAFragment(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.0.77")
	rec := hn.postLong(PathGeneratePreview, generateForm("mode", "exchange", "exchange", "nonsense", "auto", "1"), c, withHeader("HX-Request", "true"))
	body := rec.Body.String()
	if rec.Code != http.StatusOK || len(prev.got) != 0 {
		t.Fatalf("status %d, %d runs", rec.Code, len(prev.got))
	}
	if !strings.HasPrefix(strings.TrimSpace(body), `<div id="preview" class="preview" data-preview="invalid">`) ||
		!strings.Contains(body, "Foreman could not read this as a map exchange string.") {
		t.Errorf("body:\n%s", body)
	}
	for _, not := range []string{"<html", "<form", "autofocus"} {
		if strings.Contains(body, not) {
			t.Errorf("the fragment has %q", not)
		}
	}
	if rec.Header().Get("HX-Retarget") != "" {
		t.Error("the fragment retargets the form")
	}
}

// SPEC 5.6: download names come from the world, seed and planet, with
// nothing that could leave the folder or break the header.
func TestPreviewDownloadNamesAreSafe(t *testing.T) {
	cases := []struct{ world, want string }{
		{"Fresh start", "Fresh_start-7-nauvis"},
		{"", "map-7-nauvis"},
		{"../../etc/passwd", "etcpasswd-7-nauvis"},
		{"a\r\nSet-Cookie: x=1", "aSet-Cookie_x=1-7-nauvis"},
		{`say "hi"; filename=evil.exe`, "say_hi_filename=evil.exe-7-nauvis"},
		{"Мир", "Мир-7-nauvis"},
		{"...", "map-7-nauvis"},
	}
	for _, tc := range cases {
		if got := previewFileName(tc.world, "7", "nauvis"); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.world, got, tc.want)
		}
		h := attachment(previewFileName(tc.world, "7", "nauvis") + ".png")
		if strings.ContainsAny(h, "\r\n/\\") || strings.Count(h, `"`) != 2 || strings.Count(h, ";") != 2 {
			t.Errorf("%q: header %q", tc.world, h)
		}
	}
	if got := attachment("Мир-7-nauvis.png"); got != `attachment; filename="___-7-nauvis.png"; filename*=UTF-8''%D0%9C%D0%B8%D1%80-7-nauvis.png` {
		t.Errorf("header %q", got)
	}
}

// SPEC 5.6: one picture downloads as a named PNG and all drawn planets as
// a zip, only for the user who made them.
func TestPreviewDownloads(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	hn.withActive("2.0.77")
	pic := pictureOf(t, hn.postLong(PathGeneratePreview, generateForm("seed", "9"), c).Body.String())
	token := strings.Split(pic, "/")[3]

	rec := hn.getLong(pic+"/download", c)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" ||
		rec.Header().Get("Content-Disposition") != `attachment; filename="Fresh_start-9-nauvis.png"; filename*=UTF-8''Fresh_start-9-nauvis.png` {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}

	rec = hn.getLong(PathGeneratePreview+"/"+token+"/all.zip", c)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), `filename="Fresh_start-9.zip"`) {
		t.Fatalf("zip: status %d, headers %v", rec.Code, rec.Header())
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !bytes.Equal(b, pngBytes) {
			t.Errorf("%s holds %q", f.Name, b)
		}
	}
	want := []string{"Fresh_start-9-nauvis.png", "Fresh_start-9-vulcanus.png", "Fresh_start-9-gleba.png", "Fresh_start-9-fulgora.png", "Fresh_start-9-aquilo.png"}
	if !slices.Equal(names, want) {
		t.Errorf("zip holds %v", names)
	}

	hn.addUser("bob", testPassword)
	bob := sessionCookie(hn.post(PathSignIn, signInForm("bob", testPassword)))
	for _, p := range []string{pic + "/download", PathGeneratePreview + "/" + token + "/all.zip"} {
		if rec := hn.getLong(p, bob); rec.Code != http.StatusNotFound {
			t.Errorf("%s for another user: %d", p, rec.Code)
		}
	}
	if rec := hn.getLong(PathGeneratePreview+"/"+token+"/mars/download", c); rec.Code != http.StatusNotFound {
		t.Errorf("unknown planet: %d", rec.Code)
	}
}

// slowPreviewer draws after a delay, longer than the test server's write
// timeout.
type slowPreviewer struct {
	fakePreviewer
	delay time.Duration
}

func (s *slowPreviewer) Preview(ctx context.Context, req game.PreviewRequest) ([]game.PNG, error) {
	time.Sleep(s.delay)
	return s.fakePreviewer.Preview(ctx, req)
}

// A preview may take longer than the server's write timeout; its reply
// still arrives whole.
func TestASlowPreviewOutlastsTheWriteTimeout(t *testing.T) {
	hn := newGenerateHarness(t, &slowPreviewer{delay: 600 * time.Millisecond})
	c := hn.signIn()
	hn.withActive("2.0.77")
	srv := httptest.NewUnstartedServer(hn.h)
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+PathGeneratePreview, strings.NewReader(generateForm().Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(c)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("the reply was lost: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `<img src="`+PathGeneratePreview) {
		t.Errorf("status %d, %v\n%s", resp.StatusCode, err, body)
	}
}

// SPEC 5.6: a download reads a preview's pictures while the user draws
// another planet from the same settings; the two must not touch the same map.
func TestPreviewStoreAddDuringReads(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	tok, err := st.add(1, "k1", "World", "7", onePNG("nauvis"))
	if err != nil {
		t.Fatal(err)
	}
	planets := []string{"gleba", "vulcanus", "fulgora", "aquilo"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 2000 {
			_, _ = st.add(1, "k1", "World", "7", onePNG(planets[i%len(planets)]+strings.Repeat("x", i)))
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		e, ok := st.entry(tok, 1)
		if !ok {
			t.Fatal("the preview was lost")
		}
		for range e.images {
		}
		_ = len(e.planets)
		_, _ = st.get(tok, 1, "nauvis")
	}
}

// SPEC 5.6: a preview that is asked for again lives another 15 minutes, so
// the pictures its page links to do not vanish a second later.
func TestPreviewStoreFoundPreviewLivesOn(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	tok, _ := st.add(1, "k1", "World", "7", onePNG("nauvis"))
	now = now.Add(previewTTL - time.Second)
	if got, _ := st.missing(1, "k1", "Renamed", []string{"nauvis"}); got != tok {
		t.Fatal("the preview was not found")
	}
	now = now.Add(2 * time.Second)
	if _, ok := st.get(tok, 1, "nauvis"); !ok {
		t.Error("the preview expired right after it was found again")
	}
}

// DESIGN 8.8: an automatic request draws only the planet viewed, the first
// unless one is named that the settings still have, and answers with the
// preview section alone, offering the other planets.
func TestAutomaticPreviewDrawsOnePlanet(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.1.24")
	form := generateForm("game_version_typed", "2.1.24", "map_key", "2.1|1", "seed", "42", "auto", "1")
	for _, tc := range []struct{ planet, want string }{{"", "nauvis"}, {"gleba", "gleba"}, {"mars", "nauvis"}} {
		form.Set("planet", tc.planet)
		rec := hn.postLong(PathGeneratePreview, form, c, withHeader("HX-Request", "true"))
		body := rec.Body.String()
		if rec.Code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(body), `<div id="preview"`) || strings.Contains(body, "<html") {
			t.Fatalf("planet %q: status %d, body:\n%s", tc.planet, rec.Code, body)
		}
		if !strings.Contains(body, `/`+tc.want+`"`) || strings.Count(body, `<img src="`) != 1 {
			t.Errorf("planet %q: want one picture of %s:\n%s", tc.planet, tc.want, body)
		}
		if strings.Count(body, "<option ") != 5 || !strings.Contains(body, `id="preview-view-one" value="one" checked>`) || strings.Contains(body, `value="all" checked`) {
			t.Errorf("planet %q: the view choice is wrong:\n%s", tc.planet, body)
		}
	}
	// Each run drew one planet: nauvis, then gleba; nauvis again was stored.
	if len(prev.got) != 2 || !slices.Equal(prev.got[0].Planets, []string{"nauvis"}) || !slices.Equal(prev.got[1].Planets, []string{"gleba"}) {
		t.Errorf("runs %+v", prev.got)
	}
}

// DESIGN 8.8: with "All planets" chosen, an automatic request draws every
// planet, only those not drawn yet, and offers the way back to one planet.
// A world with one planet offers no choice.
func TestAutomaticPreviewDrawsAllPlanetsWhenChosen(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.1.24")
	hx := withHeader("HX-Request", "true")
	form := generateForm("game_version_typed", "2.1.24", "map_key", "2.1|1", "seed", "42", "auto", "1", "planet", "nauvis")
	hn.postLong(PathGeneratePreview, form, c, hx)
	form.Del("planet")
	form.Set("preview_view", "all")
	body := hn.postLong(PathGeneratePreview, form, c, hx).Body.String()
	if strings.Count(body, `<img src="`) != 5 || !strings.Contains(body, `id="preview-view-all" value="all" checked>`) || !strings.Contains(body, `id="preview-view-one" value="one">`) {
		t.Errorf("want five pictures with All planets chosen and One planet offered:\n%s", body)
	}
	if len(prev.got) != 2 || len(prev.got[1].Planets) != 4 || slices.Contains(prev.got[1].Planets, "nauvis") {
		t.Errorf("runs %+v; the second should draw the four planets not drawn yet", prev.got)
	}
	// Back to one planet: the stored picture, with no run.
	form.Set("preview_view", "one")
	form.Set("planet", "fulgora")
	body = hn.postLong(PathGeneratePreview, form, c, hx).Body.String()
	if strings.Count(body, `<img src="`) != 1 || !strings.Contains(body, `/fulgora"`) || len(prev.got) != 2 {
		t.Errorf("one planet is not shown from the store (%d runs):\n%s", len(prev.got), body)
	}
	one := generateForm("game_version_typed", "2.1.24", "map_key", "2.1|0", "seed", "42", "auto", "1", "preview_view", "all")
	one.Del("space_age")
	if body := hn.postLong(PathGeneratePreview, one, c, hx).Body.String(); strings.Contains(body, "preview-view-") {
		t.Errorf("a one-planet world offers the view choice:\n%s", body)
	}
}

// SPEC 5.6: the size changes every picture, so it is part of the key; the
// same size finds the pictures already drawn, and no size means the default.
func TestSettingsKeyIncludesTheSize(t *testing.T) {
	base := game.PreviewRequest{GameVersion: "2.1.24", Seed: 7, Size: 1280}
	bigger := base
	bigger.Size = 2048
	if settingsKey(base) == settingsKey(bigger) {
		t.Error("two sizes share a key")
	}
	same := base
	same.Planets = []string{"gleba"}
	if settingsKey(base) != settingsKey(same) {
		t.Error("the planet changed the key")
	}
	unset := base
	unset.Size = 0
	if settingsKey(base) != settingsKey(unset) {
		t.Error("no size and the default size have different keys")
	}
}

// SPEC 5.6: a preview of another size draws again; the same size, or none
// (the default), does not run again.
func TestPreviewAtAnotherSizeDrawsAgain(t *testing.T) {
	prev := &fakePreviewer{}
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.1.24")
	form := generateForm("game_version_typed", "2.1.24", "map_key", "2.1|1", "seed", "42", "planet", "nauvis", "preview_size", "2048")
	body := hn.postLong(PathGeneratePreview, form, c).Body.String()
	if len(prev.got) != 1 || prev.got[0].Size != 2048 {
		t.Fatalf("requests %+v", prev.got)
	}
	if !strings.Contains(body, `width="2048" height="2048"`) || !strings.Contains(body, `<option value="2048" selected>`) {
		t.Errorf("the page does not show the size:\n%s", body)
	}
	hn.postLong(PathGeneratePreview, form, c)
	if len(prev.got) != 1 {
		t.Errorf("the same size ran again: %d runs", len(prev.got))
	}
	form.Set("preview_size", "1280")
	hn.postLong(PathGeneratePreview, form, c)
	if len(prev.got) != 2 || prev.got[1].Size != 1280 {
		t.Errorf("another size did not run: %+v", prev.got)
	}
	// A form with no size field is the default size.
	form.Del("preview_size")
	hn.postLong(PathGeneratePreview, form, c)
	if len(prev.got) != 2 {
		t.Errorf("no size ran again: %d runs", len(prev.got))
	}
}

// SPEC 5.6: a size off the steps is a field error and nothing is drawn.
func TestPreviewRefusesASizeOffTheSteps(t *testing.T) {
	for _, bad := range []string{"900", "1100", "4224", "99999", "-1280", "wide", "1280.5", "1e3"} {
		prev := &fakePreviewer{}
		hn := newGenerateHarness(t, prev)
		c := hn.signIn()
		hn.withActive("2.1.24")
		form := generateForm("game_version_typed", "2.1.24", "map_key", "2.1|1", "seed", "42", "preview_size", bad)
		rec := hn.postLong(PathGeneratePreview, form, c)
		body := rec.Body.String()
		if rec.Code != http.StatusBadRequest || len(prev.got) != 0 {
			t.Errorf("size %q: status %d, %d runs", bad, rec.Code, len(prev.got))
		}
		if !strings.Contains(body, "Choose one of the sizes in the list.") || !strings.Contains(body, `<option value="1280" selected>`) {
			t.Errorf("size %q: no field error, or the bad value is kept:\n%s", bad, body)
		}
		// An automatic request says it in the fragment, with a link to the field.
		form.Set("auto", "1")
		auto := hn.postLong(PathGeneratePreview, form, c, withHeader("HX-Request", "true")).Body.String()
		if !strings.Contains(auto, `data-preview="invalid"`) || !strings.Contains(auto, `href="#generate-preview-size"`) || len(prev.got) != 0 {
			t.Errorf("size %q: the fragment is wrong, or a run was made:\n%s", bad, auto)
		}
	}
}

// SPEC 5.6: the size survives a form that is sent back with an error, and a
// bad one does not stop the world from being made.
func TestPreviewSizeSurvivesAnErrorRoundTrip(t *testing.T) {
	hn := newGenerateHarness(t, &fakePreviewer{})
	c := hn.signIn()
	hn.withActive("2.1.24")
	rec := hn.postLong(PathGenerate, generateForm("game_version_typed", "2.1.24", "map_key", "2.1|1", "name", " ", "preview_size", "2816"), c)
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, `<option value="2816" selected>`) {
		t.Errorf("status %d; the size was not kept:\n%s", rec.Code, body)
	}
	// The sizes are plain digits, with no thousands separator, in every language.
	for _, want := range []string{">1024 × 1024 tiles</option>", ">1280 × 1280 tiles (default)</option>", ">3072 × 3072 tiles</option>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the size select lacks %q", want)
		}
	}
	// One cap for every preview: the territory cap (SPEC 5.6).
	if strings.Contains(body, `<option value="3200"`) {
		t.Error("the size select offers more than 3072 tiles")
	}
	rec = hn.postLong(PathGenerate, generateForm("game_version_typed", "2.1.24", "map_key", "2.1|1", "seed", "5", "preview_size", "7"), c)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("a bad preview size stopped the world being made: status %d", rec.Code)
	}
}

// SPEC 5.6: the pictures are 16 MiB at most, and 96 MiB are held in all, so
// one Space Age set at the largest size is not evicted by a second user's.
func TestPreviewStoreHoldsTwoLargeSets(t *testing.T) {
	st := newPreviewStore(time.Now)
	var set []game.PNG
	for _, planet := range []string{"nauvis", "vulcanus", "gleba", "fulgora", "aquilo"} {
		set = append(set, game.PNG{Planet: planet, Data: make([]byte, 9<<20)})
	}
	first, err := st.add(1, "k", "", "1", set)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.add(2, "k", "", "1", set); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.get(first, 1, "gleba"); !ok {
		t.Error("the first user's set was evicted by the second's")
	}
}
