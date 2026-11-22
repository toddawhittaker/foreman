package web

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

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
	PathGeneratePreview = "/generate/preview"
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

// The two ways to make the map (SPEC 5.6, DESIGN 8.6).
const (
	modeSettings = "settings"
	modeExchange = "exchange"
)

// favoriteChoiceID is the id of Generate's favorite select.
const favoriteChoiceID = "favorite-choice"

// presetSelectID is the id of Generate's preset select.
const presetSelectID = "generate-preset"

// defaultPreset is the game's own default, which Reset to defaults returns to.
const defaultPreset = "default"

// newSeed picks the seed for a blank seed field. Tests replace it.
var newSeed = func() uint32 { return mrand.Uint32() } //nolint:gosec // a map seed is not a secret

// GeneratePage is the Generate form, with the map preview when one was made
// (SPEC 5.6, DESIGN 8.6, 8.8).
type GeneratePage struct {
	Form GenerateForm
	// CanPreview is true when this Foreman can make map previews.
	CanPreview bool
	Preview    *PreviewView
	// FavoritesOn shows "Start from a favorite"; Favorites are its choices
	// (SPEC 5.12).
	FavoritesOn bool
	Favorites   []world.Favorite
}

// FavoritesPath is the favorites list.
func (GeneratePage) FavoritesPath() string { return PathFavorites }

// AchReason is one of the game's reasons achievements are turned off, for
// the summary by the Generate button; Active when the settings give it.
type AchReason struct {
	Key    string
	Msg    i18n.Msg
	Active bool
}

// GenerateForm is the Generate form's state, as typed or as first offered:
// the world's name, version, Space Age and favorite, and its map settings.
type GenerateForm struct {
	Name string
	// Typed is the typed version. Version is the one the world will have,
	// and DefaultVersion the active world's, which the first option of the
	// select names.
	Typed          string
	Version        string
	DefaultVersion string
	SpaceAge       bool
	Download       bool
	CanDownload    bool

	// Favorite is the favorite the form started from, or nil (SPEC 5.12);
	// FavoriteError says it cannot be used, on the chooser.
	Favorite      *world.Favorite
	FavoriteError i18n.Msg

	NameError, VersionError, SpaceAgeError i18n.Msg

	MapForm
}

// MapForm is the map settings part of a form: the preset and the settings,
// or a map exchange string, laid out from a catalogue of planets and
// controls (SPEC 5.6).
type MapForm struct {
	// cat is the planets and controls the settings are laid out from.
	cat mapgen.Catalogue

	Mode    string
	Preset  string
	Presets []string
	// FilledFrom is the preset the settings were filled from. A page without
	// script posts a new preset with the old preset's values; the two differ
	// then, and the settings are filled again before anything is made.
	FilledFrom string
	// Tab is the settings tab shown.
	Tab string
	// FilledSeed carries the random seed an exchange-string preview used, so
	// the world matches it.
	FilledSeed  string
	Exchange    string
	MapGen      MapGenForm
	MapSettings MapSettingsForm
	// Modified is true once any setting differs from the preset's.
	Modified bool
	// Reasons are the line's achievement reasons, each marked when the
	// settings give it (SPEC 5.6).
	Reasons []AchReason
	// Line and MapKey say which game line and Space Age setting the map
	// options were made for.
	Line   string
	MapKey string
	// Reset says the map options were set back because the version or
	// Space Age changed; Refilled that they were filled from a newly chosen
	// preset that the page had not shown yet.
	Reset, Refilled bool

	// PreviewSize is the map preview's side in tiles; zero is the default.
	PreviewSize int

	PresetError, ExchangeError, FormError, PreviewSizeError i18n.Msg
	SettingsProblems                                        []Problem
}

func (GeneratePage) templateName() string { return "generate" }
func (GeneratePage) navItem() string      { return PathWorlds }

func (p GeneratePage) hasErrors() bool { return len(p.Problems()) > 0 }

