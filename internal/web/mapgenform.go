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
// of its own: "mg.seed", "mg.width", "mg.height", "mg.start", "mg.peaceful",
// "mg.raw", and "mg.<planet>.<control>.<setting>" for each autoplace control.

// Names of the form's fixed fields.
const (
	mgSeed     = "mg.seed"
	mgWidth    = "mg.width"
	mgHeight   = "mg.height"
	mgStart    = "mg.start"
	mgPeaceful = "mg.peaceful"
	mgRaw      = "mg.raw"
	mgPrefix   = "mg."
)

// IDs of the fixed fields, for the error summary's links.
const (
	mgSeedID   = "mg-seed"
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
var mgExtraSteps = []mgStep{{"75", 0.75}, {"150", 1.5}}

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

// MapGenSelect is one select of steps.
type MapGenSelect struct {
	ID, Name string
	Label    i18n.Msg
	// Value is the chosen step's code.
	Value   string
	Options []MapGenOption
	Error   i18n.Msg
}

// MapGenText is one text field or the raw box.
type MapGenText struct {
	ID, Name string
	Value    string
	Error    i18n.Msg
}

// MapGenGroup is the settings of one autoplace control, under its name.
type MapGenGroup struct {
	Legend  i18n.Msg
	Selects []MapGenSelect
}

// MapGenPlanet is the controls of one planet, in a details element that
// is open when it comes first or holds an error (DESIGN 8.7).
type MapGenPlanet struct {
	ID      string
	Summary i18n.Msg
	Open    bool
	Groups  []MapGenGroup
}

// MapGenForm is the view model of the partial "mapgenform" and what a
// post is read back into. Build one with NewMapGenForm or ParseMapGenForm.
type MapGenForm struct {
	line     string
	spaceAge bool

	Seed, Width, Height MapGenText
	StartingArea        MapGenSelect
	Water, Cliffs       MapGenGroup
	Peaceful            bool
	Planets             []MapGenPlanet

	// Raw is the box for every key the fields do not cover.
	Raw MapGenText
	// RawOpen opens the raw box's details; RawInvalid marks it as wrong.
	RawOpen, RawInvalid bool
	// FromPreset says the fields were just filled from a preset; zero otherwise.
	FromPreset i18n.Msg
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
	settings []mgSetting
	// topLevel is a 1.1 setting that the game stores outside
	// autoplace_controls: water and cliffs.
	topLevel bool
}

type mgPlanet struct {
	name     string
	summary  i18n.Msg
	controls []mgControl
}

func (c mgControl) isWater() bool { return c.name == "water" }
func (c mgControl) isCliffs() bool {
	return c.name == "nauvis_cliff"
}

// inMapGroup is true for Nauvis's water and cliffs, which sit in the Map
// fieldset on every line (DESIGN 8.7).
func (p mgPlanet) inMapGroup(c mgControl) bool {
	return p.name == "nauvis" && (c.isWater() || c.isCliffs())
}

// mgPlanets lists the planets a new world has with their controls, from the
// game's own data. On 1.1 water and cliffs are not autoplace controls, so
// they are added to Nauvis.
func mgPlanets(line string, spaceAge bool) []mgPlanet {
	names := mapgen.Planets(line, spaceAge)
	out := make([]mgPlanet, 0, len(names))
	for _, p := range names {
		planet := mgPlanet{name: p}
		if len(names) == 1 {
			planet.summary = i18n.M("mapgen.planet.summary_only")
		} else {
			planet.summary = i18n.M("mapgen.planet.summary", "planet", planetMsg(p))
		}
		for _, c := range mapgen.Controls(line, p) {
			planet.controls = append(planet.controls, controlFor(c))
		}
		if line == gamesettings.Line11 && p == "nauvis" {
			planet.controls = append(planet.controls,
				mgControl{name: "water", legend: controlMsg("water"), topLevel: true, settings: waterSettings()},
				mgControl{name: "nauvis_cliff", legend: controlMsg("nauvis_cliff"), topLevel: true, settings: cliffSettings()})
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
	mc := mgControl{name: c.Name, legend: controlMsg(c.Name)}
	switch {
	case c.Category == mapgen.CategoryTerrain:
		mc.settings = terrainSettings(c.Richness)
	case c.Category == mapgen.CategoryCliff:
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
	known := map[string]bool{mgSeed: true, mgWidth: true, mgHeight: true, mgStart: true, mgPeaceful: true, mgRaw: true}
	for _, line := range mapgen.Lines {
		for _, p := range mgPlanets(line, true) {
			for _, c := range p.controls {
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
	return buildMapGenForm(line, spaceAge, func(string) (string, bool) { return "", false })
}

// buildMapGenForm lays out the form; posted says what the request holds for
// a select's name.
func buildMapGenForm(line string, spaceAge bool, posted func(name string) (string, bool)) MapGenForm {
	f := MapGenForm{
		line: line, spaceAge: spaceAge,
		Seed:   MapGenText{ID: mgSeedID, Name: mgSeed},
		Width:  MapGenText{ID: mgWidthID, Name: mgWidth, Value: "0"},
		Height: MapGenText{ID: mgHeightID, Name: mgHeight, Value: "0"},
		StartingArea: MapGenSelect{ID: mgStartID, Name: mgStart, Label: i18n.M("mapgen.start.label"),
			Value: mgDefaultStep, Options: optionsWithNone},
		Raw: MapGenText{ID: mgRawID, Name: mgRaw, Value: rawPrefill(line)},
	}
	if v, ok := posted(mgStart); ok {
		f.StartingArea.Value = v
		f.StartingArea.Options = withExtra(f.StartingArea.Options, v)
	}
	for i, p := range mgPlanets(line, spaceAge) {
		planet := MapGenPlanet{ID: "mg-planet-" + p.name, Summary: p.summary, Open: i == 0}
		for _, c := range p.controls {
			g := MapGenGroup{Legend: c.legend}
			for _, s := range c.settings {
				name := fieldName(p.name, c.name, s.key)
				sel := MapGenSelect{ID: fieldID(p.name, c.name, s.key), Name: name, Label: s.label,
					Value: mgDefaultStep, Options: optionsFrequency}
				if s.none {
					sel.Options = optionsWithNone
				}
				if v, ok := posted(name); ok {
					sel.Value = v
					sel.Options = withExtra(sel.Options, v)
				}
				g.Selects = append(g.Selects, sel)
			}
			switch {
			case p.inMapGroup(c) && c.isWater():
				f.Water = g
			case p.inMapGroup(c):
				f.Cliffs = g
			default:
				planet.Groups = append(planet.Groups, g)
			}
		}
		f.Planets = append(f.Planets, planet)
	}
	return f
}

// ParseMapGenForm reads the post into the form and the text of
// map-gen-settings.json. The form always comes back holding what was posted
// (except a value the form never offers), with errors on its fields; mapGen
// is nil when problems is not empty. Form fields win over the raw box for
// every key they cover (DESIGN 8.7). The result has passed
// gamesettings.Validate for the line (SPEC 11.2).
//
// A request that the preset select started (its id in HX-Trigger) is read as
// a new start from that preset, keeping only the seed (DESIGN 8.7).
func ParseMapGenForm(r *http.Request, line string, spaceAge bool) (form MapGenForm, mapGen []byte, problems []Problem) {
	_ = r.ParseForm() // the middleware caps the body; a bad body leaves what parsed, and the fields then fail their own checks
	vals := r.PostForm
	var fromPreset i18n.Msg
	if preset := r.PostForm.Get("preset"); r.Header.Get("HX-Trigger") == PresetSelectID && slices.Contains(mapgen.Presets(line), preset) {
		vals = presetValues(line, spaceAge, preset, r.PostForm.Get(mgSeed))
		fromPreset = i18n.M("mapgen.preset.filled", "preset", PresetMsg(preset))
	}
	form, mapGen, problems = parseMapGenValues(vals, line, spaceAge)
	form.FromPreset = fromPreset
	return form, mapGen, problems
}

// PresetSelectID is the id of Generate's preset select. Changing it refills
// the custom form from the chosen preset.
const PresetSelectID = "generate-preset"

func parseMapGenValues(vals url.Values, line string, spaceAge bool) (form MapGenForm, mapGen []byte, problems []Problem) {
	posted := func(name string) (string, bool) {
		vs, ok := vals[name]
		if !ok || len(vs) == 0 {
			return "", false
		}
		return vs[0], true
	}
	form = buildMapGenForm(line, spaceAge, posted)
	form.Peaceful = vals.Get(mgPeaceful) != ""
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
			form.Seed.Error = i18n.M("mapgen.err.seed", "max", uint64(mgMaxNumber))
			fail(mgSeedID, form.Seed.Error)
		}
	}
	for _, t := range []*MapGenText{&form.Width, &form.Height} {
		if _, err := strconv.ParseUint(t.Value, 10, 32); err != nil {
			t.Error = i18n.M("mapgen.err.size", "max", uint64(mgMaxNumber))
			fail(t.ID, t.Error)
		}
	}
	checkSelect := func(s *MapGenSelect, field i18n.Msg) {
		if validStep(s.Value, s.Options) {
			return
		}
		// The bad value is not echoed or kept.
		s.Value = mgDefaultStep
		s.Error = i18n.M("mapgen.err.step", "field", field)
		fail(s.ID, s.Error)
	}
	checkSelect(&form.StartingArea, i18n.M("mapgen.start.label"))
	for _, g := range []*MapGenGroup{&form.Water, &form.Cliffs} {
		for i := range g.Selects {
			checkSelect(&g.Selects[i], i18n.M("mapgen.field.name", "control", g.Legend, "setting", g.Selects[i].Label))
		}
	}
	for pi := range form.Planets {
		p := &form.Planets[pi]
		for gi := range p.Groups {
			g := &p.Groups[gi]
			for si := range g.Selects {
				before := len(problems)
				checkSelect(&g.Selects[si], i18n.M("mapgen.field.name", "control", g.Legend, "setting", g.Selects[si].Label))
				if len(problems) > before {
					p.Open = true
				}
			}
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
		}
		keep = append(keep, m)
	}
	return string(indent(writeMembers(keep))) + "\n"
}

// num writes a multiplier without float noise.
func num(v float64) json.RawMessage {
	v = math.Round(v*1e9) / 1e9
	return json.RawMessage(strconv.FormatFloat(v, 'g', -1, 64))
}

// chosen is the step code of every select by field name.
func (f MapGenForm) chosen() map[string]string {
	m := map[string]string{mgStart: f.StartingArea.Value}
	groups := []MapGenGroup{f.Water, f.Cliffs}
	for _, p := range f.Planets {
		groups = append(groups, p.Groups...)
	}
	for _, g := range groups {
		for _, s := range g.Selects {
			m[s.Name] = s.Value
		}
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
	add("peaceful_mode", json.RawMessage(strconv.FormatBool(f.Peaceful)))

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

	rest := map[string]json.RawMessage{}
	for k, v := range raw {
		if !rawCovered(f.line, k) && k != "autoplace_controls" && k != "cliff_settings" {
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
