package web

import (
	"cmp"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/resource"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/territory"
)

// The resource amounts of one planet's map (SPEC 5.6): counted in a headless
// game on request from the large view or the One planet picture, kept with
// the pictures, shown as labels over the map with the same amounts as text,
// and drawn into a download from what is kept, never counted for it.

// pathResources answers the page's request for the resource amounts of one
// planet of the settings in the form, after its picture has arrived.
const pathResources = PathGeneratePreview + "/resources"

// amountsQuery on a download draws in the amounts held for each planet.
const amountsQuery = "amounts"

// Label kinds: an ore's amount, an infinite resource's yield, or a fluid
// spot's tiles.
const (
	resourceKindAmount = "amount"
	resourceKindYield  = "yield"
	resourceKindTiles  = "tiles"
)

// ResourceView is the amounts of one planet's picture, or why there are
// none. Labels are in the server's order, largest patch box first, which the
// page places in; Lists are the same patches as text, largest first.
type ResourceView struct {
	Labels  []ResourceLabel
	Lists   []ResourceList
	Patches int
	Error   i18n.Msg
	// Retry is true when asking again may work, as after a busy or failed run.
	Retry bool
	// Kept is true when the preview holds the count, so a download can draw
	// it; a count of settings the person has moved on from is shown, not kept.
	Kept bool
	// None is what the page says when there are no patches.
	None i18n.Msg
}

// ResourceLabel is one patch's label, centred on Left and Top percent of the
// picture. Value is what the page formats: the amount for an ore, the yield
// as a fraction (3 is 300%) for an infinite resource, the tile count for a
// spot. Text is a spot's label already worded, since its plural is the
// catalogue's.
type ResourceLabel struct {
	Left, Top float64
	Value     string
	Kind      string
	Text      string
}

// ResourceList is one resource's patches in words: its name and total, then
// each patch's amount and place; or one fluid's spots, its name and how many,
// then each spot's tiles and place.
type ResourceList struct {
	Total   string
	Patches []string
}

// resourceView turns the amounts counted on planet for a picture of size
// tiles into what the large view shows, worded in loc.
func resourceView(loc i18n.Locale, cat mapgen.Catalogue, planet string, res resource.Result, size int) *ResourceView {
	v := &ResourceView{Patches: len(res.Patches), Labels: resourceLabels(loc, res, size), None: i18n.M("worlds.generate.resources.none")}
	byName, spots := map[string][]resource.Patch{}, map[string][]resource.Patch{}
	for _, p := range res.Patches {
		if p.Fluid {
			spots[p.Name] = append(spots[p.Name], p)
			continue
		}
		byName[p.Name] = append(byName[p.Name], p)
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	// Largest total amount first, then by name.
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(res.Resources[b].Total, res.Resources[a].Total), cmp.Compare(a, b))
	})
	for _, n := range names {
		ps := byName[n]
		slices.SortStableFunc(ps, func(a, b resource.Patch) int { return cmp.Compare(b.Amount, a.Amount) })
		r := res.Resources[n]
		label := resourceName(loc, cat, planet, n)
		list := ResourceList{Total: loc.T("worlds.generate.resources.total", "resource", label, "amount", r.Total)}
		// The total is a yield by the same rule as a patch's.
		if y, ok := totalYield(r); ok {
			list.Total = loc.T("worlds.generate.resources.total_yield", "resource", label, "percent", wholePercent(y))
		}
		for _, p := range ps {
			x, y := strconv.Itoa(p.X), strconv.Itoa(p.Y)
			if pct, ok := resource.Yield(p); ok {
				list.Patches = append(list.Patches, loc.T("worlds.generate.resources.patch_yield", "percent", wholePercent(pct), "x", x, "y", y))
				continue
			}
			list.Patches = append(list.Patches, loc.T("worlds.generate.resources.patch", "amount", p.Amount, "x", x, "y", y))
		}
		v.Lists = append(v.Lists, list)
	}
	v.Lists = append(v.Lists, spotLists(loc, cat, res, spots)...)
	return v
}