// Problems lists the form's errors in the order the fields appear.
func (p GeneratePage) Problems() []Problem {
	f, m := p.Form, p.Form.MapForm
	return append(problems(
		Problem{favoriteChoiceID, f.FavoriteError},
		Problem{"generate-name", f.NameError},
		Problem{"generate-version", f.VersionError},
		Problem{"generate-space-age", f.SpaceAgeError},
	), m.Problems()...)
}

// Problems lists the map settings' errors in the order the fields appear.
func (f MapForm) Problems() []Problem {
	all := []Problem{
		{presetSelectID, f.PresetError},
		{"exchange-string", f.ExchangeError},
		{"mode-" + f.Mode, f.FormError},
	}
	if f.Mode == modeSettings {
		all = append(all, f.SettingsProblems...)
	}
	all = append(all, Problem{previewSizeID, f.PreviewSizeError})
	return problems(all...)
}

// Blocked reports whether any achievement reason is active.
func (f MapForm) Blocked() bool {
	return slices.ContainsFunc(f.Reasons, func(r AchReason) bool { return r.Active })
}

// Tabs lists the settings tabs in the game's order.
func (MapForm) Tabs() []string {
	return []string{tabResources, tabTerrain, tabEnemy, tabAdvanced}
}

// Paths the form uses.
func (GeneratePage) Path() string        { return PathGenerate }
func (GeneratePage) PreviewPath() string { return PathGeneratePreview }
func (GeneratePage) VersionsPath() string {
	return versionsPath("", "")
}

// PresetMsg is a preset's name in the game's own words.
func (MapForm) PresetMsg(name string) i18n.Msg { return PresetMsg(name) }

// AtDefaults is true while the settings are the default preset's, so
// there is nothing for Reset to defaults to do.
func (f MapForm) AtDefaults() bool { return f.Preset == defaultPreset && !f.Modified }

