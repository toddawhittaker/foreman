package web

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/modsettings"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The mod settings editor (SPEC 9.6): the page, the form that loads after
// it, and the post that saves the changed values.
const (
	PathModSettingsEdit = "/worlds/{id}/mods/settings"
	PathModSettingsForm = "/worlds/{id}/mods/settings/form"
)

func modSettingsEditPath(id string) string { return modsPath(id) + "/settings" }

// modSettingDefs reads a world's setting definitions; tests replace it.
var modSettingDefs = (*game.Controller).ModSettingDefs

func (s *server) modSettingsRoutes() {
	s.mux.HandleFunc("GET "+PathModSettingsEdit, s.requireUser(s.handleModSettingsPage))
	// A definitions cache miss runs the game, which can outlast the usual
	// write deadline, and a large mod pack's form outgrows the usual body
	// limit.
	s.handleLong("GET "+PathModSettingsForm, s.requireUser(s.handleModSettingsForm))
	s.handleUpload("POST "+PathModSettingsEdit, MaxSettingsUpload, s.requireUser(s.handleModSettingsSave))
}

// Field kinds, one control each.
const (
	kindBool   = "bool"
	kindInt    = "int"
	kindDouble = "double"
	kindSelect = "select"
	kindText   = "text"
	kindColor  = "color"
	// kindLocked has one allowed value; kindFixed is a type Foreman does
	// not edit.
	kindLocked = "locked"
	kindFixed  = "fixed"
)

// SettingField is one setting as the form shows it.
type SettingField struct {
	ID string
	// Name is the setting's name; Label and Hint are its shown name and
	// description, with the language each is in when it is not the page's.
	Name                string
	Label, Hint         string
	LabelLang, HintLang string
	// RawName says Label is the setting's own name, which no mod translated.
	RawName bool
	Kind    string
	// Value is the control's value as the form posts it; Alpha a color's.
	Value, Alpha string
	Min, Max     string
	Options      []string
	Error        i18n.Msg

	def modsettings.Definition
}

// AlphaID is the id of a color's opacity field.
func (f SettingField) AlphaID() string { return f.ID + "-alpha" }

// Editable says the field is a control rather than a value to read.
func (f SettingField) Editable() bool { return f.Kind != kindLocked && f.Kind != kindFixed }

// Required says the control must hold a value: a number, or text whose
// setting does not allow blank (SPEC 17.13).
func (f SettingField) Required() bool {
	return f.Kind == kindInt || f.Kind == kindDouble || (f.Kind == kindText && !f.def.AllowBlank)
}

// InputMode is the keyboard a number field asks for: a number pad only
// when the value cannot be negative, since phone number pads have no minus.
func (f SettingField) InputMode() string {
	if f.def.Min == nil || minBelowZero(*f.def.Min) {
		return ""
	}
	if f.Kind == kindInt {
		return "numeric"
	}
	return "decimal"
}

func minBelowZero(v modsettings.Value) bool {
	return (v.Type == modsettings.TypeInt && v.Int < 0) || (v.Type == modsettings.TypeDouble && v.Double < 0)
}

// SettingGroup is one mod's settings.
type SettingGroup struct {
	Mod    string
	Fields []SettingField
}

// ModSettingsPage edits a world's mod settings (SPEC 9.6).
type ModSettingsPage struct {
	World world.Summary
	// Loaded says the form part is filled in; until then the page shows a
	// progress line and fetches it.
	Loaded  bool
	Startup []SettingGroup
	Map     []SettingGroup
	// NewMap says the world has no save yet, so its map settings seed the
	// new map and are edited here (plan R2).
	NewMap  bool
	PerUser bool
	// HasFile says the world has a mod-settings.dat to download.
	HasFile bool
	// Fingerprint and Seen are the mods folder and the file the form was
	// drawn from (SPEC 9.13).
	Fingerprint, Seen string
	Running           bool
	CountdownForm
	// RunError says why the settings could not be read; Changed that the
	// file changed after the page opened; NoChange that the post changed
	// nothing; Busy what else runs on the world.
	RunError  i18n.Msg
	Changed   bool
	NoChange  bool
	Busy      i18n.Msg
	ImageLink string
	// Fragment says the form part is answered on its own, so it tells
	// screen readers that it loaded (WCAG 4.1.3).
	Fragment bool
}

