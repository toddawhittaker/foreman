package web

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"image/png"
	"io/fs"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/resource"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/territory"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The map preview's paths under PathGeneratePreview: one picture to show,
// the same picture to download, and all drawn planets as a zip.
const (
	pathPicture         = PathGeneratePreview + "/{token}/{planet}"
	pathPictureDownload = pathPicture + "/download"
	pathPicturesZip     = PathGeneratePreview + "/{token}/all.zip"
	zipName             = "all.zip"
)

// Form fields of the preview request beyond Generate's own.
const (
	// previewPlanetField names the one planet to draw; blank draws all.
	previewPlanetField = "planet"
	// previewAutoField is "1" on a request the page sends by itself as the
	// person types. Settings that are not valid then get the short
	// "generate-preview-invalid" fragment, with status 200, in the preview's
	// slot, and the form is left alone.
	previewAutoField = "auto"
	// previewViewField is the section's choice of one planet or all of them;
	// previewViewAll makes an automatic request draw every planet.
	previewViewField = "preview_view"
	previewViewAll   = "all"
)

// previewDeadline is how long a preview request may take to answer: the
// wait for the slot, the run, and room to write the reply. The server's own
// write timeout is shorter.
const previewDeadline = game.PreviewWait + game.PreviewLimit + 30*time.Second

// MapPreviewer makes the map preview pictures: one PNG for each planet
// asked for, from the settings Generate would write (SPEC 5.6, 14.3), and
// reads where Vulcanus's demolisher territories lie in the same map, and
// counts one planet's resource patches in it. The game controller provides it. Seed is always set, so the world made later
// has the same map.
type MapPreviewer interface {
	Preview(ctx context.Context, req game.PreviewRequest) ([]game.PNG, error)
	Territories(ctx context.Context, req game.PreviewRequest) (territory.Result, error)
	Resources(ctx context.Context, req game.PreviewRequest, planet string) (resource.Result, error)
}

// previewTTL is how long a preview's pictures are kept (SPEC 5.6).
const previewTTL = 15 * time.Minute

// The pictures held at once: 8 users' previews and 160 MiB in all, so a few
// admins cannot fill the memory (SPEC 5.6, 14.3). One user's Space Age set at
// the largest size is about 35 MB, so a second user does not evict it; a
// world's mods can add planets, hence the room above that.
const (
	maxPreviews     = 8
	maxPreviewBytes = 160 << 20
)

// previewEntry is one user's current preview: the pictures drawn so far
// from one set of settings, under a random token, and Vulcanus's demolisher
// territories once they are read, and each planet's resource amounts once
// counted. bytes counts them all.
type previewEntry struct {
	token       string
	key         previewKey // the settings drawn
	name        string     // the world name typed, for download names
	seed        string
	expires     time.Time
	planets     []string // in the order drawn
	images      map[string][]byte
	territories *territory.Result
	size        int                         // the side, in pixels, of the pictures the territories are read for
	resources   map[string]*resource.Result // by planet
	bytes       int
}

// previewStore holds each user's current preview (SPEC 5.6). Nothing else
// is served from it, and only to the user it belongs to.
type previewStore struct {
	mu     sync.Mutex
	now    func() time.Time
	byUser map[int64]*previewEntry
	order  []int64 // least recently added to first
}

func newPreviewStore(now func() time.Time) *previewStore {
	return &previewStore{now: now, byUser: map[int64]*previewEntry{}}
}

// missing returns the planets the user's current preview lacks for these
// settings, and the preview's token when it has all of them. A picture of
// other settings counts when its planet's share of them is the same; the
// preview then moves to these settings. It renames the preview, since the
// name is not part of the key.
func (p *previewStore) missing(user int64, key previewKey, name string, planets []string) (string, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	e, ok := p.byUser[user]
	if !ok {
		return "", planets
	}
	var out []string
	for _, pl := range planets {
		if _, ok := e.images[pl]; !ok || e.key.planet(pl) != key.planet(pl) {
			out = append(out, pl)
		}
	}
	if len(out) > 0 {
		return "", out
	}
	if e.key.all != key.all {
		return p.keepLocked(user, p.joinLocked(user, key, e.seed), name), nil
	}
	e.name = name
	e.expires = p.now().Add(previewTTL)
	return e.token, nil
}

