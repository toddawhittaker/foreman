package web

import (
	"slices"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/mapgen"
	"github.com/toddawhittaker/foreman/internal/resource"
)

// spotsCount is an ore patch and the spots of two fluids on cerys: methane,
// whose tiles are tied to the planet's cerys_ore control, and a fluid whose
// control the catalogue lacks. Largest box first, as resource.Parse sorts.
var spotsCount = resource.Result{
	Planet:    "cerys",
	Resources: map[string]resource.Resource{"cerys-ore-entity": {Total: 5000}},
	Fluids: map[string]resource.Fluid{
		"methane":       {Controls: []string{"cerys_ore"}, Color: [3]uint8{131, 110, 189}},
		"mystery-gas":   {Controls: []string{"no-such-control"}},
		"another-fluid": {Controls: []string{"cerys_ore"}},
	},
	Patches: []resource.Patch{
		{Name: "methane", Fluid: true, X: 0, Y: 0, Entities: 1312, Box: [4]int{-20, -20, 20, 20}},
		{Name: "cerys-ore-entity", X: 50, Y: 50, Amount: 5000, Entities: 9, Box: [4]int{48, 48, 52, 52}},
		{Name: "mystery-gas", Fluid: true, X: -60, Y: 60, Entities: 7, Box: [4]int{-61, 59, -59, 61}},
		{Name: "methane", Fluid: true, X: 100, Y: -100, Entities: 1, Box: [4]int{100, -100, 100, -100}},
	},
}

// SPEC 5.6, "Resource amounts": a spot's label gives its size in tiles, in
// the picture and in the download alike, and the text list gives each
// fluid's spots under its name, the name of the control its tiles are tied
// to, after the ores.
func TestSpotsAreLabelledInTilesAndListedByFluid(t *testing.T) {
	v := resourceView(i18n.English, cerysCatalogue(), "cerys", spotsCount, 1280)
	var labels []string
	for _, l := range v.Labels {
		labels = append(labels, l.Kind+":"+l.Value+":"+l.Text)
	}
	want := []string{"tiles:1312:1,312 tiles", "amount:5000:", "tiles:7:7 tiles", "tiles:1:1 tile"}
	if !slices.Equal(labels, want) {
		t.Errorf("labels %v, want %v", labels, want)
	}
	var lists []string
	for _, l := range v.Lists {
		lists = append(lists, l.Total+" | "+strings.Join(l.Patches, "; "))
	}
	want = []string{
		"Cerys ore: 5,000 in total | 5,000 at (50, 50)",
		"Cerys ore: 2 spots | 1,312 tiles at (0, 0); 1 tile at (100, -100)",
		"mystery-gas: 1 spot | 7 tiles at (-60, 60)",
	}
	if !slices.Equal(lists, want) {
		t.Errorf("lists\n%v\nwant\n%v", strings.Join(lists, "\n"), strings.Join(want, "\n"))
	}
	if v.Patches != 4 {
		t.Errorf("summary counts %d patches, want the ore's and the spots, 4", v.Patches)
	}
	var caps []string
	for _, c := range amountCaptions(i18n.English, spotsCount, 1280) {
		caps = append(caps, c.Text)
	}
	if want := []string{"1,312 tiles", "5K", "7 tiles", "1 tile"}; !slices.Equal(caps, want) {
		t.Errorf("download captions %v, want %v", caps, want)
	}
}

// The large view's fragment carries a spot's words for the script to show.
func TestSpotLabelsInTheFragment(t *testing.T) {
	hn, prev, c := newResourceHarness(t)
	prev.res = resource.Result{
		Planet: "nauvis",
		Fluids: map[string]resource.Fluid{"methane": {Controls: []string{"tritium"}}},
		Patches: []resource.Patch{
			{Name: "methane", Fluid: true, X: 0, Y: 0, Entities: 312, Box: [4]int{-10, -10, 10, 10}},
		},
	}
	body := hn.postLong(pathResources, planetForm("nauvis"), c, withHeader("HX-Request", "true")).Body.String()
	for _, want := range []string{
		`data-amount="312" data-kind="tiles" data-text="312 tiles"`,
		`<h4>methane: 1 spot</h4>`,
		`<li>312 tiles at (0, 0)</li>`,
		`(1 patch)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the fragment lacks %q:\n%s", want, body)
		}
	}
}

// SPEC 5.6, "Known limit": a count that finds nothing on a planet says its
// resources may be tiles only when a modded resource control is on whose
// tiles the count could not look for. Controls the count looked for spots
// of say plainly that the picture has none.
func TestNoSpotsFoundIsNotAboutTiles(t *testing.T) {
	cat := cerysCatalogue()
	req := game.PreviewRequest{Size: 1280}
	looked := resource.Result{Fluids: map[string]resource.Fluid{"methane": {Controls: []string{"cerys_ore"}}}}
	if v := countedView(i18n.English, cat, req, "cerys", looked); v.None.Key != "worlds.generate.resources.none" {
		t.Errorf("no spots of a counted control: %q", v.None.Key)
	}
	other := resource.Result{Fluids: map[string]resource.Fluid{"methane": {Controls: []string{"tritium"}}}}
	if v := countedView(i18n.English, cat, req, "cerys", other); v.None.Key != "worlds.generate.resources.not_patches" {
		t.Errorf("no spots of another planet's control: %q", v.None.Key)
	}
	// A planet with a fluid control and one the count cannot see still may have tiles.
	cat = cerysCatalogue()
	cat.Controls["cerys_rock"] = mapgen.CatalogueControl{Control: mapgen.Control{Name: "cerys_rock", Category: mapgen.CategoryResource}, Planets: []string{"cerys"}}
	for i, p := range cat.Planets {
		if p.Name == "cerys" {
			cat.Planets[i].Controls = append(slices.Clone(p.Controls), "cerys_rock")
		}
	}
	if v := countedView(i18n.English, cat, req, "cerys", looked); v.None.Key != "worlds.generate.resources.not_patches" {
		t.Errorf("a control the count cannot see: %q", v.None.Key)
	}
}

// A count's memory counts its fluids too.
func TestResourceBytesCountsFluids(t *testing.T) {
	without := spotsCount
	without.Fluids = nil
	if resourceBytes(&spotsCount) <= resourceBytes(&without) {
		t.Error("fluids are not counted in a count's size")
	}
}
