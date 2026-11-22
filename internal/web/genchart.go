package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/store"
)

// The real-game picture (SPEC 5.6, 14.3): on step 3 of a 2.1 world, one
// planet drawn from a throwaway map a real game makes with the world's mods,
// asked for by the person, never as they type. It is kept in memory by what
// makes its map, until the store needs the room or the world's map is made.

// The real-game picture's paths: the request that draws one planet, and the
// picture it made.
const (
	pathChart        = PathGeneratePreview + "/chart"
	pathChartPicture = PathGeneratePreview + "-chart/{token}"
)

// chartDeadline is how long a real-game picture's request may take to
// answer: the wait for the slot, the run, and room to write the reply.
const chartDeadline = game.PreviewWait + game.ChartLimit + 30*time.Second

// chartTiles is the side of a real-game picture in tiles (SPEC 5.6).
const chartTiles = 1024

// ChartDrawer draws one planet of a world from a real game. The game
// controller provides it; a MapPreviewer without it offers no such picture.
type ChartDrawer interface {
	Chart(ctx context.Context, req game.PreviewRequest, planet string) (game.PNG, error)
}

// ChartView is a real-game picture, or why there is none.
type ChartView struct {
	Planet  string
	Name    i18n.Msg
	Foreign bool
	Path    string
	Seed    string
	Error   i18n.Msg
}

// Tiles is the picture's side in tiles, and in pixels.
func (ChartView) Tiles() int { return chartTiles }

// ChartTip is the toggletip on what drawing from a real game does.
func (PreviewView) ChartTip() Toggletip {
	return Toggletip{ID: "preview-chart", About: i18n.M("mapgen.chart.tip_about"), Text: i18n.M("mapgen.chart.tip")}
}

// ChartPlanet is the planet a real-game picture may be drawn of: the one
// picture shown, on a world whose game can make one; blank otherwise.
func (v PreviewView) ChartPlanet() string {
	if !v.ChartOffered || !v.Error.IsZero() || len(v.Pictures) != 1 {
		return ""
	}
	return v.Pictures[0].Planet
}

// ChartPath is where the page asks for a real-game picture.
func (PreviewView) ChartPath() string { return pathChart }

// chartDrawer is the server's ChartDrawer, if its previewer is one.
func (s *server) chartDrawer() (ChartDrawer, bool) {
	if s.previewer == nil {
		return nil, false
	}
	d, ok := s.previewer.(ChartDrawer)
	return d, ok
}

// chartRoutes registers the real-game picture's routes.
func (s *server) chartRoutes() {
	s.handleUpload("POST "+pathChart, fixedLimit(generateFormLimit), s.requireUser(s.handleGenerateChart))
	s.mux.HandleFunc("GET "+pathChartPicture, s.requireUser(s.handleChartPicture))
}

// handleGenerateChart draws one planet of step 3's settings from a real game
// (SPEC 5.6, 14.3) and answers with the "generate-chart" fragment. The world
// must be Unfinished and on a line that can make one; the planet must be one
// of the world's own, from its catalogue alone (SPEC 14.6).
func (s *server) handleGenerateChart(w http.ResponseWriter, r *http.Request, u store.User) {
	drawer, ok := s.chartDrawer()
	if !ok {
		s.notFound(w, r, u)
		return
	}
	if !s.readForm(w, r) {
		return
	}
	in, handled := s.readPreview(w, r, u)
	if handled {
		return
	}
	bad := func() { s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest}) }
	if !in.world {
		bad()
		return
	}
	id := r.PostFormValue(previewWorldField)
	sum, err := s.worlds.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, "read the world for a real-game picture", err)
		return
	}
	if !sum.Unfinished {
		// Its map is made, so pictures of the map it might have had go.
		s.previews.dropCharts(id)
		s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict})
		return
	}
	planet := r.PostFormValue(previewPlanetField)
	if !game.ChartSupported(sum.GameVersion) || !slices.Contains(in.f.cat.PlanetNames(), planet) {
		bad()
		return
	}
	f := in.f
	view := &PreviewView{Size: f.PreviewSize, OfWorld: true}
	if !in.ok {
		view.Chart = &ChartView{Error: i18n.M("mapgen.chart.invalid")}
		s.fragment(w, r, s.generatePage(r.Context(), f, view), "generate-chart")
		return
	}
	key := chartKey(in.req, f.cat, in.mods, planet)
	seed := strconv.FormatUint(uint64(in.req.Seed), 10)
	n := catPlanetName(f.cat, planet)
	cv := &ChartView{Planet: planet, Name: n.Name, Foreign: n.Foreign, Seed: seed}
	token, held := s.previews.chartOf(u.ID, key)
	if !held && !s.previews.startChart(u.ID) {
		cv.Error = chartMsgs.busy
	} else if !held {
		defer s.previews.endChart(u.ID)
		if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(chartDeadline)); err != nil {
			s.log.Warn("set the real-game picture's write deadline", "error", err)
		}
		png, err := drawer.Chart(r.Context(), in.req, planet)
		if err == nil {
			token, err = s.previews.addChart(u.ID, key, id, png.Data)
			if err != nil {
				s.log.Error("keep real-game picture", "error", err)
			}
		}
		if err != nil {
			cv.Error = s.chartFailure(r, sum.GameVersion, err)
		}
	}
	if cv.Error.IsZero() {
		cv.Path = PathGeneratePreview + "-chart/" + token
	}
	view.Chart = cv
	s.fragment(w, r, s.generatePage(r.Context(), f, view), "generate-chart")
}

