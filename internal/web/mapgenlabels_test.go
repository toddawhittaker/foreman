package web

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"maps"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// sharedCatalogue is 2.1 with Space Age plus a modded planet, Moshine, whose
// own steam_geyser has a richness default of 150, whose mod relabels the
// built-in stone, and which shares stone with Nauvis (SPEC 5.6).
func sharedCatalogue() mapgen.Catalogue {
	cat := mapgen.Builtin("2.1", true)
	cat.Controls = maps.Clone(cat.Controls)
	stone := cat.Controls["stone"]
	stone.Label = "Mod stone"
	stone.Planets = append(slices.Clone(stone.Planets), "moshine")
	cat.Controls["stone"] = stone
	cat.Controls["steam_geyser"] = mapgen.CatalogueControl{
		Control: mapgen.Control{Name: "steam_geyser", Category: mapgen.CategoryResource, Richness: true, CanBeDisabled: true},
		Label:   `<b>Steam</b> geyser`, Planets: []string{"moshine"},
	}
	cat.Controls["moshine_dust"] = mapgen.CatalogueControl{
		Control: mapgen.Control{Name: "moshine_dust", Category: mapgen.CategoryTerrain},
		Label:   "Dust", Planets: []string{"moshine"},
	}
	rich := 150.0
	cat.Planets = append(slices.Clone(cat.Planets), mapgen.Planet{
		Name: "moshine", Label: "Moshine", Controls: []string{"steam_geyser", "stone", "moshine_dust"},
		Defaults: map[string]mapgen.ControlDefault{"steam_geyser": {Richness: &rich}},
	})
	return cat
}

func groupOf(t *testing.T, f MapGenForm, id string) MapGenGroup {
	t.Helper()
	for _, tab := range [][]MapGenPlanet{f.Resources, f.Terrain} {
		for _, p := range tab {
			for _, g := range p.Groups {
				if g.ID == id {
					return g
				}
			}
		}
	}
	t.Fatalf("no group %s", id)
	return MapGenGroup{}
}

func selectOf(t *testing.T, g MapGenGroup, key string) MapGenSelect {
	t.Helper()
	for _, s := range g.Selects {
		if strings.HasSuffix(s.Name, "."+key) {
			return s
		}
	}
	t.Fatalf("%s has no %s", g.ID, key)
	return MapGenSelect{}
}

// SPEC 5.6, 14.6, 22: a name Foreman knows keeps its own translated label
// even when a mod relabels it; a name it does not know shows the mod's
// label, marked foreign so the page says translate="no".
func TestModLabelsOnlyForUnknownNames(t *testing.T) {
	f := mapGenFormForPreset("2.1", sharedCatalogue(), "default")
	stone := groupOf(t, f, "mg-moshine-stone")
	if stone.Foreign || stone.Legend.Key != "mapgen.control.stone" {
		t.Errorf("stone legend %+v, foreign %t: want Foreman's own", stone.Legend, stone.Foreign)
	}
	geyser := groupOf(t, f, "mg-moshine-steam_geyser")
	if !geyser.Foreign || geyser.Legend.Key != "mapgen.mod_name" || geyser.Legend.Args["name"] != `<b>Steam</b> geyser` {
		t.Errorf("geyser legend %+v, foreign %t", geyser.Legend, geyser.Foreign)
	}
	at := slices.IndexFunc(f.Resources, func(p MapGenPlanet) bool { return p.Planet == "moshine" })
	if at < 0 || !f.Resources[at].Foreign || f.Resources[at].Summary.Args["name"] != "Moshine" {
		t.Errorf("Moshine's row is not the mod's own label")
	}
	if p := f.Resources[0]; p.Planet != "nauvis" || p.Foreign {
		t.Errorf("Nauvis's row %+v", p)
	}
}

// SPEC 14.6: a mod's label is cut, so a long one cannot flood the page.
func TestModLabelIsCut(t *testing.T) {
	long := string(bytes.Repeat([]byte("ö"), 300))
	if got := []rune(modLabel(long, "x")); len(got) != modLabelMax {
		t.Errorf("label of %d runes, want %d", len(got), modLabelMax)
	}
	if modLabel("  ", "name") != "name" {
		t.Error("an empty label does not fall back to the name")
	}
}

