package web

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// The map-gen form shown inside Generate (SPEC 5.6, 11.5; DESIGN 8.7). It
// holds the choices the game's map generator offers as plain fields and
// turns a post into the map-gen-settings.json a new world is made from. The
// raw JSON box covers every other key.
//
// Field names start with "mg." so the form can sit in a page that has fields
// of its own: "mg.width", "mg.height", "mg.start", "mg.peaceful",
// "mg.no_enemies", "mg.raw", and "mg.<planet>.<control>.<setting>" for each
// autoplace control. The seed is the page's own "seed" field, shown above the
// tabs.

// Names of the form's fixed fields.
const (
	mgSeed      = "seed"
	mgWidth     = "mg.width"
	mgHeight    = "mg.height"
	mgStart     = "mg.start"
	mgPeaceful  = "mg.peaceful"
	mgNoEnemies = "mg.no_enemies"
	mgRaw       = "mg.raw"
	mgPrefix    = "mg."
	// Nauvis's map type, and the moisture and terrain type scales.
	mgMapType       = "mg.map_type"
	mgMoistureScale = "mg.moisture.scale"
	mgAuxScale      = "mg.aux.scale"
	mgMoistureBias  = "mg.moisture.bias"
	mgAuxBias       = "mg.aux.bias"
	// mgOn ends the name of a resource's on/off box.
	mgOn = "on"
)

// IDs of the fixed fields, for the error summary's links.
const (
	mgSeedID   = "generate-seed"
	mgWidthID  = "mg-width"
	mgHeightID = "mg-height"
	mgStartID  = "mg-start"
	mgRawID    = "mg-raw"
)

// The seed, width and height are 32-bit whole numbers in the game.
const mgMaxNumber = math.MaxUint32

// mgDefaultStep is the step that means "as the game ships it".
const mgDefaultStep = "100"

// A step is one choice in a frequency, size, richness or starting-area
// list. The game's sliders offer these multipliers; the code is the
// percentage the list shows and is what the form posts.
type mgStep struct {
	code  string
	value float64
}

var mgSteps = []mgStep{
	{"17", 1.0 / 6}, {"25", 0.25}, {"33", 1.0 / 3}, {"50", 0.5}, {"71", math.Sqrt2 / 2},
	{"100", 1}, {"141", math.Sqrt2}, {"200", 2}, {"300", 3}, {"400", 4}, {"600", 6},
}

// mgExtraSteps are values a preset sets that the list does not offer (rail
// world's water coverage, ribbon world's cliff continuity). A select shows one
// only when it holds it, so opening the form never changes a preset's values
// (DESIGN 8.7).
var mgExtraSteps = []mgStep{{"75", 0.75}, {"150", 1.5}, {"133", 4.0 / 3}}

const mgNone = "0"

// MapGenOption is one entry of a select.
type MapGenOption struct {
	Value string
	Label i18n.Msg
}

// stepOptions are the entries of a select of steps. The game refuses a
// frequency of zero when it makes the map, so only sizes, richness and the
// starting area offer "None".
func stepOptions(none bool) []MapGenOption {
	var out []MapGenOption
	if none {
		out = append(out, MapGenOption{mgNone, i18n.M("mapgen.step.none")})
	}
	for _, s := range mgSteps {
		n, _ := strconv.Atoi(s.code)
		label := i18n.M("mapgen.step.percent", "percent", n)
		if s.code == mgDefaultStep {
			label = i18n.M("mapgen.step.percent_default", "percent", n)
		}
		out = append(out, MapGenOption{s.code, label})
	}
	return out
}

var (
	optionsFrequency = stepOptions(false)
	optionsWithNone  = stepOptions(true)
)

// stepValue is the multiplier a posted code stands for.
func stepValue(code string) (float64, bool) {
	if code == mgNone {
		return 0, true
	}
	for _, s := range slices.Concat(mgSteps, mgExtraSteps) {
		if s.code == code {
			return s.value, true
		}
	}
	return 0, false
}

// stepCode is the code of the step a game value stands for, to a tolerance
// that covers the game's single-precision numbers.
func stepCode(v float64) (string, bool) {
	if v == 0 {
		return mgNone, true
	}
	for _, s := range slices.Concat(mgSteps, mgExtraSteps) {
		if math.Abs(s.value-v) <= 1e-6*math.Max(1, s.value) {
			return s.code, true
		}
	}
	return "", false
}

// withExtra adds the extra step with this code to a list that lacks it, in
// order of value.
func withExtra(options []MapGenOption, code string) []MapGenOption {
	if slices.ContainsFunc(options, func(o MapGenOption) bool { return o.Value == code }) {
		return options
	}
	for _, e := range mgExtraSteps {
		if e.code != code {
			continue
		}
		n, _ := strconv.Atoi(code)
		extra := MapGenOption{code, i18n.M("mapgen.step.percent", "percent", n)}
		at := len(options)
		for i, o := range options {
			if v, _ := stepValue(o.Value); v > e.value {
				at = i
				break
			}
		}
		return slices.Insert(slices.Clone(options), at, extra)
	}
	return options
}