func (ModSettingsPage) templateName() string { return "modsettings" }
func (ModSettingsPage) navItem() string      { return PathWorlds }

// Tabs mark the Mods tab as the section the page belongs to.
func (p ModSettingsPage) Tabs() WorldTabs {
	t := worldTabs(p.World, "mods")
	t.Under = true
	return t
}

func (p ModSettingsPage) Path() string     { return modSettingsEditPath(p.World.ID) }
func (p ModSettingsPage) FormPath() string { return modSettingsEditPath(p.World.ID) + "/form" }
func (p ModSettingsPage) ModsPath() string { return modsPath(p.World.ID) }

// UploadPath opens the Mods tab's upload of mod-settings.dat.
func (p ModSettingsPage) UploadPath() string { return modsPath(p.World.ID) + "#mod-settings-replace" }
func (p ModSettingsPage) FilePath() string   { return modsPath(p.World.ID) + "/" + world.ModSettingsFile }
func (ModSettingsPage) SettingsFile() string { return world.ModSettingsFile }

// CanSave says the form has a control to change.
func (p ModSettingsPage) CanSave() bool {
	return hasEditable(p.Startup) || (p.NewMap && hasEditable(p.Map))
}

// HasRequired says a control is required, so the form explains the mark
// (DESIGN 7.2).
func (p ModSettingsPage) HasRequired() bool {
	for _, groups := range [][]SettingGroup{p.Startup, p.Map} {
		for _, g := range groups {
			if slices.ContainsFunc(g.Fields, SettingField.Required) {
				return true
			}
		}
	}
	return false
}

// Empty says the enabled mods have no setting to show.
func (p ModSettingsPage) Empty() bool { return len(p.Startup) == 0 && len(p.Map) == 0 }

func hasEditable(groups []SettingGroup) bool {
	for _, g := range groups {
		if slices.ContainsFunc(g.Fields, SettingField.Editable) {
			return true
		}
	}
	return false
}

func (p ModSettingsPage) hasErrors() bool { return len(p.Problems()) > 0 }

// Problems lists each refused value, then the countdown's.
func (p ModSettingsPage) Problems() []Problem {
	var out []Problem
	for _, groups := range [][]SettingGroup{p.Startup, p.Map} {
		for _, g := range groups {
			for _, f := range g.Fields {
				if !f.Error.IsZero() {
					out = append(out, Problem{f.ID, f.Error})
				}
			}
		}
	}
	return append(out, problems(Problem{"countdown", p.CountdownError})...)
}

// modSettingsWorld finds the world in the path when its mod settings can be
// edited, or answers the request itself.
func (s *server) modSettingsWorld(w http.ResponseWriter, r *http.Request, u store.User) (world.Summary, bool) {
	sum, ok := s.modsWorld(w, r, u)
	if ok && (sum.Newer || sum.Unreadable) {
		s.notFound(w, r, u)
		return world.Summary{}, false
	}
	return sum, ok
}

func (s *server) handleModSettingsPage(w http.ResponseWriter, r *http.Request, u store.User) {
	if sum, ok := s.modSettingsWorld(w, r, u); ok {
		s.page(w, r, http.StatusOK, u.Username, ModSettingsPage{World: sum})
	}
}

// handleModSettingsForm is the form part, which can take a game run. A
// failure of Foreman's own still answers with the form part, so the page
// offers the retry, the download and the upload.
func (s *server) handleModSettingsForm(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modSettingsWorld(w, r, u)
	if !ok {
		return
	}
	p, _, err := s.modSettingsPage(r, sum)
	if err != nil {
		if r.Context().Err() == nil {
			s.log.Error("read the mod settings", "world", sum.ID, "error", err)
		}
		p = ModSettingsPage{World: sum, Loaded: true, HasFile: p.HasFile, RunError: i18n.M("mods.settings.edit.run.failed")}
	}
	p.Fragment = true
	s.fragment(w, r, p, "modsettings-form")
}

