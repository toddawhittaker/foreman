package web

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"net/http"
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
	PathGenerateRefresh = "/generate/refresh"
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
}

// AchReason is one of the game's reasons achievements are turned off, for
// the summary by the Generate button; Active when the settings give it.
type AchReason struct {
	Key    string
	Msg    i18n.Msg
	Active bool
}

// GenerateForm is the Generate form's state, as typed or as first offered.
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
	Mode           string
	Preset         string
	Presets        []string
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

	NameError, VersionError, SpaceAgeError, PresetError, ExchangeError, FormError, PreviewSizeError i18n.Msg
	SettingsProblems                                                                                []Problem
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
func (f GenerateForm) Blocked() bool {
	return slices.ContainsFunc(f.Reasons, func(r AchReason) bool { return r.Active })
}

// Tabs lists the settings tabs in the game's order.
func (GenerateForm) Tabs() []string {
	return []string{tabResources, tabTerrain, tabEnemy, tabAdvanced}
}

// Paths the form uses.
func (GeneratePage) Path() string        { return PathGenerate }
func (GeneratePage) RefreshPath() string { return PathGenerateRefresh }
func (GeneratePage) PreviewPath() string { return PathGeneratePreview }
func (GeneratePage) VersionsPath() string {
	return versionsPath("")
}

// RefreshPath is where the settings are redrawn.
func (GenerateForm) RefreshPath() string { return PathGenerateRefresh }

// PresetMsg is a preset's name in the game's own words.
func (GenerateForm) PresetMsg(name string) i18n.Msg { return PresetMsg(name) }

// AtDefaults is true while the settings are the default preset's, so
// there is nothing for Reset to defaults to do.
func (f GenerateForm) AtDefaults() bool { return f.Preset == defaultPreset && !f.Modified }

func (s *server) generateRoutes() {
	s.mux.HandleFunc("GET "+PathGenerate, s.requireUser(s.handleGenerateForm))
	s.handleUpload("POST "+PathGenerate, generateFormLimit, s.requireUser(s.handleGenerate))
	s.handleUpload("POST "+PathGenerateRefresh, generateFormLimit, s.requireUser(s.handleGenerateRefresh))
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
		Mode: modeSettings, Preset: defaultPreset, Line: line, Presets: mapgen.Presets(line),
		MapKey: mapKey(line, spaceAge), Tab: tabResources,
	}
	f.fill(version, nil)
	rules, _ := mapgen.AchievementRules(version)
	f.MapGen.markAchievements(rules)
	f.reasons(version, nil, nil, rules)
	s.page(w, r, http.StatusOK, u.Username, s.generatePage(f, nil))
}