// MapGenSelect is one setting of steps: a select, and with script a slider
// over the same steps on a wide panel (DESIGN 8.7).
type MapGenSelect struct {
	ID, Name string
	Label    i18n.Msg
	// Value is the chosen step's code; Preset the preset's, for "[modified]".
	Value   string
	Preset  string
	Options []MapGenOption
	Error   i18n.Msg
	// Ach is the achievement line in step codes; Blocked says the value crosses it.
	Ach     *AchWarn
	Blocked bool
	// Plain is a select of names, not steps, so it never becomes a slider.
	Plain bool
	// def is the value a bad post falls back to; "" means mgDefaultStep.
	def string
	// stored is the value's path in map-gen-settings.json and whether the
	// field shows its inverse; "" for 1.1's water and cliffs.
	path   string
	invert bool
}

// MapGenText is one text field or the raw box.
type MapGenText struct {
	ID, Name string
	Label    i18n.Msg
	Value    string
	Preset   string
	Error    i18n.Msg
}

// MapGenCheck is a checkbox of the Enemy tab.
type MapGenCheck struct {
	ID, Name string
	Label    i18n.Msg
	Hint     i18n.Msg
	Checked  bool
	Preset   bool
	Ach      *AchWarn
	Blocked  bool
	path     string
}

// MapGenGroup is the settings of one autoplace control, under its name.
// A control the game lets you switch off has Toggle: its box sits in the
// legend, so it stays usable while the script disables the rest.
type MapGenGroup struct {
	ID      string
	Legend  i18n.Msg
	Toggle  *MapGenCheck
	Selects []MapGenSelect
}

// MapGenPlanet is one planet's controls in a tab, in a details element that
// is open when it comes first or holds an error (DESIGN 8.7).
type MapGenPlanet struct {
	ID      string
	Summary i18n.Msg
	Open    bool
	Groups  []MapGenGroup
}

// MapGenForm is the map-gen part of Generate's form and what a post is read
// back into. Build one with NewMapGenForm or ParseMapGenForm. Its fields fall
// into the tabs the game's map generator has: Resources and Terrain per
// planet, Enemy, and Advanced (DESIGN 8.7).
type MapGenForm struct {
	line     string
	spaceAge bool

	Seed, Width, Height MapGenText
	StartingArea        MapGenSelect
	Peaceful            MapGenCheck
	// NoEnemies is 2.x only; its Name is "" on 1.1.
	NoEnemies MapGenCheck
	Resources []MapGenPlanet
	Terrain   []MapGenPlanet
	Enemy     []MapGenGroup

	// Raw is the box for every key the fields do not cover.
	Raw MapGenText
	// RawOpen opens the raw box's details; RawInvalid marks it as wrong.
	RawOpen, RawInvalid bool
	// RawModified says the raw box differs from the preset's.
	RawModified bool
}

// selects lists every select of the form, in page order.
func (f *MapGenForm) selects() []*MapGenSelect {
	var out []*MapGenSelect
	for _, tab := range [][]MapGenPlanet{f.Resources, f.Terrain} {
		for pi := range tab {
			for gi := range tab[pi].Groups {
				for si := range tab[pi].Groups[gi].Selects {
					out = append(out, &tab[pi].Groups[gi].Selects[si])
				}
			}
		}
	}
	for gi := range f.Enemy {
		for si := range f.Enemy[gi].Selects {
			out = append(out, &f.Enemy[gi].Selects[si])
		}
	}
	return append(out, &f.StartingArea)
}

// toggles lists every resource's on/off box, in page order.
func (f *MapGenForm) toggles() []*MapGenCheck {
	var out []*MapGenCheck
	for pi := range f.Resources {
		for gi := range f.Resources[pi].Groups {
			if t := f.Resources[pi].Groups[gi].Toggle; t != nil {
				out = append(out, t)
			}
		}
	}
	return out
}

// TabOf names the tab that holds a field id, or "".
func (f MapGenForm) TabOf(id string) string {
	in := func(planets []MapGenPlanet) bool {
		for _, p := range planets {
			for _, g := range p.Groups {
				for _, s := range g.Selects {
					if s.ID == id {
						return true
					}
				}
			}
		}
		return false
	}
	switch {
	case in(f.Resources):
		return tabResources
	case in(f.Terrain):
		return tabTerrain
	case id == mgSeedID:
		return ""
	case id == mgWidthID, id == mgHeightID, id == mgRawID:
		return tabAdvanced
	}
	return tabEnemy
}

// Modified reports whether any field differs from the preset it was filled
// from. The seed is not a setting of the preset.
func (f MapGenForm) Modified() bool {
	for _, s := range f.selects() {
		if s.Value != s.Preset {
			return true
		}
	}
	for _, t := range f.toggles() {
		if t.Checked != t.Preset {
			return true
		}
	}
	return f.RawModified || f.Peaceful.Checked != f.Peaceful.Preset || f.NoEnemies.Checked != f.NoEnemies.Preset ||
		!sameShown(f.Width.Value, f.Width.Preset) || !sameShown(f.Height.Value, f.Height.Preset)
}