// add keeps new pictures and returns the preview's token. Pictures of the
// same settings join the user's current preview; other settings replace
// it. Other users' previews go, oldest first, to keep the limits. The
// planets are kept in order, the world's planet order (its catalogue's), so
// a planet drawn again after a change of its own settings keeps its place.
func (p *previewStore) add(user int64, key previewKey, name, seed string, images []game.PNG, order []string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := p.joinLocked(user, key, seed)
	grown := e.bytes
	for _, im := range images {
		if len(im.Data) > game.MaxPreviewPNG {
			return "", fmt.Errorf("the picture of %s is too large", im.Planet)
		}
		grown += len(im.Data) - len(e.images[im.Planet])
	}
	if grown > maxPreviewBytes {
		return "", errors.New("the preview pictures are too large")
	}
	for _, im := range images {
		if _, had := e.images[im.Planet]; !had {
			e.planets = append(e.planets, im.Planet)
		}
		e.images[im.Planet] = im.Data
	}
	// A planet not in order goes last; the sort is stable, so a nil order changes nothing.
	at := func(pl string) int {
		if i := slices.Index(order, pl); i >= 0 {
			return i
		}
		return len(order)
	}
	slices.SortStableFunc(e.planets, func(a, b string) int { return at(a) - at(b) })
	e.bytes = grown
	return p.keepLocked(user, e, name), nil
}

// addTerritories keeps the territories read for pictures of size pixels, as
// add keeps pictures, and returns the preview's token. Territories of
// settings other than the user's current preview are not kept, and the
// token is blank.
func (p *previewStore) addTerritories(user int64, key previewKey, name, seed string, size int, res territory.Result) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.otherLocked(user, key) {
		return "", nil
	}
	e := p.joinLocked(user, key, seed)
	grown := e.bytes - territoryBytes(e.territories) + territoryBytes(&res)
	if grown > maxPreviewBytes {
		return "", errors.New("the map preview is too large")
	}
	e.territories, e.size, e.bytes = &res, size, grown
	return p.keepLocked(user, e, name), nil
}

// territoriesOf returns the territories the user's preview holds for these
// settings, and keeps the preview another 15 minutes.
func (p *previewStore) territoriesOf(user int64, key previewKey) (territory.Result, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	e, ok := p.byUser[user]
	if !ok || e.key.all != key.all || e.territories == nil {
		return territory.Result{}, false
	}
	e.expires = p.now().Add(previewTTL)
	return *e.territories, true
}

// territoryBytes is about what a territory result holds in memory: two
// ints a chunk, and a string and a slice header a territory.
func territoryBytes(r *territory.Result) int {
	if r == nil {
		return 0
	}
	n := 0
	for _, t := range r.Territories {
		n += 40 + 16*len(t.Chunks)
	}
	return n
}

// otherLocked is true when the user's current preview is of other settings:
// the person moved on while a run went, so its result must not replace the
// newer preview.
func (p *previewStore) otherLocked(user int64, key previewKey) bool {
	p.sweepLocked()
	e, ok := p.byUser[user]
	return ok && e.key.all != key.all
}

// joinLocked returns the entry new results of these settings go into: a
// copy of the user's current one when its settings are the same, else a
// new one that keeps the pictures, counts and territories of each planet
// whose share of the settings is the same (SPEC 5.6).
func (p *previewStore) joinLocked(user int64, key previewKey, seed string) *previewEntry {
	p.sweepLocked()
	e, ok := p.byUser[user]
	if !ok {
		return &previewEntry{token: newToken(), key: key, seed: seed, images: map[string][]byte{}}
	}
	if e.key.all != key.all {
		n := &previewEntry{token: newToken(), key: key, seed: seed, images: map[string][]byte{}}
		same := func(pl string) bool { return e.key.planet(pl) == key.planet(pl) }
		for _, pl := range e.planets {
			if same(pl) {
				n.planets = append(n.planets, pl)
				n.images[pl] = e.images[pl]
				n.bytes += len(e.images[pl])
			}
		}
		for pl, r := range e.resources {
			if same(pl) {
				if n.resources == nil {
					n.resources = map[string]*resource.Result{}
				}
				n.resources[pl] = r
				n.bytes += resourceBytes(r)
			}
		}
		if e.territories != nil && same(territoryPlanet) {
			n.territories, n.size = e.territories, e.size
			n.bytes += territoryBytes(e.territories)
		}
		return n
	}
	// entry() hands copies of the stored entry to handlers that read the
	// map and slice after the lock is gone, so they are never written in
	// place. The picture bytes, territories and counts are shared; they never change.
	c := *e
	c.images, c.planets, c.resources = maps.Clone(e.images), slices.Clone(e.planets), maps.Clone(e.resources)
	return &c
}

