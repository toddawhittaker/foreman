package web

import (
	"maps"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// moddedCatalogue is the built-in 2.1 catalogue with one more planet and its
// own resource control, as a world's mods may give (SPEC 5.6).
func moddedCatalogue() mapgen.Catalogue {
	cat := mapgen.Builtin("2.1", true)
	cat.Controls = maps.Clone(cat.Controls)
	cat.Controls["moshine_ore"] = mapgen.CatalogueControl{
		Control: mapgen.Control{Name: "moshine_ore", Category: mapgen.CategoryResource, Richness: true, CanBeDisabled: true},
		Label:   "Moshine ore", Planets: []string{"moshine"},
	}
	cat.Planets = append(slices.Clone(cat.Planets), mapgen.Planet{Name: "moshine", Label: "Moshine", Controls: []string{"moshine_ore"}})
	return cat
}

func TestModdedPlanetFieldsParse(t *testing.T) {
	name := fieldName("moshine", "moshine_ore", "size")
	f, out, problems := parseMapGenIn(url.Values{name: {"200"}}, "2.1", moddedCatalogue())
	if len(problems) > 0 {
		t.Fatalf("problems %v", problems)
	}
	if !slices.ContainsFunc(f.Resources, func(p MapGenPlanet) bool { return p.Planet == "moshine" }) {
		t.Error("no Moshine rows in the form")
	}
	if !strings.Contains(string(out), `"moshine_ore"`) {
		t.Errorf("moshine_ore missing from %s", out)
	}
}

func TestUnknownPlanetFieldIsStillRefused(t *testing.T) {
	// Without the mod, the field is no field of any form.
	_, out, problems := parseMapGenIn(url.Values{fieldName("moshine", "moshine_ore", "size"): {"200"}}, "2.1", mapgen.Builtin("2.1", true))
	if out != nil || len(problems) == 0 || problems[0].Message.Key != "mapgen.err.unknown" {
		t.Errorf("out %s, problems %v", out, problems)
	}
}

func TestReadMapFormUsesTheCatalogue(t *testing.T) {
	cat := moddedCatalogue()
	field := fieldName("moshine", "moshine_ore", "size")
	form := url.Values{
		"mode": {modeSettings}, "preset": {defaultPreset}, "filled_from": {defaultPreset},
		settingsShown: {"1"}, "map_key": {"2.1|1"}, mgSeed: {"7"}, field: {"200"},
	}
	f, files, ok := readMapForm(form, kindGenerate, mapInput{Line: "2.1", Cat: cat, MapKey: "2.1|1", Version: "2.1.24"})
	if !ok {
		t.Fatalf("not ok: %v", f.Problems())
	}
	if !f.Modified || files.Preset != "" || files.Seed == nil || *files.Seed != 7 {
		t.Errorf("modified %v, preset %q, seed %v", f.Modified, files.Preset, files.Seed)
	}
	if !strings.Contains(string(files.MapGenSettings), `"moshine_ore"`) || len(files.MapSettings) == 0 {
		t.Errorf("files %s / %s", files.MapGenSettings, files.MapSettings)
	}
	if !slices.ContainsFunc(f.MapGen.Resources, func(p MapGenPlanet) bool { return p.Planet == "moshine" }) {
		t.Error("no Moshine rows in the form")
	}
}

func TestReadMapFormRefillsForAnotherCatalogue(t *testing.T) {
	// Settings laid out for another key are filled again from the preset.
	form := url.Values{"mode": {modeSettings}, "preset": {defaultPreset}, "filled_from": {defaultPreset}, settingsShown: {"1"}, "map_key": {"2.1|0"}}
	f, _, ok := readMapForm(form, kindGenerate, mapInput{Line: "2.1", Cat: moddedCatalogue(), MapKey: "2.1|1", Version: "2.1.24", Stale: true})
	if ok || !f.Reset {
		t.Errorf("ok %v, reset %v", ok, f.Reset)
	}
	if !slices.ContainsFunc(f.MapGen.Resources, func(p MapGenPlanet) bool { return p.Planet == "moshine" }) {
		t.Error("the refill dropped the catalogue's planet")
	}
}

func TestReadMapFormExchangeString(t *testing.T) {
	form := url.Values{"mode": {modeExchange}, "exchange": {""}}
	f, _, ok := readMapForm(form, kindGenerate, mapInput{Line: "2.1", Cat: moddedCatalogue(), MapKey: "2.1|1", Version: "2.1.24"})
	if ok || f.ExchangeError.Key != "worlds.generate.error.exchange_empty" {
		t.Errorf("ok %v, error %v", ok, f.ExchangeError)
	}
}

func TestFillRefusesAnEmptyCatalogue(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("fill laid out a form with no planets")
		}
	}()
	f := MapForm{Line: "2.1", Preset: defaultPreset}
	f.fill("2.1.24", nil)
}
