package web

import (
	"context"
	"fmt"
	"os"
	"slices"
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
	// slow holds every lookup until its context ends.
	slow bool
	// summary, when set, is every mod's summary.
	summary string
}

func (c *countPortal) Mod(ctx context.Context, name string) (modportal.Mod, error) {
	c.mu.Lock()
	c.asked[name]++
	c.mu.Unlock()
	if c.slow {
		<-ctx.Done()
		return modportal.Mod{}, ctx.Err()
	}
	sum := "Summary of " + name
	if c.summary != "" {
		sum = c.summary
	}
	return modportal.Mod{Name: name, Title: "Title of " + name, Summary: sum, Releases: []modportal.Release{
		{Version: "1.0.0", FactorioVersion: "2.0", ReleasedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{Version: "1.5.0", FactorioVersion: "1.1"},
	}}, nil
}

func (c *countPortal) Mods(ctx context.Context, names []string) map[string]modportal.ModResult {
	out := map[string]modportal.ModResult{}
	for _, n := range names {
		m, err := c.Mod(ctx, n)
		out[n] = modportal.ModResult{Mod: m, Err: err}
	}
	return out
}
func (c *countPortal) Search(context.Context, string, string) ([]modportal.Hit, error) {
	return nil, nil
}
func (c *countPortal) Lookup(context.Context, string, string) (modportal.Hit, error) {
	return modportal.Hit{}, nil
}
func (c *countPortal) KnownMissing(string) bool { return false }
func (c *countPortal) Download(context.Context, string, string, modportal.Release, *os.File, int64) error {
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
		p.Plan.Optional = append(p.Plan.Optional, mods.Offer{Name: fmt.Sprintf("opt-%02d", i), Of: fmt.Sprintf("of-%02d", i)})
	}
	facts := s.planFacts(context.Background(), p)
	offered, of := 0, 0
	for n := range cp.asked {
		switch {
		case strings.HasPrefix(n, "opt-"):
			offered++
		case strings.HasPrefix(n, "of-"):
			of++
		}
	}
	if offered != store.MaxModPlanNames || of != store.MaxModPlanNames {
		t.Errorf("%d offered and %d offering mods looked up, want %d of each", offered, of, store.MaxModPlanNames)
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
		`aria-describedby="mod-opt-optional-detail"> <span class="visually-hidden" translate="no">Optional thing</span></label>`,
		// The visible reason is a second label of the box, so a click on it ticks the box.
		`<label class="plan-offer__of" for="mod-opt-optional">offered by <span class="ident" translate="no">Krastorio 2</span></label>`,
		`<a href="https://mods.factorio.com/mod/opt" class="mod-title"`, "Does &lt;b&gt;things&lt;/b&gt;", `<dd translate="no">3.0.0</dd>`, `datetime="2026-03-01`)
	// Neither label holds the title link, so a click on the link never ticks the box.
	for _, start := range []string{`<label class="check plan-offer__box"`, `<label class="plan-offer__of"`} {
		label := body[strings.Index(body, start):]
		label = label[:strings.Index(label, "</label>")]
		if strings.Contains(label, "<a ") {
			t.Errorf("the offered mod's link is inside the label %s", start)
		}
	}
	if strings.Contains(body, ">k2<") || strings.Contains(body, ">dep<") {
		t.Error("a mod shows by its name where it has a title")
	}
}

// A slow portal leaves the details out instead of holding the page.
func TestPlanFactsGiveUpOnASlowPortal(t *testing.T) {
	old := planFactsWait
	planFactsWait = 50 * time.Millisecond
	t.Cleanup(func() { planFactsWait = old })
	s := &server{portal: &countPortal{asked: map[string]int{}, slow: true}}
	p := ModPlanPage{World: world.Summary{ID: "base", GameVersion: "2.0.77"}, Form: planForm{Kind: mods.KindInstall, Names: []string{"pick"}},
		Plan: mods.Plan{Changes: []mods.Change{{Name: "pick", To: "1.0.0", Action: mods.ActInstall, Why: mods.Reason{Code: mods.WhyAsked}}},
			Optional: []mods.Offer{{Name: "opt", Of: "pick"}}}}
	start := time.Now()
	facts := s.planFacts(context.Background(), p)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("planFacts took %s with a slow portal", d)
	}
	if f := facts["opt"]; f.OnPortal || f.Summary != "" {
		t.Errorf("a timed-out lookup still gave %+v", f)
	}
}