// keepLocked makes e the user's preview, the newest, and drops other users'
// previews, oldest first, to keep the limits. It returns e's token.
func (p *previewStore) keepLocked(user int64, e *previewEntry, name string) string {
	e.name = name
	e.expires = p.now().Add(previewTTL)
	p.dropLocked(user)
	for len(p.order) > 0 && (len(p.order) >= maxPreviews || p.totalLocked()+e.bytes > maxPreviewBytes) {
		p.dropLocked(p.order[0])
	}
	p.byUser[user] = e
	p.order = append(p.order, user)
	return e.token
}

// entry returns a copy of the user's preview under token, if it has not
// expired.
func (p *previewStore) entry(token string, user int64) (previewEntry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	e, ok := p.byUser[user]
	if !ok || e.token != token {
		return previewEntry{}, false
	}
	return *e, true
}

// get returns a planet's picture when the token is the user's and has not
// expired.
func (p *previewStore) get(token string, user int64, planet string) ([]byte, bool) {
	e, ok := p.entry(token, user)
	if !ok {
		return nil, false
	}
	png, ok := e.images[planet]
	return png, ok
}

func (p *previewStore) sweepLocked() {
	now := p.now()
	for _, u := range slices.Clone(p.order) {
		if !now.Before(p.byUser[u].expires) {
			p.dropLocked(u)
		}
	}
}

func (p *previewStore) dropLocked(user int64) {
	delete(p.byUser, user)
	p.order = slices.DeleteFunc(p.order, func(u int64) bool { return u == user })
}

func (p *previewStore) totalLocked() int {
	n := 0
	for _, e := range p.byUser {
		n += e.bytes
	}
	return n
}

// PreviewView is the Map preview section. Error holds why no pictures
// were made.
type PreviewView struct {
	Token string
	Seed  string
	// Size is the side of each picture in tiles.
	Size     int
	Pictures []PreviewPicture
	// Planets are every planet the world will have, for the planet choice
	// shown when only one of them was drawn (DESIGN 8.8).
	Planets []PreviewPlanet
	// ShowsAll is true when every planet was asked for, so a failed draw
	// still shows All planets as the choice made (DESIGN 8.8).
	ShowsAll bool
	Error    i18n.Msg
	// Drawn are the planets the zip holds, in the order drawn, and Counted
	// those of them whose resource amounts are held, so the page knows which
	// to count before a download with amounts (SPEC 5.6).
	Drawn   []string
	Counted []string
	// TerritoriesHeld is true when the preview holds Vulcanus's territories.
	TerritoriesHeld bool
	// Territories is the box that shows Vulcanus's demolisher territories.
	Territories TerritoryBox
	// Resources is one planet's resource amounts for the large view, or why
	// there are none; set only in the answer to its own request.
	Resources *ResourceView
	// OfWorld is true for a preview of a world's own mods, which can make
	// terrain by script the fast preview cannot draw (SPEC 5.6).
	OfWorld bool
	// Fallback is true when the world's planet list could not be read, so
	// the preview shows the game's own planets (SPEC 5.6).
	Fallback bool
	// OneColor names the pictures that are almost all one color: what is
	// near the start lies outside the map (SPEC 5.6).
	OneColor []MapGenName
}

// LimitTip is the toggletip on what the fast preview cannot show.
func (PreviewView) LimitTip() Toggletip {
	return Toggletip{ID: "preview-limit", About: i18n.M("mapgen.preview.limit_about"), Text: i18n.M("mapgen.preview.limit_tip")}
}

// PreviewPlanet is one planet the preview can draw.
type PreviewPlanet struct {
	Planet string
	Name   i18n.Msg
	// Foreign marks a mod's own label for the planet (SPEC 14.6).
	Foreign bool
}

// PreviewPicture is one planet's picture.
type PreviewPicture struct {
	Planet  string
	Name    i18n.Msg
	Foreign bool
	Path    string
}

