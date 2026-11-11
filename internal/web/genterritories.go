package web

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/territory"
)

// Vulcanus's demolisher territories over its map preview (SPEC 5.6; DESIGN
// 8.8): read on request, kept with the pictures, drawn in the page, and
// drawn into the downloaded picture while the box is ticked.
const (
	// pathTerritories answers the page's request for the territories of the
	// settings in the form, after the picture has arrived.
	pathTerritories = PathGeneratePreview + "/territories"
	// territoriesField is the "Show demolisher territories" box. It is in
	// the form, so "Preview map" without script reads them too.
	territoriesField = "territories"
	// territoriesQuery on a download draws the territories into the picture.
	territoriesQuery = "territories"
	// territoryPlanet is the one planet with demolisher territories.
	territoryPlanet = "vulcanus"
)

// TerritoryBox is the "Show demolisher territories" box and what it shows.
type TerritoryBox struct {
	// Show is true when Vulcanus's picture is shown, which only a Space Age
	// world has.
	Show bool
	// On is true when the person ticked the box, even where it is not shown,
	// so the choice survives a look at another planet.
	On bool
	// Result is the territories read, or why they were not; nil until asked.
	Result *TerritoryView
}

// Checked is true when the box shows as ticked.
func (b TerritoryBox) Checked() bool { return b.On && b.Show }

// Carry is true when the choice is kept in a hidden field, as the box is
// not there to send it.
func (b TerritoryBox) Carry() bool { return b.On && !b.Checked() }

// Drawn is true when the territories are shown, so the downloads draw them.
func (b TerritoryBox) Drawn() bool { return b.Checked() && b.Result != nil && b.Result.Error.IsZero() }

// RequestFailed is what the page shows when its request for the
// territories fails outright.
func (TerritoryBox) RequestFailed() *TerritoryView {
	return &TerritoryView{Error: i18n.M("worlds.generate.territories.request_failed"), Retry: true}
}

// TerritoryView is the overlay of one picture: in picture pixels, which the
// page scales with the picture, and labels placed in percent at a fixed
// type size. Error holds why there is none.
type TerritoryView struct {
	Size   int
	Runs   []territory.Rect
	Border string // an SVG path of every border
	Labels []TerritoryLabel
	Counts string // the sentence listing how many of each size
	Error  i18n.Msg
	// Retry is true when asking again may work, as after a busy or failed run.
	Retry bool
}

// TerritoryLabel is a territory's size in words, centred on Left and Top
// percent of the picture: Short on the small picture, Text in the large view.
type TerritoryLabel struct {
	Left, Top   float64
	Text, Short string
}

// IsTerritoryPicture is true for the picture the territories are drawn on.
func (v PreviewView) IsTerritoryPicture(p PreviewPicture) bool {
	return p.Planet == territoryPlanet && v.Territories.Show
}

// DownloadOf is where a picture is served as a file: with the territories
// drawn in, while they are shown on it.
func (v PreviewView) DownloadOf(p PreviewPicture) string {
	if v.IsTerritoryPicture(p) && v.Territories.Drawn() {
		return p.DownloadPath() + "?" + territoriesQuery + "=1"
	}
	return p.DownloadPath()
}

// territoryView turns territories read for a picture of size pixels into
// what the page draws, worded in loc.
func territoryView(loc i18n.Locale, res territory.Result, size int) *TerritoryView {
	ov := territory.Geometry(res, size)
	v := &TerritoryView{Size: size}
	var d strings.Builder
	for _, b := range ov.Borders {
		fmt.Fprintf(&d, "M%d %dL%d %d", b.X0, b.Y0, b.X1, b.Y1)
	}
	v.Border = d.String()
	pct := func(px float64) float64 { return math.Round(px/float64(size)*1e5) / 1e3 }
	for _, s := range ov.Shapes {
		v.Runs = append(v.Runs, s.Runs...)
		v.Labels = append(v.Labels, TerritoryLabel{Left: pct(s.LabelX), Top: pct(s.LabelY), Text: territory.Label(loc, s.Size), Short: territory.ShortLabel(loc, s.Size)})
	}
	if len(ov.Counts) == 0 {
		v.Counts = loc.T("worlds.generate.territories.none")
		return v
	}
	var parts []string
	for _, c := range ov.Counts {
		parts = append(parts, loc.Format(countMsg(c)))
	}
	v.Counts = loc.T("worlds.generate.territories.counts", "list", strings.Join(parts, loc.T("common.list.sep")))
	return v
}