func (s *server) generateRoutes() {
	s.mux.HandleFunc("GET "+PathGenerate, s.requireUser(s.handleGenerateForm))
	s.handleUpload("POST "+PathGenerate, fixedLimit(generateFormLimit), s.requireUser(s.handleGenerate))
	s.previewRoutes()
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

func (s *server) generatePage(ctx context.Context, f GenerateForm, preview *PreviewView) GeneratePage {
	f.CanDownload = s.game != nil
	p := GeneratePage{Form: f, CanPreview: s.previewer != nil, Preview: preview, FavoritesOn: s.favorites != nil}
	if s.favorites != nil {
		// A list that cannot be read leaves only the chooser out.
		p.Favorites, _ = s.favorites.List(ctx)
	}
	return p
}

// favoriteMap reports whether the form's map settings are laid over the
// favorite's files: it has them, the map is made from the settings, and the
// world is on the favorite's game line, which the files were written for.
func (f GenerateForm) favoriteMap() bool {
	if f.Favorite == nil || !f.Favorite.HasMapSettings || f.Mode != modeSettings {
		return false
	}
	line, ok := mapgen.Line(f.Favorite.GameVersion)
	return ok && line == f.Line
}

// readFavorite reads the favorite a form names; a missing or damaged one
// is a form error.
func (s *server) readFavorite(f *GenerateForm, id string) world.FavoriteFiles {
	if id == "" || s.favorites == nil {
		return world.FavoriteFiles{}
	}
	fav, files, err := s.favorites.Get(id)
	if err != nil {
		f.FavoriteError = i18n.M("favorites.error.gone")
		return world.FavoriteFiles{}
	}
	f.Favorite = &fav
	return files
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

// handleGenerateForm shows the empty form: the default preset on the active
// world's game version.
func (s *server) handleGenerateForm(w http.ResponseWriter, r *http.Request, u store.User) {
	version := s.activeVersion(r.Context())
	line, ok := mapgen.Line(version)
	if !ok {
		line = mapgen.Lines[len(mapgen.Lines)-1]
	}
	spaceAge := lineHasSpaceAge(line)
	f := GenerateForm{
		Version: version, DefaultVersion: version, SpaceAge: spaceAge, Download: true,
		MapForm: MapForm{
			cat: mapgen.Builtin(line, spaceAge), Mode: modeSettings, Preset: defaultPreset, Line: line,
			Presets: mapgen.Presets(line), MapKey: mapKey(line, spaceAge), Tab: tabResources,
		},
	}
	var files world.FavoriteFiles
	if id := r.URL.Query().Get(favoriteQuery); id != "" {
		files = s.readFavorite(&f, id)
		if fav := f.Favorite; fav != nil {
			f.startFrom(*fav)
			version = f.Version
		}
	}
	f.fill(version, nil)
	if f.favoriteMap() {
		f.fillFromFiles(files)
	}
	rules, _ := mapgen.AchievementRules(version)
	f.MapGen.markAchievements(rules)
	f.reasons(version, nil, nil, rules)
	status := http.StatusOK
	switch {
	case !f.FavoriteError.IsZero() && f.Favorite == nil:
		status = http.StatusNotFound
	case !f.FavoriteError.IsZero():
		status = http.StatusUnprocessableEntity
	}
	s.page(w, r, status, u.Username, s.generatePage(r.Context(), f, nil))
}

// startFrom fills the form's version, Space Age and preset from a favorite
// (SPEC 5.12); the person can change any of them.
func (f *GenerateForm) startFrom(fav world.Favorite) {
	line, ok := mapgen.Line(fav.GameVersion)
	if !ok || !world.SupportedGameVersion(fav.GameVersion) {
		return
	}
	f.Version, f.Line = fav.GameVersion, line
	if fav.GameVersion != f.DefaultVersion {
		f.Typed = fav.GameVersion
	}
	f.SpaceAge = fav.SpaceAge && lineHasSpaceAge(line)
	f.cat = mapgen.Builtin(line, f.SpaceAge)
	f.Presets = mapgen.Presets(line)
	f.Preset = defaultPreset
	if slices.Contains(f.Presets, fav.Preset) {
		f.Preset = fav.Preset
	}
	f.MapKey = mapKey(line, f.SpaceAge)
}

func (f *GenerateForm) clearErrors() {
	f.NameError, f.VersionError, f.SpaceAgeError = i18n.Msg{}, i18n.Msg{}, i18n.Msg{}
	f.MapForm.clearErrors()
}

func (f *MapForm) clearErrors() {
	f.PresetError, f.ExchangeError, f.FormError, f.SettingsProblems = i18n.Msg{}, i18n.Msg{}, i18n.Msg{}, nil
	f.PreviewSizeError = i18n.Msg{}
	f.MapGen.Seed.Error = i18n.Msg{}
}

func (f GenerateForm) failed() bool {
	return GeneratePage{Form: f}.hasErrors()
}

// errGenerateForm says the Generate form has errors, which are on the form.
var errGenerateForm = errors.New("the generate form has errors")

// generateWorld makes the world a read form describes, as Generate does
// (SPEC 5.6, 5.12): ok and nw come from readGenerateValues, and form holds
// the posted values. It audits the world, starts the image download when
// the form asks, and saves a favorite's mods as the world's picks. A form
// with errors returns errGenerateForm with them on f; a refusal from the
// world maker comes back as it is, for the caller to word.
func (s *server) generateWorld(ctx context.Context, by game.Actor, f *GenerateForm, nw world.NewWorld, ok bool, form url.Values) (string, error) {
	files := s.readFavorite(f, form.Get(favoriteQuery))
	if f.favoriteMap() {
		// Damaged files are refused here too, as the filled form refused them.
		check := *f
		check.fillFromFiles(files)
		f.FavoriteError = check.FavoriteError
	}
	if !ok || !f.FavoriteError.IsZero() {
		return "", errGenerateForm
	}
	if fav := f.Favorite; fav != nil {
		nw.ServerSettings, nw.ModSettings = files.ServerSettings, files.ModSettings
		if f.favoriteMap() && !s.favoriteMapFilesFrom(f, &nw, files, form) {
			return "", errGenerateForm
		}
	}
	id, err := s.worlds.Generate(ctx, nw)
	if err != nil {
		return "", err
	}
	how := f.Mode
	if f.Mode == modeSettings {
		how = "preset " + f.Preset
		if f.Modified {
			how += ", modified"
		}
	}
	if f.Favorite != nil {
		how += ", favorite " + f.Favorite.Name
	}
	s.audit(ctx, by, AuditGenerate, id, fmt.Sprintf("%s, %s, space age %t", how, nw.GameVersion, nw.SpaceAge))
	if f.Download {
		s.downloadFor(ctx, map[string]string{nw.GameVersion: id}, by)
	}
	if fav := f.Favorite; fav != nil {
		for _, m := range fav.Mods {
			// Saved as picks too, so leaving the review page keeps the mods.
			if err := s.store.AddModPlanName(ctx, id, m.Name, by.Username, s.now()); err != nil && !errors.Is(err, store.ErrModPlanFull) {
				s.log.Warn("save the favorite's mods as picks", "world", id, "error", err)
			}
		}
	}
	return id, nil
}

// handleGenerate makes the world (SPEC 5.6).
func (s *server) handleGenerate(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	f, nw, ok := s.readStepOne(r.Context(), r.PostForm, true)
	id, err := s.generateWorld(r.Context(), actor(u), &f, nw, ok, r.PostForm)
	switch {
	case errors.Is(err, errGenerateForm):
		status := http.StatusBadRequest
		if f.Reset || f.Refilled {
			status = http.StatusOK
		}
		s.page(w, r, status, u.Username, s.generatePage(r.Context(), f, nil))
		return
	case err != nil:
		status, msg, ok := s.refusal(w, r, u, "generate world", err)
		if !ok {
			return
		}
		f.place(err, msg)
		s.page(w, r, status, u.Username, s.generatePage(r.Context(), f, nil))
		return
	}
	// The favorite's mods go through the review page's checks and one
	// install (SPEC 5.12, 9.13).
	if fav := f.Favorite; fav != nil && len(fav.Mods) > 0 {
		// The review reads the names and versions from the favorite, so the
		// address carries its id alone (SPEC 17).
		q := url.Values{favoriteQuery: {fav.ID}, generatedQuery: {"1"}}
		http.Redirect(w, r, planPath(id, q), http.StatusSeeOther) //nolint:gosec // id is a world id Foreman made, a local path segment
		return
	}
	// Step 2, the world's mods, which can be skipped to the map (SPEC 5.6, 17.9).
	http.Redirect(w, r, modsPath(id)+"?"+generatedQuery+"=1", http.StatusSeeOther) //nolint:gosec // id is a world id Foreman made, a local path segment
}

// readStepOne reads step 1's values, which name no map settings: the map is
// the preset's, or a favorite's own files for a favorite on the world's
// line (SPEC 5.6, 5.12). MCP's generate_world reads its arguments through
// it too; favSeed, for the page, takes a favorite's seed when none is posted.
func (s *server) readStepOne(ctx context.Context, form url.Values, favSeed bool) (f GenerateForm, nw world.NewWorld, ok bool) {
	id := form.Get(favoriteQuery)
	var files world.FavoriteFiles
	if id != "" && s.favorites != nil {
		_, files, _ = s.favorites.Get(id)
	}
	if favSeed && strings.TrimSpace(form.Get(mgSeed)) == "" && len(files.MapGenSettings) > 0 {
		var doc struct {
			Seed *uint32 `json:"seed"`
		}
		if json.Unmarshal(files.MapGenSettings, &doc) == nil && doc.Seed != nil {
			form.Set(mgSeed, strconv.FormatUint(uint64(*doc.Seed), 10))
		}
	}
	version := cmp.Or(chosenVersion(form.Get("game_version"), form.Get("game_version_typed")), s.activeVersion(ctx))
	preset := cmp.Or(form.Get("preset"), defaultPreset)
	// A favorite's map settings file stands in for the fields its values would fill.
	if len(files.MapSettings) > 0 {
		for k, v := range mapSettingsValues(version, preset, files.MapSettings) {
			form[k] = v
		}
	}
	f, nw, ok = s.readGenerateValues(ctx, form, kindGenerate)
	if ok && id != "" {
		s.readFavorite(&f, id)
		if f.favoriteMap() {
			f.fillFromFiles(files)
			if nw.Seed != nil {
				f.MapGen = f.MapGen.WithSeed(*nw.Seed)
			}
		}
	}
	return f, nw, ok
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

// previewSizeField and previewSizeID name the map preview's size control.
const (
	previewSizeField = "preview_size"
	previewSizeID    = "generate-preview-size"
)

// PreviewSizeOption is one choice of the preview size select.
type PreviewSizeOption struct {
	Size            int
	Default, Chosen bool
}

// PreviewSizes lists every size the preview can be drawn at, the form's
// marked as chosen.
func (f MapForm) PreviewSizes() []PreviewSizeOption {
	chosen := cmp.Or(f.PreviewSize, game.PreviewSizeDefault)
	var out []PreviewSizeOption
	for n := game.PreviewSizeMin; n <= game.PreviewSizeMax; n += game.PreviewSizeStep {
		out = append(out, PreviewSizeOption{Size: n, Default: n == game.PreviewSizeDefault, Chosen: n == chosen})
	}
	return out
}

// readPreviewSize reads the preview size. A blank one is the default. One
// off the steps is a field error for a preview, which draws at it; the
// world's own maker never uses the size, so it falls back to the default.
func (f *MapForm) readPreviewSize(raw string, kind readKind) {
	f.PreviewSize = game.PreviewSizeDefault
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	n, err := strconv.Atoi(raw)
	switch {
	case err == nil && game.ValidPreviewSize(n):
		f.PreviewSize = n
	case kind == kindPreview:
		f.PreviewSizeError = i18n.M("worlds.generate.error.preview_size")
	}
}

// What a posted form is read for.
type readKind int

const (
	kindGenerate readKind = iota // make the world
	kindPreview                  // make the map pictures; the name is not needed yet
	kindRefresh                  // redraw the map options for a changed version or preset
)

// fill sets the settings to the preset's values for the version, keeping
// the seed (DESIGN 8.7).
func (f *MapForm) fill(version string, keep *MapGenText) {
	if len(f.cat.Planets) == 0 {
		// A form laid out from no catalogue would show no planet rows at all.
		panic("web: the map form has no catalogue")
	}
	seed := MapGenText{}
	if keep != nil {
		seed = *keep
	}
	f.MapGen = mapGenFormForPreset(f.Line, f.cat, f.Preset)
	f.MapGen.withPreset(f.MapGen)
	f.MapGen.Seed.Value = seed.Value
	f.MapSettings, _, _ = readMapSettings(version, f.Preset, nil)
	f.FilledFrom = f.Preset
}

// settingsShown is sent with the map settings, so a post that never showed
// them is told apart from one whose checkboxes are all off.
const settingsShown = "settings_shown"

// checkSeed refuses a seed that is not a 32-bit whole number.
func (f *MapForm) checkSeed() {
	s := strings.TrimSpace(f.MapGen.Seed.Value)
	if _, err := strconv.ParseUint(s, 10, 32); s != "" && err != nil {
		f.MapGen.Seed.Error = i18n.M("mapgen.err.seed", "max", strconv.FormatUint(mgMaxNumber, 10))
		if f.Mode == modeSettings {
			f.SettingsProblems = append(f.SettingsProblems, Problem{FieldID: mgSeedID, Message: f.MapGen.Seed.Error})
		}
	}
}

// tabOf names the settings tab a field id is in.
func (f MapForm) tabOf(id string) string {
	if strings.HasPrefix(id, "ms-") {
		for _, g := range f.MapSettings.Groups {
			for _, fl := range g.Fields {
				if fl.ID == id {
					return g.Tab
				}
			}
		}
	}
	return f.MapGen.TabOf(id)
}

// readGenerate reads and checks the form. It always returns the form to
// show again. ok is true when the world, or the preview, can be made. When
// the version or Space Age moved on from what the map settings were made
// for, or a new preset was chosen without the page filling its values, the
// settings are filled from the preset and ok is false with no error (Reset,
// Refilled); the exchange string does not depend on them. The caller has
// already parsed the post with readForm.
func (s *server) readGenerate(r *http.Request, kind readKind) (f GenerateForm, nw world.NewWorld, ok bool) {
	return s.readGenerateValues(r.Context(), r.PostForm, kind)
}

// readGenerateValues is readGenerate on the posted values, so MCP can make
// a world through the same checks.
func (s *server) readGenerateValues(ctx context.Context, form url.Values, kind readKind) (f GenerateForm, nw world.NewWorld, ok bool) {
	f = GenerateForm{
		Name:        form.Get("name"),
		Typed:       chosenVersion(form.Get("game_version"), form.Get("game_version_typed")),
		SpaceAge:    form.Get("space_age") == "on",
		Download:    form.Get("download") == "on",
		CanDownload: s.game != nil,
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
	if versionOK && !lineHasSpaceAge(line) && f.SpaceAge {
		f.SpaceAgeError = msgSpaceAgeOld()
	}
	if versionOK && f.Typed != "" && f.Typed != f.DefaultVersion && f.SpaceAgeError.IsZero() {
		check := ImportForm{GameVersion: f.Typed}
		s.checkTypedVersion(ctx, &check)
		f.VersionError = check.VersionError
	}
	posted := form.Get("map_key")
	if !versionOK {
		if line = keyLine(posted); line == "" {
			line = mapgen.Lines[len(mapgen.Lines)-1]
		}
	}
	mapSpaceAge := f.SpaceAge && lineHasSpaceAge(line)
	in := mapInput{Line: line, Cat: mapgen.Builtin(line, mapSpaceAge), MapKey: mapKey(line, mapSpaceAge)}
	if versionOK {
		in.Version = f.Version
	}
	// The settings are stale when made for another line, or for the other Space Age setting.
	in.Stale = versionOK && form.Get(settingsShown) != "" &&
		(keyLine(posted) != line || (f.SpaceAgeError.IsZero() && posted != in.MapKey))

	if kind == kindGenerate && strings.TrimSpace(f.Name) == "" {
		f.NameError = msgNameEmpty()
	}
	var files world.NewMapFiles
	f.MapForm, files, ok = readMapForm(form, kind, in)
	if f.Reset || f.Refilled {
		f.clearErrors()
		return f, nw, false
	}
	if kind == kindRefresh || !versionOK {
		return f, nw, false
	}
	nw = world.NewWorld{Name: strings.TrimSpace(f.Name), GameVersion: f.Version, SpaceAge: f.SpaceAge,
		Preset: files.Preset, Seed: files.Seed, MapGenSettings: files.MapGenSettings, MapSettings: files.MapSettings}
	return f, nw, ok && !f.failed()
}

// mapInput says what the map part of a form is laid out for.
type mapInput struct {
	Line string
	// Cat is the planets and controls: the line's built-in ones, or a
	// world's own (SPEC 5.6).
	Cat mapgen.Catalogue
	// MapKey names the line and Space Age setting the settings are for.
	MapKey string
	// Version is the world's game version, or "" while it is not settled:
	// the settings are then laid out for the line's own and nothing is made.
	Version string
	// Stale says the posted settings were laid out for another MapKey, so
	// they are filled again from the preset.
	Stale bool
}

// readMapForm reads and checks the map part of a posted form: the preset
// and the settings, or the map exchange string. It always returns the form
// to show again. ok is true when the map files can be made; files then
// holds them, with the seed. Reset and Refilled are as readGenerate says.
func readMapForm(form url.Values, kind readKind, in mapInput) (f MapForm, files world.NewMapFiles, ok bool) {
	f = MapForm{
		cat:        in.Cat,
		Mode:       form.Get("mode"),
		Preset:     form.Get("preset"),
		FilledFrom: form.Get("filled_from"),
		Tab:        form.Get("tab"),
		FilledSeed: strings.TrimSpace(form.Get("filled_seed")),
		Exchange:   form.Get("exchange"),
		Line:       in.Line,
		MapKey:     in.MapKey,
		Presets:    mapgen.Presets(in.Line),
	}
	if f.Mode != modeExchange {
		f.Mode = modeSettings
	}
	f.readPreviewSize(form.Get(previewSizeField), kind)
	if !slices.Contains(f.Tabs(), f.Tab) {
		f.Tab = tabResources
	}
	// A post that never showed the settings (no settingsShown), such as a preset
	// and a seed alone, means the preset as it is.
	shown := form.Get(settingsShown) != ""
	f.Reset = in.Stale && f.Mode == modeSettings
	if !slices.Contains(f.Presets, f.Preset) {
		if kind == kindRefresh || f.Preset == "" {
			f.Preset = defaultPreset
		} else {
			f.PresetError = i18n.M("worlds.generate.error.preset")
		}
	}
	// Reset to defaults is for the whole world: the version's default preset (SPEC 5.6).
	if form.Get("reset") != "" && f.PresetError.IsZero() {
		f.Preset = defaultPreset
	}
	// The version the map settings are laid out for: the world's, or the
	// line's own when the version is not settled yet.
	layoutVersion := cmp.Or(in.Version, mapgen.DataVersion(in.Line))

	// Always read the settings, so they are shown again whatever the mode.
	var mapGen []byte
	var overrides map[string]any
	seed := MapGenText{Value: strings.TrimSpace(form.Get(mgSeed))}
	mismatch := f.PresetError.IsZero() && f.FilledFrom != f.Preset
	refill := in.Stale || form.Get("reset") != "" || mismatch || !shown
	if refill {
		f.Refilled = kind == kindGenerate && shown && mismatch && !in.Stale && f.Mode == modeSettings && form.Get("reset") == ""
		f.fill(layoutVersion, &seed)
		f.checkSeed()
		if f.Refilled {
			f.Tab = tabResources
		}
		mapGen, _ = buildMapGen(f.MapGen, rawOrEmpty(f.MapGen.Raw.Value))
	} else {
		var probs, msProbs []Problem
		f.MapGen, mapGen, probs = parseMapGenIn(form, in.Line, in.Cat)
		f.MapGen.withPreset(mapGenFormForPreset(in.Line, in.Cat, f.Preset))
		f.MapSettings, overrides, msProbs = readMapSettings(layoutVersion, f.Preset, form)
		if f.Mode == modeSettings {
			f.SettingsProblems = append(probs, msProbs...)
			// The seed is in no tab: keep the tab the page was on.
			if len(f.SettingsProblems) > 0 {
				if t := f.tabOf(f.SettingsProblems[0].FieldID); t != "" {
					f.Tab = t
				}
			}
		}
	}
	// The open planet is one choice for the whole form, kept over a round trip.
	f.MapGen.setOpen(form.Get("open_planet"))
	f.Modified = f.MapGen.Modified() || f.MapSettings.Modified()
	rules, _ := mapgen.AchievementRules(layoutVersion)
	f.MapGen.markAchievements(rules)
	if f.Reset || f.Refilled {
		f.clearErrors()
		f.reasons(layoutVersion, mapGen, nil, rules)
		return f, files, false
	}
	if kind == kindRefresh {
		if mapGen == nil {
			mapGen, _ = buildMapGen(f.MapGen, rawOrEmpty(f.MapGen.Raw.Value))
		}
		f.reasons(layoutVersion, mapGen, overrides, rules)
		return f, files, false
	}
	if in.Version == "" {
		f.reasons(layoutVersion, mapGen, overrides, rules)
		return f, files, false
	}

	switch f.Mode {
	case modeSettings:
		files.Seed = f.settingsFiles(&files, in.Version, mapGen, overrides)
	case modeExchange:
		if res, decoded := decodeExchange(&f, in.Version, generateOtherLine); decoded {
			files.MapGenSettings, files.MapSettings = res.MapGenSettings, res.MapSettings
			files.Seed = settingsSeed(&f, res.MapGenSettings)
		}
	}
	f.reasons(in.Version, mapGen, overrides, rules)
	return f, files, files.Seed != nil && len(f.Problems()) == 0
}

// settingsFiles sets how the map is made from the settings: an unchanged
// preset passes the preset alone and writes no file; any change writes both
// files complete and passes no preset, because the game lets a preset win
// over the files (SPEC 5.6). It returns the seed, filling a blank one in the
// form so the world matches the preview, or nil after a problem.
func (f *MapForm) settingsFiles(files *world.NewMapFiles, version string, mapGen []byte, overrides map[string]any) *uint32 {
	if len(f.SettingsProblems) > 0 || !f.PresetError.IsZero() {
		return nil
	}
	if f.Modified {
		switch {
		case !f.MapSettings.Untested:
			ms, err := mapgen.MapSettings(version, f.Preset, overrides)
			if err != nil {
				f.FormError = i18n.M("worlds.generate.error.settings")
				return nil
			}
			files.MapGenSettings, files.MapSettings = mapGen, ms
		case f.Preset == defaultPreset:
			// The game's own map settings are the default preset's.
			files.MapGenSettings = mapGen
		default:
			// Without the layout, the preset's map settings cannot be written,
			// and passing the preset would undo the changes.
			f.PresetError = i18n.M("worlds.generate.error.untested_preset", "version", version)
			return nil
		}
	} else if f.Preset != defaultPreset {
		files.Preset = f.Preset
	}
	if n := f.MapGen.SeedNumber(); n != nil {
		return n
	}
	v := newSeed()
	f.MapGen = f.MapGen.WithSeed(v)
	return &v
}

// reasons marks the achievement reasons the settings give. With both files
// at hand the answer is mapgen.AchievementBlockers'; otherwise, as while a
// field has an error, it is the fields' own (the same rules).
func (f *MapForm) reasons(version string, mapGen []byte, overrides map[string]any, rules []mapgen.AchievementRule) {
	active := map[string]bool{}
	ms, msErr := mapgen.MapSettings(version, f.Preset, overrides)
	if mapGen != nil && msErr == nil {
		if bs, err := mapgen.AchievementBlockers(version, mapGen, ms); err == nil {
			for _, b := range bs {
				active[b.Message] = true
			}
		}
	} else {
		for _, k := range f.MapGen.blockedReasons() {
			active[k] = true
		}
		for _, g := range f.MapSettings.Groups {
			for _, fl := range g.Fields {
				if fl.Blocked && fl.Ach != nil {
					active[fl.Ach.Reason] = true
				}
			}
		}
	}
	f.Reasons = nil
	for _, r := range rules {
		if !slices.ContainsFunc(f.Reasons, func(a AchReason) bool { return a.Key == r.Message }) {
			f.Reasons = append(f.Reasons, AchReason{Key: r.Message, Msg: achievementMsgs[r.Message], Active: active[r.Message]})
		}
	}
}

// settingsSeed takes the seed from the map-gen settings, else the one filled
// by an earlier preview, else a random one kept in the form.
func settingsSeed(f *MapForm, mapGen []byte) *uint32 {
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
// otherLine words a string of another game line, made by made for a world
// on game: Generate's says to choose another version, step 3's to change the
// world's.
func decodeExchange(f *MapForm, version string, otherLine func(made, game string) i18n.Msg) (mapexchange.Result, bool) {
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
		f.ExchangeError = otherLine(made, version)
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

// generateOtherLine is Generate's refusal of a string of another game line.
func generateOtherLine(made, game string) i18n.Msg {
	return i18n.M("worlds.generate.error.exchange_other_line", "version", made, "game", game)
}

// versionText is the first three numbers of a version; the fourth is the
// build.
func versionText(v [4]uint16) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// generatedFacts adds the Done message after Generate to a world's page.
func (s *server) generatedFacts(p *WorldPage, r *http.Request) {
	if r.URL.Query().Get(generatedQuery) != "" {
		p.Done = i18n.M("worlds.world.generated", "name", p.World.Name)
	}
}

// lineHasSpaceAge reports whether a game line ("2.0") can have Space Age.
func lineHasSpaceAge(line string) bool {
	l, ok := mapgen.LineOf(line)
	return ok && len(l.SpaceAgeMods) > 0
}