// Choice is true when the world has several planets, so the section offers
// one planet or all of them (DESIGN 8.8).
func (v PreviewView) Choice() bool { return len(v.Planets) > 1 }

// All is true when several pictures are shown, or were asked for.
func (v PreviewView) All() bool { return v.ShowsAll || len(v.Pictures) > 1 }

// ZipPlanets and ZipCounted are Drawn and Counted for a data attribute.
func (v PreviewView) ZipPlanets() string { return strings.Join(v.Drawn, " ") }
func (v PreviewView) ZipCounted() string { return strings.Join(v.Counted, " ") }

// DownloadPath is where the picture is served as a file.
func (p PreviewPicture) DownloadPath() string { return p.Path + "/download" }

// ZipPath is where all the drawn planets are served as one zip, Vulcanus
// with its territories while they are shown.
func (v PreviewView) ZipPath() string {
	p := PathGeneratePreview + "/" + v.Token + "/" + zipName
	if v.Territories.Drawn() {
		p += "?" + territoriesQuery + "=1"
	}
	return p
}

func (s *server) previewRoutes() {
	s.handleUpload("POST "+PathGeneratePreview, fixedLimit(generateFormLimit), s.requireUser(s.handleGeneratePreview))
	s.handleUpload("POST "+pathTerritories, fixedLimit(generateFormLimit), s.requireUser(s.handleGenerateTerritories))
	s.handleUpload("POST "+pathResources, fixedLimit(generateFormLimit), s.requireUser(s.handleGenerateResources))
	s.mux.HandleFunc("GET "+pathPicture, s.requireUser(s.handlePicture))
	// A zip of five planets is several MB; a slow link needs the idle deadline.
	s.handleLong("GET "+pathPictureDownload, s.requireUser(s.handlePictureDownload))
	s.handleLong("GET "+pathPicturesZip, s.requireUser(s.handlePicturesZip))
}

