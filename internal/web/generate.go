package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapexchange"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// Generate's paths. They sit outside /worlds/{id}, so no world id can hide
// them.
const (
	PathGenerate        = "/generate"
	PathGenerateRefresh = "/generate/refresh"
	PathGeneratePreview = "/generate/preview"
	pathPicture         = PathGeneratePreview + "/{token}/{planet}"
)

// AuditGenerate is the audit action of a world made by Generate (SPEC 12.9).
const AuditGenerate = "generate"

// generatedQuery marks a world page that follows Generate, for its Done
// message.
const generatedQuery = "generated"

// generateFormLimit is the body limit of Generate's routes. A map exchange
// string may be mapexchange.MaxInputBytes long, and the form escapes up to
// three bytes for each of its characters (SPEC 14.6).
const generateFormLimit = 4 * mapexchange.MaxInputBytes

// How the map is made.
const (
	modePreset   = "preset"
	modeExchange = "exchange"
	modeCustom   = "custom"
)

// newSeed picks the seed for a blank seed field. Tests replace it.
var newSeed = func() uint32 { return mrand.Uint32() } //nolint:gosec // a map seed is not a secret

// MapPreviewer makes the map preview pictures: one PNG for each planet
// asked for, from the settings Generate would write (SPEC 5.6, 14.3). The
// game controller provides it. Seed is always set, so the world made later
// has the same map.
type MapPreviewer interface {
	Preview(ctx context.Context, req game.PreviewRequest) ([]game.PNG, error)
}

// previewTTL is how long a preview's pictures are kept (SPEC 5.6).
const previewTTL = 15 * time.Minute

// The pictures held at once: 8 previews and 48 MB in all, so a few admins
// cannot fill the memory.
const (
	maxPreviews     = 8
	maxPreviewBytes = 48 << 20
)

type previewItem struct {
	user    int64
	expires time.Time
	images  map[string][]byte
	bytes   int
}

// previewStore holds the pictures of previews under a random token, each
// tied to the user who asked (SPEC 5.6). Nothing else is served from it.
type previewStore struct {
	mu    sync.Mutex
	now   func() time.Time
	items map[string]*previewItem
	order []string // oldest first
}

func newPreviewStore(now func() time.Time) *previewStore {
	return &previewStore{now: now, items: map[string]*previewItem{}}
}

// put keeps the pictures and returns their token. A user keeps one preview
// at a time: a new one replaces the last.
func (p *previewStore) put(user int64, images []game.PNG) (string, error) {
	item := &previewItem{user: user, expires: p.now().Add(previewTTL), images: map[string][]byte{}}
	for _, im := range images {
		item.images[im.Planet] = im.Data
		item.bytes += len(im.Data)
	}
	if item.bytes > maxPreviewBytes {
		return "", errors.New("the preview pictures are too large")
	}
	token := newToken()

	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	for _, t := range slices.Clone(p.order) {
		if p.items[t].user == user {
			p.dropLocked(t)
		}
	}
	for len(p.order) >= maxPreviews || (len(p.order) > 0 && p.totalLocked()+item.bytes > maxPreviewBytes) {
		p.dropLocked(p.order[0])
	}
	p.items[token] = item
	p.order = append(p.order, token)
	return token, nil
}

// get returns a planet's picture when the token is the user's and has not
// expired.
func (p *previewStore) get(token string, user int64, planet string) ([]byte, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	item, ok := p.items[token]
	if !ok || item.user != user {
		return nil, false
	}
	png, ok := item.images[planet]
	return png, ok
}

func (p *previewStore) sweepLocked() {
	now := p.now()
	for _, t := range slices.Clone(p.order) {
		if !now.Before(p.items[t].expires) {
			p.dropLocked(t)
		}
	}
}

func (p *previewStore) dropLocked(token string) {
	delete(p.items, token)
	p.order = slices.DeleteFunc(p.order, func(t string) bool { return t == token })
}

func (p *previewStore) totalLocked() int {
	n := 0
	for _, it := range p.items {
		n += it.bytes
	}
	return n
}

