package web

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io/fs"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/factorymap"
	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The factory map's routes (SPEC 5.16). The page and its two forms:
//
//	GET  /worlds/{id}/map                  the viewer; ?surface=<index> picks a
//	                                       surface, ?fragment=1 the state region alone
//	POST /worlds/{id}/map/draw             the "Draw factory map" button
//	POST /worlds/{id}/map/mode             the mode form
//
// and the pictures, built from a surface's index and category names from the
// fixed list, never from anything else the client sends:
//
//	GET /worlds/{id}/map/{surface}.png?layer=<category>   the base picture, or one layer
//	GET /worlds/{id}/map/{surface}/download?layer=a&layer=b   the base with the layers, as a file
const (
	pathFactoryMap      = PathWorlds + "/{id}/map"
	pathFactoryDraw     = pathFactoryMap + "/draw"
	pathFactoryMode     = pathFactoryMap + "/mode"
	pathFactoryPicture  = pathFactoryMap + "/{file}"
	pathFactoryDownload = pathFactoryMap + "/{surface}/download"
)

// The form and query fields of the map pages.
const (
	factoryModeField          = "mode"
	factorySkipField          = "skip_online"
	factoryConfirmMemoryField = "confirm_memory"
	factorySurfaceQuery       = "surface"
	factoryFragmentQuery      = "fragment"
	factoryLayerQuery         = "layer"
	// factoryStateRegion is the template (and element id) that holds what
	// the map is doing, which the page fetches alone while a drawing runs.
	factoryStateRegion = "factorymap-state"
)

// The states of a world's factory map, as game.FactoryMapView.State says them
// (SPEC 5.16). F5's file defines the same strings.
const (
	factoryDrawing     = "drawing"
	factoryUnsupported = "unsupported"
)

// The refusals of a click on the draw button, as the names of the
// "factorymap.refused.<name>" words (SPEC 5.16).
const (
	refusedOff           = "off"
	refusedNotSupported  = "not_supported"
	refusedPlayersOnline = "players_online"
	// refusedNoSave and refusedArchived are the controller's refusals, mapped
	// once its errors exist.
	refusedNoSave   = "no_save"
	refusedArchived = "archived"
)

// refusedMsgs are the words of each refusal; the keys are written out so each
// is seen where it is used.
var refusedMsgs = map[string]string{
	refusedOff:           "factorymap.refused.off",
	refusedNotSupported:  "factorymap.refused.not_supported",
	refusedPlayersOnline: "factorymap.refused.players_online",
	refusedNoSave:        "factorymap.refused.no_save",
	refusedArchived:      "factorymap.refused.archived",
}

// refusedMsg is the words for the refusal named name; the zero Msg for a name
// that is not one.
func refusedMsg(name string) i18n.Msg {
	if key, ok := refusedMsgs[name]; ok {
		return i18n.M(key)
	}
	return i18n.Msg{}
}

func factoryMapPath(id string) string { return worldPath(id) + "/map" }

// FactoryMapPage is the factory map viewer (SPEC 5.16). Its template is
// templates/factorymap.html; it also defines "factorymap-state", the region
// served alone for ?fragment=1.
type FactoryMapPage struct {
	World world.Summary
	View  game.FactoryMapView
	// Categories are the layers, in the page's order.
	Categories []factorymap.Category
	// Surface is the Index of the surface shown: ?surface=, else the first.
	Surface int
	// CanDraw is whether the "Draw factory map" button shows: not when the
	// mode is off, the game is too old, or a drawing runs (SPEC 5.16).
	CanDraw bool
	// CanSetMode is whether the mode form shows: not for a game too old to
	// have a map.
	CanSetMode bool
	// Refused says why the last click on the draw button was refused; zero
	// when it was not.
	Refused i18n.Msg
	// NeedMemory is set when the click needs the "confirm_memory" box first,
	// because a game runs: the page shows MemoryBytes, about what the running
	// game uses now and so about what the drawing needs (0 when unknown).
	NeedMemory  bool
	MemoryBytes uint64
}

func (FactoryMapPage) templateName() string { return "factorymap" }
func (FactoryMapPage) navItem() string      { return PathWorlds }

// Tabs links the world's pages (DESIGN 6).
func (p FactoryMapPage) Tabs() WorldTabs {
	return WorldTabs{Path: worldPath(p.World.ID), Current: "map", Settings: true}
}

