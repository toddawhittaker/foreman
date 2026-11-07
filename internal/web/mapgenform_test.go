package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/gamesettings"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// The form turns a post into map-gen-settings.json for a new world (SPEC 5.6,
// 11.5). The tests state what the file must hold for the choices made, from
// the game's own example files, not from what the code writes.

var testLines = []struct {
	line     string
	spaceAge bool
}{
	{"1.1", false}, {"2.0", false}, {"2.0", true}, {"2.1", false}, {"2.1", true},
}

func postForm(t *testing.T, v url.Values) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/worlds/generate", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

// parseOK posts v and wants no problem.
func parseOK(t *testing.T, line string, spaceAge bool, v url.Values) (MapGenForm, map[string]any) {
	t.Helper()
	form, out, problems := ParseMapGenForm(postForm(t, v), line, spaceAge)
	if len(problems) > 0 || out == nil {
		t.Fatalf("%s: problems %+v", line, problems)
	}
	if fes := gamesettings.Validate(gamesettings.FileMapGen, line, out); len(fes) > 0 {
		t.Fatalf("%s: output fails Validate: %+v\n%s", line, fes, out)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("%s: output is not JSON: %v\n%s", line, err, out)
	}
	return form, m
}

func sub(t *testing.T, m map[string]any, path ...string) map[string]any {
	t.Helper()
	for _, p := range path {
		next, ok := m[p].(map[string]any)
		if !ok {
			t.Fatalf("no object %q in %v", p, m)
		}
		m = next
	}
	return m
}