// withPreset records the preset's values beside the form's, for "[modified]".
func (f *MapGenForm) withPreset(p MapGenForm) {
	codes := map[string]string{}
	for _, s := range p.selects() {
		codes[s.Name] = s.Value
	}
	for _, s := range f.selects() {
		s.Preset = codes[s.Name]
	}
	on := map[string]bool{}
	for _, t := range p.toggles() {
		on[t.Name] = t.Checked
	}
	for _, t := range f.toggles() {
		t.Preset = on[t.Name]
	}
	f.Width.Preset, f.Height.Preset = p.Width.Value, p.Height.Value
	f.Peaceful.Preset, f.NoEnemies.Preset = p.Peaceful.Checked, p.NoEnemies.Checked
	f.RawModified = !sameJSON(f.Raw.Value, p.Raw.Value)
}

// sameJSON compares two JSON texts, ignoring layout; text that is not JSON
// matches only itself.
func sameJSON(a, b string) bool {
	var x, y bytes.Buffer
	if json.Compact(&x, []byte(a)) != nil || json.Compact(&y, []byte(b)) != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return bytes.Equal(x.Bytes(), y.Bytes())
}

// markAchievements sets each field's achievement line from the version's
// rules and whether its value crosses it (SPEC 5.6).
func (f *MapGenForm) markAchievements(rules []mapgen.AchievementRule) {
	byPath := map[string]mapgen.AchievementRule{}
	for _, r := range rules {
		if !r.MapSettings {
			byPath[r.Field] = r
		}
	}
	for _, s := range f.selects() {
		r, ok := byPath[s.path]
		if !ok || s.path == "" {
			continue
		}
		w := &AchWarn{Test: r.Test, Reason: r.Message}
		limit := r.Limit
		if s.invert && limit != 0 {
			// The field shows the inverse, so the line is crossed the other way.
			limit = 1 / limit
			w.Test = map[string]string{"below": "above", "above": "below"}[r.Test]
		}
		w.Limit = shown(limit, 100)
		s.Ach = w
		v, _ := stepValue(s.Value)
		if s.invert && v != 0 {
			v = 1 / v
		}
		s.Blocked = r.Blocks(v)
	}
	for _, c := range []*MapGenCheck{&f.Peaceful, &f.NoEnemies} {
		if r, ok := byPath[c.path]; ok && c.Name != "" {
			c.Ach = &AchWarn{Test: r.Test, Reason: r.Message}
			c.Blocked = r.Blocks(c.Checked)
		}
	}
}

// blockedReasons lists the message keys of the reasons the form's fields
// cross, each once, in page order.
func (f MapGenForm) blockedReasons() []string {
	var out []string
	add := func(blocked bool, a *AchWarn) {
		if blocked && a != nil && !slices.Contains(out, a.Reason) {
			out = append(out, a.Reason)
		}
	}
	for _, s := range f.selects() {
		add(s.Blocked, s.Ach)
	}
	add(f.Peaceful.Blocked, f.Peaceful.Ach)
	add(f.NoEnemies.Blocked, f.NoEnemies.Ach)
	return out
}

// SeedNumber is the seed as a number, or nil for a random one. It reports
// nil for a seed that is not a number, so call it after a successful parse.
func (f MapGenForm) SeedNumber() *uint32 {
	s := strings.TrimSpace(f.Seed.Value)
	if s == "" {
		return nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return nil
	}
	v := uint32(n)
	return &v
}

// WithSeed returns the form holding seed, as when a preview fills a blank
// seed with the random one it used.
func (f MapGenForm) WithSeed(seed uint32) MapGenForm {
	f.Seed.Value = strconv.FormatUint(uint64(seed), 10)
	return f
}

// A setting of a control: its key in the file and how the form shows it.
type mgSetting struct {
	key    string
	label  i18n.Msg
	none   bool
	invert bool // the form shows the inverse of the stored value (water scale)
}

// A control as the form knows it.
type mgControl struct {
	name     string
	legend   i18n.Msg
	category string
	settings []mgSetting
	// topLevel is a 1.1 setting that the game stores outside
	// autoplace_controls: water and cliffs.
	topLevel bool
	// toggle is a resource the game lets you switch off; off is size 0.
	toggle bool
}

type mgPlanet struct {
	name     string
	controls []mgControl
}

func (c mgControl) isWater() bool { return c.name == "water" }

// tab names the tab a control's fields sit in (DESIGN 8.7).
func (c mgControl) tab() string {
	switch c.category {
	case mapgen.CategoryTerrain, mapgen.CategoryCliff:
		return tabTerrain
	case "enemy":
		return tabEnemy
	}
	return tabResources
}