// GeneratePage is the Generate form, with the map preview when one was made
// (SPEC 5.6, DESIGN 8.6, 8.8).
type GeneratePage struct {
	Form GenerateForm
	// CanPreview is true when this Foreman can make map previews.
	CanPreview bool
	Preview    *PreviewView
}

// GenerateForm is the Generate form's state, as typed or as first offered.
type GenerateForm struct {
	Name string
	// Picked is the select's choice; Typed the typed version. Version is
	// the one the world will have, and DefaultVersion the active world's,
	// which the first option of the select names.
	Typed          string
	Version        string
	DefaultVersion string
	SpaceAge       bool
	Download       bool
	CanDownload    bool
	Mode           string
	Preset         string
	Presets        []string
	// Seed is the preset mode's field; FilledSeed carries the random seed
	// a custom or exchange-string preview used, so the world matches it.
	Seed       string
	FilledSeed string
	Exchange   string
	MapGen     MapGenForm
	// Line and MapKey say which game line and Space Age setting the map
	// options were made for.
	Line   string
	MapKey string
	// Reset says the map options were set back because the version or
	// Space Age changed.
	Reset bool

	NameError, VersionError, SpaceAgeError, PresetError, SeedError, ExchangeError, FormError i18n.Msg
	CustomProblems                                                                           []Problem
}

// PreviewView is the Map preview section. Error holds why no pictures
// were made; Log is the game's output when it failed.
type PreviewView struct {
	Token    string
	Seed     string
	Pictures []PreviewPicture
	Error    i18n.Msg
}

// PreviewPicture is one planet's picture.
type PreviewPicture struct {
	Planet string
	Name   i18n.Msg
	Path   string
}

func (GeneratePage) templateName() string { return "generate" }
func (GeneratePage) navItem() string      { return PathWorlds }

func (p GeneratePage) hasErrors() bool { return len(p.Problems()) > 0 }

// Problems lists the form's errors in the order the fields appear.
func (p GeneratePage) Problems() []Problem {
	f := p.Form
	all := []Problem{
		{"generate-name", f.NameError},
		{"generate-version", f.VersionError},
		{"generate-space-age", f.SpaceAgeError},
		{"generate-preset", f.PresetError},
		{"generate-seed", f.SeedError},
		{"exchange-string", f.ExchangeError},
		{"mode-" + f.Mode, f.FormError},
	}
	if f.Mode == modeCustom {
		all = append(all, f.CustomProblems...)
	}
	return problems(all...)
}

// Paths the form uses.
func (GeneratePage) Path() string        { return PathGenerate }
func (GeneratePage) RefreshPath() string { return PathGenerateRefresh }
func (GeneratePage) PreviewPath() string { return PathGeneratePreview }
func (GeneratePage) VersionsPath() string {
	return versionsPath("")
}

// PresetMsg is a preset's name in the game's own words.
func (GenerateForm) PresetMsg(name string) i18n.Msg { return PresetMsg(name) }

func (s *server) generateRoutes() {
	s.mux.HandleFunc("GET "+PathGenerate, s.requireUser(s.handleGenerateForm))
	s.handleUpload("POST "+PathGenerate, generateFormLimit, s.requireUser(s.handleGenerate))
	s.handleUpload("POST "+PathGenerateRefresh, generateFormLimit, s.requireUser(s.handleGenerateRefresh))
	s.handleUpload("POST "+PathGeneratePreview, generateFormLimit, s.requireUser(s.handleGeneratePreview))
	s.mux.HandleFunc("GET "+pathPicture, s.requireUser(s.handlePicture))
}

// activeVersion is the game version of the active world, or "".
func (s *server) activeVersion(ctx context.Context) string {
	list, err := s.worlds.List(ctx)
	if err != nil {
		return ""
	}
	for _, w := range list {
		if w.Active && !w.Unreadable && !w.Newer && world.SupportedGameVersion(w.GameVersion) {
			return w.GameVersion
		}
	}
	return ""
}

func (s *server) generatePage(f GenerateForm, preview *PreviewView) GeneratePage {
	f.CanDownload = s.game != nil
	return GeneratePage{Form: f, CanPreview: s.previewer != nil, Preview: preview}
}