// SPEC 5.6: a control shared between planets says which others it changes,
// under each planet, and is written once.
func TestSharedControlSaysWhatElseItChanges(t *testing.T) {
	cat := sharedCatalogue()
	f := mapGenFormForPreset("2.1", cat, "default")
	nauvis := groupOf(t, f, "mg-nauvis-stone")
	if len(nauvis.Also) != 1 || nauvis.Also[0].Name.Args["name"] != "Moshine" || !nauvis.Also[0].Foreign {
		t.Errorf("Nauvis's stone also changes %+v", nauvis.Also)
	}
	moshine := groupOf(t, f, "mg-moshine-stone")
	if len(moshine.Also) != 1 || moshine.Also[0].Name.Key != "mapgen.planet.nauvis" {
		t.Errorf("Moshine's stone also changes %+v", moshine.Also)
	}
	if g := groupOf(t, f, "mg-moshine-steam_geyser"); len(g.Also) != 0 {
		t.Errorf("an own control also changes %+v", g.Also)
	}
	_, out, problems := parseMapGenIn(url.Values{mgSeed: {"1"}}, "2.1", cat)
	if len(problems) > 0 || bytes.Count(out, []byte(`"stone"`)) != 1 {
		t.Errorf("problems %v; stone written %d times in %s", problems, bytes.Count(out, []byte(`"stone"`)), out)
	}
	// Either planet's copy sets the one value.
	for _, planet := range []string{"nauvis", "moshine"} {
		_, out, _ := parseMapGenIn(url.Values{mgSeed: {"1"}, fieldName(planet, "stone", "size"): {"300"}}, "2.1", cat)
		var file struct {
			Controls map[string]map[string]any `json:"autoplace_controls"`
		}
		if err := json.Unmarshal(out, &file); err != nil || file.Controls["stone"]["size"] != 3.0 {
			t.Errorf("%s's stone moved to 300%%: %v (%v)", planet, file.Controls["stone"], err)
		}
	}
}

// SPEC 5.6: richness is offered only where the control has it.
func TestRichnessOnlyWhereTheControlHasIt(t *testing.T) {
	f := mapGenFormForPreset("2.1", sharedCatalogue(), "default")
	for id, want := range map[string]bool{"mg-moshine-steam_geyser": true, "mg-moshine-moshine_dust": false} {
		g := groupOf(t, f, id)
		has := slices.ContainsFunc(g.Selects, func(s MapGenSelect) bool { return strings.HasSuffix(s.Name, ".richness") })
		if has != want {
			t.Errorf("%s has richness %t, want %t", id, has, want)
		}
	}
}

// SPEC 5.6: a planet's own default no step stands for (Moshine's geyser
// richness 150) is shown as its value and written back unchanged, unless
// the control is moved.
func TestPlanetDefaultIsShownAndKept(t *testing.T) {
	cat := sharedCatalogue()
	f := mapGenFormForPreset("2.1", cat, "default")
	sel := selectOf(t, groupOf(t, f, "mg-moshine-steam_geyser"), "richness")
	if sel.Value != "x150" {
		t.Fatalf("richness starts on %q, want x150", sel.Value)
	}
	at := slices.IndexFunc(sel.Options, func(o MapGenOption) bool { return o.Value == "x150" })
	if at < 0 || sel.Options[at].Label.Key != "mapgen.step.planet_default" || sel.Options[at].Label.Args["percent"] != "15000" {
		t.Fatalf("options %+v", sel.Options)
	}
	if at != len(sel.Options)-1 {
		t.Errorf("15000%% sits at %d of %d, not by value", at, len(sel.Options))
	}
	f.withPreset(mapGenFormForPreset("2.1", cat, "default"))
	if f.Modified() {
		t.Error("the form opens modified")
	}

	richness := func(vals url.Values) any {
		t.Helper()
		_, out, problems := parseMapGenIn(vals, "2.1", cat)
		if len(problems) > 0 {
			t.Fatalf("problems %v", problems)
		}
		var file struct {
			Controls map[string]map[string]any `json:"autoplace_controls"`
		}
		if err := json.Unmarshal(out, &file); err != nil {
			t.Fatal(err)
		}
		return file.Controls["steam_geyser"]["richness"]
	}
	field := fieldName("moshine", "steam_geyser", "richness")
	if got := richness(url.Values{mgSeed: {"1"}}); got != 150.0 {
		t.Errorf("untouched: richness %v, want 150", got)
	}
	if got := richness(url.Values{mgSeed: {"1"}, field: {"x150"}}); got != 150.0 {
		t.Errorf("posted as shown: richness %v, want 150", got)
	}
	if got := richness(url.Values{mgSeed: {"1"}, field: {"200"}}); got != 2.0 {
		t.Errorf("moved to 200%%: richness %v, want 2", got)
	}
	// Another value outside the steps is refused, not written.
	if _, out, problems := parseMapGenIn(url.Values{field: {"x999"}}, "2.1", cat); out != nil || len(problems) == 0 {
		t.Errorf("x999 accepted: %s", out)
	}
	// The value is the planet's alone.
	if _, out, problems := parseMapGenIn(url.Values{fieldName("nauvis", "iron-ore", "richness"): {"x150"}}, "2.1", cat); out != nil || len(problems) == 0 {
		t.Errorf("x150 accepted on Nauvis: %s", out)
	}
}

