package web

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/world"
)

// fakeMapBackend stands in for the controller and the world manager, so the
// handlers are tested apart from the runs that make the pictures.
type fakeMapBackend struct {
	view      game.FactoryMapView
	viewErr   error
	drawErr   error
	drawn     int
	afterDraw *game.FactoryMapView // the view once a draw failed
	modes     []world.FactoryMapMode
	modeErr   error
	store     *fakePictures
	memory    uint64
}

func (f *fakeMapBackend) FactoryMap(string) (game.FactoryMapView, error) {
	if f.drawn > 0 && f.afterDraw != nil {
		return *f.afterDraw, f.viewErr
	}
	return f.view, f.viewErr
}

func (f *fakeMapBackend) DrawFactoryMap(context.Context, string) error {
	f.drawn++
	return f.drawErr
}

func (f *fakeMapBackend) Pictures(string) (pictureStore, error) { return f.store, nil }

func (f *fakeMapBackend) SetMode(_ string, m world.FactoryMapMode) error {
	f.modes = append(f.modes, m)
	return f.modeErr
}

func (f *fakeMapBackend) RunningMemory(string) uint64 { return f.memory }

// fakePictures keeps one PNG file per (surface, layer) and records what was
// asked for.
type fakePictures struct {
	dir       string
	opened    []string
	composed  [][]factorymap.Category
	composeAt int
}

func (p *fakePictures) OpenPNG(surface int, layer factorymap.Category) (*os.File, error) {
	p.opened = append(p.opened, string(layer))
	return os.Open(filepath.Join(p.dir, string(layer)+".png"))
}

func (p *fakePictures) Composite(surface int, layers []factorymap.Category) (image.Image, error) {
	p.composed = append(p.composed, layers)
	p.composeAt = surface
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.NRGBA{R: 200, A: 255})
	return img, nil
}

type mapRig struct {
	hn     *harness
	be     *fakeMapBackend
	mux    *http.ServeMux
	cookie *http.Cookie
	id     string
}

func newMapRig(t *testing.T) *mapRig {
	t.Helper()
	hn := newHarness(t, nil)
	cookie := hn.signIn()
	id, err := hn.worlds.Create(context.Background(), "Home Base", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"", "belts"} {
		f, err := os.Create(filepath.Join(dir, name+".png")) //nolint:gosec // a test temp folder
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}
	be := &fakeMapBackend{
		store: &fakePictures{dir: dir},
		view: game.FactoryMapView{
			State: "drawn",
			Mode:  world.FactoryMapMode{Mode: world.FactoryMapManual, SkipOnline: true},
			Drawing: factorymap.Drawing{Surfaces: []factorymap.Surface{
				{Index: 1, Name: "nauvis", Kind: "planet"},
				{Index: 4, Name: "Evil/../name\"", Kind: "surface"},
			}},
		},
	}
	mux := http.NewServeMux()
	h := &factoryMapHandlers{s: hn.h.(*Handler).srv, b: be}
	h.routes(mux, func(p string, f http.HandlerFunc) { mux.HandleFunc(p, f) })
	return &mapRig{hn: hn, be: be, mux: mux, cookie: cookie, id: id}
}

func (m *mapRig) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.RemoteAddr = peerUntrusted
	r.AddCookie(m.cookie)
	rec := httptest.NewRecorder()
	m.mux.ServeHTTP(rec, r)
	return rec
}

func (m *mapRig) base() string { return "/worlds/" + m.id + "/map" }