// chartMsgs is how a failed real-game picture is worded. A helper that gives
// up and a game that crashes share the one "could not draw" sentence.
var chartMsgs = failureMsgs{
	busy:    i18n.M("mapgen.chart.busy"),
	timeout: i18n.M("mapgen.chart.timeout"),
	oom:     i18n.M("mapgen.chart.oom"),
	failed:  i18n.M("mapgen.chart.failed"),
}

// chartFailure words why no real-game picture was made. A chart that is
// refused may also match game.ErrSaveCopyBadOutput, as it is read with the
// factory map's readers, so it is tested first (SPEC 14.6).
func (s *server) chartFailure(r *http.Request, version string, err error) i18n.Msg {
	if errors.Is(err, game.ErrChartBadOutput) {
		s.log.Warn("real-game picture refused", "version", version)
		return chartMsgs.failed
	}
	return s.controlModFailure(r, version, err, "real-game picture", chartMsgs)
}

// chartKey is what makes a planet's real-game picture (SPEC 5.6): the
// planet's share of the map settings, the seed, the world's mods key and the
// game version, all in its share of settingsKey. The preview size draws no
// part of it, so it is left out.
func chartKey(req game.PreviewRequest, cat mapgen.Catalogue, mods, planet string) string {
	req.Size = 0
	return hashJSON([]string{planet, settingsKey(req, cat, mods).planet(planet)})
}

// handleChartPicture serves a real-game picture to the user who drew it,
// while its world's map is not made. Nothing else is read through this route.
func (s *server) handleChartPicture(w http.ResponseWriter, r *http.Request, u store.User) {
	c, ok := s.previews.chart(r.PathValue("token"), u.ID)
	if !ok {
		s.notFound(w, r, u)
		return
	}
	if sum, err := s.worlds.Get(r.Context(), c.world); err != nil || !sum.Unfinished {
		s.previews.dropCharts(c.world)
		s.notFound(w, r, u)
		return
	}
	writePNG(w, c.png)
}

// chartEntry is one kept real-game picture.
type chartEntry struct {
	token string
	user  int64
	key   string
	world string
	png   []byte
}

// addChart keeps a user's real-game picture and returns its token. Only the
// store's size limit evicts one, oldest first (SPEC 5.6).
func (p *previewStore) addChart(user int64, key, world string, png []byte) (string, error) {
	if len(png) > maxPreviewBytes {
		return "", errors.New("the real-game picture is too large")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.charts = slices.DeleteFunc(p.charts, func(c *chartEntry) bool { return c.user == user && c.key == key })
	c := &chartEntry{token: newToken(), user: user, key: key, world: world, png: png}
	p.charts = append(p.charts, c)
	p.fitLocked()
	return c.token, nil
}

// chartOf returns the token of the user's real-game picture of key.
func (p *previewStore) chartOf(user int64, key string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.charts {
		if c.user == user && c.key == key {
			return c.token, true
		}
	}
	return "", false
}

// chart returns the user's real-game picture under token.
func (p *previewStore) chart(token string, user int64) (chartEntry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.charts {
		if c.token == token && c.user == user {
			return *c, true
		}
	}
	return chartEntry{}, false
}

// dropCharts drops every real-game picture of a world, once its map is made.
func (p *previewStore) dropCharts(world string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.charts = slices.DeleteFunc(p.charts, func(c *chartEntry) bool { return c.world == world })
}

// chartBytesLocked is what the kept real-game pictures hold.
func (p *previewStore) chartBytesLocked() int {
	n := 0
	for _, c := range p.charts {
		n += len(c.png)
	}
	return n
}

// fitLocked keeps everything the store holds within its size limit: the
// oldest users' previews go first, then the oldest real-game pictures, so
// the newest picture stays.
func (p *previewStore) fitLocked() {
	for len(p.order) > 0 && p.totalLocked() > maxPreviewBytes {
		p.dropLocked(p.order[0])
	}
	for len(p.charts) > 1 && p.totalLocked() > maxPreviewBytes {
		p.charts = p.charts[1:]
	}
}

// startChart marks the user's real-game picture as running; false when one
// already runs, so a second is refused with the busy sentence (SPEC 14.3).
func (p *previewStore) startChart(user int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.charting[user] {
		return false
	}
	if p.charting == nil {
		p.charting = map[int64]bool{}
	}
	p.charting[user] = true
	return true
}

// endChart marks the user's real-game picture as done.
func (p *previewStore) endChart(user int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.charting, user)
}
