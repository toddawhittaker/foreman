package web

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// newMapWaitQuery asks for the whole page at once: the server waits for the
// world's planet list first. Without script the page is reached this way;
// with script the form part is loaded from it (SPEC 5.6, 17.19).
const newMapWaitQuery = "wait"

// newMapSavedQuery marks the page after a save, for its Done message.
const newMapSavedQuery = "saved"

// NewMapPage is step 3 of the guided Generate: the map settings and preview
// of a world whose map is not made yet, laid out from the planets its mods
// add (SPEC 5.6).
type NewMapPage struct {
	World world.Summary
	// Why says this world's map settings cannot be set here, and the page
	// shows only that.
	Why i18n.Msg
	// Loading is the page without its form, which the script loads after it.
	Loading bool
	Form    NewMapForm
	// CanPreview is true when this Foreman can make map previews.
	CanPreview bool
	Preview    *PreviewView
	// Builtin says why the built-in planets are used instead of the world's
	// own list; zero when the list is the world's (SPEC 5.6, 14.6).
	Builtin i18n.Msg
	// Gone lists the controls the saved settings name that the world's mods
	// no longer add; the next save leaves them out (SPEC 5.6).
	Gone []string
	// SaveError says why the save was refused; Done follows a save.
	SaveError i18n.Msg
	Done      bool
	// SaveLinks are the links that do what SaveError says to (SPEC 17.9).
	SaveLinks []NewMapLink
	// Unsaved says the form holds settings the world does not have yet,
	// though the page loaded with them, as after a fill (SPEC 5.6).
	Unsaved bool
}

// NewMapLink is a link a message offers, to do what it says (SPEC 17.9).
type NewMapLink struct {
	ID, Path string
	Label    i18n.Msg
}

// msgExchangeOtherLine refuses a map exchange string of another game line,
// telling the person to change the world's version (SPEC 5.6).
const msgExchangeOtherLine = "newmap.error.exchange_other_line"

// ExchangeOtherLine is true when the exchange string was of another game
// line, so the error offers the world's page, where its version changes.
func (p NewMapPage) ExchangeOtherLine() bool { return p.Form.ExchangeError.Key == msgExchangeOtherLine }

// BuiltinFailed is true when the world's planets could not be read at all,
// or because a mod is named like Foreman's helper, so the warning offers its
// Mods tab.
func (p NewMapPage) BuiltinFailed() bool {
	return p.Builtin.Key == "newmap.builtin.failed" || p.Builtin.Key == "mapgen.preview.control_mod_taken"
}

// ModsPath is the world's Mods tab, step 2.
func (p NewMapPage) ModsPath() string { return modsPath(p.World.ID) }

// newMapSaveIdle checks the world's server is down before a save; tests
// replace it. A mode without server control runs no server.
var newMapSaveIdle = func(s *server, id string) error {
	if s.game == nil {
		return nil
	}
	return s.newMapIdle(id)
}

// NewMapForm is the map part of step 3, with the world it is for.
type NewMapForm struct {
	MapForm
	ID       string
	Name     string
	Version  string
	SpaceAge bool
}

// RefreshPath is where step 3's settings are redrawn.
func (f NewMapForm) RefreshPath() string { return newMapPath(f.ID) + "/refresh" }

func (NewMapPage) templateName() string { return "newmap" }
func (NewMapPage) navItem() string      { return PathWorlds }

func (p NewMapPage) hasErrors() bool { return len(p.Problems()) > 0 }

// Problems lists the form's errors in the order the fields appear.
func (p NewMapPage) Problems() []Problem {
	return append(p.Form.Problems(), problems(Problem{"newmap-save", p.SaveError})...)
}

// Paths the page uses.
func (p NewMapPage) Path() string      { return newMapPath(p.World.ID) }
func (p NewMapPage) WaitPath() string  { return newMapPath(p.World.ID) + "?" + newMapWaitQuery + "=1" }
func (p NewMapPage) WorldPath() string { return worldPath(p.World.ID) }
func (NewMapPage) PreviewPath() string { return PathGeneratePreview }
func (p NewMapPage) Steps() []FlowStep { return flowSteps(p.World.ID, flowStepMap) }
func (p NewMapPage) ShowForm() bool    { return p.Why.IsZero() && !p.Loading }
func (p NewMapPage) DoneMsg() i18n.Msg { return i18n.M("newmap.done", "name", p.World.Name) }

// The steps of the guided Generate (SPEC 5.6).
const (
	flowStepWorld = iota + 1
	flowStepMods
	flowStepMap
)