// mgPlanets lists the planets a new world has with their controls, from the
// game's own data. On 1.1 water and cliffs are not autoplace controls, so
// they are added to Nauvis.
func mgPlanets(line string, spaceAge bool) []mgPlanet {
	names := mapgen.Planets(line, spaceAge)
	out := make([]mgPlanet, 0, len(names))
	for _, p := range names {
		planet := mgPlanet{name: p}
		for _, c := range mapgen.Controls(line, p) {
			planet.controls = append(planet.controls, controlFor(c))
		}
		if line == gamesettings.Line11 && p == "nauvis" {
			planet.controls = append(planet.controls,
				mgControl{name: "water", legend: controlMsg("water"), category: mapgen.CategoryTerrain, topLevel: true, settings: waterSettings()},
				mgControl{name: "nauvis_cliff", legend: controlMsg("nauvis_cliff"), category: mapgen.CategoryCliff, topLevel: true, settings: cliffSettings()})
		}
		out = append(out, planet)
	}
	return out
}

func waterSettings() []mgSetting {
	return []mgSetting{
		{key: "frequency", label: i18n.M("mapgen.setting.scale"), invert: true},
		{key: "size", label: i18n.M("mapgen.setting.coverage"), none: true},
	}
}

func cliffSettings() []mgSetting {
	return []mgSetting{
		{key: "frequency", label: i18n.M("mapgen.setting.frequency")},
		{key: "size", label: i18n.M("mapgen.setting.continuity"), none: true},
	}
}

// terrainSettings are the settings of a terrain control, worded as the game
// words them: Scale (the inverse of the stored frequency) and Coverage.
func terrainSettings(richness bool) []mgSetting {
	out := waterSettings()
	if richness {
		out = append(out, mgSetting{key: "richness", label: i18n.M("mapgen.setting.richness"), none: true})
	}
	return out
}

func controlFor(c mapgen.Control) mgControl {
	mc := mgControl{name: c.Name, legend: controlMsg(c.Name), category: c.Category,
		toggle: c.Category == mapgen.CategoryResource && c.CanBeDisabled}
	switch c.Category {
	case mapgen.CategoryTerrain:
		mc.settings = terrainSettings(c.Richness)
	case mapgen.CategoryCliff:
		mc.settings = cliffSettings()
	default:
		mc.settings = []mgSetting{
			{key: "frequency", label: i18n.M("mapgen.setting.frequency")},
			{key: "size", label: i18n.M("mapgen.setting.size"), none: true},
		}
		if c.Richness {
			mc.settings = append(mc.settings, mgSetting{key: "richness", label: i18n.M("mapgen.setting.richness"), none: true})
		}
	}
	return mc
}

func fieldName(planet, control, setting string) string {
	return mgPrefix + planet + "." + control + "." + setting
}

func fieldID(planet, control, setting string) string {
	return "mg-" + planet + "-" + control + "-" + setting
}

// knownFieldNames is every field name any line can post, so a post with
// another "mg." name is told apart from one meant for another line or for
// a world without Space Age.
var knownFieldNames = func() map[string]bool {
	known := map[string]bool{mgWidth: true, mgHeight: true, mgStart: true, mgPeaceful: true, mgNoEnemies: true, mgRaw: true,
		mgMapType: true, mgMoistureScale: true, mgAuxScale: true, mgMoistureBias: true, mgAuxBias: true}
	for _, line := range mapgen.Lines {
		for _, p := range mgPlanets(line, true) {
			for _, c := range p.controls {
				if c.toggle {
					known[fieldName(p.name, c.name, mgOn)] = true
				}
				for _, s := range c.settings {
					known[fieldName(p.name, c.name, s.key)] = true
				}
			}
		}
	}
	return known
}()

// NewMapGenForm is the form as it first opens: every setting as the game
// ships it, for a world of the given game line (SPEC 5.6).
func NewMapGenForm(line string, spaceAge bool) MapGenForm {
	f := buildMapGenForm(line, spaceAge, func(string) (string, bool) { return "", false })
	f.withPreset(f)
	return f
}