// A portal summary is cleaned like a title: a right-to-left override
// cannot turn the text around, and it is cut at 300 characters.
func TestPlanFactsCleanTheSummary(t *testing.T) {
	long := strings.Repeat("a", 400)
	s := &server{portal: &countPortal{asked: map[string]int{}, summary: "safe\u202Eevil " + long}}
	p := ModPlanPage{World: world.Summary{ID: "base", GameVersion: "2.0.77"}, Plan: mods.Plan{Optional: []mods.Offer{{Name: "opt", Of: "pick"}}}}
	got := s.planFacts(context.Background(), p)["opt"].Summary
	if strings.ContainsRune(got, '\u202E') || !strings.HasPrefix(got, "safe evil ") {
		t.Errorf("summary %q keeps the override", got)
	}
	if n := len([]rune(got)); n != maxPortalSummary+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("summary is %d characters, want %d and an ellipsis", n, maxPortalSummary+1)
	}
}

// Ruling 15: the review page's heading, conflicts and fix buttons name a mod
// by its title where it has one, and by its name where it has none.
func TestPlanTextsUseTitles(t *testing.T) {
	p := ModPlanPage{World: world.Summary{ID: "base", Name: "Base"}, Form: planForm{Kind: mods.KindInstall, Names: []string{"k2"}},
		Have: []world.InstalledMod{{Name: "inst", Title: "Installed one", Enabled: true}},
		Plan: mods.Plan{Conflicts: []mods.Conflict{
			{Code: mods.ConflictIncompatible, Mod: "k2", Other: "inst"},
			{Code: mods.ConflictNotOnPortal, Mod: "bare"},
		}},
		facts: map[string]ModFacts{"k2": {Title: "Krastorio 2", OnPortal: true}, "inst": {Title: "Installed one"}}}
	if got := english(p.Heading()); !strings.Contains(got, "Krastorio 2") || strings.Contains(got, "k2") {
		t.Errorf("heading %q", got)
	}
	rows := p.Conflicts()
	if got := english(rows[0].Text); !strings.Contains(got, "Krastorio 2") || !strings.Contains(got, "Installed one") || strings.Contains(got, "k2") || strings.Contains(got, "inst ") {
		t.Errorf("conflict %q", got)
	}
	if got := english(rows[0].Fix); !strings.Contains(got, "Installed one") {
		t.Errorf("fix %q", got)
	}
	if got := english(rows[1].Text); !strings.Contains(got, "bare") {
		t.Errorf("a mod without a title falls back to its name: %q", got)
	}
}

// Ruling 15: the panel's warnings and its screen-reader messages name the
// other mod by title.
func TestPlanPanelWarningsAndMessagesUseTitles(t *testing.T) {
	tr := planTree{plan: mods.Plan{Conflicts: []mods.Conflict{{Code: mods.ConflictIncompatible, Mod: "a", Other: "b"}}},
		title: func(n string) string { return "Title of " + n }, titles: map[string]string{}, changes: map[string]mods.Change{}}
	if got := englishAll(tr.warnings("a")); !slices.Equal(got, []string{"Cannot run with Title of b"}) {
		t.Errorf("a's warnings %v", got)
	}
	if got := englishAll(tr.warnings("b")); !slices.Equal(got, []string{"Cannot run with Title of a"}) {
		t.Errorf("b's warnings %v", got)
	}
}
