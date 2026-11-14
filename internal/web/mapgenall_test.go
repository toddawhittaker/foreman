package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/toddawhittaker/foreman/internal/i18n"
)

// A planet with two or more resources gets the All row, decided by count;
// a planet with one does not. The row's selects have no name, so the form
// sends nothing new (SPEC 5.6; DESIGN 8.7).
func TestAllResourcesRowByCount(t *testing.T) {
	reRow := regexp.MustCompile(`(?s)<fieldset class="autoplace autoplace--all" id="(mg-[a-z]+-all)".*?</fieldset>`)
	reSelect := regexp.MustCompile(`<select ([^>]*)>`)
	for _, c := range testLines {
		f := NewMapGenFormForPreset(c.line, c.spaceAge, "default")
		body := renderPartial(t, i18n.English, f)
		shown := map[string]bool{}
		for _, m := range reRow.FindAllStringSubmatch(body, -1) {
			shown[m[1]] = true
			selects := reSelect.FindAllStringSubmatch(m[0], -1)
			if len(selects) < 2 {
				t.Errorf("%s %s: the row has %d selects", c.line, m[1], len(selects))
			}
			for _, s := range selects {
				if strings.Contains(s[1], "name=") || strings.Contains(s[1], "data-preset") {
					t.Errorf("%s %s: a select of the row is named or has a preset: %s", c.line, m[1], s[1])
				}
			}
		}
		many := 0
		for _, p := range f.Resources {
			want := len(p.Groups) >= 2
			if (p.All != nil) != want {
				t.Errorf("%s %s: %d resources but All is %v", c.line, p.Planet, len(p.Groups), p.All != nil)
			}
			if shown["mg-"+p.Planet+"-all"] != want {
				t.Errorf("%s %s: row shown %v, want %v", c.line, p.Planet, shown["mg-"+p.Planet+"-all"], want)
			}
			if want {
				many++
			}
		}
		if many == 0 {
			t.Errorf("%s: no planet has the row", c.line)
		}
		for _, p := range f.Terrain {
			if p.All != nil {
				t.Errorf("%s %s: the Terrain tab has an All row", c.line, p.Planet)
			}
		}
	}
	// Gleba and Fulgora hold one resource each on 2.1 (decided by count, not name).
	f := NewMapGenFormForPreset("2.1", true, "default")
	for _, p := range f.Resources {
		if (p.Planet == "gleba" || p.Planet == "fulgora") && p.All != nil {
			t.Errorf("%s has %d resources and an All row", p.Planet, len(p.Groups))
		}
		if p.Planet == "nauvis" && p.All == nil {
			t.Error("nauvis has no All row")
		}
	}
}

// The row offers the steps the resources do: "None" for richness, not for
// a size a resource's box stands in for, and never a frequency of zero.
func TestAllResourcesSteps(t *testing.T) {
	f := NewMapGenFormForPreset("2.1", true, "default")
	var all *MapGenAll
	for _, p := range f.Resources {
		if p.Planet == "nauvis" {
			all = p.All
		}
	}
	if all == nil {
		t.Fatal("no All row on nauvis")
	}
	hasNone := map[string]bool{}
	for _, s := range all.Selects {
		hasNone[s.Key] = len(s.Options) > 0 && s.Options[0].Value == mgNone
		if s.Value != mgDefaultStep {
			t.Errorf("%s starts on %q", s.Key, s.Value)
		}
	}
	if hasNone["frequency"] || hasNone["size"] || !hasNone["richness"] {
		t.Errorf("None offered: %v, want richness only", hasNone)
	}
	for _, id := range strings.Fields(all.Groups) {
		if !strings.HasPrefix(id, "mg-nauvis-") {
			t.Errorf("group %s is not a nauvis resource", id)
		}
		if strings.Contains(id, "cliff") {
			t.Errorf("group %s is not a resource", id)
		}
	}
}