// WorldPath links back to the world page.
func (p FactoryMapPage) WorldPath() string { return worldPath(p.World.ID) }

// Path is the page itself, and FragmentPath its state region alone.
func (p FactoryMapPage) Path() string { return factoryMapPath(p.World.ID) }

// FragmentPath is where the state region is fetched while a drawing runs; it
// keeps the surface shown.
func (p FactoryMapPage) FragmentPath() string {
	return p.SurfacePath(p.Surface) + "&" + factoryFragmentQuery + "=1"
}

// SurfacePath is the page with surface index i shown.
func (p FactoryMapPage) SurfacePath(i int) string {
	return factoryMapPath(p.World.ID) + "?" + factorySurfaceQuery + "=" + strconv.Itoa(i)
}

// DrawPath is where the "Draw factory map" form posts.
func (p FactoryMapPage) DrawPath() string { return factoryMapPath(p.World.ID) + "/draw" }

// ModePath is where the mode form posts.
func (p FactoryMapPage) ModePath() string { return factoryMapPath(p.World.ID) + "/mode" }

// PicturePath is a surface's base picture, or the layer's when layer is not
// empty.
func (p FactoryMapPage) PicturePath(surface int, layer factorymap.Category) string {
	path := factoryMapPath(p.World.ID) + "/" + strconv.Itoa(surface) + ".png"
	if layer != "" {
		path += "?" + factoryLayerQuery + "=" + string(layer)
	}
	return path
}

// DownloadPath is a surface's base picture as a file; the page's script adds
// "layer=<category>" for each ticked layer.
func (p FactoryMapPage) DownloadPath(surface int) string {
	return factoryMapPath(p.World.ID) + "/" + strconv.Itoa(surface) + "/download"
}

// Shown is the surface shown, when the drawing has it.
func (p FactoryMapPage) Shown() (factorymap.Surface, bool) {
	i := slices.IndexFunc(p.View.Drawing.Surfaces, func(s factorymap.Surface) bool { return s.Index == p.Surface })
	if i < 0 {
		return factorymap.Surface{}, false
	}
	return p.View.Drawing.Surfaces[i], true
}

// pictureStore is the part of a world's factorymap.Store the handlers use.
type pictureStore interface {
	OpenPNG(surface int, layer factorymap.Category) (*os.File, error)
	Composite(surface int, layers []factorymap.Category) (image.Image, error)
}

// factoryMapBackend is what the map handlers need of the game controller and
// the world manager.
type factoryMapBackend interface {
	FactoryMap(id string) (game.FactoryMapView, error)
	DrawFactoryMap(ctx context.Context, id string) error
	Pictures(id string) (pictureStore, error)
	SetMode(id string, mode world.FactoryMapMode) error
	// RunningMemory is what the running game of world id uses, 0 if unknown.
	RunningMemory(id string) uint64
}

// liveFactoryMap is the backend of a server with server control.
type liveFactoryMap struct {
	c *game.Controller
	w *world.Manager
}

func (l liveFactoryMap) FactoryMap(id string) (game.FactoryMapView, error) {
	return l.c.FactoryMap(id)
}

func (l liveFactoryMap) DrawFactoryMap(ctx context.Context, id string) error {
	return l.c.DrawFactoryMap(ctx, id)
}