// handleGeneratePreview makes the map pictures from the settings Generate
// would write, and shows the form again with them under it (SPEC 5.6). A
// planet field draws only that planet. Pictures already drawn from the
// same settings are not drawn again.
func (s *server) handleGeneratePreview(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.previewer == nil {
		s.notFound(w, r, u)
		return
	}
	if !s.readForm(w, r) {
		return
	}
	auto := r.PostFormValue(previewAutoField) == "1"
	in, handled := s.readPreview(w, r, u)
	if handled {
		return
	}
	f, ok := in.f, in.ok
	if !ok {
		if auto {
			s.fragment(w, r, s.generatePage(r.Context(), f, nil), "generate-preview-invalid")
			return
		}
		// htmx swaps only a 2xx answer, and then the whole form, with its errors.
		status := http.StatusBadRequest
		if isHTMX(r) && in.world {
			// Step 3's form is not Generate's, so only the preview slot
			// changes, with the problems linked to their fields.
			s.fragment(w, r, s.generatePage(r.Context(), f, nil), "generate-preview-invalid")
			return
		}
		if isHTMX(r) {
			status = http.StatusOK
			h := w.Header()
			h.Set("HX-Retarget", "#generate-form")
			h.Set("HX-Reselect", "#generate-form")
			h.Set("HX-Reswap", "outerHTML")
		}
		if f.Reset {
			status = http.StatusOK
		}
		s.page(w, r, status, u.Username, s.generatePage(r.Context(), f, nil))
		return
	}
	all := f.cat.PlanetNames()
	planets := all
	switch p := r.PostFormValue(previewPlanetField); {
	case p != "" && slices.Contains(all, p):
		planets = []string{p}
	case auto && r.PostFormValue(previewViewField) == previewViewAll:
		// "All planets" is chosen, so every planet is drawn (DESIGN 8.8).
	case auto && len(all) > 0:
		// An automatic preview draws only the planet viewed: the first, unless
		// one was picked that these settings still have (DESIGN 8.8).
		planets = all[:1]
	case p != "":
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	req := in.req
	view := &PreviewView{Seed: strconv.FormatUint(uint64(req.Seed), 10), Size: f.PreviewSize, ShowsAll: len(planets) > 1,
		OfWorld: in.world, Fallback: in.fallback}
	for _, planet := range all {
		n := catPlanetName(f.cat, planet)
		view.Planets = append(view.Planets, PreviewPlanet{Planet: planet, Name: n.Name, Foreign: n.Foreign})
	}
	key := in.key
	view.Token, req.Planets = s.previews.missing(u.ID, key, f.Name, planets)
	var err error
	if view.Token == "" {
		view.Token, err = s.drawPreview(w, r, u, req, key, all, f.Name, view.Seed)
	}
	if err == nil {
		counted := s.previews.countedPlanets(u.ID, key)
		if e, ok := s.previews.entry(view.Token, u.ID); ok {
			view.Drawn = e.planets
		}
		for _, planet := range view.Drawn {
			if counted[planet] {
				view.Counted = append(view.Counted, planet)
			}
		}
		for _, planet := range planets {
			n := catPlanetName(f.cat, planet)
			view.Pictures = append(view.Pictures, PreviewPicture{Planet: planet, Name: n.Name, Foreign: n.Foreign, Path: pictureURL(view.Token, planet)})
			if b, ok := s.previews.get(view.Token, u.ID, planet); ok && oneColor(b) {
				view.OneColor = append(view.OneColor, n)
			}
		}
	} else {
		view.Token = ""
		view.Error = s.previewFailure(r, req.GameVersion, err)
	}
	// Without script the territories are read here, after the pictures; with
	// it the page asks for them once the picture has arrived (DESIGN 8.8).
	script := auto || isHTMX(r)
	view.Territories = s.territoryBox(w, r, u, req, key, f.Name, view, !script)
	if view.Token != "" {
		_, view.TerritoriesHeld = s.previews.territoriesOf(u.ID, key)
	}
	if auto {
		// The page keeps the form as typed and swaps in the section alone.
		s.fragment(w, r, s.generatePage(r.Context(), f, view), "generate-preview")
		return
	}
	s.page(w, r, http.StatusOK, u.Username, s.generatePage(r.Context(), f, view))
}

// previewRequest is the preview of the settings Generate would write.
func previewRequest(f GenerateForm, nw world.NewWorld) game.PreviewRequest {
	return game.PreviewRequest{
		GameVersion: nw.GameVersion, SpaceAge: nw.SpaceAge, Preset: nw.Preset, Seed: *nw.Seed,
		MapGenSettings: nw.MapGenSettings, MapSettings: nw.MapSettings, Size: f.PreviewSize,
	}
}

// previewWorldField names the world whose mods a preview loads (SPEC 5.6,
// ADR 0033). Without it the preview is of the base game and Space Age alone.
const previewWorldField = "world"

// previewInput is a posted preview form read: the form to show again, the
// request it makes, and its key in the store. ok is as readGenerate says.
type previewInput struct {
	f   GenerateForm
	req game.PreviewRequest
	key previewKey
	ok  bool
	// world is true for a world's own preview (step 3), and fallback when
	// its planet list could not be read.
	world, fallback bool
}

// worldCatalogue is a world's planets and controls, from its mod-settings
// run (SPEC 5.6); tests replace it.
var worldCatalogue = func(s *server, ctx context.Context, id string, loc i18n.Locale) (mapgen.Catalogue, error) {
	if s.game == nil {
		return mapgen.Catalogue{}, errNoGame
	}
	return s.game.WorldCatalogue(ctx, id, loc.String())
}

// errNoGame means this mode runs no games, so it has no world's planets.
var errNoGame = errors.New("no game controller")

// previewModsKey is what of a world's mods changes its map: the mods, their
// files and their setting values (SPEC 5.6, ADR 0033). ModSettingsKey adds
// what is inside folder mods; ModsFingerprint adds mod-settings.dat.
func (s *server) previewModsKey(id string) (string, error) {
	defs, err := s.worlds.ModSettingsKey(id)
	if err != nil {
		return "", err
	}
	files, err := s.worlds.ModsFingerprint(id)
	if err != nil {
		return "", err
	}
	return hashJSON([]string{defs, files}), nil
}

// readPreview reads a posted preview form. With the world field the
// settings are laid out from that world's own catalogue, and the planets
// the run may draw come from it alone, never from the form (SPEC 5.6, 14.3,
// 14.6). handled is true when it answered the request itself.
func (s *server) readPreview(w http.ResponseWriter, r *http.Request, u store.User) (in previewInput, handled bool) {
	id := r.PostFormValue(previewWorldField)
	if id == "" {
		f, nw, ok := s.readGenerate(r, kindPreview)
		in = previewInput{f: f, ok: ok}
		if ok {
			in.req = previewRequest(f, nw)
		}
		in.key = settingsKey(in.req, f.cat, "")
		return in, false
	}
	ctx := r.Context()
	sum, err := s.worlds.Get(ctx, id)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.notFound(w, r, u)
		} else {
			s.fail(w, r, "read the world for a map preview", err)
		}
		return in, true
	}
	line, lineOK := mapgen.Line(sum.GameVersion)
	if !lineOK {
		// The world is there, but its game has no map settings Foreman knows.
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest, Detail: i18n.M("worlds.generate.preview.failed")})
		return in, true
	}
	cat, catErr := worldCatalogue(s, ctx, id, localeOf(r))
	if catErr != nil {
		// The built-in planets come back with the error, and are used (SPEC 5.6).
		s.log.Warn("read the world's planets for a map preview", "world", id, "error", catErr)
	}
	mods, err := s.previewModsKey(id)
	if err != nil {
		s.fail(w, r, "read the world's mods for a map preview", err)
		return in, true
	}
	if catErr != nil {
		// Pictures from the built-in planets never stand for the world's own
		// once its planet run works.
		mods += " fallback"
	}
	// The catalogue says whether the world's mods enable Space Age (SPEC 5.6).
	spaceAge := cat.SpaceAge && lineHasSpaceAge(line)
	form := r.PostForm
	mk := mapKey(line, spaceAge)
	mf, files, ok := readMapForm(form, kindPreview, mapInput{
		Line: line, Cat: cat, MapKey: mk, Version: sum.GameVersion,
		Stale: form.Get(settingsShown) != "" && form.Get("map_key") != mk,
	})
	f := GenerateForm{Name: sum.Name, Typed: sum.GameVersion, Version: sum.GameVersion, SpaceAge: spaceAge, MapForm: mf}
	if f.Reset || f.Refilled {
		f.clearErrors()
		ok = false
	}
	in = previewInput{f: f, ok: ok && !f.failed() && files.Seed != nil, world: true, fallback: catErr != nil}
	if in.ok {
		in.req = game.PreviewRequest{
			GameVersion: sum.GameVersion, SpaceAge: spaceAge, Preset: files.Preset, Seed: *files.Seed,
			MapGenSettings: files.MapGenSettings, MapSettings: files.MapSettings, Size: f.PreviewSize,
			World: id, KnownPlanets: cat.PlanetNames(),
		}
	}
	in.key = settingsKey(in.req, cat, mods)
	return in, false
}

