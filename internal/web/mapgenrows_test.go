package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// Cliffs off writes the game's own off value for each line and planet, proved
// on real games: 1.1 cliff_settings.richness 0; 2.x the planet's cliff
// control at size 0, and never cliff_settings.richness, which would reach
// every planet (SPEC 5.6).
func TestCliffsOffKeys(t *testing.T) {
	_, m := parseOK(t, "1.1", false, url.Values{fieldName("nauvis", "nauvis_cliff", mgOn): {"off"}})
	approx(t, "1.1 cliff richness", sub(t, m, "cliff_settings")["richness"], 0)

	for _, line := range []string{"2.0", "2.1"} {
		for _, planet := range []string{"nauvis", "gleba", "fulgora"} {
			control := planet + "_cliff"
			_, m := parseOK(t, line, true, url.Values{fieldName(planet, control, mgOn): {"off"}})
			approx(t, line+" "+control+" size", sub(t, m, "autoplace_controls", control)["size"], 0)
			if _, ok := m["cliff_settings"]; ok {
				t.Errorf("%s %s off: cliff_settings written", line, planet)
			}
			for _, other := range []string{"nauvis", "gleba", "fulgora"} {
				if other != planet {
					approx(t, line+" "+other+"_cliff size", sub(t, m, "autoplace_controls", other+"_cliff")["size"], 1)
				}
			}
		}
	}
}

// Cliffs on keep the continuity chosen, and a continuity of none (an old post
// or a preset) shows as the box off.
func TestCliffsOnAndNone(t *testing.T) {
	_, m := parseOK(t, "1.1", false, url.Values{fieldName("nauvis", "nauvis_cliff", "size"): {"200"}})
	approx(t, "1.1 cliff richness", sub(t, m, "cliff_settings")["richness"], 2)

	f, m := parseOK(t, "2.1", true, url.Values{fieldName("gleba", "gleba_cliff", "size"): {mgNone}})
	approx(t, "gleba cliff size", sub(t, m, "autoplace_controls", "gleba_cliff")["size"], 0)
	if f.resourceOn("gleba", "gleba_cliff") {
		t.Error("a continuity of none leaves the cliffs box ticked")
	}
	f.withPreset(NewMapGenFormForPreset("2.1", true, "default"))
	if !f.Modified() {
		t.Error("cliffs off does not mark the form modified")
	}
}

// Vulcanus has no cliffs box, and its select of continuity has no box to stand in for.
func TestCliffBoxes(t *testing.T) {
	f := NewMapGenFormForPreset("2.1", true, "default")
	boxes := map[string]bool{}
	for _, p := range f.Terrain {
		for _, g := range p.Groups {
			if g.Toggle != nil {
				boxes[p.Planet] = true
				for _, s := range g.Selects {
					if hasOption(s, mgNone) {
						t.Errorf("%s offers none beside the cliffs box", s.Name)
					}
				}
			}
		}
	}
	for _, p := range []string{"nauvis", "gleba", "fulgora"} {
		if !boxes[p] {
			t.Errorf("%s has no cliffs box", p)
		}
	}
	if boxes["vulcanus"] {
		t.Error("vulcanus has a cliffs box")
	}
}

// One planet is open in every tab: the first error's, else the one posted,
// else the first; a planet the line lacks is not kept.
func TestOpenPlanet(t *testing.T) {
	open := func(f MapGenForm) []string {
		var out []string
		for _, tab := range [][]MapGenPlanet{f.Resources, f.Terrain} {
			for _, p := range tab {
				if p.Open {
					out = append(out, p.Planet)
				}
			}
		}
		return out
	}
	want := func(name string, f MapGenForm, planet string) {
		t.Helper()
		got := open(f)
		if f.OpenPlanet != planet || len(got) != 2 || got[0] != planet || got[1] != planet {
			t.Errorf("%s: open %v (%q), want %s in both tabs", name, got, f.OpenPlanet, planet)
		}
	}
	f := NewMapGenFormForPreset("2.1", true, "default")
	want("default", f, "nauvis")
	f.setOpen("fulgora")
	want("posted", f, "fulgora")
	f.setOpen("aquilo-nowhere")
	want("unknown", f, "nauvis")

	f, _, probs := parseMapGenValues(url.Values{fieldName("gleba", "gleba_cliff", "frequency"): {"9"}}, "2.1", true)
	if len(probs) == 0 {
		t.Fatal("bad step accepted")
	}
	f.setOpen("fulgora")
	want("error", f, "gleba")
}

// A needed resource that is off warns under its box and in the summary, in
// the same words; one that is on does not.
func TestNeedsWarnings(t *testing.T) {
	f, _ := parseOK(t, "2.1", true, url.Values{fieldName("nauvis", "coal", mgOn): {"off"}})
	if !f.NeedsOff() {
		t.Fatal("coal off is not a needed resource off")
	}
	keys := map[string]bool{}
	for _, n := range f.Needs() {
		keys[n.NeedKey] = !n.Checked
	}
	for _, k := range []string{"iron-ore", "copper-ore", "stone", "crude-oil", "coal"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("%s has no warning", k)
		}
	}
	if _, ok := keys["uranium-ore"]; ok {
		t.Error("uranium warns")
	}
	if !keys["coal"] || keys["iron-ore"] {
		t.Errorf("off state wrong: %v", keys)
	}
	body := renderPartial(t, i18n.English, f)
	if !strings.Contains(body, `id="mg-nauvis-coal-on-need">Without coal on Nauvis`) {
		t.Error("coal's warning is not under its box")
	}
	if strings.Contains(body, `id="mg-nauvis-iron-ore-on-need">Without`) {
		t.Error("iron ore warns while on")
	}
}