// buildMapGenForm lays out the form; posted says what the request holds for
// a select's name.
func buildMapGenForm(line string, spaceAge bool, posted func(name string) (string, bool)) MapGenForm {
	f := MapGenForm{
		line: line, spaceAge: spaceAge,
		Seed:   MapGenText{ID: mgSeedID, Name: mgSeed},
		Width:  MapGenText{ID: mgWidthID, Name: mgWidth, Label: i18n.M("mapgen.width.label"), Value: "0"},
		Height: MapGenText{ID: mgHeightID, Name: mgHeight, Label: i18n.M("mapgen.height.label"), Value: "0"},
		StartingArea: MapGenSelect{ID: mgStartID, Name: mgStart, Label: i18n.M("mapgen.start.label"),
			Value: mgDefaultStep, Options: optionsWithNone, path: "starting_area"},
		Peaceful: MapGenCheck{ID: "mg-peaceful", Name: mgPeaceful, Label: i18n.M("mapgen.peaceful.label"),
			Hint: i18n.M("mapgen.peaceful.hint"), path: "peaceful_mode"},
		Raw: MapGenText{ID: mgRawID, Name: mgRaw, Value: rawPrefill(line)},
	}
	if line != gamesettings.Line11 {
		f.NoEnemies = MapGenCheck{ID: "mg-no-enemies", Name: mgNoEnemies, Label: i18n.M("mapgen.no_enemies.label"),
			Hint: i18n.M("mapgen.no_enemies.hint"), path: "no_enemies_mode"}
	}
	if v, ok := posted(mgStart); ok {
		f.StartingArea.Value = v
		f.StartingArea.Options = withExtra(f.StartingArea.Options, v)
	}
	for _, p := range mgPlanets(line, spaceAge) {
		summary := planetMsg(p.name)
		res := MapGenPlanet{ID: "mg-resources-" + p.name, Summary: summary, Open: len(f.Resources) == 0}
		ter := MapGenPlanet{ID: "mg-terrain-" + p.name, Summary: summary, Open: len(f.Terrain) == 0}
		if p.name == "nauvis" {
			ter.Groups = append(ter.Groups, mapTypeGroup(line, posted))
		}
		for _, c := range p.controls {
			g := MapGenGroup{ID: "mg-" + p.name + "-" + c.name, Legend: c.legend}
			if c.toggle {
				g.Toggle = &MapGenCheck{ID: fieldID(p.name, c.name, mgOn), Name: fieldName(p.name, c.name, mgOn),
					Label: c.legend, Checked: true, Preset: true}
			}
			for _, s := range c.settings {
				name := fieldName(p.name, c.name, s.key)
				sel := MapGenSelect{ID: fieldID(p.name, c.name, s.key), Name: name, Label: s.label,
					Value: mgDefaultStep, Options: optionsFrequency, invert: s.invert}
				if !c.topLevel {
					sel.path = "autoplace_controls." + c.name + "." + s.key
				}
				// A resource's box stands in for its size's "None", as in the game.
				if s.none && (!c.toggle || s.key != "size") {
					sel.Options = optionsWithNone
				}
				if v, ok := posted(name); ok {
					sel.Value = v
					sel.Options = withExtra(sel.Options, v)
				}
				g.Selects = append(g.Selects, sel)
			}
			switch c.tab() {
			case tabTerrain:
				ter.Groups = append(ter.Groups, g)
			case tabEnemy:
				f.Enemy = append(f.Enemy, g)
			default:
				res.Groups = append(res.Groups, g)
			}
		}
		if _, ok := mapgen.TerrainKeysOf(line); ok && p.name == "nauvis" {
			ter.Groups = append(ter.Groups,
				terrainGroup("moisture", i18n.M("mapgen.moisture.legend"), posted),
				terrainGroup("aux", i18n.M("mapgen.aux.legend"), posted))
		}
		if len(res.Groups) > 0 {
			f.Resources = append(f.Resources, res)
		}
		if len(ter.Groups) > 0 {
			f.Terrain = append(f.Terrain, ter)
		}
	}
	return f
}

// ParseMapGenForm reads the post into the form and the text of
// map-gen-settings.json. The form always comes back holding what was posted
// (except a value the form never offers), with errors on its fields; mapGen
// is nil when problems is not empty. Form fields win over the raw box for
// every key they cover (DESIGN 8.7). The result has passed
// gamesettings.Validate for the line (SPEC 11.2).
func ParseMapGenForm(r *http.Request, line string, spaceAge bool) (form MapGenForm, mapGen []byte, problems []Problem) {
	_ = r.ParseForm() // the middleware caps the body; a bad body leaves what parsed, and the fields then fail their own checks
	return parseMapGenValues(r.PostForm, line, spaceAge)
}