// handleGenerateRefresh redraws the page for a changed game version, Space
// Age or preset, or the preset's reset; the browser swaps in only the parts
// that change.
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
	f.ExchangeError, f.FormError, f.SettingsProblems = i18n.Msg{}, i18n.Msg{}, nil
	f.PreviewSizeError = i18n.Msg{}
	f.MapGen.Seed.Error = i18n.Msg{}
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
		if f.Reset || f.Refilled {
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
	how := f.Mode
	if f.Mode == modeSettings {
		how = "preset " + f.Preset
		if f.Modified {
			how += ", modified"
		}
	}
	s.audit(r.Context(), u, AuditGenerate, id, fmt.Sprintf("%s, %s, space age %t", how, nw.GameVersion, nw.SpaceAge))
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
func (f GenerateForm) PreviewSizes() []PreviewSizeOption {
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
func (f *GenerateForm) readPreviewSize(raw string, kind readKind) {
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
func (f *GenerateForm) fill(version string, keep *MapGenText) {
	seed := MapGenText{}
	if keep != nil {
		seed = *keep
	}
	f.MapGen = NewMapGenFormForPreset(f.Line, f.SpaceAge && lineHasSpaceAge(f.Line), f.Preset)
	f.MapGen.withPreset(f.MapGen)
	f.MapGen.Seed.Value = seed.Value
	f.MapSettings, _, _ = readMapSettings(version, f.Preset, nil)
	f.FilledFrom = f.Preset
}

// settingsShown is sent with the map settings, so a post that never showed
// them is told apart from one whose checkboxes are all off.
const settingsShown = "settings_shown"

// checkSeed refuses a seed that is not a 32-bit whole number.
func (f *GenerateForm) checkSeed() {
	s := strings.TrimSpace(f.MapGen.Seed.Value)
	if _, err := strconv.ParseUint(s, 10, 32); s != "" && err != nil {
		f.MapGen.Seed.Error = i18n.M("mapgen.err.seed", "max", strconv.FormatUint(mgMaxNumber, 10))
		if f.Mode == modeSettings {
			f.SettingsProblems = append(f.SettingsProblems, Problem{FieldID: mgSeedID, Message: f.MapGen.Seed.Error})
		}
	}
}

// tabOf names the settings tab a field id is in.
func (f GenerateForm) tabOf(id string) string {
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
// Refilled); the exchange string does not depend on them.
func (s *server) readGenerate(r *http.Request, kind readKind) (f GenerateForm, nw world.NewWorld, ok bool) {
	ctx := r.Context()
	f = GenerateForm{
		Name:        r.PostFormValue("name"),
		Typed:       chosenVersion(r.PostFormValue("game_version"), r.PostFormValue("game_version_typed")),
		SpaceAge:    r.PostFormValue("space_age") == "on",
		Download:    r.PostFormValue("download") == "on",
		Mode:        r.PostFormValue("mode"),
		Preset:      r.PostFormValue("preset"),
		FilledFrom:  r.PostFormValue("filled_from"),
		Tab:         r.PostFormValue("tab"),
		FilledSeed:  strings.TrimSpace(r.PostFormValue("filled_seed")),
		Exchange:    r.PostFormValue("exchange"),
		CanDownload: s.game != nil,
	}
	if f.Mode != modeExchange {
		f.Mode = modeSettings
	}
	f.readPreviewSize(r.PostFormValue(previewSizeField), kind)
	if !slices.Contains(f.Tabs(), f.Tab) {
		f.Tab = tabResources
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
	posted := r.PostFormValue("map_key")
	if !versionOK {
		if line = keyLine(posted); line == "" {
			line = mapgen.Lines[len(mapgen.Lines)-1]
		}
	}
	mapSpaceAge := f.SpaceAge && lineHasSpaceAge(line)
	f.Line, f.MapKey = line, mapKey(line, mapSpaceAge)
	f.Presets = mapgen.Presets(line)
	// A post that never showed the settings (no settingsShown), such as a preset
	// and a seed alone, means the preset as it is.
	shown := r.PostFormValue(settingsShown) != ""
	// The settings are stale when made for another line, or for the other Space Age setting.
	stale := versionOK && shown && (keyLine(posted) != line || (f.SpaceAgeError.IsZero() && posted != f.MapKey))
	f.Reset = stale && f.Mode == modeSettings

	if kind == kindGenerate && strings.TrimSpace(f.Name) == "" {
		f.NameError = msgNameEmpty()
	}
	if !slices.Contains(f.Presets, f.Preset) {
		if kind == kindRefresh || f.Preset == "" {
			f.Preset = defaultPreset
		} else {
			f.PresetError = i18n.M("worlds.generate.error.preset")
		}
	}
	// Reset to defaults is for the whole world: the version's default preset (SPEC 5.6).
	if r.PostFormValue("reset") != "" && f.PresetError.IsZero() {
		f.Preset = defaultPreset
	}
	// The version the map settings are laid out for: the world's, or the
	// line's own when the version is not settled yet.
	layoutVersion := f.Version
	if !versionOK {
		layoutVersion = mapgen.DataVersion(line)
	}

	// Always read the settings, so they are shown again whatever the mode.
	var mapGen []byte
	var overrides map[string]any
	seed := MapGenText{Value: strings.TrimSpace(r.PostFormValue(mgSeed))}
	mismatch := f.PresetError.IsZero() && f.FilledFrom != f.Preset
	refill := stale || r.PostFormValue("reset") != "" || mismatch || !shown
	if refill {
		f.Refilled = kind == kindGenerate && shown && mismatch && !stale && f.Mode == modeSettings && r.PostFormValue("reset") == ""
		f.fill(layoutVersion, &seed)
		f.checkSeed()
		if f.Refilled {
			f.Tab = tabResources
		}
		mapGen, _ = buildMapGen(f.MapGen, rawOrEmpty(f.MapGen.Raw.Value))
	} else {
		var probs, msProbs []Problem
		f.MapGen, mapGen, probs = ParseMapGenForm(r, line, mapSpaceAge)
		f.MapGen.withPreset(NewMapGenFormForPreset(line, mapSpaceAge, f.Preset))
		f.MapSettings, overrides, msProbs = readMapSettings(layoutVersion, f.Preset, r.PostForm)
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
	f.MapGen.setOpen(r.PostFormValue("open_planet"))
	f.Modified = f.MapGen.Modified() || f.MapSettings.Modified()
	rules, _ := mapgen.AchievementRules(layoutVersion)
	f.MapGen.markAchievements(rules)
	if f.Reset || f.Refilled {
		f.clearErrors()
		f.reasons(layoutVersion, mapGen, nil, rules)
		return f, nw, false
	}
	if kind == kindRefresh {
		if mapGen == nil {
			mapGen, _ = buildMapGen(f.MapGen, rawOrEmpty(f.MapGen.Raw.Value))
		}
		f.reasons(layoutVersion, mapGen, overrides, rules)
		return f, nw, false
	}
	if !versionOK {
		f.reasons(layoutVersion, mapGen, overrides, rules)
		return f, nw, false
	}

	nw = world.NewWorld{Name: strings.TrimSpace(f.Name), GameVersion: f.Version, SpaceAge: f.SpaceAge}
	var seedValue *uint32
	switch f.Mode {
	case modeSettings:
		seedValue = s.settingsWorld(&f, &nw, mapGen, overrides)
	case modeExchange:
		var res mapexchange.Result
		if res, ok = decodeExchange(&f, f.Version); ok {
			nw.MapGenSettings, nw.MapSettings = res.MapGenSettings, res.MapSettings
			seedValue = s.settingsSeed(&f, res.MapGenSettings)
		}
	}
	f.reasons(f.Version, mapGen, overrides, rules)
	nw.Seed = seedValue
	if f.failed() || seedValue == nil {
		return f, nw, false
	}
	return f, nw, true
}

// settingsWorld sets how the world's map is made from the settings: an
// unchanged preset passes the preset alone and writes no file; any change
// writes both files complete and passes no preset, because the game lets a
// preset win over the files (SPEC 5.6). It returns the seed, filling a blank
// one in the form so the world matches the preview, or nil after a problem.
func (s *server) settingsWorld(f *GenerateForm, nw *world.NewWorld, mapGen []byte, overrides map[string]any) *uint32 {
	if len(f.SettingsProblems) > 0 || !f.PresetError.IsZero() {
		return nil
	}
	if f.Modified {
		switch {
		case !f.MapSettings.Untested:
			ms, err := mapgen.MapSettings(f.Version, f.Preset, overrides)
			if err != nil {
				f.FormError = i18n.M("worlds.generate.error.settings")
				return nil
			}
			nw.MapGenSettings, nw.MapSettings = mapGen, ms
		case f.Preset == defaultPreset:
			// The game's own map settings are the default preset's.
			nw.MapGenSettings = mapGen
		default:
			// Without the layout, the preset's map settings cannot be written,
			// and passing the preset would undo the changes.
			f.PresetError = i18n.M("worlds.generate.error.untested_preset", "version", f.Version)
			return nil
		}
	} else if f.Preset != defaultPreset {
		nw.Preset = f.Preset
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
func (f *GenerateForm) reasons(version string, mapGen []byte, overrides map[string]any, rules []mapgen.AchievementRule) {
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