// mapKey names the game line and Space Age setting the map options are for.
func mapKey(line string, spaceAge bool) string {
	if spaceAge {
		return line + "|1"
	}
	return line + "|0"
}

// keyLine is the game line a posted map key names, or "".
func keyLine(key string) string {
	line, _, _ := strings.Cut(key, "|")
	if slices.Contains(mapgen.Lines, line) {
		return line
	}
	return ""
}

// handleGenerateForm shows the empty form: a preset map on the active
// world's game version.
func (s *server) handleGenerateForm(w http.ResponseWriter, r *http.Request, u store.User) {
	version := s.activeVersion(r.Context())
	line, ok := mapgen.Line(version)
	if !ok {
		line = mapgen.Lines[len(mapgen.Lines)-1]
	}
	spaceAge := line != "1.1"
	f := GenerateForm{
		Version: version, DefaultVersion: version, SpaceAge: spaceAge, Download: true,
		Mode: modePreset, Preset: "default", Line: line, Presets: mapgen.Presets(line),
		MapKey: mapKey(line, spaceAge), MapGen: NewMapGenForm(line, spaceAge),
	}
	s.page(w, r, http.StatusOK, u.Username, s.generatePage(f, nil))
}

// handleGenerateRefresh redraws the page for a changed game version or
// Space Age; the browser swaps in only the map options.
func (s *server) handleGenerateRefresh(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	f, _, _ := s.readGenerate(r, kindRefresh)
	f.clearErrors()
	s.page(w, r, http.StatusOK, u.Username, s.generatePage(f, nil))
}

func (f *GenerateForm) clearErrors() {
	f.NameError, f.VersionError, f.SpaceAgeError, f.PresetError = i18n.Msg{}, i18n.Msg{}, i18n.Msg{}, i18n.Msg{}
	f.SeedError, f.ExchangeError, f.FormError, f.CustomProblems = i18n.Msg{}, i18n.Msg{}, i18n.Msg{}, nil
}

func (f GenerateForm) failed() bool {
	return GeneratePage{Form: f}.hasErrors()
}

// handleGenerate makes the world (SPEC 5.6).
func (s *server) handleGenerate(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	f, nw, ok := s.readGenerate(r, kindGenerate)
	if !ok {
		status := http.StatusBadRequest
		if f.Reset {
			status = http.StatusOK
		}
		s.page(w, r, status, u.Username, s.generatePage(f, nil))
		return
	}
	id, err := s.worlds.Generate(r.Context(), nw)
	if err != nil {
		status, msg, ok := s.refusal(w, r, u, "generate world", err)
		if !ok {
			return
		}
		f.place(err, msg)
		s.page(w, r, status, u.Username, s.generatePage(f, nil))
		return
	}
	s.audit(r.Context(), u, AuditGenerate, id, fmt.Sprintf("%s, %s, space age %t", f.Mode, nw.GameVersion, nw.SpaceAge))
	if f.Download {
		s.downloadFor(r.Context(), map[string]string{nw.GameVersion: id}, actor(u))
	}
	http.Redirect(w, r, worldPath(id)+"?"+generatedQuery+"=1", http.StatusSeeOther) //nolint:gosec // id is a world id Foreman made, a local path segment
}

// place puts a refusal's words on the field it is about.
func (f *GenerateForm) place(err error, msg i18n.Msg) {
	switch {
	case errors.Is(err, world.ErrNameEmpty), errors.Is(err, world.ErrNameTaken):
		f.NameError = msg
	case errors.Is(err, world.ErrBadGameVersion):
		f.VersionError = msg
	case errors.Is(err, world.ErrSpaceAgeOld):
		f.SpaceAgeError = msgSpaceAgeOld()
	case errors.Is(err, world.ErrBadPreset):
		f.PresetError = i18n.M("worlds.generate.error.preset")
	default:
		f.FormError = msg
	}
}

// audit records an action that is already done. A failed write is logged:
// the world exists, and undoing it would lose more than the entry.
func (s *server) audit(ctx context.Context, u store.User, action, worldID, detail string) {
	err := s.store.AddAudit(ctx, store.AuditEntry{
		Time: s.now(), UserID: u.ID, Username: u.Username, Action: action, WorldID: worldID, Detail: detail,
	})
	if err != nil {
		s.log.Error("could not record the action in the audit log", "action", action, "world", worldID, "error", err)
	}
}