// settingsState is what a post needs beyond the page: the world's file
// (or the defaults when it has none) and the defaults file, both decoded.
// defaults is nil when the run's defaults file cannot be read.
type settingsState struct {
	file, defaults *modsettings.File
}

// modSettingsPage reads the definitions and the world's file and fills
// the form. A failure the page can word is in RunError; err is Foreman's
// own fault.
func (s *server) modSettingsPage(r *http.Request, sum world.Summary) (ModSettingsPage, settingsState, error) {
	p := ModSettingsPage{World: sum, Loaded: true, CountdownForm: defaultCountdown()}
	p.Running = game.IsRunning(s.game.Status(sum.ID).State)
	p.HasFile = s.hasModSettings(sum.ID)
	var st settingsState
	var err error
	if p.Fingerprint, err = s.worlds.ModsFingerprint(sum.ID); err != nil {
		return p, st, err
	}
	if p.NewMap, err = s.makesNewMap(sum.ID); err != nil {
		return p, st, err
	}
	defs, err := modSettingDefs(s.game, r.Context(), sum.ID)
	if err != nil {
		// The error may hold text a mod wrote or a host path, so it goes to
		// the log only.
		var known bool
		p.RunError, known = modSettingsRunError(sum, err)
		switch {
		case r.Context().Err() != nil:
		case known:
			s.log.Warn("read the mod setting definitions", "world", sum.ID, "error", err)
		default:
			s.log.Error("read the mod setting definitions", "world", sum.ID, "error", err)
		}
		if errors.Is(err, game.ErrModSettingsImageMissing) {
			p.ImageLink = worldPath(sum.ID)
			if sum.Active {
				p.ImageLink = PathServer
			}
		}
		return p, st, nil
	}
	current, err := s.readModSettings(sum.ID)
	if err != nil {
		return p, st, err
	}
	sum256 := sha256.Sum256(current)
	p.Seen = hex.EncodeToString(sum256[:])
	// Only a missing entry reads the defaults; a bad defaults file leaves
	// the definition's own default.
	st.defaults, _ = modsettings.Decode(defs.Defaults)
	if current == nil {
		st.file = st.defaults
	} else if st.file, err = modsettings.Decode(current); err != nil {
		st.file = nil
	}
	if st.file == nil {
		s.log.Warn("decode mod-settings.dat", "world", sum.ID, "from_defaults", current == nil, "error", err)
		p.RunError = i18n.M("mods.settings.edit.unreadable")
		return p, st, nil
	}
	p.Startup, p.Map, p.PerUser = settingGroups(defs, st.file, st.defaults, localeOf(r).String())
	return p, st, nil
}

// makesNewMap reports whether the world's next start makes its map: it has
// new_map set and no save (SPEC 5.6, plan R2).
func (s *server) makesNewMap(id string) (bool, error) {
	root, err := s.worlds.OpenWorld(id)
	if err != nil {
		return false, err
	}
	defer func() { _ = root.Close() }()
	meta, err := world.ReadMeta(root)
	if err != nil || meta.NewMap == nil {
		return false, err
	}
	save, err := s.loadingSave(id)
	return save == "", err
}