// FlowStep is one step of the guided Generate's step bar, a link to it, or
// plain text when Path is empty because the step cannot be opened yet.
type FlowStep struct {
	Number  int
	Path    string
	Label   i18n.Msg
	Current bool
}

// flowSteps is the step bar of world id, with current marked. With no world
// yet (step 1), step 1 is Generate's page and steps 2 and 3 have no path.
func flowSteps(id string, current int) []FlowStep {
	steps := []FlowStep{
		{flowStepWorld, PathGenerate, i18n.M("newmap.steps.world"), false},
		{flowStepMods, "", i18n.M("newmap.steps.mods"), false},
		{flowStepMap, "", i18n.M("newmap.steps.map"), false},
	}
	if id != "" {
		steps[0].Path, steps[1].Path, steps[2].Path = worldPath(id), modsPath(id), newMapPath(id)
	}
	for i := range steps {
		steps[i].Current = steps[i].Number == current
	}
	return steps
}

// newMapRoutes registers step 3. The preview endpoints stay Generate's.
func (s *server) newMapRoutes() {
	s.handleLong("GET "+PathNewMap, s.requireUser(s.handleNewMap))
	s.handleUpload("POST "+PathNewMap, fixedLimit(generateFormLimit), s.requireUser(s.handleNewMapSave))
	s.handleUpload("POST "+PathNewMap+"/refresh", fixedLimit(generateFormLimit), s.requireUser(s.handleNewMapRefresh))
}

// newMapWorld reads the world of a step 3 request, and the page that says
// why its map cannot be set, if it cannot: only an Unfinished world, one
// with new_map and no save, has step 3 (SPEC 5.6).
func (s *server) newMapWorld(w http.ResponseWriter, r *http.Request, u store.User) (NewMapPage, bool) {
	sum, ok := s.findWorld(w, r, u)
	if !ok {
		return NewMapPage{}, false
	}
	p := NewMapPage{World: sum}
	_, lineOK := mapgen.Line(sum.GameVersion)
	switch {
	case sum.Unreadable:
		p.Why = i18n.M("newmap.why.unreadable")
	case sum.Newer:
		p.Why = i18n.M("newmap.why.newer")
	case sum.Archived:
		p.Why = i18n.M("newmap.why.archived")
	case !sum.Unfinished:
		p.Why = i18n.M("newmap.why.made")
	case !lineOK:
		p.Why = i18n.M("newmap.why.version", "version", sum.GameVersion)
	}
	if !p.Why.IsZero() {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return p, false
	}
	return p, true
}

// catalogue is the world's planets and controls, and why the built-in ones
// are used when the world's own list cannot be (SPEC 5.6, 14.6). It never
// fails: the planet list comes only from the world's mods, never from a form.
func (s *server) catalogue(ctx context.Context, r *http.Request, sum world.Summary) (mapgen.Catalogue, i18n.Msg) {
	cat, err := worldCatalogue(s, ctx, sum, localeOf(r))
	if err == nil {
		return cat, i18n.Msg{}
	}
	// The reason may hold text a mod wrote, so it goes to the log only.
	s.log.Warn("read the world's planets", "world", sum.ID, "error", err)
	switch {
	case errors.Is(err, game.ErrControlModTaken):
		// A world mod named like Foreman's helper: it stays so until that mod
		// goes (SPEC 5.6, ADR 0033).
		return cat, i18n.M("mapgen.preview.control_mod_taken")
	case errors.Is(err, game.ErrPlanetsRefused):
		return cat, i18n.M("newmap.builtin.refused")
	case errors.Is(err, game.ErrPlanetsMissing):
		return cat, i18n.M("newmap.builtin.missing")
	}
	return cat, i18n.M("newmap.builtin.failed")
}

// newMapInput is what step 3's map form is laid out for.
func newMapInput(sum world.Summary, cat mapgen.Catalogue) mapInput {
	line, _ := mapgen.Line(sum.GameVersion)
	return mapInput{Line: line, Cat: cat, MapKey: mapKey(line, cat.SpaceAge), Version: sum.GameVersion}
}