// What a posted form is read for.
type readKind int

const (
	kindGenerate readKind = iota // make the world
	kindPreview                  // make the map pictures; the name is not needed yet
	kindRefresh                  // redraw the map options for a changed version
)

// readGenerate reads and checks the form. It always returns the form to
// show again. ok is true when the world, or the preview, can be made. When
// the version or Space Age moved on from what the custom map options were
// made for, those options are set back and ok is false with no error
// (Reset); the other ways to make a map do not depend on them.
func (s *server) readGenerate(r *http.Request, kind readKind) (f GenerateForm, nw world.NewWorld, ok bool) {
	ctx := r.Context()
	f = GenerateForm{
		Name:        r.PostFormValue("name"),
		Typed:       chosenVersion(r.PostFormValue("game_version"), r.PostFormValue("game_version_typed")),
		SpaceAge:    r.PostFormValue("space_age") == "on",
		Download:    r.PostFormValue("download") == "on",
		Mode:        r.PostFormValue("mode"),
		Preset:      r.PostFormValue("preset"),
		Seed:        strings.TrimSpace(r.PostFormValue("seed")),
		FilledSeed:  strings.TrimSpace(r.PostFormValue("filled_seed")),
		Exchange:    r.PostFormValue("exchange"),
		CanDownload: s.game != nil,
	}
	if !slices.Contains([]string{modePreset, modeExchange, modeCustom}, f.Mode) {
		f.Mode = modePreset
	}
	f.DefaultVersion = s.activeVersion(ctx)
	f.Version = f.Typed
	if f.Version == "" {
		f.Version = f.DefaultVersion
	}

	// The version decides the game line, so it is read first.
	line, versionOK := "", false
	switch {
	case f.Version == "":
		f.VersionError = i18n.M("worlds.generate.error.version_required")
	case !world.SupportedGameVersion(f.Version):
		f.VersionError = msgBadGameVersion()
	default:
		line, versionOK = mapgen.Line(f.Version)
		if !versionOK {
			f.VersionError = msgBadGameVersion()
		}
	}
	if versionOK && strings.HasPrefix(f.Version, "1.") && f.SpaceAge {
		f.SpaceAgeError = msgSpaceAgeOld()
	}
	if versionOK && f.Typed != "" && f.Typed != f.DefaultVersion && f.SpaceAgeError.IsZero() {
		check := ImportForm{GameVersion: f.Typed}
		s.checkTypedVersion(ctx, &check)
		f.VersionError = check.VersionError
	}
	posted := r.PostFormValue("map_key")
	if !versionOK {
		if line = keyLine(posted); line == "" {
			line = mapgen.Lines[len(mapgen.Lines)-1]
		}
	}
	mapSpaceAge := f.SpaceAge && line != "1.1"
	f.Line, f.MapKey = line, mapKey(line, mapSpaceAge)
	f.Presets = mapgen.Presets(line)
	stale := versionOK && f.SpaceAgeError.IsZero() && posted != f.MapKey
	f.Reset = stale && f.Mode == modeCustom

	if kind == kindGenerate && strings.TrimSpace(f.Name) == "" {
		f.NameError = msgNameEmpty()
	}
	if !slices.Contains(f.Presets, f.Preset) {
		if kind == kindRefresh || f.Preset == "" {
			f.Preset = "default"
		} else {
			f.PresetError = i18n.M("worlds.generate.error.preset")
		}
	}

	// Always read the custom form, so it is shown again whatever the mode.
	var mapGen []byte
	if stale {
		f.MapGen = NewMapGenForm(line, mapSpaceAge)
	} else {
		var probs []Problem
		f.MapGen, mapGen, probs = ParseMapGenForm(r, line, mapSpaceAge)
		if f.Mode == modeCustom {
			f.CustomProblems = probs
		}
	}
	if f.Reset {
		f.clearErrors()
		return f, nw, false
	}
	if !versionOK {
		return f, nw, false
	}

	nw = world.NewWorld{Name: strings.TrimSpace(f.Name), GameVersion: f.Version, SpaceAge: f.SpaceAge}
	var seed *uint32
	switch f.Mode {
	case modePreset:
		seed = s.presetSeed(&f)
		if f.Preset != "default" {
			nw.Preset = f.Preset
		}
	case modeExchange:
		var res mapexchange.Result
		if res, ok = decodeExchange(&f, f.Version); ok {
			nw.MapGenSettings, nw.MapSettings = res.MapGenSettings, res.MapSettings
			seed = s.settingsSeed(&f, res.MapGenSettings)
		}
	case modeCustom:
		if len(f.CustomProblems) == 0 {
			nw.MapGenSettings = mapGen
			// A blank seed is filled in the form, so the world matches the preview.
			v, ok := uint32(0), false
			if n := f.MapGen.SeedNumber(); n != nil {
				v, ok = *n, true
			}
			if !ok {
				v = newSeed()
				f.MapGen = f.MapGen.WithSeed(v)
			}
			seed = &v
		}
	}
	nw.Seed = seed
	if f.failed() || seed == nil {
		return f, nw, false
	}
	return f, nw, true
}