func parseMapGenValues(vals url.Values, line string, spaceAge bool) (form MapGenForm, mapGen []byte, problems []Problem) {
	posted := func(name string) (string, bool) {
		vs, ok := vals[name]
		if !ok || len(vs) == 0 {
			return "", false
		}
		return vs[0], true
	}
	form = buildMapGenForm(line, spaceAge, posted)
	form.Peaceful.Checked = vals.Get(mgPeaceful) != ""
	if form.NoEnemies.Name != "" {
		form.NoEnemies.Checked = vals.Get(mgNoEnemies) != ""
	}
	// Each box comes after a hidden "off" of the same name, so an unticked
	// box posts "off" alone. A post without the name leaves the resource on.
	for _, t := range form.toggles() {
		if vs, ok := vals[t.Name]; ok {
			t.Checked = slices.Contains(vs, "on")
		}
	}
	if v, ok := posted(mgSeed); ok {
		form.Seed.Value = strings.TrimSpace(v)
	}
	if v, ok := posted(mgWidth); ok {
		form.Width.Value = strings.TrimSpace(v)
	}
	if v, ok := posted(mgHeight); ok {
		form.Height.Value = strings.TrimSpace(v)
	}
	if v, ok := posted(mgRaw); ok {
		form.Raw.Value = v
	}

	fail := func(id string, msg i18n.Msg) { problems = append(problems, Problem{FieldID: id, Message: msg}) }

	if !slices.Contains(mapgen.Lines, line) {
		fail(mgSeedID, i18n.M("mapgen.err.unknown"))
		return form, nil, problems
	}
	for name := range vals {
		if strings.HasPrefix(name, mgPrefix) && !knownFieldNames[name] {
			// Never echoed: it is not one of this form's fields.
			fail(mgSeedID, i18n.M("mapgen.err.unknown"))
			break
		}
	}

	if form.Seed.Value != "" {
		if _, err := strconv.ParseUint(form.Seed.Value, 10, 32); err != nil {
			form.Seed.Error = i18n.M("mapgen.err.seed", "max", strconv.FormatUint(mgMaxNumber, 10))
			fail(mgSeedID, form.Seed.Error)
		}
	}
	checkSelect := func(s *MapGenSelect, field i18n.Msg) bool {
		if validStep(s.Value, s.Options) {
			return true
		}
		// The bad value is not echoed or kept.
		s.Value = mgDefaultStep
		if s.def != "" {
			s.Value = s.def
		}
		s.Error = i18n.M("mapgen.err.step", "field", field)
		fail(s.ID, s.Error)
		return false
	}
	for _, tab := range [][]MapGenPlanet{form.Resources, form.Terrain} {
		for pi := range tab {
			p := &tab[pi]
			for gi := range p.Groups {
				g := &p.Groups[gi]
				for si := range g.Selects {
					if !checkSelect(&g.Selects[si], i18n.M("mapgen.field.name", "control", g.Legend, "setting", g.Selects[si].Label)) {
						p.Open = true
					}
				}
			}
		}
	}
	for gi := range form.Enemy {
		g := &form.Enemy[gi]
		for si := range g.Selects {
			checkSelect(&g.Selects[si], i18n.M("mapgen.field.name", "control", g.Legend, "setting", g.Selects[si].Label))
		}
	}
	checkSelect(&form.StartingArea, i18n.M("mapgen.start.label"))
	for _, t := range []*MapGenText{&form.Width, &form.Height} {
		if _, err := strconv.ParseUint(t.Value, 10, 32); err != nil {
			t.Error = i18n.M("mapgen.err.size", "max", uint64(mgMaxNumber))
			fail(t.ID, t.Error)
		}
	}

	raw := rawOrEmpty(form.Raw.Value)
	if fes := gamesettings.Validate(gamesettings.FileMapGen, line, raw); len(fes) > 0 {
		form.RawOpen, form.RawInvalid = true, true
		for _, fe := range fes {
			fail(mgRawID, rawErrorMsg(fe, form.Raw.Value))
		}
	}
	if len(problems) > 0 {
		return form, nil, problems
	}

	out, ok := buildMapGen(form, raw)
	if !ok || len(gamesettings.Validate(gamesettings.FileMapGen, line, out)) > 0 {
		fail(mgSeedID, i18n.M("mapgen.err.unknown"))
		return form, nil, problems
	}
	return form, out, nil
}

func validStep(code string, options []MapGenOption) bool {
	for _, o := range options {
		if o.Value == code {
			return true
		}
	}
	return false
}

func rawOrEmpty(s string) []byte {
	if strings.TrimSpace(s) == "" {
		return []byte("{}")
	}
	return []byte(s)
}

// A JSON object whose members keep their order.
type jsonMember struct {
	key string
	val json.RawMessage
}

func orderedMembers(b []byte) ([]jsonMember, bool) {
	dec := json.NewDecoder(bytes.NewReader(b))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	var out []jsonMember
	for dec.More() {
		t, err := dec.Token()
		key, isStr := t.(string)
		if err != nil || !isStr {
			return nil, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		out = append(out, jsonMember{key, v})
	}
	return out, true
}

func writeMembers(ms []jsonMember) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range ms {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(m.key)
		b.Write(k)
		b.WriteByte(':')
		var c bytes.Buffer
		if json.Compact(&c, m.val) != nil {
			c.Reset()
			c.WriteString("null")
		}
		b.Write(c.Bytes())
	}
	b.WriteByte('}')
	return b.Bytes()
}

func indent(b []byte) []byte {
	var out bytes.Buffer
	if json.Indent(&out, b, "", "  ") != nil {
		return b
	}
	return out.Bytes()
}

// rawCovered are the top-level keys the fields decide. For cliff_settings
// and autoplace_controls only the covered members are decided.
func rawCovered(line, key string) bool {
	switch key {
	case "width", "height", "starting_area", "peaceful_mode", "seed":
		return true
	case "no_enemies_mode":
		return line != gamesettings.Line11
	case "terrain_segmentation", "water":
		return line == gamesettings.Line11
	}
	return false
}

// rawPrefill is the raw box as it first opens: the game's example file
// without the keys the fields cover and without its comments.
func rawPrefill(line string) string {
	ms, ok := orderedMembers(mapgen.DefaultMapGen(line))
	if !ok {
		return "{}"
	}
	var keep []jsonMember
	for _, m := range ms {
		switch {
		case strings.HasPrefix(m.key, "_"), rawCovered(line, m.key), m.key == "autoplace_controls":
			continue
		case m.key == "cliff_settings":
			// 2.x sets cliffs through the Nauvis cliff control; this file's
			// block would override the planet's own (SPEC 5.6).
			if line != gamesettings.Line11 {
				continue
			}
			inner, ok := orderedMembers(m.val)
			if !ok {
				continue
			}
			var kept []jsonMember
			for _, im := range inner {
				if strings.HasPrefix(im.key, "_") || im.key == "cliff_elevation_interval" || im.key == "richness" {
					continue
				}
				kept = append(kept, im)
			}
			m.val = writeMembers(kept)
		case m.key == "property_expression_names":
			m.val = withoutCoveredPen(line, m.val)
		}
		keep = append(keep, m)
	}
	return string(indent(writeMembers(keep))) + "\n"
}

