package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// An unticked resource is written with size 0, the value that removes it on
// every line; frequency 0 crashes 2.1 (SPEC 5.6). Its other fields, which a
// disabled fieldset does not send, keep their defaults.
func TestResourceOffIsSizeZero(t *testing.T) {
	for _, line := range []string{"1.1", "2.0", "2.1"} {
		_, m := parseOK(t, line, false, url.Values{fieldName("nauvis", "iron-ore", mgOn): {"off"}})
		iron := sub(t, m, "autoplace_controls", "iron-ore")
		approx(t, line+" iron size", iron["size"], 0)
		approx(t, line+" iron frequency", iron["frequency"], 1)
		approx(t, line+" coal size", sub(t, m, "autoplace_controls", "coal")["size"], 1)
	}
}

// Off wins over a size the post still carries.
func TestResourceOffWinsOverItsSize(t *testing.T) {
	_, m := parseOK(t, "2.1", false, url.Values{
		fieldName("nauvis", "coal", mgOn):   {"off"},
		fieldName("nauvis", "coal", "size"): {"200"},
	})
	approx(t, "coal size", sub(t, m, "autoplace_controls", "coal")["size"], 0)
}

// A ticked box posts "off" then "on"; a post without the name at all keeps
// the resource on, so a stray post never removes one.
func TestResourceOnPosts(t *testing.T) {
	for name, v := range map[string]url.Values{
		"ticked":  {fieldName("nauvis", "coal", mgOn): {"off", "on"}},
		"missing": {},
	} {
		f, m := parseOK(t, "2.1", false, v)
		approx(t, name+" coal size", sub(t, m, "autoplace_controls", "coal")["size"], 1)
		f.withPreset(NewMapGenFormForPreset("2.1", false, "default"))
		if f.Modified() {
			t.Errorf("%s: form is marked modified", name)
		}
	}
	f, _ := parseOK(t, "2.1", false, url.Values{fieldName("nauvis", "coal", mgOn): {"off"}})
	f.withPreset(NewMapGenFormForPreset("2.1", false, "default"))
	if !f.Modified() {
		t.Error("switching a resource off does not mark the form modified")
	}
}

// Only resources get a box, and their size then offers no "None".
func TestOnlyResourcesHaveABox(t *testing.T) {
	f := NewMapGenFormForPreset("2.1", true, "default")
	for _, p := range f.Resources {
		for _, g := range p.Groups {
			if g.Toggle == nil {
				t.Errorf("%s: resource %v has no on/off box", p.ID, g.Legend)
				continue
			}
			for _, s := range g.Selects {
				if strings.HasSuffix(s.Name, ".size") && hasOption(s, mgNone) {
					t.Errorf("%s offers None beside its box", s.Name)
				}
			}
		}
	}
	for _, p := range f.Terrain {
		for _, g := range p.Groups {
			if g.Toggle != nil {
				t.Errorf("%s: terrain control has a box", p.ID)
			}
		}
	}
	for _, g := range f.Enemy {
		if g.Toggle != nil {
			t.Error("enemy bases have a box")
		}
	}
}

// The map type goes into property_expression_names; the default leaves the
// key out, as the game's example says.
func TestMapTypeIsWritten(t *testing.T) {
	cases := []struct{ line, mapType string }{{"2.0", "elevation_lakes"}, {"2.1", "elevation_island"}, {"1.1", "0_17-island"}}
	for _, c := range cases {
		_, m := parseOK(t, c.line, false, url.Values{mgMapType: {c.mapType}})
		if got := sub(t, m, "property_expression_names")["elevation"]; got != c.mapType {
			t.Errorf("%s: elevation = %v, want %s", c.line, got, c.mapType)
		}
		_, m = parseOK(t, c.line, false, url.Values{mgMapType: {"elevation"}})
		if got, ok := sub(t, m, "property_expression_names")["elevation"]; ok {
			t.Errorf("%s: default map type wrote elevation %v", c.line, got)
		}
	}
	_, _, problems := ParseMapGenForm(postForm(t, url.Values{mgMapType: {"elevation_lakes"}}), "1.1", false)
	if len(problems) != 1 || problems[0].FieldID != "mg-map-type" {
		t.Errorf("a map type 1.1 lacks: problems %+v", problems)
	}
}