// handleNewMap shows step 3. Without wait the page comes at once, and its
// script loads the form, which waits for the world's planet list; without
// script the page reloads itself with wait (SPEC 5.6, 17.19).
func (s *server) handleNewMap(w http.ResponseWriter, r *http.Request, u store.User) {
	p, ok := s.newMapWorld(w, r, u)
	if !ok {
		return
	}
	q := r.URL.Query()
	p.Done = q.Get(newMapSavedQuery) != ""
	if q.Get(newMapWaitQuery) == "" && !p.Done {
		p.Loading = true
		p.CanPreview = s.previewer != nil
		s.page(w, r, http.StatusOK, u.Username, p)
		return
	}
	cat, why := s.catalogue(r.Context(), r, p.World)
	p.Builtin = why
	in := newMapInput(p.World, cat)
	f := MapForm{cat: cat, Mode: modeSettings, Preset: defaultPreset, Line: in.Line,
		Presets: mapgen.Presets(in.Line), MapKey: in.MapKey, Tab: tabResources}
	seed := MapGenText{}
	meta, err := s.readWorldMeta(p.World.ID)
	if err != nil {
		s.fail(w, r, "read the world's new map", err)
		return
	}
	if nm := meta.NewMap; nm != nil {
		if slices.Contains(f.Presets, nm.Preset) {
			f.Preset = nm.Preset
		}
		if nm.Seed != nil {
			seed.Value = strconv.FormatUint(uint64(*nm.Seed), 10)
		}
	}
	f.fill(in.Version, &seed)
	files := world.FavoriteFiles{}
	for _, name := range []string{world.MapGenSettingsFile, world.MapSettingsFile} {
		c, err := s.worlds.ReadConfig(p.World.ID, name)
		if err != nil {
			s.fail(w, r, "read the world's map settings", err)
			return
		}
		if name == world.MapGenSettingsFile {
			files.MapGenSettings = c.Data
		} else {
			files.MapSettings = c.Data
		}
	}
	if len(files.MapGenSettings) > 0 || len(files.MapSettings) > 0 {
		// A control the mods no longer add would ride along in the raw box,
		// so it is taken out before the form is filled (SPEC 5.6).
		p.Gone = mapgen.StaleControls(files.MapGenSettings, cat)
		files.MapGenSettings = withoutControls(files.MapGenSettings, p.Gone)
		// The files are laid over the preset; damaged ones leave the preset.
		g := GenerateForm{Version: in.Version, MapForm: f}
		g.fillFromFiles(files)
		if g.FavoriteError.IsZero() {
			f = g.MapForm
			if f.MapGen.Seed.Value == "" {
				f.MapGen.Seed.Value = seed.Value
			}
		}
	}
	rules, _ := mapgen.AchievementRules(in.Version)
	f.MapGen.markAchievements(rules)
	f.reasons(in.Version, nil, nil, rules)
	p.Form = newMapForm(p.World, f)
	p.CanPreview = s.previewer != nil
	s.page(w, r, http.StatusOK, u.Username, p)
}

// withoutControls is a map-gen-settings.json without the named autoplace
// controls, its other members in their order; a file it cannot read comes
// back as it is.
func withoutControls(mapGen []byte, names []string) []byte {
	if len(names) == 0 {
		return mapGen
	}
	top, ok := orderedMembers(mapGen)
	if !ok {
		return mapGen
	}
	for i, m := range top {
		if m.key != "autoplace_controls" {
			continue
		}
		controls, ok := orderedMembers(m.val)
		if !ok {
			return mapGen
		}
		controls = slices.DeleteFunc(controls, func(c jsonMember) bool { return slices.Contains(names, c.key) })
		top[i].val = writeMembers(controls)
	}
	return writeMembers(top)
}

// newMapForm wraps a map form with the world it is for.
func newMapForm(sum world.Summary, f MapForm) NewMapForm {
	return NewMapForm{MapForm: f, ID: sum.ID, Name: sum.Name, Version: sum.GameVersion, SpaceAge: f.cat.SpaceAge}
}

// readNewMap reads a posted step 3 form with the world's own catalogue.
func (s *server) readNewMap(r *http.Request, p *NewMapPage, kind readKind) (world.NewMapFiles, bool) {
	cat, why := s.catalogue(r.Context(), r, p.World)
	p.Builtin = why
	in := newMapInput(p.World, cat)
	// The settings are stale when laid out for another line or Space Age setting.
	in.Stale = r.PostForm.Get(settingsShown) != "" && r.PostForm.Get("map_key") != in.MapKey
	f, files, ok := readMapForm(r.PostForm, kind, in)
	p.Form = newMapForm(p.World, f)
	p.CanPreview = s.previewer != nil
	return files, ok
}