func (l liveFactoryMap) Pictures(id string) (pictureStore, error) {
	st, err := l.c.FactoryMapStore(id)
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (l liveFactoryMap) SetMode(id string, mode world.FactoryMapMode) error {
	return l.w.SetFactoryMapMode(id, mode)
}

func (l liveFactoryMap) RunningMemory(id string) uint64 {
	if st := l.c.Status(id); st.HaveUsage {
		return st.MemoryBytes
	}
	return 0
}

// factoryMapHandlers serves the factory map pages. b is nil without server
// control, and every route then answers as an unknown path.
type factoryMapHandlers struct {
	s *server
	b factoryMapBackend

	// combine holds a slot for each combined download being drawn, which
	// holds about 150 MiB while it is; combineWait is how long a request
	// waits for one. Both default (maxCombined, combineWait) when unset.
	combine     chan struct{}
	combineWait time.Duration
	once        sync.Once
}

// The combined download's limit: at most maxCombined are drawn at once in
// the process (each holds about 150 MiB), and a request waits combineWait for
// a slot before it is told to retry (SPEC 5.16, 14).
const (
	maxCombined        = 2
	defaultCombineWait = 3 * time.Second
	combineRetryAfter  = "10"
)

// slot takes a combined-download slot, waiting a little for one. The caller
// calls the function it returns when done; ok is false when none came free.
func (h *factoryMapHandlers) slot(ctx context.Context) (release func(), ok bool) {
	h.once.Do(func() {
		if h.combine == nil {
			h.combine = make(chan struct{}, maxCombined)
		}
		if h.combineWait == 0 {
			h.combineWait = defaultCombineWait
		}
	})
	t := time.NewTimer(h.combineWait)
	defer t.Stop()
	select {
	case h.combine <- struct{}{}:
		return func() { <-h.combine }, true
	case <-t.C:
	case <-ctx.Done():
	}
	return nil, false
}

// factoryMapRoutes registers the factory map page and its pictures (SPEC
// 5.16).
func (s *server) factoryMapRoutes() {
	h := &factoryMapHandlers{s: s}
	if s.game != nil {
		h.b = liveFactoryMap{c: s.game, w: s.worlds}
	}
	h.routes(s.mux, s.handleLong)
}

// routes registers the handlers on mux. long registers the draw button,
// which may wait for a run to start.
func (h *factoryMapHandlers) routes(mux *http.ServeMux, long func(string, http.HandlerFunc)) {
	s := h.s
	mux.HandleFunc("GET "+pathFactoryMap, s.requireUser(h.handlePage))
	long("POST "+pathFactoryDraw, s.requireUser(h.handleDraw))
	mux.HandleFunc("POST "+pathFactoryMode, s.requireUser(h.handleMode))
	mux.HandleFunc("GET "+pathFactoryPicture, s.requireUser(h.handlePicture))
	mux.HandleFunc("GET "+pathFactoryDownload, s.requireUser(h.handleDownload))
}

// world finds the world of the request. ok is false when the reply is
// written: no server control, no such world, or one whose settings pages are
// not open (a newer Foreman's or a broken one).
func (h *factoryMapHandlers) world(w http.ResponseWriter, r *http.Request, u store.User) (world.Summary, bool) {
	if h.b == nil {
		h.s.notFound(w, r, u)
		return world.Summary{}, false
	}
	sum, ok := h.s.findWorld(w, r, u)
	if !ok {
		return sum, false
	}
	if sum.Newer || sum.Unreadable {
		h.s.notFound(w, r, u)
		return sum, false
	}
	return sum, true
}

// page builds the viewer's data for sum, showing the surface named by
// surface (a ?surface= value; blank or unknown shows the first).
func (h *factoryMapHandlers) page(sum world.Summary, surface string) (FactoryMapPage, error) {
	v, err := h.b.FactoryMap(sum.ID)
	if err != nil {
		return FactoryMapPage{}, err
	}
	p := FactoryMapPage{
		World: sum, View: v, Categories: factorymap.Categories,
		CanDraw:    v.Mode.Mode != world.FactoryMapOff && v.State != factoryUnsupported && v.State != factoryDrawing,
		CanSetMode: v.State != factoryUnsupported,
	}
	surfaces := v.Drawing.Surfaces
	if len(surfaces) > 0 {
		p.Surface = surfaces[0].Index
		if n, ok := parseSurface(surface); ok && slices.ContainsFunc(surfaces, func(s factorymap.Surface) bool { return s.Index == n }) {
			p.Surface = n
		}
	}
	return p, nil
}

func (h *factoryMapHandlers) handlePage(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := h.world(w, r, u)
	if !ok {
		return
	}
	p, err := h.page(sum, r.URL.Query().Get(factorySurfaceQuery))
	if err != nil {
		h.s.notFoundOrFail(w, r, u, "read factory map", err)
		return
	}
	if r.URL.Query().Get(factoryFragmentQuery) == "1" {
		h.s.fragment(w, r, p, factoryStateRegion)
		return
	}
	h.s.page(w, r, http.StatusOK, u.Username, p)
}

// factoryRefusal says which of the spec's refusals a click on the draw button meets
// in view v, or "" when none: off hides the button, an old game has no map,
// and a click beside a running game is refused while players are online when
// "skip while players are online" is on (SPEC 5.16).
func factoryRefusal(v game.FactoryMapView) string {
	switch {
	case v.Mode.Mode == world.FactoryMapOff:
		return refusedOff
	case v.State == factoryUnsupported:
		return refusedNotSupported
	case v.Running && v.PlayersOnline && v.Mode.SkipOnline:
		return refusedPlayersOnline
	}
	return ""
}

// drawOutcome is what a click on the draw button came to.
type drawOutcome struct {
	// Refused is the name of the refusal; "" when none.
	Refused string
	// NeedMemory is true when a game runs and the memory note was not
	// confirmed.
	NeedMemory bool
	// Started is true when the run is under way.
	Started bool
}

// draw is the draw button's work. confirmed is the "confirm_memory" box.
func (h *factoryMapHandlers) draw(ctx context.Context, id string, confirmed bool) (drawOutcome, error) {
	v, err := h.b.FactoryMap(id)
	if err != nil {
		return drawOutcome{}, err
	}
	if n := factoryRefusal(v); n != "" {
		return drawOutcome{Refused: n}, nil
	}
	if v.Running && !confirmed {
		return drawOutcome{NeedMemory: true}, nil
	}
	if err := h.b.DrawFactoryMap(ctx, id); err != nil {
		// A refusal the controller made after our look (a player joined
		// meanwhile) reads the same as ours.
		if v, verr := h.b.FactoryMap(id); verr == nil {
			if n := factoryRefusal(v); n != "" {
				return drawOutcome{Refused: n}, nil
			}
		}
		return drawOutcome{}, err
	}
	return drawOutcome{Started: true}, nil
}

// handleDraw is the "Draw factory map" button (SPEC 5.16). It changes no
// world file, so it asks for no snapshot and is not audited.
func (h *factoryMapHandlers) handleDraw(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := h.world(w, r, u)
	if !ok {
		return
	}
	if !h.s.readForm(w, r) {
		return
	}
	out, err := h.draw(r.Context(), sum.ID, r.PostFormValue(factoryConfirmMemoryField) == "1")
	if err != nil {
		h.s.notFoundOrFail(w, r, u, "draw factory map", err)
		return
	}
	if out.Started {
		http.Redirect(w, r, factoryMapPath(sum.ID), http.StatusSeeOther)
		return
	}
	p, err := h.page(sum, "")
	if err != nil {
		h.s.notFoundOrFail(w, r, u, "read factory map", err)
		return
	}
	status := http.StatusConflict
	if out.NeedMemory {
		p.NeedMemory, p.MemoryBytes, status = true, h.b.RunningMemory(sum.ID), http.StatusOK
	} else {
		p.Refused = refusedMsg(out.Refused)
	}
	h.s.page(w, r, status, u.Username, p)
}

// handleMode saves the world's factory map mode (SPEC 5.16): Foreman's own
// setting, so no snapshot and no audit entry.
func (h *factoryMapHandlers) handleMode(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := h.world(w, r, u)
	if !ok {
		return
	}
	if !h.s.readForm(w, r) {
		return
	}
	mode := world.FactoryMapMode{Mode: r.PostFormValue(factoryModeField), SkipOnline: r.PostFormValue(factorySkipField) == "on"}
	switch mode.Mode {
	case world.FactoryMapManual, world.FactoryMapAutomatic, world.FactoryMapOff:
	default:
		h.s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	if err := h.b.SetMode(sum.ID, mode); err != nil {
		h.s.notFoundOrFail(w, r, u, "set factory map mode", err)
		return
	}
	http.Redirect(w, r, factoryMapPath(sum.ID), http.StatusSeeOther)
}

// parseSurface reads a surface index: ASCII digits in their one spelling, so
// nothing like a sign, a space, a leading zero or a path gets through.
func parseSurface(s string) (int, bool) {
	if s == "" || len(s) > 6 {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && strconv.Itoa(n) == s
}

// parseLayers reads the "layer" values of a request: each from the fixed
// list, returned in the page's order without repeats. ok is false for any
// other value.
func parseLayers(values []string) (layers []factorymap.Category, ok bool) {
	for _, v := range values {
		if !slices.ContainsFunc(factorymap.Categories, func(c factorymap.Category) bool { return string(c) == v }) {
			return nil, false
		}
	}
	for _, c := range factorymap.Categories {
		if slices.Contains(values, string(c)) {
			layers = append(layers, c)
		}
	}
	return layers, true
}

// picture finds the store and the surface a picture request names, after
// checking that the kept drawing has the surface. ok is false when the reply
// is written.
func (h *factoryMapHandlers) picture(w http.ResponseWriter, r *http.Request, u store.User, surface string) (sum world.Summary, pics pictureStore, s factorymap.Surface, ok bool) {
	sum, ok = h.world(w, r, u)
	if !ok {
		return
	}
	n, ok := parseSurface(surface)
	if !ok {
		h.s.notFound(w, r, u)
		return sum, nil, s, false
	}
	v, err := h.b.FactoryMap(sum.ID)
	if err != nil {
		h.s.notFoundOrFail(w, r, u, "read factory map", err)
		return sum, nil, s, false
	}
	i := slices.IndexFunc(v.Drawing.Surfaces, func(s factorymap.Surface) bool { return s.Index == n })
	if i < 0 {
		h.s.notFound(w, r, u)
		return sum, nil, s, false
	}
	pics, err = h.b.Pictures(sum.ID)
	if err != nil {
		h.s.notFoundOrFail(w, r, u, "open factory map", err)
		return sum, nil, s, false
	}
	return sum, pics, v.Drawing.Surfaces[i], true
}

// handlePicture serves the base picture of a surface, or one layer of it
// with ?layer= (SPEC 5.16).
func (h *factoryMapHandlers) handlePicture(w http.ResponseWriter, r *http.Request, u store.User) {
	name, found := strings.CutSuffix(r.PathValue("file"), ".png")
	layers, ok := parseLayers(r.URL.Query()[factoryLayerQuery])
	if !found || !ok || len(layers) > 1 || len(r.URL.Query()[factoryLayerQuery]) > 1 {
		h.s.notFound(w, r, u)
		return
	}
	_, pics, surface, ok := h.picture(w, r, u, name)
	if !ok {
		return
	}
	var layer factorymap.Category
	if len(layers) == 1 {
		layer = layers[0]
	}
	f, err := pics.OpenPNG(surface.Index, layer)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
			h.s.notFound(w, r, u)
			return
		}
		h.s.fail(w, r, "open factory map picture", err)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		h.s.fail(w, r, "stat factory map picture", err)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", "image/png")
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// handleDownload serves a surface's base picture with the layers asked for
// stacked on it, drawn on the server, as a file (SPEC 5.16).
func (h *factoryMapHandlers) handleDownload(w http.ResponseWriter, r *http.Request, u store.User) {
	layers, ok := parseLayers(r.URL.Query()[factoryLayerQuery])
	if !ok {
		h.s.notFound(w, r, u)
		return
	}
	sum, pics, surface, ok := h.picture(w, r, u, r.PathValue("surface"))
	if !ok {
		return
	}
	release, ok := h.slot(r.Context())
	if !ok {
		w.Header().Set("Retry-After", combineRetryAfter)
		h.s.page(w, r, http.StatusServiceUnavailable, u.Username, ErrorPage{Status: http.StatusServiceUnavailable})
		return
	}
	// The drawn picture is dropped inside combined, so the slot is released
	// when the memory is.
	body, err := combined(pics, surface.Index, layers)
	release()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
			h.s.notFound(w, r, u)
			return
		}
		h.s.fail(w, r, "combine factory map pictures", err)
		return
	}
	stem := downloadStem(sum.Name, "factory-map", pictureStem(surface))
	w.Header().Set("Content-Disposition", attachment(stem+".png"))
	writePNG(w, body)
}

// combined draws the base picture with layers stacked and encodes it, before
// any reply starts so a failure can still say so.
func combined(pics pictureStore, surface int, layers []factorymap.Category) ([]byte, error) {
	img, err := pics.Composite(surface, layers)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// pictureStem is a surface's part of a download's name: its name with path,
// control and quoting characters dropped (the name is the game's, so
// untrusted), or the word "map" when none is left.
func pictureStem(s factorymap.Surface) string {
	return strings.TrimSuffix(downloadStem(s.Name, "", ""), "-")
}