// resourceOn reports whether a resource's on/off box is ticked; a control
// with no box is always on.
func (f MapGenForm) resourceOn(planet, control string) bool {
	name := fieldName(planet, control, mgOn)
	for _, t := range f.toggles() {
		if t.Name == name {
			return t.Checked
		}
	}
	return true
}

// num writes a multiplier without float noise.
func num(v float64) json.RawMessage {
	v = math.Round(v*1e9) / 1e9
	return json.RawMessage(strconv.FormatFloat(v, 'g', -1, 64))
}

// chosen is the step code of every select by field name.
func (f MapGenForm) chosen() map[string]string {
	m := map[string]string{}
	for _, s := range f.selects() {
		m[s.Name] = s.Value
	}
	return m
}

// withPreferred lists m's members, the preferred keys first and then the
// rest in key order.
func withPreferred(m map[string]json.RawMessage, preferred ...string) []jsonMember {
	var out []jsonMember
	seen := map[string]bool{}
	for _, k := range preferred {
		if v, ok := m[k]; ok {
			out = append(out, jsonMember{k, v})
			seen[k] = true
		}
	}
	var rest []string
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		out = append(out, jsonMember{k, m[k]})
	}
	return out
}

func asMap(ms []jsonMember) map[string]json.RawMessage {
	m := make(map[string]json.RawMessage, len(ms))
	for _, x := range ms {
		m[x.key] = x.val
	}
	return m
}