// presetSeed reads the seed field, filling a blank one with a random seed
// that the form then shows.
func (s *server) presetSeed(f *GenerateForm) *uint32 {
	if f.Seed != "" {
		n, err := strconv.ParseUint(f.Seed, 10, 32)
		if err != nil {
			f.SeedError = i18n.M("worlds.generate.error.seed")
			return nil
		}
		v := uint32(n)
		f.Seed = strconv.FormatUint(n, 10)
		return &v
	}
	v := newSeed()
	f.Seed = strconv.FormatUint(uint64(v), 10)
	f.FilledSeed = f.Seed
	return &v
}

// settingsSeed takes the seed from the map-gen settings, else the one filled
// by an earlier preview, else a random one kept in the form.
func (s *server) settingsSeed(f *GenerateForm, mapGen []byte) *uint32 {
	var doc struct {
		Seed json.RawMessage `json:"seed"`
	}
	if err := json.Unmarshal(mapGen, &doc); err != nil {
		f.FormError = i18n.M("worlds.generate.error.settings")
		return nil
	}
	if raw := strings.TrimSpace(string(doc.Seed)); raw != "" && raw != "null" {
		n, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			f.FormError = i18n.M("worlds.generate.error.seed_settings")
			return nil
		}
		v := uint32(n)
		return &v
	}
	if n, err := strconv.ParseUint(f.FilledSeed, 10, 32); err == nil {
		v := uint32(n)
		return &v
	}
	v := newSeed()
	f.FilledSeed = strconv.FormatUint(uint64(v), 10)
	return &v
}

// decodeExchange reads the map exchange string and checks it fits the
// world's game version: same line, and not newer (SPEC 5.6).
func decodeExchange(f *GenerateForm, version string) (mapexchange.Result, bool) {
	text := strings.TrimSpace(f.Exchange)
	switch {
	case text == "":
		f.ExchangeError = i18n.M("worlds.generate.error.exchange_empty")
		return mapexchange.Result{}, false
	case len(text) > mapexchange.MaxInputBytes:
		f.ExchangeError = i18n.M("worlds.generate.error.exchange_too_long")
		return mapexchange.Result{}, false
	}
	res, err := mapexchange.Decode(text)
	if err != nil {
		if ue, ok := errors.AsType[*mapexchange.UnsupportedError](err); ok {
			f.ExchangeError = i18n.M("worlds.generate.error.exchange_unsupported", "version", versionText(ue.Version))
		} else {
			f.ExchangeError = i18n.M("worlds.generate.error.exchange_unreadable")
		}
		return mapexchange.Result{}, false
	}
	made := versionText(res.Version)
	madeLine := fmt.Sprintf("%d.%d", res.Version[0], res.Version[1])
	if line, _ := mapgen.Line(version); madeLine != line {
		f.ExchangeError = i18n.M("worlds.generate.error.exchange_other_line", "version", made, "game", version)
		return mapexchange.Result{}, false
	}
	if world.CompareGameVersions(made, version) > 0 {
		f.ExchangeError = i18n.M("worlds.generate.error.exchange_newer", "version", made, "game", version)
		return mapexchange.Result{}, false
	}
	// An older patch of the line may lay its settings out differently from the world's.
	if err := mapexchange.Compatible(res.Version, version); err != nil {
		f.ExchangeError = i18n.M("worlds.generate.error.exchange_layout", "version", made, "game", version)
		if ue, ok := errors.AsType[*mapexchange.UnsupportedError](err); ok && ue.From != "" && ue.Until != "" {
			f.ExchangeError = i18n.M("worlds.generate.error.exchange_layout_range", "version", made, "game", version, "from", ue.From, "until", ue.Until)
		}
		return mapexchange.Result{}, false
	}
	return res, true
}