// spotLists are the lists of each fluid's spots, after the ores: most tiles
// first, then by name, and each fluid's spots largest first.
func spotLists(loc i18n.Locale, cat mapgen.Catalogue, res resource.Result, spots map[string][]resource.Patch) []ResourceList {
	tiles := map[string]int{}
	fluids := make([]string, 0, len(spots))
	for n, ps := range spots {
		fluids = append(fluids, n)
		for _, p := range ps {
			tiles[n] += p.Entities
		}
	}
	slices.SortFunc(fluids, func(a, b string) int { return cmp.Or(cmp.Compare(tiles[b], tiles[a]), cmp.Compare(a, b)) })
	var lists []ResourceList
	for _, n := range fluids {
		ps := spots[n]
		slices.SortStableFunc(ps, func(a, b resource.Patch) int { return cmp.Compare(b.Entities, a.Entities) })
		list := ResourceList{Total: loc.T("worlds.generate.resources.spots", "resource", fluidName(loc, cat, n, res.Fluids[n]), "count", len(ps))}
		for _, p := range ps {
			list.Patches = append(list.Patches, loc.T("worlds.generate.resources.spot", "count", p.Entities, "x", strconv.Itoa(p.X), "y", strconv.Itoa(p.Y)))
		}
		lists = append(lists, list)
	}
	return lists
}

// resourceLabels are the labels of the amounts counted for a picture of size
// tiles, in the server's order, a spot's worded in loc.
func resourceLabels(loc i18n.Locale, res resource.Result, size int) []ResourceLabel {
	half := float64(size / 2)
	// The middle of a tile, as a percent of the picture, which spans [-size/2, size/2).
	pct := func(t int) float64 { return math.Round((float64(t)+half+0.5)/float64(size)*1e5) / 1e3 }
	var out []ResourceLabel
	hidden := resource.HiddenLabels(res.Patches)
	for i, p := range res.Patches {
		if hidden[i] {
			continue
		}
		l := ResourceLabel{Left: pct(p.X), Top: pct(p.Y), Kind: resourceKindAmount, Value: strconv.FormatInt(p.Amount, 10)}
		if p.Fluid {
			l.Kind, l.Value = resourceKindTiles, strconv.Itoa(p.Entities)
			l.Text = loc.T("worlds.generate.resources.spot_label", "count", p.Entities)
		} else if y, ok := resource.Yield(p); ok {
			l.Kind, l.Value = resourceKindYield, strconv.FormatFloat(y/100, 'f', -1, 64)
		}
		out = append(out, l)
	}
	return out
}

// amountCaptions are the labels a download draws from amounts already
// counted, for a picture of size pixels, worded in loc as the page words them.
func amountCaptions(loc i18n.Locale, res resource.Result, size int) []territory.Caption {
	var caps []territory.Caption
	for _, l := range resourceLabels(loc, res, size) {
		text := l.Text
		if l.Kind != resourceKindTiles {
			text = labelText(loc, l)
		}
		caps = append(caps, territory.Caption{Text: text, U: l.Left / 100, V: l.Top / 100})
	}
	return caps
}

// totalYield is an infinite resource's total as a yield, by the rule
// resource.Yield gives a patch; ok is false for an ore.
func totalYield(r resource.Resource) (percent float64, ok bool) {
	if !r.Infinite {
		return 0, false
	}
	return resource.Yield(resource.Patch{Amount: r.Total, Normal: r.Normal})
}

// wholePercent is a yield, as resource.Yield gives it, in whole percent.
func wholePercent(y float64) int64 { return int64(math.Round(y)) }

// resourceName is the name of the planet's map generator control that sets
// the resource, as the settings show it; a resource with none shows the
// name the catalogue resolved for it (a world's mods name their own, SPEC
// 5.6, 14.6), else its game name. cat is the request's planets and controls.
func resourceName(loc i18n.Locale, cat mapgen.Catalogue, planet, entity string) string {
	for _, c := range cat.PlanetControls(planet) {
		if c.Category != mapgen.CategoryResource {
			continue
		}
		if resource.EntityName(planet, c.Name) == entity || slices.ContainsFunc(cat.Resources, func(r mapgen.Resource) bool {
			return r.Name == entity && r.Control == c.Name
		}) {
			return controlLabel(loc, cat, c.Name)
		}
	}
	for _, r := range cat.Resources {
		if r.Name == entity && r.Label != "" {
			return r.Label
		}
	}
	return entity
}

// controlLabel is a map generator control's name as the settings show it.
func controlLabel(loc i18n.Locale, cat mapgen.Catalogue, control string) string {
	if _, builtin := controlNames[control]; !builtin {
		if l := cat.Controls[control].Label; l != "" {
			return l
		}
	}
	return loc.Format(controlMsg(control))
}