// drawPreview runs the preview for req.Planets and keeps the pictures, in
// the order of order, the request's planets. The request gets its own write
// deadline, as the run may outlast the server's.
func (s *server) drawPreview(w http.ResponseWriter, r *http.Request, u store.User, req game.PreviewRequest, key previewKey, order []string, name, seed string) (string, error) {
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(previewDeadline)); err != nil {
		s.log.Warn("set the map preview's write deadline", "error", err)
	}
	images, err := s.previewer.Preview(r.Context(), req)
	if err != nil {
		return "", err
	}
	token, err := s.previews.add(u.ID, key, name, seed, images, order)
	if err != nil {
		s.log.Error("keep map preview", "error", err)
		return "", errPreviewNotKept
	}
	return token, nil
}

// errPreviewNotKept means the pictures were drawn but could not be held.
var errPreviewNotKept = errors.New("the map preview could not be kept")

// previewFailure words why a preview made no pictures, and logs what is
// Foreman's fault.
func (s *server) previewFailure(r *http.Request, version string, err error) i18n.Msg {
	pf, failure := errors.AsType[*game.PreviewError](err)
	switch {
	case errors.Is(err, game.ErrPreviewImageMissing):
		return i18n.M("worlds.generate.preview.no_image", "version", version)
	case errors.Is(err, game.ErrPreviewBusy):
		return i18n.M("worlds.generate.preview.busy")
	case errors.Is(err, game.ErrPreviewTimeout):
		return i18n.M("worlds.generate.preview.timeout")
	case failure:
		s.log.Warn("map preview failed", "version", version, "oom", pf.OOM)
		if pf.OOM {
			return i18n.M("worlds.generate.preview.oom")
		}
	case errors.Is(err, errPreviewNotKept):
	case errors.Is(err, game.ErrControlModTaken):
		// A world mod named like Foreman's helper: the same on every try
		// until that mod goes (SPEC 5.6, ADR 0033).
		s.log.Warn("map preview refused", "version", version, "error", err)
		return i18n.M("mapgen.preview.control_mod_taken")
	case errors.Is(err, game.ErrPreviewInvalid):
		// A request the world's game does not take (SPEC 5.6, ADR 0033).
		s.log.Warn("map preview refused", "version", version, "error", err)
	default:
		if r.Context().Err() == nil {
			s.log.Error("map preview", "version", version, "error", err)
		}
	}
	return i18n.M("worlds.generate.preview.failed")
}