func approx(t *testing.T, what string, got any, want float64) {
	t.Helper()
	g, ok := got.(float64)
	if !ok || g < want-1e-6 || g > want+1e-6 {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestEveryControlHasAName(t *testing.T) {
	for _, line := range mapgen.Lines {
		for _, p := range mapgen.Planets(line, true) {
			if _, ok := planetNames[p]; !ok {
				t.Errorf("%s: planet %s has no name", line, p)
			}
			for _, c := range mapgen.Controls(line, p) {
				if _, ok := controlNames[c.Name]; !ok {
					t.Errorf("%s %s: control %s has no name; add it to mapgenlabels.go and the catalogue", line, p, c.Name)
				}
			}
		}
		for _, pr := range mapgen.Presets(line) {
			if _, ok := presetNames[pr]; !ok {
				t.Errorf("%s: preset %s has no name", line, pr)
			}
		}
	}
}

// The groups follow the game's data for each line, in the tabs the game's
// map generator has: resources, terrain (with cliffs) and enemies (SPEC 5.6,
// DESIGN 8.7).
func TestFormFollowsTheGameLine(t *testing.T) {
	for _, c := range testLines {
		f := NewMapGenForm(c.line, c.spaceAge)
		wantPlanets := mapgen.Planets(c.line, c.spaceAge)
		names := f.chosen()
		for _, p := range wantPlanets {
			for _, ctl := range mapgen.Controls(c.line, p) {
				want := map[string]string{"resource": tabResources, "terrain": tabTerrain, "cliff": tabTerrain, "enemy": tabEnemy}[ctl.Category]
				for _, s := range []string{"frequency", "size"} {
					if _, ok := names[fieldName(p, ctl.Name, s)]; !ok {
						t.Errorf("%s: no %s field for %s %s", c.line, s, p, ctl.Name)
					}
					if got := f.TabOf(fieldID(p, ctl.Name, s)); got != want {
						t.Errorf("%s: %s %s %s is in tab %q, want %q", c.line, p, ctl.Name, s, got, want)
					}
				}
				_, has := names[fieldName(p, ctl.Name, "richness")]
				if has != ctl.Richness {
					t.Errorf("%s: %s %s richness field = %v, game says %v", c.line, p, ctl.Name, has, ctl.Richness)
				}
			}
		}
		// Each planet has its own group of resources, the first open.
		if len(f.Resources) != len(wantPlanets) {
			t.Errorf("%s sa=%v: %d planets with resources, want %d", c.line, c.spaceAge, len(f.Resources), len(wantPlanets))
		}
		for i, pl := range f.Resources {
			if pl.Open != (i == 0) || pl.Summary.IsZero() {
				t.Errorf("%s: resources of %s open %v, summary %v", c.line, pl.ID, pl.Open, pl.Summary)
			}
		}
		// Water and cliffs are terrain on every line, 1.1's included.
		for _, id := range []string{fieldID("nauvis", "water", "frequency"), fieldID("nauvis", "nauvis_cliff", "size")} {
			if f.TabOf(id) != tabTerrain {
				t.Errorf("%s: %s is not in Terrain", c.line, id)
			}
		}
		if (f.NoEnemies.Name != "") != (c.line != "1.1") {
			t.Errorf("%s: No enemies offered = %v", c.line, f.NoEnemies.Name != "")
		}
	}
	// Per-line differences: 2.x has rocks and Space Age's planets; 1.1 has neither.
	if _, ok := NewMapGenForm("1.1", true).chosen()[fieldName("nauvis", "rocks", "size")]; ok {
		t.Error("1.1 offers rocks")
	}
	if _, ok := NewMapGenForm("2.0", false).chosen()[fieldName("vulcanus", "calcite", "size")]; ok {
		t.Error("2.0 without Space Age offers Vulcanus")
	}
	if _, ok := NewMapGenForm("2.0", true).chosen()[fieldName("vulcanus", "calcite", "size")]; !ok {
		t.Error("2.0 with Space Age lacks Vulcanus")
	}
	// 1.1 trees have richness; 2.x trees do not.
	if _, ok := NewMapGenForm("1.1", false).chosen()[fieldName("nauvis", "trees", "richness")]; !ok {
		t.Error("1.1 trees lack richness")
	}
	if _, ok := NewMapGenForm("2.0", false).chosen()[fieldName("nauvis", "trees", "richness")]; ok {
		t.Error("2.0 trees have richness")
	}
}

// Left alone, the form writes what the game's own example file says
// (SPEC 5.6: the default map is the game's default map).
func TestUntouchedFormMatchesTheGamesExample(t *testing.T) {
	for _, c := range testLines {
		_, got := parseOK(t, c.line, c.spaceAge, url.Values{})
		var want map[string]any
		if err := json.Unmarshal(mapgen.DefaultMapGen(c.line), &want); err != nil {
			t.Fatal(err)
		}
		for k, wv := range want {
			if strings.HasPrefix(k, "_") {
				continue
			}
			if k == "cliff_settings" && c.line != gamesettings.Line11 {
				continue // 2.x cliffs come from the Nauvis cliff control
			}
			gv, ok := got[k]
			if !ok {
				t.Errorf("%s: the example has %s, the form's file does not", c.line, k)
				continue
			}
			if k == "autoplace_controls" {
				for name, e := range wv.(map[string]any) {
					ge, ok := gv.(map[string]any)[name].(map[string]any)
					if !ok {
						t.Errorf("%s: control %s missing", c.line, name)
						continue
					}
					for sk, sv := range e.(map[string]any) {
						// The game's data says which controls have richness; the
						// example file lists it for some that do not.
						if _, offered := ge[sk]; !offered && sk == "richness" {
							continue
						}
						if ge[sk] != sv {
							t.Errorf("%s: %s.%s = %v, example %v", c.line, name, sk, ge[sk], sv)
						}
					}
				}
				continue
			}
			if k == "cliff_settings" {
				for sk, sv := range wv.(map[string]any) {
					if !strings.HasPrefix(sk, "_") && gv.(map[string]any)[sk] != sv {
						t.Errorf("%s: cliff_settings.%s = %v, example %v", c.line, sk, gv.(map[string]any)[sk], sv)
					}
				}
				continue
			}
			if keys, ok := mapgen.TerrainKeysOf(c.line); ok && k == "property_expression_names" {
				// A bias of 0 is left out, as the game's own generator writes it.
				ex := wv.(map[string]any)
				delete(ex, keys.MoistureBias)
				delete(ex, keys.AuxBias)
			}
			wj, _ := json.Marshal(wv)
			gj, _ := json.Marshal(gv)
			if !bytes.Equal(wj, gj) {
				t.Errorf("%s: %s = %s, example %s", c.line, k, gj, wj)
			}
		}
		// Every control the game lists for the world is written, so nothing
		// is left to a default that may differ.
		ac := sub(t, got, "autoplace_controls")
		for _, p := range mapgen.Planets(c.line, c.spaceAge) {
			for _, ctl := range mapgen.Controls(c.line, p) {
				if _, ok := ac[ctl.Name]; !ok {
					t.Errorf("%s: control %s not written", c.line, ctl.Name)
				}
			}
		}
	}
}

// Each choice reaches the key the game reads, with the game's meaning of it.
func TestChoicesBecomeSettings(t *testing.T) {
	t.Run("2.0 with Space Age", func(t *testing.T) {
		_, m := parseOK(t, "2.0", true, url.Values{
			mgSeed: {"  1234567 "}, mgWidth: {"4096"}, mgHeight: {"2048"}, mgStart: {"141"}, mgPeaceful: {"on"},
			fieldName("nauvis", "iron-ore", "richness"):      {"200"},
			fieldName("nauvis", "iron-ore", "frequency"):     {"33"},
			fieldName("nauvis", "enemy-base", "size"):        {"0"},
			fieldName("nauvis", "water", "frequency"):        {"50"}, // scale 50% is frequency 2
			fieldName("nauvis", "water", "size"):             {"141"},
			fieldName("nauvis", "nauvis_cliff", "size"):      {"0"},
			fieldName("vulcanus", "tungsten_ore", "size"):    {"600"},
			fieldName("aquilo", "lithium_brine", "richness"): {"17"},
		})
		approx(t, "seed", m["seed"], 1234567)
		approx(t, "width", m["width"], 4096)
		approx(t, "height", m["height"], 2048)
		approx(t, "starting_area", m["starting_area"], 1.414214)
		if m["peaceful_mode"] != true {
			t.Errorf("peaceful_mode = %v", m["peaceful_mode"])
		}
		ac := sub(t, m, "autoplace_controls")
		approx(t, "iron richness", sub(t, ac, "iron-ore")["richness"], 2)
		approx(t, "iron frequency", sub(t, ac, "iron-ore")["frequency"], 1.0/3)
		approx(t, "iron size", sub(t, ac, "iron-ore")["size"], 1)
		approx(t, "enemy size", sub(t, ac, "enemy-base")["size"], 0)
		approx(t, "water frequency", sub(t, ac, "water")["frequency"], 2)
		approx(t, "water size", sub(t, ac, "water")["size"], 1.414214)
		approx(t, "cliff continuity", sub(t, ac, "nauvis_cliff")["size"], 0)
		approx(t, "tungsten size", sub(t, ac, "tungsten_ore")["size"], 6)
		approx(t, "lithium richness", sub(t, ac, "lithium_brine")["richness"], 1.0/6)
		if _, ok := m["cliff_settings"]; ok {
			t.Error("2.x writes cliff_settings, which would override the planet's cliff control")
		}
	})
	t.Run("1.1", func(t *testing.T) {
		_, m := parseOK(t, "1.1", false, url.Values{
			fieldName("nauvis", "water", "frequency"):        {"25"},  // scale 25% is terrain_segmentation 4
			fieldName("nauvis", "water", "size"):             {"50"},  // coverage
			fieldName("nauvis", "nauvis_cliff", "frequency"): {"200"}, // interval 40/2
			fieldName("nauvis", "nauvis_cliff", "size"):      {"0"},   // no cliffs
			fieldName("nauvis", "trees", "richness"):         {"300"},
		})
		approx(t, "terrain_segmentation", m["terrain_segmentation"], 4)
		approx(t, "water", m["water"], 0.5)
		cs := sub(t, m, "cliff_settings")
		approx(t, "cliff interval", cs["cliff_elevation_interval"], 20)
		approx(t, "cliff richness", cs["richness"], 0)
		if cs["name"] != "cliff" || cs["cliff_elevation_0"] == nil {
			t.Errorf("cliff_settings lost the game's own keys: %v", cs)
		}
		approx(t, "trees richness", sub(t, m, "autoplace_controls", "trees")["richness"], 3)
		if m["seed"] != nil {
			t.Errorf("a blank seed must be null, got %v", m["seed"])
		}
	})
}

// A blank seed is random (null); a number is exact, including the largest.
func TestSeed(t *testing.T) {
	f, m := parseOK(t, "2.0", false, url.Values{mgSeed: {"4294967295"}})
	if s := f.SeedNumber(); s == nil || *s != 4294967295 {
		t.Errorf("SeedNumber = %v", s)
	}
	approx(t, "seed", m["seed"], 4294967295)
	f, _ = parseOK(t, "2.0", false, url.Values{mgSeed: {" "}})
	if f.SeedNumber() != nil {
		t.Error("a blank seed has a number")
	}
	if got := f.WithSeed(77).Seed.Value; got != "77" {
		t.Errorf("WithSeed = %q", got)
	}
}

// Bad numbers and tampered selects are refused at their field, are not
// echoed, and a closed group holding one opens (DESIGN 8.7).
func TestBadValuesAreRefusedAtTheirField(t *testing.T) {
	bad := fieldName("vulcanus", "calcite", "size")
	f, out, problems := ParseMapGenForm(postForm(t, url.Values{
		mgSeed: {"abc"}, mgWidth: {"-1"}, mgHeight: {"1.5"}, mgStart: {"<script>"}, bad: {"9999"},
	}), "2.0", true)
	if out != nil {
		t.Error("a file was built from bad input")
	}
	var ids []string
	for _, p := range problems {
		ids = append(ids, p.FieldID)
		if p.Message.IsZero() {
			t.Errorf("problem for %s has no message", p.FieldID)
		}
	}
	for _, want := range []string{mgSeedID, mgWidthID, mgHeightID, mgStartID, fieldID("vulcanus", "calcite", "size")} {
		if !slices.Contains(ids, want) {
			t.Errorf("no problem for %s; got %v", want, ids)
		}
	}
	if f.StartingArea.Value != mgDefaultStep {
		t.Errorf("a bad choice was kept: %q", f.StartingArea.Value)
	}
	if !f.Resources[1].Open {
		t.Error("the Vulcanus group holding an error is closed")
	}
	if f.Resources[2].Open {
		t.Error("a group without an error opened")
	}
	body := renderPartial(t, i18n.English, f)
	for _, leaked := range []string{"<script>", "9999"} {
		if strings.Contains(body, leaked) {
			t.Errorf("the page echoes %q", leaked)
		}
	}
	// The wrong seed is kept, as typed, so it can be fixed.
	if !strings.Contains(body, `value="abc"`) {
		t.Error("the typed seed was not kept")
	}
}

// A field the form never offers is refused and never shown (DESIGN 8.7:
// control names are checked against the game's list).
func TestUnknownControlIsRefused(t *testing.T) {
	for _, name := range []string{
		"mg.nauvis.bogus.frequency", "mg.nauvis.iron-ore.speed", "mg.mars.iron-ore.size", "mg.rubbish",
		"mg.nauvis.iron-ore.frequency.extra", "mg.__proto__.x.y",
	} {
		f, out, problems := ParseMapGenForm(postForm(t, url.Values{name: {"200"}}), "2.0", true)
		if out != nil || len(problems) == 0 {
			t.Errorf("%s: accepted (problems %v)", name, problems)
			continue
		}
		if problems[0].Message.Key != "mapgen.err.unknown" {
			t.Errorf("%s: message %v", name, problems[0].Message)
		}
		body := renderPartial(t, i18n.English, f)
		if strings.Contains(body, strings.TrimPrefix(name, "mg.")) && !strings.Contains(name, "iron-ore") {
			t.Errorf("%s: echoed", name)
		}
	}
	// A line the game data lacks is refused whole.
	if _, out, problems := ParseMapGenForm(postForm(t, url.Values{}), "3.0", false); out != nil || len(problems) == 0 {
		t.Error("an unknown line was accepted")
	}
	// A real field for another line or for Space Age is not an attack: it
	// is left out when the world cannot use it (the page may be stale).
	_, m := parseOK(t, "2.0", false, url.Values{fieldName("vulcanus", "calcite", "size"): {"600"}})
	if _, ok := sub(t, m, "autoplace_controls")["calcite"]; ok {
		t.Error("Vulcanus written for a world without Space Age")
	}
}

// What the form shows, posted back, writes the same file (the page holds the
// whole state, SPEC 5.6).
func TestFormRoundTripsThroughItsPage(t *testing.T) {
	for _, c := range testLines {
		first := url.Values{
			mgSeed: {"42"}, mgWidth: {"1000"}, mgHeight: {"2000"}, mgStart: {"71"}, mgPeaceful: {"on"},
			fieldName("nauvis", "coal", "frequency"):       {"200"},
			fieldName("nauvis", "water", "frequency"):      {"17"},
			fieldName("nauvis", "nauvis_cliff", "size"):    {"400"},
			fieldName("nauvis", "enemy-base", "frequency"): {"600"},
			mgRaw: {"{\"property_expression_names\": {\"elevation\": \"elevation_island\"}, \"starting_points\": [{\"x\": 5, \"y\": 6}]}"},
		}
		f1, m1 := parseOK(t, c.line, c.spaceAge, first)
		second := formValues(t, renderPartial(t, i18n.English, f1))
		f2, m2 := parseOK(t, c.line, c.spaceAge, second)
		j1, _ := json.Marshal(m1)
		j2, _ := json.Marshal(m2)
		if !bytes.Equal(j1, j2) {
			t.Errorf("%s sa=%v: file changed on the second trip\n%s\n%s", c.line, c.spaceAge, j1, j2)
		}
		if f2.Seed.Value != "42" || !f2.Peaceful.Checked || f2.StartingArea.Value != "71" {
			t.Errorf("%s: the page lost the fixed fields: %+v", c.line, f2.Seed)
		}
		if !strings.Contains(f2.Raw.Value, "elevation_island") {
			t.Errorf("%s: the raw box lost its text", c.line)
		}
	}
}

var (
	reSelect   = regexp.MustCompile(`(?s)<select id="[^"]*" name="([^"]+)"[^>]*>(.*?)</select>`)
	reSelected = regexp.MustCompile(`<option value="([^"]*)" selected>`)
)

// formValues reads a page as a browser would post it.
func formValues(t *testing.T, page string) url.Values {
	t.Helper()
	return fieldValues(page)
}

func renderPartial(t *testing.T, loc i18n.Locale, f MapGenForm) string {
	t.Helper()
	r := newTestRenderer(t)
	var b bytes.Buffer
	if err := r.pages[loc]["generate"].ExecuteTemplate(&b, "map-settings", GenerateForm{MapGen: f, Preset: "default", Presets: []string{"default"}}); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// The raw box starts as the game's example without what the fields cover,
// and what it holds passes the same check as a typed edit (SPEC 11.2).
func TestRawBoxPrefill(t *testing.T) {
	for _, line := range mapgen.Lines {
		raw := NewMapGenForm(line, false).Raw.Value
		if fes := gamesettings.Validate(gamesettings.FileMapGen, line, []byte(raw)); len(fes) > 0 {
			t.Errorf("%s: prefill fails Validate: %+v\n%s", line, fes, raw)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		for _, covered := range []string{"width", "height", "starting_area", "peaceful_mode", "seed", "autoplace_controls"} {
			if _, ok := m[covered]; ok {
				t.Errorf("%s: the raw box repeats %s", line, covered)
			}
		}
		for k := range m {
			if strings.HasPrefix(k, "_") {
				t.Errorf("%s: comment key %s in the raw box", line, k)
			}
		}
		if _, ok := m["property_expression_names"]; !ok {
			t.Errorf("%s: the raw box lacks property_expression_names", line)
		}
		_, has := m["cliff_settings"]
		if has != (line == gamesettings.Line11) {
			t.Errorf("%s: cliff_settings in the raw box = %v", line, has)
		}
		if line == gamesettings.Line11 {
			cs := m["cliff_settings"].(map[string]any)
			if _, ok := cs["cliff_elevation_interval"]; ok {
				t.Errorf("%s: the raw box repeats the cliff interval", line)
			}
			if _, ok := cs["richness"]; ok {
				t.Errorf("%s: the raw box repeats cliff continuity", line)
			}
		}
		if _, ok := m["terrain_segmentation"]; ok {
			t.Errorf("%s: raw repeats terrain_segmentation", line)
		}
	}
}

// The form wins over the raw box for the keys it covers; the raw box adds
// the rest, including a mod's control (DESIGN 8.7).
func TestFormWinsOverRawBox(t *testing.T) {
	_, m := parseOK(t, "2.0", false, url.Values{
		mgWidth:                             {"300"},
		fieldName("nauvis", "coal", "size"): {"200"},
		mgRaw: {`{
  "width": 99, "seed": 5, "peaceful_mode": true, "starting_area": 9,
  "autoplace_controls": {
    "coal": {"size": 4, "note": 1},
    "modded-ore": {"frequency": 2, "size": 3, "richness": 4}
  },
  "property_expression_names": {"elevation": "elevation_island", "aux": "aux_basic"},
  "default_enable_all_autoplace_controls": false
}`},
	})
	approx(t, "width", m["width"], 300)
	if m["seed"] != nil || m["peaceful_mode"] != false {
		t.Errorf("raw keys beat the form: seed %v, peaceful %v", m["seed"], m["peaceful_mode"])
	}
	approx(t, "starting_area", m["starting_area"], 1)
	ac := sub(t, m, "autoplace_controls")
	approx(t, "coal size", sub(t, ac, "coal")["size"], 2)
	approx(t, "kept raw sub-key", sub(t, ac, "coal")["note"], 1)
	approx(t, "mod control", sub(t, ac, "modded-ore")["richness"], 4)
	pen := sub(t, m, "property_expression_names")
	if pen["aux"] != "aux_basic" {
		t.Error("the raw box's own key was dropped")
	}
	if _, ok := pen["elevation"]; ok {
		t.Errorf("the raw box's map type beat the form's default: %v", pen["elevation"])
	}
	if m["default_enable_all_autoplace_controls"] != false {
		t.Error("an uncovered raw key was dropped")
	}
}

// A mistake in the raw box is reported by line and reason, with the line's
// text, all at once (SPEC 11.2), and the typed text is kept.
func TestRawBoxErrors(t *testing.T) {
	cases := []struct {
		name, raw string
		line      int
		key       string
	}{
		{"syntax", "{\n  \"width\": 1,\n  \"seed\": ,\n}", 3, "mapgen.raw.err.syntax"},
		{"not an object", "[1, 2]", 0, "mapgen.raw.err.not_object"},
		{"wrong type", "{\n  \"peaceful_mode\": 3,\n  \"starting_points\": []\n}", 2, "mapgen.raw.err.type"},
		{"out of range", "{\n  \"cliff_settings\": {\n    \"richness\": 99\n  }\n}", 3, "mapgen.raw.err.range"},
	}
	for _, c := range cases {
		f, out, problems := ParseMapGenForm(postForm(t, url.Values{mgRaw: {c.raw}}), "1.1", false)
		if out != nil || len(problems) == 0 {
			t.Errorf("%s: accepted", c.name)
			continue
		}
		p := problems[0]
		if p.FieldID != mgRawID || p.Message.Key != c.key {
			t.Errorf("%s: problem %s %v", c.name, p.FieldID, p.Message)
		}
		if c.line > 0 && p.Message.Args["line"] != c.line {
			t.Errorf("%s: line %v, want %d", c.name, p.Message.Args["line"], c.line)
		}
		if !f.RawOpen || !f.RawInvalid || f.Raw.Value != c.raw {
			t.Errorf("%s: raw box open=%v invalid=%v kept=%v", c.name, f.RawOpen, f.RawInvalid, f.Raw.Value == c.raw)
		}
		body := renderPartial(t, i18n.English, f)
		if !strings.Contains(body, `<details id="mg-raw-details" open>`) || !strings.Contains(body, `aria-invalid="true" aria-describedby="mg-raw-hint error-summary"`) {
			t.Errorf("%s: the raw box does not show the error", c.name)
		}
	}
	// Two mistakes are two problems.
	_, _, problems := ParseMapGenForm(postForm(t, url.Values{mgRaw: {"{\"width\": \"a\", \"peaceful_mode\": 3}"}}), "2.0", false)
	if len(problems) != 2 {
		t.Errorf("%d problems for two mistakes", len(problems))
	}
	// The limit is the settings limit (SPEC 14.6).
	big := "{\"x\": \"" + strings.Repeat("a", gamesettings.MaxBytes) + "\"}"
	if _, out, problems := ParseMapGenForm(postForm(t, url.Values{mgRaw: {big}}), "2.0", false); out != nil || len(problems) == 0 || problems[0].Message.Key != "mapgen.raw.err.too_large" {
		t.Error("an over-size raw box was not refused")
	}
	// An empty box is allowed.
	parseOK(t, "2.0", false, url.Values{mgRaw: {"   \n"}})
}

// Every message the form can show reads in every language, with all its
// placeholders filled (SPEC 22.3).
var rePlaceholderLeft = regexp.MustCompile(`\{[a-z_]+\}`)

func TestMessagesRenderInEveryLanguage(t *testing.T) {
	_, _, problems := ParseMapGenForm(postForm(t, url.Values{
		mgSeed: {"x"}, mgWidth: {"x"}, mgStart: {"x"}, fieldName("nauvis", "coal", "size"): {"x"}, "mg.bogus": {"1"},
		fieldName("vulcanus", "calcite", "size"): {"x"},
		mgRaw:                                    {"{\"width\": [1], \"cliff_settings\": {\"richness\": 99}, \"seed\": \"s\",\n\"peaceful_mode\": 3,\n \"x\": ,}"},
	}), "2.0", true)
	_, _, more := ParseMapGenForm(postForm(t, url.Values{mgRaw: {"{\"cliff_settings\": {\"richness\": 99},\n \"starting_areas\": 1,\n \"seed\": \"s\", \"autoplace_controls\": {\"coal\": {\"size\": -1}}}"}}), "1.1", false)
	problems = append(problems, more...)
	_, _, more = ParseMapGenForm(postForm(t, url.Values{mgRaw: {"[1]"}}), "1.1", false)
	problems = append(problems, more...)
	if len(problems) < 8 {
		t.Fatalf("only %d problems to check", len(problems))
	}
	for _, l := range i18n.Languages {
		for _, p := range problems {
			s := l.Tag.Format(p.Message)
			if s == "" || rePlaceholderLeft.MatchString(s) || strings.Contains(s, "mapgen.") {
				t.Errorf("%s: %s renders as %q", l.Tag, p.Message.Key, s)
			}
		}
	}
}

// The page is well formed in every language: unique ids, a label for every
// field, every described-by target present, nothing hard-coded (SPEC 17, 22).
func TestPartialMarkup(t *testing.T) {
	reID := regexp.MustCompile(`\sid="([^"]+)"`)
	reFor := regexp.MustCompile(`<label[^>]* for="([^"]+)"`)
	reDesc := regexp.MustCompile(`aria-describedby="([^"]+)"`)
	reWrapped := regexp.MustCompile(`<label[^>]*><input[^>]* id="([^"]+)"`)
	reField := regexp.MustCompile(`<(?:select|textarea|input)[^>]* id="([^"]+)"`)
	reLabelledBy := regexp.MustCompile(`<input[^>]* id="([^"]+)"[^>]* aria-labelledby="([^"]+)"`)
	for _, c := range testLines {
		for _, l := range i18n.Languages {
			body := renderPartial(t, l.Tag, NewMapGenForm(c.line, c.spaceAge))
			ids := map[string]int{}
			for _, m := range reID.FindAllStringSubmatch(body, -1) {
				ids[m[1]]++
			}
			for id, n := range ids {
				if n > 1 {
					t.Errorf("%s %s: id %s appears %d times", c.line, l.Tag, id, n)
				}
			}
			labelled := map[string]bool{}
			// A checkbox inside its label has no "for".
			for _, m := range reWrapped.FindAllStringSubmatch(body, -1) {
				labelled[m[1]] = true
			}
			// A slider is named by its select's label.
			for _, m := range reLabelledBy.FindAllStringSubmatch(body, -1) {
				labelled[m[1]] = ids[m[2]] > 0
			}
			for _, m := range reFor.FindAllStringSubmatch(body, -1) {
				labelled[m[1]] = true
				if ids[m[1]] == 0 {
					t.Errorf("%s %s: label for missing %s", c.line, l.Tag, m[1])
				}
			}
			for _, m := range reField.FindAllStringSubmatch(body, -1) {
				if !labelled[m[1]] {
					t.Errorf("%s %s: field %s has no label", c.line, l.Tag, m[1])
				}
			}
			for _, m := range reDesc.FindAllStringSubmatch(body, -1) {
				for _, id := range strings.Fields(m[1]) {
					// The error summary is the page's, not the partial's.
					if ids[id] == 0 && id != "error-summary" {
						t.Errorf("%s %s: aria-describedby points at missing %s", c.line, l.Tag, id)
					}
				}
			}
			if strings.Contains(body, "mapgen.") || strings.Contains(body, "%!") || strings.Contains(body, "{percent}") {
				t.Errorf("%s %s: unfilled text in the page", c.line, l.Tag)
			}
		}
	}
}

// Closed groups still ship every field, but the page stays small enough for
// a phone (SPEC 17.2).
func TestPartialSize(t *testing.T) {
	body := renderPartial(t, i18n.English, NewMapGenForm("2.1", true))
	if len(body) > 100_000 {
		t.Errorf("the form is %d bytes", len(body))
	}
}