// fluidName is a fluid's name in the list: the name of the first control
// its tiles are tied to that the catalogue knows, as the settings show it
// (Vesta's methane is "Methane gas"), else the fluid's game name. The
// catalogue holds no fluid names.
func fluidName(loc i18n.Locale, cat mapgen.Catalogue, name string, f resource.Fluid) string {
	for _, c := range f.Controls {
		if _, ok := cat.Controls[c]; ok {
			return controlLabel(loc, cat, c)
		}
	}
	return name
}

// resources returns the amounts of planet in req: from the user's preview
// when they were counted already, else counted now.
// cat is req's planets and controls and key its key in the store.
func (s *server) resources(w http.ResponseWriter, r *http.Request, u store.User, req game.PreviewRequest, cat mapgen.Catalogue, key previewKey, name, planet string) *ResourceView {
	if res, ok := s.previews.resourcesOf(u.ID, key, planet); ok {
		v := countedView(localeOf(r), cat, req, planet, res)
		v.Kept = true
		return v
	}
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(previewDeadline)); err != nil {
		s.log.Warn("set the resource count's write deadline", "error", err)
	}
	res, err := s.previewer.Resources(r.Context(), req, planet)
	if err != nil {
		// A missing image, or a count the game wrote wrongly, is the same on a second try.
		retry := !errors.Is(err, game.ErrPreviewImageMissing) && !errors.Is(err, resource.ErrInvalid)
		return &ResourceView{Error: s.controlModFailure(r, req.GameVersion, err, "resource count", resourceMsgs), Retry: retry}
	}
	token, err := s.previews.addResources(u.ID, key, name, strconv.FormatUint(uint64(req.Seed), 10), planet, res)
	if err != nil {
		s.log.Error("keep resource amounts", "error", err)
		return &ResourceView{Error: i18n.M("worlds.generate.resources.failed"), Retry: true}
	}
	v := countedView(localeOf(r), cat, req, planet, res)
	v.Kept = token != ""
	return v
}

// countedView is resourceView for a count of req. A planet whose modded
// resource controls are on but whose count found no patches may have
// resources the count cannot see, such as tiles, and says so (SPEC 5.6);
// a control whose fluid tiles the count looked for is not one of them.
func countedView(loc i18n.Locale, cat mapgen.Catalogue, req game.PreviewRequest, planet string, res resource.Result) *ResourceView {
	v := resourceView(loc, cat, planet, res, req.Size)
	counted := map[string]bool{}
	for _, f := range res.Fluids {
		for _, c := range f.Controls {
			counted[c] = true
		}
	}
	if len(res.Patches) == 0 && hasResourceControlOn(cat, req.MapGenSettings, planet, counted) {
		v.None = i18n.M("worlds.generate.resources.not_patches")
	}
	return v
}

// hasResourceControlOn is whether planet has a resource control from a mod,
// other than the counted ones, that the map generator settings do not switch
// off with a size of zero. The game's own resources are all patches.
func hasResourceControlOn(cat mapgen.Catalogue, mapGen []byte, planet string, counted map[string]bool) bool {
	var settings struct {
		Controls map[string]struct {
			Size *float64 `json:"size"`
		} `json:"autoplace_controls"`
	}
	// Settings that are not JSON switch nothing off.
	_ = json.Unmarshal(mapGen, &settings)
	for _, c := range cat.PlanetControls(planet) {
		if _, builtin := controlNames[c.Name]; builtin || c.Category != mapgen.CategoryResource || counted[c.Name] {
			continue
		}
		if size := settings.Controls[c.Name].Size; size == nil || *size != 0 {
			return true
		}
	}
	return false
}

// resourceMsgs is how a failed resource count is worded.
var resourceMsgs = failureMsgs{
	busy:    i18n.M("worlds.generate.resources.busy"),
	timeout: i18n.M("worlds.generate.resources.timeout"),
	oom:     i18n.M("worlds.generate.resources.oom"),
	failed:  i18n.M("worlds.generate.resources.failed"),
}

// failureMsgs is how a failed control-mod run (a resource count or a
// territory run) is worded. Whole messages, not a key prefix, so that every
// catalogue key stays a literal the key check can find.
type failureMsgs struct{ busy, timeout, oom, failed i18n.Msg }