// versionText is the first three numbers of a version; the fourth is the
// build.
func versionText(v [4]uint16) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// handleGeneratePreview makes the map pictures from the settings Generate
// would write, and shows the form again with them under it (SPEC 5.6).
func (s *server) handleGeneratePreview(w http.ResponseWriter, r *http.Request, u store.User) {
	if s.previewer == nil {
		s.notFound(w, r, u)
		return
	}
	if !s.readForm(w, r) {
		return
	}
	f, nw, ok := s.readGenerate(r, kindPreview)
	if !ok {
		// htmx swaps only a 2xx answer, and then the whole form, with its errors.
		status := http.StatusBadRequest
		if r.Header.Get("HX-Request") == "true" {
			status = http.StatusOK
			h := w.Header()
			h.Set("HX-Retarget", "#generate-form")
			h.Set("HX-Reselect", "#generate-form")
			h.Set("HX-Reswap", "outerHTML")
		}
		if f.Reset {
			status = http.StatusOK
		}
		s.page(w, r, status, u.Username, s.generatePage(f, nil))
		return
	}
	line, _ := mapgen.Line(nw.GameVersion)
	req := game.PreviewRequest{
		GameVersion: nw.GameVersion, SpaceAge: nw.SpaceAge, Preset: nw.Preset, Seed: *nw.Seed,
		MapGenSettings: nw.MapGenSettings, MapSettings: nw.MapSettings,
		Planets: mapgen.Planets(line, nw.SpaceAge),
	}
	view := &PreviewView{Seed: strconv.FormatUint(uint64(req.Seed), 10)}
	images, err := s.previewer.Preview(r.Context(), req)
	pf, failure := errors.AsType[*game.PreviewError](err)
	switch {
	case err == nil:
		view.Token, err = s.previews.put(u.ID, images)
		if err != nil {
			s.log.Error("keep map preview", "error", err)
			view.Error = i18n.M("worlds.generate.preview.failed")
			break
		}
		for _, planet := range req.Planets {
			view.Pictures = append(view.Pictures, PreviewPicture{Planet: planet, Name: planetMsg(planet), Path: pictureURL(view.Token, planet)})
		}
	case errors.Is(err, game.ErrPreviewImageMissing):
		view.Error = i18n.M("worlds.generate.preview.no_image", "version", nw.GameVersion)
	case errors.Is(err, game.ErrPreviewBusy):
		view.Error = i18n.M("worlds.generate.preview.busy")
	case errors.Is(err, game.ErrPreviewTimeout):
		view.Error = i18n.M("worlds.generate.preview.timeout")
	case failure:
		s.log.Warn("map preview failed", "version", nw.GameVersion, "oom", pf.OOM)
		view.Error = i18n.M("worlds.generate.preview.failed")
		if pf.OOM {
			view.Error = i18n.M("worlds.generate.preview.oom")
		}
	default:
		if r.Context().Err() == nil {
			s.log.Error("map preview", "version", nw.GameVersion, "error", err)
		}
		view.Error = i18n.M("worlds.generate.preview.failed")
	}
	s.page(w, r, http.StatusOK, u.Username, s.generatePage(f, view))
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
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(png)))
	_, _ = w.Write(png)
}

// generatedFacts adds the Done message after Generate to a world's page.
func (s *server) generatedFacts(p *WorldPage, r *http.Request) {
	if r.URL.Query().Get(generatedQuery) != "" {
		p.Done = i18n.M("worlds.world.generated", "name", p.World.Name)
	}
}
