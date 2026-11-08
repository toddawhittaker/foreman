package web

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/territory"
)

// The previewer most tests use reads no territories; they never ask.
func (f *fakePreviewer) Territories(context.Context, game.PreviewRequest) (territory.Result, error) {
	return territory.Result{}, errors.New("the fake previewer reads no territories")
}

// fakeTerritories draws real PNGs and answers territory runs as set.
type fakeTerritories struct {
	fakePreviewer
	runs []game.PreviewRequest
	res  territory.Result
	err  error
}

func (f *fakeTerritories) Territories(_ context.Context, req game.PreviewRequest) (territory.Result, error) {
	f.runs = append(f.runs, req)
	return f.res, f.err
}

// realPNG is a grey square of size pixels, so DrawPNG has a picture to draw on.
func realPNG(t *testing.T, size int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for i := range img.Pix {
		img.Pix[i] = 100
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// threeTerritories is one small, one medium and one big territory, each a
// chunk near the picture's centre.
var threeTerritories = territory.Result{Territories: []territory.Territory{
	{Size: territory.Small, Chunks: []territory.Chunk{{X: -2, Y: -2}}},
	{Size: territory.Medium, Chunks: []territory.Chunk{{X: 1, Y: -2}, {X: 2, Y: -2}}},
	{Size: territory.Big, Chunks: []territory.Chunk{{X: -2, Y: 1}}},
}}

func newTerritoryHarness(t *testing.T) (*harness, *fakeTerritories, *http.Cookie) {
	t.Helper()
	prev := &fakeTerritories{res: threeTerritories}
	prev.png = realPNG(t, game.PreviewSizeDefault)
	hn := newGenerateHarness(t, prev)
	c := hn.signIn()
	hn.withActive("2.1.24")
	return hn, prev, c
}

// vulcanusForm is the page's automatic request for Vulcanus on 2.1 with
// Space Age, with the territory box ticked.
func vulcanusForm(extra ...string) url.Values {
	return generateForm(append([]string{"game_version_typed", "2.1.24", "map_key", "2.1|1", "seed", "123456", "planet", "vulcanus", "territories", "1"}, extra...)...)
}

// SPEC 5.6: the territories are read once for a set of settings; asking
// again answers from the store, and an automatic preview of the same
// settings shows them at once. The fragment is the overlay and the list.
func TestTerritoriesAnswerFromTheStore(t *testing.T) {
	hn, prev, c := newTerritoryHarness(t)
	hx := withHeader("HX-Request", "true")
	pic := hn.postLong(PathGeneratePreview, vulcanusForm("auto", "1"), c, hx).Body.String()
	if len(prev.runs) != 0 || !strings.Contains(pic, `id="preview-territories" name="territories" value="1" aria-describedby="preview-territories-hint" checked>`) {
		t.Fatalf("the picture's reply ran territories (%d) or lacks the ticked box:\n%s", len(prev.runs), pic)
	}
	if strings.Contains(pic, `class="territories"`) {
		t.Error("the picture's reply has an overlay before any was read")
	}
	rec := hn.postLong(pathTerritories, vulcanusForm(), c, hx)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || len(prev.runs) != 1 || prev.runs[0].Size != game.PreviewSizeDefault || prev.runs[0].Seed != 123456 {
		t.Fatalf("status %d, runs %+v", rec.Code, prev.runs)
	}
	for _, want := range []string{
		`<div id="territory-reply">`, `<span class="territories" aria-hidden="true"><svg viewBox="0 0 1280 1280" preserveAspectRatio="none"`,
		`<pattern id="territory-stripes" width="56" height="56" patternUnits="userSpaceOnUse">`, `vector-effect="non-scaling-stroke"`,
		`<rect x="576" y="576" width="32" height="32"/>`, `<rect x="672" y="576" width="64" height="32"/>`,
		// Short words for the small picture, whole ones for the large view, and where each sits for the style sheet.
		`<span class="territories__label" style="left: 46.25%; top: 46.25%; --x: 46.25; --y: 46.25"><span class="territories__short">Sm.</span><span class="territories__long">Small</span></span>`,
		`<span class="territories__short">Med.</span><span class="territories__long">Medium</span>`,
		`<span class="territories__short">Big</span><span class="territories__long">Big</span>`,
		// The counts are for screen readers; the picture shows the territories.
		`<p class="territory-counts visually-hidden">Demolisher territories in this picture: 1 small, 1 medium, 1 big.</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the fragment lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "<form") {
		t.Error("the fragment is a page")
	}
	hn.postLong(pathTerritories, vulcanusForm(), c, hx)
	if len(prev.runs) != 1 {
		t.Errorf("the same settings ran again: %d runs", len(prev.runs))
	}
	// The section drawn again from the same settings shows them with no run,
	// and the download draws them in.
	again := hn.postLong(PathGeneratePreview, vulcanusForm("auto", "1"), c, hx).Body.String()
	if len(prev.runs) != 1 || !strings.Contains(again, `class="territories"`) || !strings.Contains(again, "1 small, 1 medium, 1 big") ||
		!strings.Contains(again, `/vulcanus/download?territories=1"`) || !strings.Contains(again, "Download Vulcanus with territories (PNG)") {
		t.Errorf("the stored territories are not shown (%d runs):\n%s", len(prev.runs), again)
	}
	// Other settings read them again.
	hn.postLong(pathTerritories, vulcanusForm("seed", "7"), c, hx)
	if len(prev.runs) != 2 || prev.runs[1].Seed != 7 {
		t.Errorf("new settings did not run: %+v", prev.runs)
	}
}

// SPEC 5.6: each refusal and failure says what happened and what to do; the
// ones known beforehand run nothing.
func TestTerritoryRefusalsAreWorded(t *testing.T) {
	cases := []struct {
		name  string
		form  url.Values
		err   error
		want  string
		runs  int
		retry bool
	}{
		{"busy", vulcanusForm(), game.ErrPreviewBusy, "Another map preview is being made, so the demolisher territories were not found.", 1, true},
		{"timeout", vulcanusForm(), game.ErrPreviewTimeout, "Finding the demolisher territories took too long and was stopped.", 1, true},
		{"no image", vulcanusForm(), game.ErrPreviewImageMissing, "the game image for Factorio 2.1.24 is not downloaded here", 1, false},
		{"memory", vulcanusForm(), &game.PreviewError{ExitCode: 137, OOM: true}, "The game ran out of memory finding the demolisher territories.", 1, true},
		{"game failed", vulcanusForm(), &game.PreviewError{ExitCode: 1}, "The game could not find the demolisher territories.", 1, true},
		{"other", vulcanusForm(), errors.New("docker is gone"), "The game could not find the demolisher territories.", 1, true},
		{"above the size cap", vulcanusForm("preview_size", "3200"), nil, "The settings have problems, so the demolisher territories were not found.", 0, false},
		{"no space age", vulcanusForm("space_age", "", "map_key", "2.1|0"), nil, "Only Space Age worlds have demolisher territories", 0, false},
		{"bad settings", vulcanusForm("preview_size", "999"), nil, "The settings have problems, so the demolisher territories were not found.", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hn, prev, c := newTerritoryHarness(t)
			prev.err = tc.err
			rec := hn.postLong(pathTerritories, tc.form, c, withHeader("HX-Request", "true"))
			body := rec.Body.String()
			if rec.Code != http.StatusOK || len(prev.runs) != tc.runs {
				t.Fatalf("status %d, %d runs", rec.Code, len(prev.runs))
			}
			if !strings.Contains(body, tc.want) || !strings.Contains(body, "message--error") || strings.Contains(body, `class="territories"`) {
				t.Errorf("want %q:\n%s", tc.want, body)
			}
			if strings.Contains(body, "data-territory-retry") != tc.retry {
				t.Errorf("retry offered: %v, want %v", !tc.retry, tc.retry)
			}
		})
	}
}

// DESIGN 8.8: the box shows only beside Vulcanus's picture on a Space Age
// world, at every size offered; a ticked choice is kept while another planet
// is viewed.
func TestTerritoryBoxShowsOnlyForVulcanus(t *testing.T) {
	hn, _, c := newTerritoryHarness(t)
	hx := withHeader("HX-Request", "true")
	box := `id="preview-territories"`
	nauvis := hn.postLong(PathGeneratePreview, vulcanusForm("auto", "1", "planet", "nauvis"), c, hx).Body.String()
	if strings.Contains(nauvis, box) || !strings.Contains(nauvis, `<input type="hidden" name="territories" value="1">`) {
		t.Errorf("Nauvis shows the box, or loses the choice:\n%s", nauvis)
	}
	off := hn.postLong(PathGeneratePreview, vulcanusForm("auto", "1", "territories", ""), c, hx).Body.String()
	if !strings.Contains(off, box) || strings.Contains(off, " checked>") || strings.Contains(off, `name="territories" value="1">`) {
		t.Errorf("Vulcanus lacks the box, or it is ticked when off:\n%s", off)
	}
	if !strings.Contains(off, "Show demolisher territories") || !strings.Contains(off, "Finding them takes a few seconds more.") {
		t.Errorf("the box's words are missing:\n%s", off)
	}
	// Every size offered can show them, the largest too (SPEC 5.6).
	if body := hn.postLong(PathGeneratePreview, vulcanusForm("auto", "1", "preview_size", "3072"), c, hx).Body.String(); !strings.Contains(body, `id="preview-territories" name="territories"`) {
		t.Errorf("3072 cannot send the box:\n%s", body)
	}
	noSA := hn.postLong(PathGeneratePreview, vulcanusForm("auto", "1", "space_age", "", "map_key", "2.1|0", "planet", ""), c, hx).Body.String()
	if strings.Contains(noSA, box) {
		t.Error("a world without Space Age shows the box")
	}
}

// SPEC 5.6: without script, Preview map with the box ticked draws the
// pictures and then reads the territories in the same request; the page
// shows them and its downloads draw them in. With script, Preview map
// leaves them to the page's own request, so the picture never waits.
func TestPreviewMapWithoutScriptReadsTerritories(t *testing.T) {
	hn, prev, c := newTerritoryHarness(t)
	form := vulcanusForm()
	form.Del("planet")
	hx := hn.postLong(PathGeneratePreview, form, c, withHeader("HX-Request", "true")).Body.String()
	if len(prev.runs) != 0 || len(prev.got) != 1 || strings.Contains(hx, `class="territories"`) {
		t.Fatalf("with script Preview map read territories: %d runs", len(prev.runs))
	}
	body := hn.postLong(PathGeneratePreview, vulcanusForm("seed", "5", "planet", ""), c).Body.String()
	if len(prev.runs) != 1 || len(prev.got) != 2 {
		t.Fatalf("runs %d, pictures %d", len(prev.runs), len(prev.got))
	}
	for _, want := range []string{`class="territories"`, "1 small, 1 medium, 1 big", `/vulcanus/download?territories=1"`, `/all.zip?territories=1"`, `/nauvis/download"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if n := strings.Count(body, `class="territories"`); n != 1 {
		t.Errorf("%d overlays, want only Vulcanus's", n)
	}
}

// SPEC 5.6: ?territories=1 draws them into Vulcanus's download, named so,
// on request; without it, or for another planet, the plain picture; with
// none read, nothing. The zip draws them into Vulcanus only.
func TestTerritoryDownloadsDrawThem(t *testing.T) {
	hn, prev, c := newTerritoryHarness(t)
	form := vulcanusForm()
	form.Del("planet")
	body := hn.postLong(PathGeneratePreview, form, c, withHeader("HX-Request", "true")).Body.String()
	vulcanus := PathGeneratePreview + "/" + strings.Split(pictureOf(t, body), "/")[3] + "/vulcanus"
	token := strings.Split(vulcanus, "/")[3]
	if rec := hn.getLong(vulcanus+"/download?territories=1", c); rec.Code != http.StatusNotFound {
		t.Errorf("territories asked for before any were read: %d", rec.Code)
	}
	if rec := hn.getLong(PathGeneratePreview+"/"+token+"/all.zip?territories=1", c); rec.Code != http.StatusNotFound {
		t.Errorf("zip with territories before any were read: %d", rec.Code)
	}
	hn.postLong(pathTerritories, vulcanusForm(), c, withHeader("HX-Request", "true"))

	plain := hn.getLong(vulcanus+"/download", c)
	if !bytes.Equal(plain.Body.Bytes(), prev.png) {
		t.Error("the plain download is not the picture")
	}
	rec := hn.getLong(vulcanus+"/download?territories=1", c)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), `filename="Fresh_start-123456-vulcanus-territories.png"`) {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	drawn, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	// Pixel 577,606 is in the small territory, on a stripe, clear of its
	// border and label; 0,0 is in no territory.
	if c := color.RGBAModel.Convert(drawn.At(577, 606)).(color.RGBA); c.R <= c.G {
		t.Errorf("the territory is not drawn: %v", c)
	}
	if c := color.RGBAModel.Convert(drawn.At(0, 0)).(color.RGBA); c != (color.RGBA{100, 100, 100, 255}) {
		t.Errorf("outside the territories the picture changed: %v", c)
	}
	nauvis := hn.getLong(strings.Replace(vulcanus, "vulcanus", "nauvis", 1)+"/download?territories=1", c)
	if !bytes.Equal(nauvis.Body.Bytes(), prev.png) || strings.Contains(nauvis.Header().Get("Content-Disposition"), "territories") {
		t.Error("Nauvis's download was drawn on")
	}

	zrec := hn.getLong(PathGeneratePreview+"/"+token+"/all.zip?territories=1", c)
	zr, err := zip.NewReader(bytes.NewReader(zrec.Body.Bytes()), int64(zrec.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		isVulcanus := strings.Contains(f.Name, "vulcanus")
		if isVulcanus != !bytes.Equal(b, prev.png) {
			t.Errorf("%s: drawn on is %v", f.Name, !bytes.Equal(b, prev.png))
		}
		if isVulcanus && f.Name != "Fresh_start-123456-vulcanus-territories.png" {
			t.Errorf("Vulcanus is named %s", f.Name)
		}
	}
	if len(zr.File) != 5 {
		t.Errorf("%d files in the zip", len(zr.File))
	}
}

// SPEC 5.6: the territories count in the store's total, join the pictures
// of the same settings, and go with them when the settings change.
func TestPreviewStoreCountsTerritories(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	st := newPreviewStore(func() time.Time { return now })
	tok, _ := st.add(1, "k1", "World", "7", onePNG("vulcanus"))
	before := st.byUser[1].bytes
	tok2, err := st.addTerritories(1, "k1", "World", "7", 1280, threeTerritories)
	if err != nil || tok2 != tok {
		t.Fatalf("token %q, %v", tok2, err)
	}
	want := before + territoryBytes(&threeTerritories)
	if got := st.byUser[1].bytes; got != want || territoryBytes(&threeTerritories) != 3*40+4*16 {
		t.Errorf("%d bytes held, want %d", got, want)
	}
	if _, ok := st.territoriesOf(1, "k1"); !ok {
		t.Error("the territories are not found")
	}
	if _, ok := st.territoriesOf(2, "k1"); ok {
		t.Error("another user found them")
	}
	// Read again, they replace the old ones rather than adding to the total.
	_, _ = st.addTerritories(1, "k1", "World", "7", 1280, threeTerritories)
	if got := st.byUser[1].bytes; got != want {
		t.Errorf("read twice: %d bytes, want %d", got, want)
	}
	// More pictures of the same settings keep them.
	_, _ = st.add(1, "k1", "World", "7", onePNG("nauvis"))
	if _, ok := st.territoriesOf(1, "k1"); !ok {
		t.Error("another planet's picture lost the territories")
	}
	_, _ = st.add(1, "k2", "World", "8", onePNG("vulcanus"))
	if _, ok := st.territoriesOf(1, "k2"); ok {
		t.Error("new settings kept the old territories")
	}
	if got := st.byUser[1].bytes; got != len(pngBytes) {
		t.Errorf("new settings hold %d bytes, want %d", got, len(pngBytes))
	}
	// Territories that take the total over the limit push other users out.
	big := territory.Result{Territories: []territory.Territory{{Size: territory.Small, Chunks: make([]territory.Chunk, maxPreviewBytes/16-3)}}}
	if _, err := st.addTerritories(3, "k", "", "1", 1280, big); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.byUser[1]; ok || st.totalLocked() > maxPreviewBytes {
		t.Errorf("the limit was not kept: %d bytes", st.totalLocked())
	}
	tooBig := territory.Result{Territories: []territory.Territory{{Chunks: make([]territory.Chunk, maxPreviewBytes/16+1)}}}
	if _, err := st.addTerritories(4, "k", "", "1", 1280, tooBig); err == nil {
		t.Error("territories over the whole limit were kept")
	}
}

// SPEC 14.1: the territory route is for a signed-in user's own page only.
func TestTerritoriesNeedSignInAndTheSameOrigin(t *testing.T) {
	hn, prev, c := newTerritoryHarness(t)
	hn.wantRedirect(hn.postLong(pathTerritories, vulcanusForm(), nil), PathSignIn)
	if rec := hn.postLong(pathTerritories, vulcanusForm(), c, withHeader("Sec-Fetch-Site", "cross-site")); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site: %d", rec.Code)
	}
	if len(prev.runs) != 0 {
		t.Errorf("%d runs", len(prev.runs))
	}
	none := newGenerateHarness(t, nil)
	nc := none.signIn()
	if rec := none.postLong(pathTerritories, vulcanusForm(), nc); rec.Code != http.StatusNotFound {
		t.Errorf("no previewer: %d", rec.Code)
	}
}

type drawCtxKey struct{}

// SPEC 14.1: a download draws with its request's context, so a browser that
// leaves stops waiting for the render slot; a full queue is a 503 the user
// can retry, not a server fault.
func TestTerritoryDownloadDrawsWithTheRequestContextAndRefusesWhenBusy(t *testing.T) {
	hn, _, c := newTerritoryHarness(t)
	form := vulcanusForm()
	form.Del("planet")
	body := hn.postLong(PathGeneratePreview, form, c, withHeader("HX-Request", "true")).Body.String()
	vulcanus := PathGeneratePreview + "/" + strings.Split(pictureOf(t, body), "/")[3] + "/vulcanus"
	hn.postLong(pathTerritories, vulcanusForm(), c, withHeader("HX-Request", "true"))

	orig := drawPNG
	t.Cleanup(func() { drawPNG = orig })
	var got any
	drawPNG = func(ctx context.Context, _ []byte, _ territory.Overlay, _ func(string) string) ([]byte, error) {
		got = ctx.Value(drawCtxKey{})
		return nil, territory.ErrBusy
	}
	for _, path := range []string{vulcanus + "/download?territories=1", strings.TrimSuffix(vulcanus, "/vulcanus") + "/all.zip?territories=1"} {
		got = nil
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(c)
		rec := hn.serveLong(r.WithContext(context.WithValue(r.Context(), drawCtxKey{}, "mine")))
		if got != "mine" {
			t.Errorf("%s: drawn without the request's context", path)
		}
		if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" ||
			!strings.Contains(rec.Body.String(), "Wait a few seconds, then download again.") {
			t.Errorf("%s: status %d, headers %v:\n%s", path, rec.Code, rec.Header(), rec.Body.String())
		}
	}
	if strings.Contains(hn.logs.String(), `"level":"ERROR"`) {
		t.Errorf("a busy queue was logged as an error:\n%s", hn.logs.String())
	}

	drawPNG = func(context.Context, []byte, territory.Overlay, func(string) string) ([]byte, error) {
		return nil, errors.New("broken")
	}
	if rec := hn.getLong(vulcanus+"/download?territories=1", c); rec.Code != http.StatusInternalServerError {
		t.Errorf("another failure: %d", rec.Code)
	}
}