// handleNewMapRefresh redraws step 3's settings for a changed preset or the
// reset; the browser swaps in only the parts that change.
func (s *server) handleNewMapRefresh(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	p, ok := s.newMapWorld(w, r, u)
	if !ok {
		return
	}
	s.readNewMap(r, &p, kindRefresh)
	p.Form.clearErrors()
	status := http.StatusOK
	if r.PostForm.Get("fill") != "" {
		p.Unsaved = true
		if !fillFromExchange(&p.Form) {
			status = http.StatusBadRequest
		}
	}
	s.page(w, r, status, u.Username, p)
}

// fillFromExchange fills the settings from the posted map exchange string,
// which must be of the world's game line (SPEC 5.6). The string itself is
// not kept: the settings it gave are what is saved.
func fillFromExchange(f *NewMapForm) bool {
	res, ok := decodeExchange(&f.MapForm, f.Version, func(made, game string) i18n.Msg {
		return i18n.M("newmap.error.exchange_other_line", "version", made, "game", game)
	})
	if !ok {
		return false
	}
	g := GenerateForm{Version: f.Version, MapForm: f.MapForm}
	g.fillFromFiles(world.FavoriteFiles{MapGenSettings: res.MapGenSettings, MapSettings: res.MapSettings})
	if !g.FavoriteError.IsZero() {
		f.ExchangeError = i18n.M("worlds.generate.error.exchange_unreadable")
		return false
	}
	f.MapForm = g.MapForm
	f.Exchange = ""
	rules, _ := mapgen.AchievementRules(f.Version)
	f.MapGen.markAchievements(rules)
	mapGen, _ := buildMapGen(f.MapGen, rawOrEmpty(f.MapGen.Raw.Value))
	f.reasons(f.Version, mapGen, nil, rules)
	return true
}

// handleNewMapSave saves step 3's map settings (SPEC 5.6). It takes no
// snapshot, as the world has no save yet (SPEC 11.3). A running or starting
// server is refused: its first start makes the map from what it read.
func (s *server) handleNewMapSave(w http.ResponseWriter, r *http.Request, u store.User) {
	if !s.readForm(w, r) {
		return
	}
	p, ok := s.newMapWorld(w, r, u)
	if !ok {
		return
	}
	files, ok := s.readNewMap(r, &p, kindGenerate)
	if !ok {
		status := http.StatusBadRequest
		if p.Form.Reset || p.Form.Refilled {
			status = http.StatusOK
		}
		s.page(w, r, status, u.Username, p)
		return
	}
	if !p.Builtin.IsZero() {
		// The world's own planets could not be read, so saving the built-in
		// ones would drop every setting of its mods' planets (SPEC 5.6).
		p.SaveError = i18n.M("newmap.error.builtin")
		p.SaveLinks = []NewMapLink{
			{ID: "newmap-retry", Path: newMapPath(p.World.ID), Label: i18n.M("newmap.link.retry")},
			{ID: "newmap-to-mods", Path: modsPath(p.World.ID), Label: i18n.M("newmap.link.mods")},
		}
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	// Checked under the world's lock, so a start cannot slip in between.
	id := p.World.ID
	files.Idle = func() error { return newMapSaveIdle(s, id) }
	err := s.worlds.SetNewMap(r.Context(), p.World.ID, files)
	switch {
	case err == nil:
	case errors.Is(err, errNewMapServerUp):
		p.SaveError = i18n.M("newmap.error.running")
		p.SaveLinks = []NewMapLink{{ID: "newmap-to-server", Path: PathServer, Label: i18n.M("newmap.link.server")}}
	case errors.Is(err, world.ErrBusy):
		p.SaveError = i18n.M("newmap.error.busy")
	case errors.Is(err, world.ErrMapMade), errors.Is(err, world.ErrNotNewMap):
		p.SaveError = i18n.M("newmap.why.made")
	case errors.Is(err, world.ErrWorldArchived):
		p.SaveError = i18n.M("newmap.why.archived")
	case errors.Is(err, world.ErrNewerFormat):
		p.SaveError = i18n.M("newmap.why.newer")
	default:
		s.fail(w, r, "save the world's map settings", err)
		return
	}
	if !p.SaveError.IsZero() {
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	s.audit(r.Context(), actor(u), AuditNewMap, p.World.ID, newMapDetail(p.Form.MapForm, p.World.GameVersion))
	http.Redirect(w, r, newMapPath(p.World.ID)+"?"+newMapSavedQuery+"=1", http.StatusSeeOther) //nolint:gosec // the id is a world Foreman found, a local path segment
}