// SPEC 5.6: a default above 1000 is not a real one and is left alone.
func TestPlanetDefaultAboveTheCeilingIsIgnored(t *testing.T) {
	for _, v := range []float64{1001, 0, -2} {
		if _, ok := (mgControl{def: mapgen.ControlDefault{Richness: &v}}).defaultOf(mgSetting{key: "richness"}); ok {
			t.Errorf("default %v used", v)
		}
	}
	two := 2.0
	f := mapGenFormForPreset("2.1", withGeyserDefault(two), "default")
	if sel := selectOf(t, groupOf(t, f, "mg-moshine-steam_geyser"), "richness"); sel.Value != "200" {
		t.Errorf("a default on a step starts on %q, want 200", sel.Value)
	}
}

func withGeyserDefault(v float64) mapgen.Catalogue {
	cat := sharedCatalogue()
	cat.Planets = slices.Clone(cat.Planets)
	last := &cat.Planets[len(cat.Planets)-1]
	last.Defaults = map[string]mapgen.ControlDefault{"steam_geyser": {Richness: &v}}
	return cat
}

func solidPNG(t *testing.T, size int, split bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			c := color.RGBA{10, 20, 30, 255}
			if split && x < size/2 {
				c = color.RGBA{200, 20, 30, 255}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// SPEC 5.6: a picture of about one color gets the note; others and
// unreadable ones do not.
func TestOneColor(t *testing.T) {
	if !oneColor(solidPNG(t, 256, false)) {
		t.Error("a solid picture is not one color")
	}
	if oneColor(solidPNG(t, 256, true)) {
		t.Error("a two-color picture is one color")
	}
	if oneColor([]byte("not a png")) {
		t.Error("an unreadable picture is one color")
	}
}

// stoneSize is the size the file writes for stone.
func stoneSize(t *testing.T, out []byte) any {
	t.Helper()
	var file struct {
		Controls map[string]map[string]any `json:"autoplace_controls"`
	}
	if err := json.Unmarshal(out, &file); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return file.Controls["stone"]["size"]
}

// renderedPost is the post the form makes as shown: every select and box
// with its value, and each shared field's start value.
func renderedPost(f MapGenForm) url.Values {
	vals := url.Values{mgSeed: {"1"}}
	for _, s := range f.selects() {
		vals.Set(s.Name, s.Value)
		if s.Shared {
			vals.Set(s.StartName(), s.StartValue())
		}
	}
	for _, c := range f.toggles() {
		vals.Set(c.Name, map[bool]string{true: "on", false: "off"}[c.Checked])
		if c.Shared {
			vals.Set(c.StartName(), c.StartValue())
		}
	}
	return vals
}

// SPEC 5.6: a form opened from a file with stone at 300% keeps the user's
// edit of either planet's copy, and two copies moved to different values
// are an error that names both planets.
func TestSharedControlKeepsTheEditFromAFile(t *testing.T) {
	cat := sharedCatalogue()
	opened, _, problems := parseMapGenIn(fileValuesIn("2.1", cat, []byte(`{"autoplace_controls":{"stone":{"size":3}}}`), "1", "", true), "2.1", cat)
	if len(problems) > 0 {
		t.Fatalf("opening: %v", problems)
	}
	for planet, want := range map[string]float64{"nauvis": 0.5, "moshine": 0.5} {
		vals := renderedPost(opened)
		vals.Set(fieldName(planet, "stone", "size"), "50")
		_, out, problems := parseMapGenIn(vals, "2.1", cat)
		if len(problems) > 0 || stoneSize(t, out) != want {
			t.Errorf("%s moved to 50%%: problems %v, size %v", planet, problems, stoneSize(t, out))
		}
	}
	// Untouched, the file's value stays.
	if _, out, _ := parseMapGenIn(renderedPost(opened), "2.1", cat); stoneSize(t, out) != 3.0 {
		t.Errorf("untouched: size %v, want 3", stoneSize(t, out))
	}
	vals := renderedPost(opened)
	vals.Set(fieldName("nauvis", "stone", "size"), "50")
	vals.Set(fieldName("moshine", "stone", "size"), "200")
	f, out, problems := parseMapGenIn(vals, "2.1", cat)
	if out != nil || len(problems) != 2 || problems[0].Message.Key != "mapgen.err.shared" {
		t.Fatalf("both moved: out %s, problems %v", out, problems)
	}
	if m := problems[0].Message; m.Args["first"].(i18n.Msg).Key != "mapgen.planet.nauvis" || m.Args["second"].(i18n.Msg).Args["name"] != "Moshine" {
		t.Errorf("the error names %v", m.Args)
	}
	if s := selectOf(t, groupOf(t, f, "mg-moshine-stone"), "size"); s.Value != "200" || s.Start != "300" {
		t.Errorf("Moshine's copy came back as %q from %q", s.Value, s.Start)
	}
}

// SPEC 5.6: a small inverted default (a scale of 1/0.0005) is not shown,
// so it can never be written as 0.
func TestInvertedPlanetDefaultIsBounded(t *testing.T) {
	tiny, ok := 0.0005, false
	scale := mgSetting{key: "frequency", invert: true}
	if _, ok = (mgControl{def: mapgen.ControlDefault{Frequency: &tiny}}).defaultOf(scale); ok {
		t.Error("a default shown as 2000 is used")
	}
	cat := sharedCatalogue()
	cat.Planets = slices.Clone(cat.Planets)
	last := &cat.Planets[len(cat.Planets)-1]
	last.Defaults = map[string]mapgen.ControlDefault{"moshine_dust": {Frequency: &tiny}}
	_, out, problems := parseMapGenIn(url.Values{mgSeed: {"1"}}, "2.1", cat)
	var file struct {
		Controls map[string]map[string]any `json:"autoplace_controls"`
	}
	if len(problems) > 0 || json.Unmarshal(out, &file) != nil || file.Controls["moshine_dust"]["frequency"] == 0.0 {
		t.Errorf("problems %v, dust %v", problems, file.Controls["moshine_dust"])
	}
	half := 0.5
	if v, ok := (mgControl{def: mapgen.ControlDefault{Frequency: &half}}).defaultOf(scale); !ok || v != 0.5 {
		t.Errorf("a scale of 200%% is not used: %v %t", v, ok)
	}
}

// SPEC 5.6: a saved file or favorite holding the planet's own 150 opens on
// that entry again.
func TestSavedPlanetDefaultReopens(t *testing.T) {
	cat := sharedCatalogue()
	file := []byte(`{"autoplace_controls":{"steam_geyser":{"frequency":1,"size":1,"richness":150}}}`)
	f, out, problems := parseMapGenIn(fileValuesIn("2.1", cat, file, "1", "", true), "2.1", cat)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if s := selectOf(t, groupOf(t, f, "mg-moshine-steam_geyser"), "richness"); s.Value != "x150" {
		t.Errorf("reopened on %q, want x150", s.Value)
	}
	if !bytes.Contains(out, []byte(`"richness": 150`)) {
		t.Errorf("not written back as 150: %s", out)
	}
}