// pictureURL is where a planet's picture is served.
func pictureURL(token, planet string) string {
	return PathGeneratePreview + "/" + token + "/" + planet
}

// handlePicture serves one planet's preview picture to the user who asked
// for it. Nothing else is read through this route.
func (s *server) handlePicture(w http.ResponseWriter, r *http.Request, u store.User) {
	png, ok := s.previews.get(r.PathValue("token"), u.ID, r.PathValue("planet"))
	if !ok {
		s.notFound(w, r, u)
		return
	}
	writePNG(w, png)
}

// handlePictureDownload serves one planet's picture as a file named after
// the world, seed and planet (SPEC 5.6).
func (s *server) handlePictureDownload(w http.ResponseWriter, r *http.Request, u store.User) {
	planet := r.PathValue("planet")
	e, ok := s.previews.entry(r.PathValue("token"), u.ID)
	png, have := e.images[planet]
	if !ok || !have {
		s.notFound(w, r, u)
		return
	}
	png, suffix, err := drawnDownload(r, e, planet, png)
	switch {
	case errors.Is(err, errNotCounted):
		s.notCounted(w, r, u)
		return
	case err != nil:
		s.drawFailure(w, r, u, err)
		return
	}
	w.Header().Set("Content-Disposition", attachment(downloadStem(e.name, e.seed, planet)+suffix+".png"))
	writePNG(w, png)
}

// handlePicturesZip serves every planet drawn so far as one zip, each
// picture named as its own download would be. withoutQuery leaves planets
// out, so a zip with amounts can be had without a planet whose count failed
// (SPEC 5.6).
func (s *server) handlePicturesZip(w http.ResponseWriter, r *http.Request, u store.User) {
	e, ok := s.previews.entry(r.PathValue("token"), u.ID)
	if !ok {
		s.notFound(w, r, u)
		return
	}
	without := r.URL.Query()[withoutQuery]
	for _, planet := range without {
		if !slices.Contains(e.planets, planet) {
			s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
			return
		}
	}
	planets := slices.DeleteFunc(slices.Clone(e.planets), func(p string) bool { return slices.Contains(without, p) })
	if len(planets) == 0 {
		s.notFound(w, r, u)
		return
	}
	// Drawn before the reply starts, so a failure can still say so.
	images := maps.Clone(e.images)
	suffixes := map[string]string{}
	for _, planet := range planets {
		drawn, suffix, err := drawnDownload(r, e, planet, images[planet])
		switch {
		case errors.Is(err, errNotCounted):
			s.notCounted(w, r, u)
			return
		case err != nil:
			s.drawFailure(w, r, u, err)
			return
		}
		images[planet], suffixes[planet] = drawn, suffix
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", attachment(downloadStem(e.name, e.seed, "")+".zip"))
	zw := zip.NewWriter(w)
	for _, planet := range planets {
		// A PNG is compressed already.
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: downloadStem(e.name, e.seed, planet) + suffixes[planet] + ".png", Method: zip.Store})
		if err != nil {
			return
		}
		if _, err := fw.Write(images[planet]); err != nil {
			return
		}
	}
	if err := zw.Close(); err != nil && !clientGone(r, err) {
		s.log.Warn("finish the preview pictures zip", "path", r.URL.Path, "error", err)
	}
}