// controlModFailure words why a control-mod run, called what in the log,
// gave nothing, and logs what is Foreman's fault. Nothing from the game's
// file is logged (SPEC 14.1).
func (s *server) controlModFailure(r *http.Request, version string, err error, what string, m failureMsgs) i18n.Msg {
	pf, failure := errors.AsType[*game.PreviewError](err)
	switch {
	case errors.Is(err, game.ErrPreviewImageMissing):
		return i18n.M("worlds.generate.preview.no_image", "version", version)
	case errors.Is(err, game.ErrPreviewBusy):
		return m.busy
	case errors.Is(err, game.ErrPreviewTimeout):
		return m.timeout
	case failure:
		s.log.Warn(what+" failed", "version", version, "oom", pf.OOM)
		if pf.OOM {
			return m.oom
		}
	default:
		if r.Context().Err() == nil {
			s.log.Error(what, "version", version, "error", err)
		}
	}
	return m.failed
}

// handleGenerateResources answers the large view's request for the amounts
// of one planet of the settings in the form (SPEC 5.6): the labels and the
// text list, or why there are none, as the "generate-resources" fragment.
func (s *server) handleGenerateResources(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.previewer == nil {
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
	f := in.f
	view := &PreviewView{Size: f.PreviewSize}
	if !in.ok {
		view.Resources = &ResourceView{Error: i18n.M("worlds.generate.resources.invalid")}
		s.fragment(w, r, previewCarrier(in, view), "generate-resources")
		return
	}
	// The planet is one of the world's own, never other text (SPEC 14.1).
	planet := r.PostFormValue(previewPlanetField)
	if !slices.Contains(f.cat.PlanetNames(), planet) {
		s.page(w, r, http.StatusBadRequest, u.Username, ErrorPage{Status: http.StatusBadRequest})
		return
	}
	view.Resources = s.resources(w, r, u, in.req, f.cat, in.key, f.Name, planet)
	s.fragment(w, r, previewCarrier(in, view), "generate-resources")
}

// addResources keeps the amounts counted on planet, as add keeps pictures,
// and returns the preview's token. A count of settings other than the
// user's current preview is kept only when the planet's share of them is
// the same; otherwise it is not kept, and the token is blank.
func (p *previewStore) addResources(user int64, key previewKey, name, seed, planet string, res resource.Result) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.otherLocked(user, key) {
		if p.byUser[user].key.planet(planet) != key.planet(planet) {
			return "", nil
		}
		// The count fits the newer preview too, so it joins that one.
		key = p.byUser[user].key
	}
	e := p.joinLocked(user, key, seed)
	grown := e.bytes - resourceBytes(e.resources[planet]) + resourceBytes(&res)
	if grown > maxPreviewBytes {
		return "", errors.New("the map preview is too large")
	}
	if e.resources == nil {
		e.resources = map[string]*resource.Result{}
	}
	e.resources[planet], e.bytes = &res, grown
	return p.keepLocked(user, e, name), nil
}

// resourcesOf returns the amounts the user's preview holds for planet with
// these settings, or with any whose share for planet is the same, and keeps
// the preview another 15 minutes.
func (p *previewStore) resourcesOf(user int64, key previewKey, planet string) (resource.Result, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	e, ok := p.byUser[user]
	if !ok || e.key.planet(planet) != key.planet(planet) || e.resources[planet] == nil {
		return resource.Result{}, false
	}
	e.expires = p.now().Add(previewTTL)
	return *e.resources[planet], true
}

// countedPlanets are the planets the user's preview holds amounts for with
// these settings, or with any whose share for the planet is the same, so
// their downloads can draw them.
func (p *previewStore) countedPlanets(user int64, key previewKey) map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	e, ok := p.byUser[user]
	if !ok {
		return nil
	}
	out := map[string]bool{}
	for planet, res := range e.resources {
		if res != nil && e.key.planet(planet) == key.planet(planet) {
			out[planet] = true
		}
	}
	return out
}

// resourceBytes is about what a count holds in memory: a patch's numbers
// and name, and a resource's totals and name.
func resourceBytes(r *resource.Result) int {
	if r == nil {
		return 0
	}
	n := len(r.Planet)
	for _, p := range r.Patches {
		n += 96 + len(p.Name)
	}
	for name := range r.Resources {
		n += 64 + len(name)
	}
	for name, f := range r.Fluids {
		n += 64 + len(name)
		for _, c := range f.Controls {
			n += 16 + len(c)
		}
	}
	return n
}
