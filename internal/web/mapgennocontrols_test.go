package web

import (
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
)

// SPEC 5.6: a planet whose mod places everything without controls has a row
// in Resources and Terrain that says so; a planet with controls in only one
// tab gets nothing new.
func TestPlanetWithoutControlsSaysSo(t *testing.T) {
	cat := moddedCatalogue()
	cat.Planets = append(cat.Planets, mapgen.Planet{Name: "igrys", Label: "Igrys"})
	f := buildMapGenFormIn("2.1", cat, func(string) (string, bool) { return "", false })
	for tab, rows := range map[string][]MapGenPlanet{"resources": f.Resources, "terrain": f.Terrain} {
		at := slices.IndexFunc(rows, func(p MapGenPlanet) bool { return p.Planet == "igrys" })
		if at < 0 || !rows[at].NoControls || len(rows[at].Groups) != 0 {
			t.Errorf("%s: igrys row missing or not marked: %+v", tab, rows)
		}
		if i := slices.IndexFunc(rows, func(p MapGenPlanet) bool { return p.NoControls && p.Planet != "igrys" }); i >= 0 {
			t.Errorf("%s: %s marked as having no controls", tab, rows[i].Planet)
		}
	}
	if slices.ContainsFunc(f.Terrain, func(p MapGenPlanet) bool { return p.Planet == "moshine" }) {
		t.Error("moshine, with resource controls only, has a Terrain row")
	}
	html := renderPartial(t, i18n.English, f)
	want := "Igrys has no resource or terrain settings. Its mod places them without controls, so the game offers none either."
	if n := strings.Count(html, want); n != 2 {
		t.Errorf("the sentence shows %d times, want once a tab", n)
	}
}
