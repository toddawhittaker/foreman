package web

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/mods"
	"github.com/toddawhittaker/foreman/internal/store"
	"github.com/toddawhittaker/foreman/internal/world"
)

// countPortal answers every mod with a title unlike its name and counts
// the lookups.
type countPortal struct {
	mu    sync.Mutex
	asked map[string]int
}

func (c *countPortal) Mod(_ context.Context, name string) (modportal.Mod, error) {
	c.mu.Lock()
	c.asked[name]++
	c.mu.Unlock()
	return modportal.Mod{Name: name, Title: "Title of " + name, Summary: "Summary of " + name, Releases: []modportal.Release{
		{Version: "1.0.0", FactorioVersion: "2.0", ReleasedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{Version: "1.5.0", FactorioVersion: "1.1"},
	}}, nil
}

func (c *countPortal) Mods(context.Context, []string) map[string]modportal.ModResult { return nil }
func (c *countPortal) Search(context.Context, string, string) ([]modportal.Hit, error) {
	return nil, nil
}
func (c *countPortal) Lookup(context.Context, string, string) (modportal.Hit, error) {
	return modportal.Hit{}, nil
}
func (c *countPortal) Download(context.Context, string, string, modportal.Release, *os.File) error {
	return nil
}

// Ruling 22: an offered mod's details come from at most the plan's cap of
// lookups; the rest show by name alone. The release shown is the newest on
// the world's line, with its date.
func TestPlanFactsCapsOfferedLookups(t *testing.T) {
	cp := &countPortal{asked: map[string]int{}}
	s := &server{portal: cp}
	p := ModPlanPage{World: world.Summary{ID: "base", GameVersion: "2.0.77"}, Form: planForm{Kind: mods.KindInstall, Names: []string{"pick"}},
		Plan: mods.Plan{Changes: []mods.Change{{Name: "pick", To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyAsked}}}}}
	for i := range store.MaxModPlanNames + 10 {
		p.Plan.Optional = append(p.Plan.Optional, mods.Offer{Name: fmt.Sprintf("opt-%02d", i), Of: "pick"})
	}
	facts := s.planFacts(context.Background(), p)
	offered := 0
	for n := range cp.asked {
		if strings.HasPrefix(n, "opt-") {
			offered++
		}
	}
	if offered != store.MaxModPlanNames {
		t.Errorf("%d offered mods looked up, want %d", offered, store.MaxModPlanNames)
	}
	f := facts["opt-00"]
	if f.Title != "Title of opt-00" || f.Summary != "Summary of opt-00" || f.Version != "1.0.0" || f.Released.IsZero() || !f.OnPortal {
		t.Errorf("opt-00 facts %+v", f)
	}
	last := fmt.Sprintf("opt-%02d", store.MaxModPlanNames+9)
	p.facts = facts
	if got := p.Mod(last); got.Title != last || got.Link {
		t.Errorf("an offer past the cap shows %+v, want its name without a link", got)
	}
}

// Issue 620, ruling 15: the review page names mods by title, the title is
// the portal link, the remove cross names the title, and an offered mod's
// link and details sit outside its tick box's label.
func TestPlanShowsTitles(t *testing.T) {
	r := newTestRenderer(t)
	p := ModPlanPage{World: world.Summary{ID: "base", Name: "Base", GameVersion: "2.0.77"}, Form: planForm{Kind: mods.KindInstall, Names: []string{"k2", "lib"}}, Hash: "h",
		Plan: mods.Plan{
			Changes: []mods.Change{
				{Name: "k2", To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyAsked}},
				{Name: "lib", To: "2.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyAsked}},
				{Name: "dep", To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyRequiredBy, Of: "k2"}},
			},
			Optional: []mods.Offer{{Name: "opt", Of: "k2"}},
		},
		facts: map[string]ModFacts{
			"k2":  {Title: "Krastorio 2", OnPortal: true},
			"lib": {Title: "Lib", OnPortal: true},
			"dep": {Title: "Dependency", OnPortal: true},
			"opt": {Title: "Optional thing", Summary: "Does <b>things</b>", Version: "3.0.0", Released: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), OnPortal: true},
		}}
	_, body := renderIn(t, r, pageCase{"plan", "todd", 200, p}, "en")
	wantIn(t, "rows", planRows(body), "Krastorio 2 1.0.0", "Dependency 1.0.0 (Krastorio 2 needs it)")
	wantIn(t, "page", body,
		`<a href="https://mods.factorio.com/mod/k2" class="mod-title" target="_blank" rel="noopener noreferrer"><span translate="no">Krastorio 2</span>`,
		`<span class="visually-hidden">Remove <span class="ident" translate="no">Krastorio 2</span> from the plan</span>`,
		`aria-describedby="mod-opt-optional-detail"> <span class="visually-hidden"><span class="ident" translate="no">Optional thing</span>, offered by <span class="ident" translate="no">Krastorio 2</span></span></label>`,
		`<a href="https://mods.factorio.com/mod/opt" class="mod-title"`, "Does &lt;b&gt;things&lt;/b&gt;", `<dd translate="no">3.0.0</dd>`, `datetime="2026-03-01`)
	// The label ends before the title link, so a click on the link never ticks the box.
	label := body[strings.Index(body, `<label class="check plan-offer__box"`):]
	label = label[:strings.Index(label, "</label>")]
	if strings.Contains(label, "<a ") {
		t.Error("the offered mod's link is inside its tick box's label")
	}
	if strings.Contains(body, ">k2<") || strings.Contains(body, ">dep<") {
		t.Error("a mod shows by its name where it has a title")
	}
}