// drawnDownload is a planet's picture src with what the download asks for
// drawn in (SPEC 5.6): Vulcanus's territories (territoriesQuery), and the
// planet's resource amounts (amountsQuery). A download never counts, and
// never comes out plain when amounts or territories were asked for: a
// planet without them held gives errNotCounted. suffix names what was
// drawn, for the file's name.
func drawnDownload(r *http.Request, e previewEntry, planet string, src []byte) (out []byte, suffix string, err error) {
	q := r.URL.Query()
	var ov territory.Overlay
	if q.Get(territoriesQuery) == "1" && planet == territoryPlanet {
		if e.territories == nil {
			return nil, "", errNotCounted
		}
		ov, suffix = territory.Geometry(*e.territories, e.size), "-territories"
	}
	loc := localeOf(r)
	if q.Get(amountsQuery) == "1" {
		if e.resources[planet] == nil {
			return nil, "", errNotCounted
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(src))
		if err != nil {
			return nil, "", err
		}
		if ov.Size == 0 {
			ov.Size = cfg.Width
		} else if ov.Size != cfg.Width {
			return nil, "", fmt.Errorf("the picture is %d wide, its territories %d", cfg.Width, ov.Size)
		}
		ov.Captions, suffix = amountCaptions(loc, *e.resources[planet], cfg.Width), suffix+"-amounts"
	}
	if suffix == "" {
		return src, "", nil
	}
	out, err = drawPNG(r.Context(), src, ov, func(size string) string { return territory.Label(loc, size) })
	return out, suffix, err
}

// withoutQuery names a planet the zip leaves out; it may be given more than once.
const withoutQuery = "without"

// errNotCounted means a download asked for amounts the preview does not hold.
var errNotCounted = errors.New("the planet's resource amounts are not counted")

// notCounted refuses a download with amounts the preview does not hold. The
// page counts them first, so only a stale or hand-made link gets here.
func (s *server) notCounted(w http.ResponseWriter, r *http.Request, u store.User) {
	s.page(w, r, http.StatusConflict, u.Username, ErrorPage{Status: http.StatusConflict, Detail: i18n.M("worlds.generate.resources.not_counted")})
}

func writePNG(w http.ResponseWriter, png []byte) {
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(png)))
	_, _ = w.Write(png)
}

// maxFileStem caps the world name's part of a download name, in characters.
const maxFileStem = 60

// downloadStem is a download's name without its extension: the world's
// name, then a seed (or a word such as "mods" or a save's time) and the
// planet, joined by dashes. The world name loses path, control and quoting
// characters; a blank one is "map". planet may be blank, for a zip or a
// save.
func downloadStem(world, seed, planet string) string {
	var b strings.Builder
	n := 0
	for _, c := range strings.TrimSpace(world) {
		if n == maxFileStem {
			break
		}
		switch {
		case unicode.IsControl(c) || unicode.Is(unicode.Cf, c) || strings.ContainsRune(`/\:*?"<>|;%`, c):
			continue
		case unicode.IsSpace(c):
			c = '_'
		}
		b.WriteRune(c)
		n++
	}
	stem := strings.Trim(b.String(), "._")
	if stem == "" {
		stem = "map"
	}
	parts := []string{stem, seed}
	if planet != "" {
		parts = append(parts, planet)
	}
	return strings.Join(parts, "-")
}

// attachment is a Content-Disposition header for name: an ASCII filename
// for old clients and the full name as RFC 5987 filename*.
func attachment(name string) string {
	return `attachment; filename="` + asciiName(name) + `"; filename*=UTF-8''` + rfc5987(name)
}

// asciiName keeps the letters, digits, dot, dash and underscore of name and
// turns the rest into underscores.
func asciiName(name string) string {
	var b strings.Builder
	for _, c := range name {
		if c < utf8.RuneSelf && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-", c)) {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// rfc5987 percent-encodes every byte of s that is not an attr-char.
func rfc5987(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// oneColorShare is how much of a picture one color must cover for the
// note that the start is outside the map (SPEC 5.6).
const oneColorShare = 0.99

// oneColor reports whether a preview picture is almost all one color. It
// samples a grid of pixels; a picture it cannot read is not.
func oneColor(b []byte) bool {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return false
	}
	bounds := img.Bounds()
	step := max(1, min(bounds.Dx(), bounds.Dy())/128)
	counts := map[color.RGBA64]int{}
	total, most := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			r, g, bl, a := img.At(x, y).RGBA()
			c := color.RGBA64{uint16(r), uint16(g), uint16(bl), uint16(a)} //nolint:gosec // RGBA returns 16-bit values
			counts[c]++
			most = max(most, counts[c])
			total++
		}
	}
	return total > 0 && float64(most) >= oneColorShare*float64(total)
}