// buildMapGen lays the fields over the raw object (DESIGN 8.7). ok is false
// only if the raw object, which has been validated, cannot be read.
func buildMapGen(f MapGenForm, rawB []byte) ([]byte, bool) {
	rawMembers, ok := orderedMembers(rawB)
	if !ok {
		return nil, false
	}
	raw := asMap(rawMembers)
	chosen := f.chosen()
	var out []jsonMember
	add := func(k string, v json.RawMessage) { out = append(out, jsonMember{k, v}) }

	width, _ := strconv.ParseUint(f.Width.Value, 10, 32)
	height, _ := strconv.ParseUint(f.Height.Value, 10, 32)
	start, _ := stepValue(chosen[mgStart])
	add("width", json.RawMessage(strconv.FormatUint(width, 10)))
	add("height", json.RawMessage(strconv.FormatUint(height, 10)))
	add("starting_area", num(start))
	add("peaceful_mode", json.RawMessage(strconv.FormatBool(f.Peaceful.Checked)))
	if f.NoEnemies.Name != "" {
		add("no_enemies_mode", json.RawMessage(strconv.FormatBool(f.NoEnemies.Checked)))
	}

	// A setting's multiplier as the file stores it.
	stored := func(planet string, c mgControl, s mgSetting) float64 {
		v, _ := stepValue(chosen[fieldName(planet, c.name, s.key)])
		if s.invert && v != 0 {
			v = 1 / v
		}
		return v
	}

	var rawControls map[string]json.RawMessage
	if b, ok := raw["autoplace_controls"]; ok {
		ms, ok := orderedMembers(b)
		if !ok {
			return nil, false
		}
		rawControls = asMap(ms)
	}
	known := map[string]bool{}
	var controlOrder []jsonMember
	water := map[string]json.RawMessage{} // 1.1 only: stored outside autoplace_controls
	cliff := map[string]json.RawMessage{}
	for _, p := range mgPlanets(f.line, f.spaceAge) {
		for _, c := range p.controls {
			switch {
			case c.topLevel && c.isWater():
				water["terrain_segmentation"] = num(stored(p.name, c, c.settings[0]))
				water["water"] = num(stored(p.name, c, c.settings[1]))
			case c.topLevel:
				cliff["cliff_elevation_interval"] = num(40 / stored(p.name, c, c.settings[0]))
				cliff["richness"] = num(stored(p.name, c, c.settings[1]))
			default:
				entry := map[string]json.RawMessage{}
				if b, ok := rawControls[c.name]; ok {
					ms, ok := orderedMembers(b)
					if !ok {
						return nil, false
					}
					entry = asMap(ms)
				}
				for _, s := range c.settings {
					entry[s.key] = num(stored(p.name, c, s))
				}
				if c.toggle && !f.resourceOn(p.name, c.name) {
					// The game's own off value; frequency 0 crashes 2.1.
					entry["size"] = num(0)
				}
				known[c.name] = true
				controlOrder = append(controlOrder, jsonMember{c.name, writeMembers(withPreferred(entry, "frequency", "size", "richness"))})
			}
		}
	}
	// Controls only the raw box knows, such as a mod's, are kept as written.
	var extra []string
	for k := range rawControls {
		if !known[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	for _, k := range extra {
		controlOrder = append(controlOrder, jsonMember{k, rawControls[k]})
	}

	for _, m := range withPreferred(water, "terrain_segmentation", "water") {
		add(m.key, m.val)
	}
	add("autoplace_controls", writeMembers(controlOrder))

	if f.line == gamesettings.Line11 {
		// The game's defaults for what the fields do not cover, then the raw
		// box over them, then the fields.
		defaults, _ := orderedMembers(mapgen.DefaultMapGen(f.line))
		merged := map[string]json.RawMessage{}
		for _, src := range []json.RawMessage{asMap(defaults)["cliff_settings"], raw["cliff_settings"]} {
			ms, _ := orderedMembers(src)
			for _, m := range ms {
				if !strings.HasPrefix(m.key, "_") {
					merged[m.key] = m.val
				}
			}
		}
		for k, v := range cliff {
			merged[k] = v
		}
		add("cliff_settings", writeMembers(withPreferred(merged, "name", "cliff_elevation_0", "cliff_elevation_interval", "richness")))
	} else if v, ok := raw["cliff_settings"]; ok {
		add("cliff_settings", v)
	}

	pen, ok := buildPen(f, raw["property_expression_names"], chosen)
	if !ok {
		return nil, false
	}
	add("property_expression_names", pen)

	rest := map[string]json.RawMessage{}
	for k, v := range raw {
		if !rawCovered(f.line, k) && k != "autoplace_controls" && k != "cliff_settings" && k != "property_expression_names" {
			rest[k] = v
		}
	}
	out = append(out, withPreferred(rest)...)

	if seed := f.SeedNumber(); seed != nil {
		add("seed", json.RawMessage(strconv.FormatUint(uint64(*seed), 10)))
	} else {
		add("seed", json.RawMessage("null"))
	}
	return append(indent(writeMembers(out)), '\n'), true
}

// rawErrorMsg words one finding of gamesettings.Validate for the error
// summary: the line, the reason, and what the line reads (DESIGN 8.10).
func rawErrorMsg(fe gamesettings.FieldError, text string) i18n.Msg {
	switch fe.Code {
	case gamesettings.CodeTooLarge:
		return i18n.M("mapgen.raw.err.too_large", "size", i18n.Size(gamesettings.MaxBytes))
	case gamesettings.CodeNotObject:
		return i18n.M("mapgen.raw.err.not_object")
	case gamesettings.CodeSyntax:
		return i18n.M("mapgen.raw.err.syntax", "line", fe.Line, "text", lineText(text, fe.Line))
	case gamesettings.CodeWrongType:
		return i18n.M("mapgen.raw.err.type", "line", fe.Line, "key", fe.Key, "want", wantTypes(fe.Want), "text", lineText(text, fe.Line))
	case gamesettings.CodeOutOfRange:
		if want, ok := wantRange(fe.Want); ok {
			return i18n.M("mapgen.raw.err.range", "line", fe.Line, "key", fe.Key, "want", want, "text", lineText(text, fe.Line))
		}
	}
	return i18n.M("mapgen.raw.err.other", "line", fe.Line, "key", fe.Key, "text", lineText(text, fe.Line))
}

// lineText is line n of text, trimmed and shortened to fit a message.
func lineText(text string, n int) string {
	lines := strings.Split(text, "\n")
	if n < 1 || n > len(lines) {
		return ""
	}
	s := strings.TrimSpace(lines[n-1])
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:79]) + "…"
	}
	return s
}

// wantTypes words "boolean or string" as the catalogue's phrases joined.
func wantTypes(want string) i18n.Msg {
	var m i18n.Msg
	for i, w := range strings.Split(want, " or ") {
		t := typeMsg(w)
		if i == 0 {
			m = t
			continue
		}
		m = i18n.M("mapgen.raw.want.or", "first", m, "second", t)
	}
	return m
}

func typeMsg(w string) i18n.Msg {
	switch w {
	case "boolean":
		return i18n.M("mapgen.raw.type.boolean")
	case "string":
		return i18n.M("mapgen.raw.type.string")
	case "integer":
		return i18n.M("mapgen.raw.type.integer")
	case "number":
		return i18n.M("mapgen.raw.type.number")
	case "array":
		return i18n.M("mapgen.raw.type.array")
	case "object":
		return i18n.M("mapgen.raw.type.object")
	default:
		return i18n.M("mapgen.raw.type.null")
	}
}

// wantRange words "lo..hi", "lo.." or "..hi".
func wantRange(want string) (i18n.Msg, bool) {
	lo, hi, ok := strings.Cut(want, "..")
	switch {
	case !ok:
		return i18n.Msg{}, false
	case lo != "" && hi != "":
		return i18n.M("mapgen.raw.want.between", "low", lo, "high", hi), true
	case lo != "":
		return i18n.M("mapgen.raw.want.at_least", "low", lo), true
	case hi != "":
		return i18n.M("mapgen.raw.want.at_most", "high", hi), true
	}
	return i18n.Msg{}, false
}