func TestFactoryRefusal(t *testing.T) {
	manual := world.FactoryMapMode{Mode: world.FactoryMapManual, SkipOnline: true}
	cases := []struct {
		name string
		v    game.FactoryMapView
		want string
	}{
		{"idle", game.FactoryMapView{Mode: manual}, ""},
		{"mode off", game.FactoryMapView{Mode: world.FactoryMapMode{Mode: world.FactoryMapOff}}, "off"},
		{"old game", game.FactoryMapView{Mode: manual, State: "unsupported"}, "not_supported"},
		{"running, nobody online", game.FactoryMapView{Mode: manual, Running: true}, ""},
		{"running, players, skip on", game.FactoryMapView{Mode: manual, Running: true, PlayersOnline: true}, "players_online"},
		{"running, players, skip off", game.FactoryMapView{Mode: world.FactoryMapMode{Mode: world.FactoryMapManual}, Running: true, PlayersOnline: true}, ""},
		{"stopped, players flag stale", game.FactoryMapView{Mode: manual, PlayersOnline: true}, ""},
	}
	for _, c := range cases {
		if got := factoryRefusal(c.v); got != c.want {
			t.Errorf("%s: refusal %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDrawNeedsMemoryConfirmWhileRunning(t *testing.T) {
	m := newMapRig(t)
	m.be.view.Running = true
	h := &factoryMapHandlers{s: m.hn.h.(*Handler).srv, b: m.be}
	out, err := h.draw(context.Background(), m.id, false)
	if err != nil || !out.NeedMemory || out.Started || m.be.drawn != 0 {
		t.Fatalf("unconfirmed click beside a running game: %+v, err %v, draws %d", out, err, m.be.drawn)
	}
	out, err = h.draw(context.Background(), m.id, true)
	if err != nil || !out.Started || m.be.drawn != 1 {
		t.Fatalf("confirmed click: %+v, err %v, draws %d", out, err, m.be.drawn)
	}
}

func TestDrawStoppedWorldNeedsNoConfirm(t *testing.T) {
	m := newMapRig(t)
	h := &factoryMapHandlers{s: m.hn.h.(*Handler).srv, b: m.be}
	out, err := h.draw(context.Background(), m.id, false)
	if err != nil || !out.Started || m.be.drawn != 1 {
		t.Fatalf("click on a stopped world: %+v, err %v, draws %d", out, err, m.be.drawn)
	}
}

func TestDrawRefusalsNeverStartARun(t *testing.T) {
	for name, view := range map[string]game.FactoryMapView{
		"off":            {Mode: world.FactoryMapMode{Mode: world.FactoryMapOff}},
		"old":            {State: "unsupported", Mode: world.FactoryMapMode{Mode: world.FactoryMapManual}},
		"players online": {Running: true, PlayersOnline: true, Mode: world.FactoryMapMode{Mode: world.FactoryMapManual, SkipOnline: true}},
	} {
		m := newMapRig(t)
		m.be.view = view
		h := &factoryMapHandlers{s: m.hn.h.(*Handler).srv, b: m.be}
		// Confirmed too: the memory box never overrides a refusal.
		out, err := h.draw(context.Background(), m.id, true)
		if err != nil || out.Refused == "" || out.Started || m.be.drawn != 0 {
			t.Errorf("%s: %+v, err %v, draws %d", name, out, err, m.be.drawn)
		}
	}
}

func TestDrawRefusedByControllerAfterOurLook(t *testing.T) {
	m := newMapRig(t)
	m.be.drawErr = errors.New("refused")
	after := m.be.view
	after.Running, after.PlayersOnline = true, true
	m.be.afterDraw = &after
	h := &factoryMapHandlers{s: m.hn.h.(*Handler).srv, b: m.be}
	out, err := h.draw(context.Background(), m.id, true)
	if err != nil || out.Refused != "players_online" {
		t.Fatalf("got %+v, err %v; want the players-online refusal", out, err)
	}
}

func TestDrawOtherFailureIsAnError(t *testing.T) {
	m := newMapRig(t)
	m.be.drawErr = errors.New("docker is down")
	h := &factoryMapHandlers{s: m.hn.h.(*Handler).srv, b: m.be}
	if _, err := h.draw(context.Background(), m.id, true); err == nil {
		t.Fatal("a failed start was not reported")
	}
}

func TestDrawPostRedirectsToThePage(t *testing.T) {
	m := newMapRig(t)
	rec := m.do(http.MethodPost, m.base()+"/draw", url.Values{})
	m.hn.wantRedirect(rec, m.base())
	if m.be.drawn != 1 {
		t.Fatalf("draws: %d", m.be.drawn)
	}
}

func TestModePost(t *testing.T) {
	m := newMapRig(t)
	for _, c := range []struct {
		form url.Values
		want world.FactoryMapMode
	}{
		{url.Values{"mode": {"automatic"}, "skip_online": {"on"}}, world.FactoryMapMode{Mode: "automatic", SkipOnline: true}},
		{url.Values{"mode": {"manual"}}, world.FactoryMapMode{Mode: "manual"}},
		{url.Values{"mode": {"off"}, "skip_online": {"on"}}, world.FactoryMapMode{Mode: "off", SkipOnline: true}},
	} {
		m.be.modes = nil
		m.hn.wantRedirect(m.do(http.MethodPost, m.base()+"/mode", c.form), m.base())
		if len(m.be.modes) != 1 || m.be.modes[0] != c.want {
			t.Errorf("form %v saved %v, want %v", c.form, m.be.modes, c.want)
		}
	}
}

func TestModePostRejectsUnknownMode(t *testing.T) {
	m := newMapRig(t)
	for _, mode := range []string{"", "Automatic", "always", "manual ", "../off"} {
		rec := m.do(http.MethodPost, m.base()+"/mode", url.Values{"mode": {mode}})
		if rec.Code != http.StatusBadRequest || len(m.be.modes) != 0 {
			t.Errorf("mode %q: status %d, saves %v", mode, rec.Code, m.be.modes)
		}
	}
}

func TestModePostFailureIsNotARedirect(t *testing.T) {
	m := newMapRig(t)
	m.be.modeErr = errors.New("disk full")
	rec := m.do(http.MethodPost, m.base()+"/mode", url.Values{"mode": {"manual"}})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
}

func TestBasePicture(t *testing.T) {
	m := newMapRig(t)
	rec := m.do(http.MethodGet, m.base()+"/1.png", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("base: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Content-Disposition") != "" {
		t.Errorf("headers: %v", rec.Header())
	}
	if _, err := png.Decode(rec.Body); err != nil {
		t.Errorf("body is not a PNG: %v", err)
	}
	if got := m.be.store.opened; len(got) != 1 || got[0] != "" {
		t.Errorf("opened %q, want the base", got)
	}
}

func TestLayerPicture(t *testing.T) {
	m := newMapRig(t)
	rec := m.do(http.MethodGet, m.base()+"/4.png?layer=belts", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("layer: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if got := m.be.store.opened; len(got) != 1 || got[0] != "belts" {
		t.Errorf("opened %q", got)
	}
}

func TestLayerWithoutAFileIs404(t *testing.T) {
	m := newMapRig(t)
	// "power" is a category, but the fake keeps no picture for it.
	if rec := m.do(http.MethodGet, m.base()+"/1.png?layer=power", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestPictureRefusesWhatIsNotOnTheList(t *testing.T) {
	m := newMapRig(t)
	for _, p := range []string{
		"/2.png",             // no such surface
		"/-1.png", "/+1.png", // signs
		"/01.png", "/1.PNG", "/1", "/1.png.png", "/abc.png", "/%2e%2e.png", "/1e0.png",
		"/99999999999999999999.png",
		"/1.png?layer=../belts", "/1.png?layer=Belts", "/1.png?layer=", "/1.png?layer=nope",
		"/1.png?layer=belts&layer=power", "/1.png?layer=belts&layer=belts",
		"/1/base.png",
	} {
		rec := m.do(http.MethodGet, m.base()+p, nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, rec.Code)
		}
	}
	if len(m.be.store.opened) != 0 {
		t.Errorf("the store was opened for: %q", m.be.store.opened)
	}
}

func TestPictureOfAWorldWithNoDrawing(t *testing.T) {
	m := newMapRig(t)
	m.be.view.Drawing = factorymap.Drawing{}
	if rec := m.do(http.MethodGet, m.base()+"/1.png", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	if rec := m.do(http.MethodGet, m.base()+"/1/download", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("download status %d", rec.Code)
	}
}

func TestDownloadCombinesLayersInThePagesOrder(t *testing.T) {
	m := newMapRig(t)
	rec := m.do(http.MethodGet, m.base()+"/4/download?layer=power&layer=belts&layer=power", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("download: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition %q", cd)
	}
	for _, bad := range []string{"/", `"`, `\`} {
		name := strings.SplitN(strings.TrimPrefix(cd, `attachment; filename="`), `"`, 2)[0]
		if strings.Contains(name, bad) {
			t.Errorf("the file name %q holds %q, from the surface's name", name, bad)
		}
	}
	if got := m.be.store.composed; len(got) != 1 || len(got[0]) != 2 || got[0][0] != factorymap.CategoryBelts || got[0][1] != factorymap.CategoryPower {
		t.Errorf("composed %v, want belts then power once each", got)
	}
	if m.be.store.composeAt != 4 {
		t.Errorf("surface %d", m.be.store.composeAt)
	}
	img, err := png.Decode(rec.Body)
	if err != nil || img.Bounds().Dx() != 3 {
		t.Errorf("body: %v %v", img, err)
	}
}

func TestDownloadWithNoLayersIsTheBase(t *testing.T) {
	m := newMapRig(t)
	rec := m.do(http.MethodGet, m.base()+"/1/download", nil)
	if rec.Code != http.StatusOK || len(m.be.store.composed) != 1 || len(m.be.store.composed[0]) != 0 {
		t.Fatalf("%d, composed %v", rec.Code, m.be.store.composed)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "Home_Base-factory-map-nauvis.png") {
		t.Errorf("Content-Disposition %q", cd)
	}
}

func TestDownloadRefusesWhatIsNotOnTheList(t *testing.T) {
	m := newMapRig(t)
	for _, p := range []string{"/9/download", "/-1/download", "/x/download", "/1/download?layer=nope", "/1/download?layer=../x", "/1/download?layer=belts&layer="} {
		if rec := m.do(http.MethodGet, m.base()+p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", p, rec.Code)
		}
	}
	if len(m.be.store.composed) != 0 {
		t.Errorf("composed %v", m.be.store.composed)
	}
}

func TestMapRoutesNeedSignIn(t *testing.T) {
	m := newMapRig(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/1.png"}, {http.MethodGet, "/1/download"}, {http.MethodPost, "/draw"}, {http.MethodPost, "/mode"},
	} {
		r := httptest.NewRequest(c.method, m.base()+c.path, strings.NewReader("mode=off"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = peerUntrusted
		rec := httptest.NewRecorder()
		m.mux.ServeHTTP(rec, r)
		if rec.Code == http.StatusOK || rec.Code == http.StatusSeeOther && strings.HasSuffix(rec.Header().Get("Location"), "/map") {
			t.Errorf("%s %s without a session: %d", c.method, c.path, rec.Code)
		}
	}
	if m.be.drawn != 0 || len(m.be.modes) != 0 || len(m.be.store.opened) != 0 {
		t.Error("an unsigned request reached the backend")
	}
}

func TestUnknownWorldIs404(t *testing.T) {
	m := newMapRig(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/worlds/nope/map/1.png"}, {http.MethodPost, "/worlds/nope/map/draw"}, {http.MethodPost, "/worlds/nope/map/mode"},
	} {
		if rec := m.do(c.method, c.path, url.Values{"mode": {"off"}}); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: %d", c.method, c.path, rec.Code)
		}
	}
}

func TestMapRoutesWithoutServerControl(t *testing.T) {
	hn := newHarness(t, nil) // no controller
	cookie := hn.signIn()
	id, err := hn.worlds.Create(context.Background(), "W", "2.1.24", false)
	if err != nil {
		t.Fatal(err)
	}
	// The real routes answer 404 for the pages (the draw button's route is a
	// long one, which a recorder cannot take, so it is tried below).
	for _, p := range []string{"/map", "/map/1.png", "/map/1/download"} {
		if rec := hn.get("/worlds/"+id+p, withCookie(cookie)); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", p, rec.Code)
		}
	}
	mux := http.NewServeMux()
	(&factoryMapHandlers{s: hn.h.(*Handler).srv}).routes(mux, func(p string, f http.HandlerFunc) { mux.HandleFunc(p, f) })
	for _, p := range []string{"/map/draw", "/map/mode"} {
		r := httptest.NewRequest(http.MethodPost, "/worlds/"+id+p, strings.NewReader("mode=off"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = peerUntrusted
		r.AddCookie(cookie)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: %d, want 404", p, rec.Code)
		}
	}
}

func TestMapPageData(t *testing.T) {
	m := newMapRig(t)
	h := &factoryMapHandlers{s: m.hn.h.(*Handler).srv, b: m.be}
	sum := world.Summary{ID: m.id, Name: "Home Base"}
	for _, c := range []struct {
		name        string
		view        func(*game.FactoryMapView)
		surface     string
		wantSurface int
		draw, mode  bool
	}{
		{"first surface by default", func(*game.FactoryMapView) {}, "", 1, true, true},
		{"surface asked for", func(*game.FactoryMapView) {}, "4", 4, true, true},
		{"unknown surface falls back", func(*game.FactoryMapView) {}, "7", 1, true, true},
		{"junk surface falls back", func(*game.FactoryMapView) {}, "-4", 1, true, true},
		{"mode off keeps the picture, hides the button", func(v *game.FactoryMapView) { v.Mode.Mode = world.FactoryMapOff }, "", 1, false, true},
		{"old game", func(v *game.FactoryMapView) { v.State = "unsupported"; v.Drawing = factorymap.Drawing{} }, "", 0, false, false},
		{"drawing", func(v *game.FactoryMapView) { v.State = "drawing" }, "", 1, false, true},
		{"never drawn", func(v *game.FactoryMapView) { v.State = "never"; v.Drawing = factorymap.Drawing{} }, "", 0, true, true},
	} {
		m.be.view = game.FactoryMapView{State: "drawn", Mode: world.FactoryMapMode{Mode: world.FactoryMapManual}, Drawing: factorymap.Drawing{Surfaces: []factorymap.Surface{{Index: 1}, {Index: 4}}}}
		c.view(&m.be.view)
		p, err := h.page(sum, c.surface)
		if err != nil {
			t.Fatal(err)
		}
		if p.Surface != c.wantSurface || p.CanDraw != c.draw || p.CanSetMode != c.mode {
			t.Errorf("%s: surface %d draw %v mode %v; want %d %v %v", c.name, p.Surface, p.CanDraw, p.CanSetMode, c.wantSurface, c.draw, c.mode)
		}
		if len(p.Categories) != len(factorymap.Categories) {
			t.Errorf("%s: categories %v", c.name, p.Categories)
		}
	}
}

func TestMapPageViewErrorIs404OrFailure(t *testing.T) {
	m := newMapRig(t)
	m.be.viewErr = fs.ErrNotExist
	if rec := m.do(http.MethodGet, m.base(), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMapPagePaths(t *testing.T) {
	p := FactoryMapPage{World: world.Summary{ID: "w1"}, Surface: 4}
	for got, want := range map[string]string{
		p.PicturePath(4, ""):                       "/worlds/w1/map/4.png",
		p.PicturePath(4, factorymap.CategoryPower): "/worlds/w1/map/4.png?layer=power",
		p.DownloadPath(4):                          "/worlds/w1/map/4/download",
		p.DrawPath():                               "/worlds/w1/map/draw",
		p.ModePath():                               "/worlds/w1/map/mode",
		p.FragmentPath():                           "/worlds/w1/map?surface=4&fragment=1",
		p.SurfacePath(2):                           "/worlds/w1/map?surface=2",
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	if p.Tabs().Current != "map" || !p.Tabs().Settings {
		t.Errorf("tabs %+v", p.Tabs())
	}
}