// countMsg words how many territories of one size a picture has.
func countMsg(c territory.Count) i18n.Msg {
	switch c.Size {
	case territory.Small:
		return i18n.M("worlds.generate.territories.count.small", "count", c.N)
	case territory.Medium:
		return i18n.M("worlds.generate.territories.count.medium", "count", c.N)
	case territory.Big:
		return i18n.M("worlds.generate.territories.count.big", "count", c.N)
	}
	return i18n.M("worlds.generate.territories.count.unknown", "count", c.N)
}

// territoryRefusal says why the territories of these settings are not read,
// before anything runs, or is zero when they can be.
func territoryRefusal(req game.PreviewRequest) i18n.Msg {
	line, _ := mapgen.Line(req.GameVersion)
	if !req.SpaceAge || !slices.Contains(mapgen.Planets(line, true), territoryPlanet) {
		return i18n.M("worlds.generate.territories.no_vulcanus")
	}
	return i18n.Msg{}
}

// territories returns the territories of req: from the user's preview when
// they were read already, otherwise read now when run is true, else nil.
func (s *server) territories(w http.ResponseWriter, r *http.Request, u store.User, req game.PreviewRequest, name string, run bool) *TerritoryView {
	if no := territoryRefusal(req); !no.IsZero() {
		return &TerritoryView{Error: no}
	}
	key := settingsKey(req)
	if res, ok := s.previews.territoriesOf(u.ID, key); ok {
		return territoryView(localeOf(r), res, req.Size)
	}
	if !run {
		return nil
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(previewDeadline)); err != nil {
		s.log.Warn("set the territory run's write deadline", "error", err)
	}
	res, err := s.previewer.Territories(r.Context(), req)
	if err != nil {
		// A missing image is not there on a second try either.
		return &TerritoryView{Error: s.controlModFailure(r, req.GameVersion, err, "territory run", territoryMsgs), Retry: !errors.Is(err, game.ErrPreviewImageMissing)}
	}
	if _, err := s.previews.addTerritories(u.ID, key, name, strconv.FormatUint(uint64(req.Seed), 10), req.Size, res); err != nil {
		s.log.Error("keep demolisher territories", "error", err)
		return &TerritoryView{Error: i18n.M("worlds.generate.territories.failed"), Retry: true}
	}
	return territoryView(localeOf(r), res, req.Size)
}

// territoryMsgs is how a failed territory run is worded.
var territoryMsgs = failureMsgs{
	busy:    i18n.M("worlds.generate.territories.busy"),
	timeout: i18n.M("worlds.generate.territories.timeout"),
	oom:     i18n.M("worlds.generate.territories.oom"),
	failed:  i18n.M("worlds.generate.territories.failed"),
}

// territoryBox is the box under the pictures of view, with the territories
// from the store, or read now when run is true.
func (s *server) territoryBox(w http.ResponseWriter, r *http.Request, u store.User, req game.PreviewRequest, name string, view *PreviewView, run bool) TerritoryBox {
	b := TerritoryBox{
		On:   r.PostFormValue(territoriesField) != "",
		Show: view.Error.IsZero() && slices.ContainsFunc(view.Pictures, func(p PreviewPicture) bool { return p.Planet == territoryPlanet }),
	}
	if b.Checked() {
		b.Result = s.territories(w, r, u, req, name, run)
	}
	return b
}

// handleGenerateTerritories answers the page's request for the territories
// of the settings in the form (SPEC 5.6): the overlay and the list, or why
// there are none, as the "generate-territories" fragment. The page asks
// after the picture has arrived, so the picture never waits for them.
func (s *server) handleGenerateTerritories(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.previewer == nil {
		s.notFound(w, r, u)
		return
	}
	if !s.readForm(w, r) {
		return
	}
	f, nw, ok := s.readGenerate(r, kindPreview)
	view := &PreviewView{Size: f.PreviewSize, Territories: TerritoryBox{On: true, Show: true}}
	if ok {
		view.Territories.Result = s.territories(w, r, u, previewRequest(f, nw), f.Name, true)
	} else {
		view.Territories.Result = &TerritoryView{Error: i18n.M("worlds.generate.territories.invalid")}
	}
	s.fragment(w, r, s.generatePage(f, view), "generate-territories")
}

// drawPNG is territory.DrawPNG; a test swaps it to fill the render queue.
var drawPNG = territory.DrawPNG

// drawFailure answers a download whose territories or amounts could not be
// drawn: a busy render queue is a 503 the user can retry, anything else a 500.
func (s *server) drawFailure(w http.ResponseWriter, r *http.Request, u store.User, err error) {
	if !errors.Is(err, territory.ErrBusy) {
		s.fail(w, r, "draw the demolisher territories", err)
		return
	}
	w.Header().Set("Retry-After", "5")
	s.page(w, r, http.StatusServiceUnavailable, u.Username, ErrorPage{Status: http.StatusServiceUnavailable,
		Detail: i18n.M("worlds.generate.territories.draw_busy")})
}