// readModSettings is the world's mod-settings.dat, or nil when it has none.
func (s *server) readModSettings(id string) ([]byte, error) {
	root, err := s.worlds.OpenWorld(id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	b, err := world.ReadSmallFile(root, path.Join(world.ModsDir, world.ModSettingsFile), MaxSettingsUpload)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// modSettingsRunError words why the definitions could not be read; known
// is false for a cause the page has no words of its own for. Each leaves
// the file download and upload, which the page offers.
func modSettingsRunError(sum world.Summary, err error) (msg i18n.Msg, known bool) {
	switch {
	case errors.Is(err, game.ErrModSettingsImageMissing):
		return i18n.M("mods.settings.edit.run.no_image", "version", sum.GameVersion), true
	case errors.Is(err, game.ErrModSettingsOutOfMemory):
		return i18n.M("mods.settings.edit.run.oom"), true
	case errors.Is(err, game.ErrModSettingsTimeout):
		return i18n.M("mods.settings.edit.run.timeout"), true
	case errors.Is(err, game.ErrPreviewBusy):
		return i18n.M("mods.settings.edit.run.busy"), true
	case errors.Is(err, world.ErrBusy):
		return i18n.M("mods.settings.edit.run.world_busy"), true
	case errors.Is(err, world.ErrModsTooBig):
		return i18n.M("mods.settings.edit.run.too_big"), true
	case errors.Is(err, game.ErrModSettingsBadOutput):
		return i18n.M("mods.settings.edit.run.bad_output"), true
	}
	return i18n.M("mods.settings.edit.run.failed"), false
}

// settingGroups turns the definitions into the form's startup and map
// groups, by mod, then order, then name. Hidden settings are left out;
// per-player ones only say whether there are any.
func settingGroups(defs *game.ModSettingDefs, file, defaults *modsettings.File, lang string) (startup, mapGroups []SettingGroup, perUser bool) {
	list := slices.Clone(defs.Defs)
	slices.SortStableFunc(list, func(a, b modsettings.Definition) int {
		return cmp.Or(cmp.Compare(a.Mod, b.Mod), cmp.Compare(a.Order, b.Order), cmp.Compare(a.Name, b.Name))
	})
	add := func(groups []SettingGroup, f SettingField) []SettingGroup {
		if n := len(groups); n > 0 && groups[n-1].Mod == f.def.Mod {
			groups[n-1].Fields = append(groups[n-1].Fields, f)
			return groups
		}
		return append(groups, SettingGroup{Mod: f.def.Mod, Fields: []SettingField{f}})
	}
	for _, d := range list {
		if d.Hidden {
			continue
		}
		switch d.Setting {
		case modsettings.Startup:
			startup = add(startup, newField(d, file, defaults, defs.Locale, lang))
		case modsettings.RuntimeGlobal:
			mapGroups = add(mapGroups, newField(d, file, defaults, defs.Locale, lang))
		case modsettings.RuntimePerUser:
			perUser = true
		}
	}
	return startup, mapGroups, perUser
}

// settingFieldID is a setting's field id and form name; the name is hashed so any
// setting name makes a valid id.
func settingFieldID(d modsettings.Definition) string {
	sum := sha256.Sum256([]byte(string(d.Setting) + "\x00" + d.Name))
	return "ms-" + hex.EncodeToString(sum[:6])
}

func newField(d modsettings.Definition, file, defaults *modsettings.File, loc modsettings.Locale, lang string) SettingField {
	f := SettingField{ID: settingFieldID(d), Name: d.Name, def: d}
	f.Label, f.LabelLang = localeText(loc, lang, d.Name, func(t modsettings.LocaleText) string { return t.Name })
	if f.Label == "" {
		f.Label, f.RawName = d.Name, true
	}
	f.Hint, f.HintLang = localeText(loc, lang, d.Name, func(t modsettings.LocaleText) string { return t.Description })
	v, ok := currentValue(d, file, defaults)
	// Factorio 1.1 stores an int setting as a double.
	if ok && d.Type == modsettings.TypeInt && v.Type == modsettings.TypeDouble {
		v = modsettings.Value{Type: modsettings.TypeInt, Int: int64(math.Round(v.Double))}
	}
	switch {
	case !d.Editable:
		f.Kind = kindFixed
		return f
	case len(d.AllowedValues) == 1:
		f.Kind = kindLocked
	case len(d.AllowedValues) > 1:
		f.Kind = kindSelect
		for _, a := range d.AllowedValues {
			f.Options = append(f.Options, formatValue(a))
		}
	default:
		f.Kind = map[modsettings.ValueType]string{
			modsettings.TypeBool: kindBool, modsettings.TypeInt: kindInt, modsettings.TypeDouble: kindDouble,
			modsettings.TypeString: kindText, modsettings.TypeColor: kindColor,
		}[d.Type]
		if f.Kind == "" {
			f.Kind = kindFixed
			return f
		}
	}
	if d.Min != nil {
		f.Min = formatValue(*d.Min)
	}
	if d.Max != nil {
		f.Max = formatValue(*d.Max)
	}
	if !ok {
		if f.Kind != kindLocked {
			// Nothing to show or keep: not offered for editing.
			f.Kind = kindFixed
			return f
		}
		v = d.AllowedValues[0]
	}
	if d.Type == modsettings.TypeColor {
		f.Value, f.Alpha = colorHex(v.Color), formatFloat(scaleColor(v.Color).A)
	} else {
		f.Value = formatValue(v)
	}
	return f
}

// localeText picks a setting's text in the page's language, else English,
// and says the language when it is not the page's (SPEC 22.3).
func localeText(loc modsettings.Locale, lang, name string, pick func(modsettings.LocaleText) string) (text, textLang string) {
	if t := pick(loc[lang][name]); t != "" {
		return t, ""
	}
	if lang != "en" {
		if t := pick(loc["en"][name]); t != "" {
			return t, "en"
		}
	}
	return "", ""
}

// currentValue is the setting's value in the world's file, else in the
// defaults file, else the definition's default.
func currentValue(d modsettings.Definition, file, defaults *modsettings.File) (modsettings.Value, bool) {
	for _, f := range []*modsettings.File{file, defaults} {
		if f == nil {
			continue
		}
		if v, ok, err := f.Get(d.Setting, d.Name); err == nil && ok {
			return v, true
		}
	}
	if d.Default != nil {
		return *d.Default, true
	}
	return modsettings.Value{}, false
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// formatValue writes a value as its control posts it.
func formatValue(v modsettings.Value) string {
	switch v.Type {
	case modsettings.TypeBool:
		return strconv.FormatBool(v.Bool)
	case modsettings.TypeInt:
		return strconv.FormatInt(v.Int, 10)
	case modsettings.TypeDouble:
		return formatFloat(v.Double)
	case modsettings.TypeString:
		return v.String
	case modsettings.TypeColor:
		return colorHex(v.Color)
	}
	return ""
}

// scaleColor reads a color the game wrote in 0 to 255 as 0 to 1, as the
// game does when any component is above 1.
func scaleColor(c modsettings.Color) modsettings.Color {
	if c.R > 1 || c.G > 1 || c.B > 1 || c.A > 1 {
		return modsettings.Color{R: c.R / 255, G: c.G / 255, B: c.B / 255, A: c.A / 255}
	}
	return c
}

// colorHex is a color's #rrggbb, for the color input.
func colorHex(c modsettings.Color) string {
	c = scaleColor(c)
	byteOf := func(f float64) int {
		if math.IsNaN(f) {
			return 0
		}
		return int(math.Round(math.Min(math.Max(f, 0), 1) * 255))
	}
	return fmt.Sprintf("#%02x%02x%02x", byteOf(c.R), byteOf(c.G), byteOf(c.B))
}

// colorRaw turns the color input and its opacity into the four
// components Validate reads; ok is false for a color that is not #rrggbb.
func colorRaw(hexColor, alpha string) (string, bool) {
	b, err := hex.DecodeString(strings.TrimPrefix(hexColor, "#"))
	if err != nil || len(b) != 3 || !strings.HasPrefix(hexColor, "#") {
		return "", false
	}
	parts := []string{}
	for _, c := range b {
		parts = append(parts, formatFloat(float64(c)/255))
	}
	return strings.Join(append(parts, strings.TrimSpace(alpha)), ","), true
}

// settingChange is one value the post changes.
type settingChange struct {
	def   modsettings.Definition
	value modsettings.Value
}

// handleModSettingsSave writes the changed settings as one mod change
// (SPEC 9.6, 9.13): on a running world the countdown, stop, snapshot,
// write, check and start; on a stopped one the snapshot and the write.
func (s *server) handleModSettingsSave(w http.ResponseWriter, r *http.Request, u store.User) {
	sum, ok := s.modSettingsWorld(w, r, u)
	if !ok {
		return
	}
	if !s.readForm(w, r) {
		return
	}
	countdown, stop := readCountdown(r)
	p, st, err := s.modSettingsPage(r, sum)
	if err != nil {
		s.fail(w, r, "read the mod settings", err)
		return
	}
	if !p.RunError.IsZero() {
		s.page(w, r, http.StatusBadGateway, u.Username, p)
		return
	}
	// The form was drawn from other files, or said what saving does for a
	// game that has since started or stopped, or showed the map settings
	// otherwise (SPEC 17.8): show the current form, with nothing kept from
	// the post. A form without the flags counts as stopped and saved.
	if r.PostFormValue("fingerprint") != p.Fingerprint || r.PostFormValue("seen") != p.Seen ||
		(r.PostFormValue("running") == "1") != p.Running || (r.PostFormValue("map") == "1") != p.NewMap {
		p.Changed = true
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	mapToo := p.NewMap
	var changes []settingChange
	read := func(groups []SettingGroup) {
		for gi := range groups {
			for fi := range groups[gi].Fields {
				if c, ok := readField(r, &groups[gi].Fields[fi]); ok {
					changes = append(changes, c)
				}
			}
		}
	}
	read(p.Startup)
	if mapToo {
		read(p.Map)
	}
	p.Countdown = countdown.Countdown
	if p.Running {
		p.CountdownError = countdown.CountdownError
	}
	if p.hasErrors() {
		s.page(w, r, http.StatusUnprocessableEntity, u.Username, p)
		return
	}
	if len(changes) == 0 {
		p.NoChange = true
		s.page(w, r, http.StatusOK, u.Username, p)
		return
	}
	if busy := s.worldBusy(sum.ID); !busy.IsZero() {
		p.Busy = busy
		s.page(w, r, http.StatusConflict, u.Username, p)
		return
	}
	data, err := encodeChanges(st, changes)
	if err != nil {
		s.fail(w, r, "write the changed mod settings", err)
		return
	}
	names := make([]string, len(changes))
	for i, c := range changes {
		names[i] = c.def.Name
	}
	summary := "edit mod settings: " + strings.Join(names, ", ")
	if len(summary) > maxSummary {
		summary = summary[:maxSummary] + "…"
	}
	op := game.ModOp{Change: world.ModChange{Settings: data, SettingsSeen: p.Seen}, Fingerprint: p.Fingerprint, Summary: summary}
	s.applyMods(w, r, u, sum, op, stop, "", modResult{Retry: p.Path()})
}

// readField reads one control from the post. A value the same as the
// form showed is untouched and never checked (plan R9). A refused value
// stays in the field with its error.
func readField(r *http.Request, f *SettingField) (settingChange, bool) {
	if !f.Editable() {
		return settingChange{}, false
	}
	var raw string
	switch f.Kind {
	case kindBool:
		// An unticked box posts nothing.
		raw = strconv.FormatBool(r.PostFormValue(f.ID) == "true")
		if raw == f.Value {
			return settingChange{}, false
		}
		f.Value = raw
	case kindColor:
		hexColor, alpha := strings.ToLower(r.PostFormValue(f.ID)), r.PostFormValue(f.AlphaID())
		if hexColor == f.Value && alpha == f.Alpha {
			return settingChange{}, false
		}
		f.Value, f.Alpha = hexColor, alpha
		var ok bool
		if raw, ok = colorRaw(hexColor, decimalPoint(kindDouble, strings.TrimSpace(alpha))); !ok {
			f.Error = fieldError(f, &modsettings.ValidationError{MessageID: "color"})
			return settingChange{}, false
		}
	default:
		raw = r.PostFormValue(f.ID)
		// A text input drops line breaks from its value, so a value that
		// holds one posts without it untouched.
		if raw == f.Value || (f.Kind == kindText && raw == stripLineBreaks(f.Value)) {
			return settingChange{}, false
		}
		f.Value = raw
	}
	v, verr := modsettings.Validate(f.def, decimalPoint(f.Kind, raw))
	if verr != nil {
		f.Error = fieldError(f, verr)
		return settingChange{}, false
	}
	return settingChange{def: f.def, value: v}, true
}

// decimalPoint reads a decimal comma, as many languages write one, as the
// point the game's numbers use: "2,5" is 2.5. Whole numbers and text are
// left as typed.
func decimalPoint(kind, raw string) string {
	if kind == kindDouble && strings.Count(raw, ",") == 1 && !strings.Contains(raw, ".") {
		return strings.Replace(raw, ",", ".", 1)
	}
	return raw
}

func stripLineBreaks(s string) string { return strings.NewReplacer("\r", "", "\n", "").Replace(s) }

// fieldError words a refused value with the setting's shown name. The
// message ids are listed one by one so the catalogue check sees each.
func fieldError(f *SettingField, e *modsettings.ValidationError) i18n.Msg {
	var args []any
	for k, v := range e.Params {
		args = append(args, k, v)
	}
	// The name keeps its language and its no-translate mark (SPEC 22.3).
	var name any = f.Label
	switch {
	case f.RawName:
		name = i18n.Ident(f.Label)
	case f.LabelLang != "":
		name = i18n.Game(f.Label)
	}
	args = append(args, "setting", name)
	switch strings.TrimPrefix(e.MessageID, "mods.settings.edit.err.") {
	case "range":
		_, hasMin := e.Params["min"]
		_, hasMax := e.Params["max"]
		switch {
		case hasMin && !hasMax:
			return i18n.M("mods.settings.edit.err.range_min", args...)
		case hasMax && !hasMin:
			return i18n.M("mods.settings.edit.err.range_max", args...)
		case !hasMin && !hasMax:
			// Beyond what the file can hold, with no limit of the mod's own.
			return i18n.M("mods.settings.edit.err.number", args...)
		}
		return i18n.M("mods.settings.edit.err.range", args...)
	case "whole":
		return i18n.M("mods.settings.edit.err.whole", args...)
	case "number":
		return i18n.M("mods.settings.edit.err.number", args...)
	case "allowed":
		return i18n.M("mods.settings.edit.err.allowed", args...)
	case "blank":
		return i18n.M("mods.settings.edit.err.blank", args...)
	case "long":
		return i18n.M("mods.settings.edit.err.long", args...)
	case "text":
		return i18n.M("mods.settings.edit.err.text", args...)
	case "color":
		return i18n.M("mods.settings.edit.err.color", args...)
	case "bool":
		return i18n.M("mods.settings.edit.err.bool", args...)
	case "locked":
		return i18n.M("mods.settings.edit.err.locked", args...)
	case "hidden":
		return i18n.M("mods.settings.edit.err.hidden", args...)
	}
	return i18n.M("mods.settings.edit.err.other", args...)
}

// encodeChanges sets each change in the world's file, or in the defaults
// file when the world has none, and keeps every other byte (plan R8).
func encodeChanges(st settingsState, changes []settingChange) ([]byte, error) {
	for _, c := range changes {
		if err := st.file.Set(c.def.Setting, c.def.Name, c.value, st.defaults); err != nil {
			return nil, fmt.Errorf("set %s: %w", c.def.Name, err)
		}
	}
	return st.file.Encode()
}

// EditSettingsPath is the mod settings editor, linked first in the Mods
// tab's Mod settings panel.
func (p ModsPage) EditSettingsPath() string { return modSettingsEditPath(p.World.ID) }