// The scales are stored inverted, as text, under each line's own keys.
func TestTerrainScalesAreWritten(t *testing.T) {
	_, m := parseOK(t, "2.1", false, url.Values{mgMoistureScale: {"133"}, mgAuxScale: {"17"}})
	pen := sub(t, m, "property_expression_names")
	if pen["control:moisture:frequency"] != "0.75" {
		t.Errorf("moisture %v, want \"0.75\"", pen["control:moisture:frequency"])
	}
	if pen["control:aux:frequency"] != "6" {
		t.Errorf("aux %v, want \"6\"", pen["control:aux:frequency"])
	}
	_, _, problems := ParseMapGenForm(postForm(t, url.Values{mgMoistureScale: {"141"}}), "2.1", false)
	if len(problems) != 1 || problems[0].FieldID != "mg-moisture-scale" {
		t.Errorf("water's 141%% step is not the moisture scale's: problems %+v", problems)
	}
	_, m = parseOK(t, "2.0", false, url.Values{mgMoistureScale: {"50"}})
	if got := sub(t, m, "property_expression_names")["control:moisture:frequency"]; got != "2" {
		t.Errorf("2.0 moisture %v, want \"2\"", got)
	}
	_, m = parseOK(t, "1.1", false, url.Values{mgMoistureScale: {"200"}})
	if got := sub(t, m, "property_expression_names")["control-setting:moisture:frequency:multiplier"]; got != "0.5" {
		t.Errorf("1.1 moisture = %v, want \"0.5\"", got)
	}
}

// The scale steps are the ones the game's slider shows.
func TestTerrainScaleSteps(t *testing.T) {
	var codes []string
	for _, o := range optionsTerrainScale {
		codes = append(codes, o.Value)
		if _, ok := stepValue(o.Value); !ok {
			t.Errorf("step %s has no value", o.Value)
		}
	}
	if strings.Join(codes, ",") != "17,25,33,50,75,100,133,150,200,300,400,600" {
		t.Errorf("scale steps %v", codes)
	}
}

// Map type and the scales sit on Nauvis's Terrain tab; the map type is a
// plain select, never a slider.
func TestTerrainFieldsSitOnTheTerrainTab(t *testing.T) {
	f := NewMapGenFormForPreset("2.1", true, "default")
	for _, id := range []string{"mg-map-type", "mg-moisture-scale", "mg-aux-scale"} {
		if tab := f.TabOf(id); tab != tabTerrain {
			t.Errorf("%s is on tab %q", id, tab)
		}
	}
	page := renderPartial(t, i18n.Locale("en"), f)
	if !strings.Contains(page, `<div class="field">
  <label for="mg-map-type"`) {
		t.Error("map type is not a plain field")
	}
	for _, want := range []string{`<option value="elevation_island">Island elevation</option>`,
		`data-disables="mg-nauvis-iron-ore"`, `<fieldset class="autoplace" id="mg-nauvis-iron-ore">`,
		`<input type="hidden" name="mg.nauvis.iron-ore.on" value="off">`, `>Moisture</legend>`, `>Terrain type</legend>`} {
		if !strings.Contains(page, want) {
			t.Errorf("partial lacks %s", want)
		}
	}
}

// A bias is written only when it is not 0, as the game's own generator
// writes it, under each line's own key.
func TestBiasIsWrittenOnlyWhenSet(t *testing.T) {
	_, m := parseOK(t, "2.1", false, url.Values{mgMoistureBias: {"50"}, mgAuxBias: {"-45"}})
	pen := sub(t, m, "property_expression_names")
	if pen["control:moisture:bias"] != "0.5" || pen["control:aux:bias"] != "-0.45" {
		t.Errorf("biases = %v, %v; want \"0.5\", \"-0.45\"", pen["control:moisture:bias"], pen["control:aux:bias"])
	}
	_, m = parseOK(t, "2.1", false, url.Values{})
	pen = sub(t, m, "property_expression_names")
	for _, k := range []string{"control:moisture:bias", "control:aux:bias"} {
		if v, ok := pen[k]; ok {
			t.Errorf("default bias wrote %s = %v", k, v)
		}
	}
	_, _, problems := ParseMapGenForm(postForm(t, url.Values{mgAuxBias: {"7"}}), "2.1", false)
	if len(problems) != 1 || problems[0].FieldID != "mg-aux-bias" {
		t.Errorf("a bias off the steps: problems %+v", problems)
	}
	_, m = parseOK(t, "1.1", false, url.Values{mgMoistureBias: {"-50"}})
	if got := sub(t, m, "property_expression_names")["control-setting:moisture:bias"]; got != "-0.5" {
		t.Errorf("1.1 moisture bias = %v, want \"-0.5\"", got)
	}
	_, m = parseOK(t, "2.0", false, url.Values{mgAuxBias: {"5"}})
	if got := sub(t, m, "property_expression_names")["control:aux:bias"]; got != "0.05" {
		t.Errorf("2.0 aux bias = %v, want \"0.05\"", got)
	}
}

// The bias steps are the game's: -0.5 to 0.5 by 0.05.
func TestBiasSteps(t *testing.T) {
	if len(optionsBias) != 21 || optionsBias[0].Value != "-50" || optionsBias[20].Value != "50" || optionsBias[10].Value != mgBiasDefault {
		t.Errorf("bias options %v", optionsBias)
	}
}
