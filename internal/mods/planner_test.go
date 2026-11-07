package mods

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/modportal/modportaltest"
	"github.com/toddawhittaker/foreman/internal/savefile"
	"github.com/toddawhittaker/foreman/internal/world"
)

type oneActive struct{ id string }

func (a *oneActive) ActiveWorld(context.Context) (string, bool, error) { return a.id, a.id != "", nil }
func (a *oneActive) SetActiveWorld(_ context.Context, id string, _ time.Time) error {
	a.id = id
	return nil
}

// saveWorlds stands in a fixed save's mod list for the world's saves.
type saveWorlds struct {
	*world.Manager
	save []savefile.Mod
}

func (s saveWorlds) SaveMods(string, string) (savefile.Save, error) {
	return savefile.Save{Mods: s.save}, nil
}

type fixture struct {
	t       *testing.T
	m       *world.Manager
	id      string
	portal  *modportaltest.Portal
	planner *Planner
}

// newFixture makes a 2.0.72 Space Age world in a temp folder holding the
// given zips (named name_version.zip) and a planner on the fake portal.
func newFixture(t *testing.T, zips ...string) *fixture {
	t.Helper()
	data := t.TempDir()
	m, err := world.NewManager(world.Dirs{
		Worlds:    filepath.Join(data, "worlds"),
		Snapshots: filepath.Join(data, "snapshots"),
		Imports:   filepath.Join(data, "import"),
	}, &oneActive{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.Create(context.Background(), "Test", "2.0.72", true)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(m.Dirs().Worlds, id, world.ModsDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, z := range zips {
		if err := os.WriteFile(filepath.Join(dir, z+".zip"), []byte("not read"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	p := modportaltest.New(t)
	return &fixture{t: t, m: m, id: id, portal: p, planner: &Planner{Worlds: m, Portal: modportal.NewForTest(p.URL)}}
}

func (f *fixture) addFolder(name, version string) {
	f.t.Helper()
	dir := filepath.Join(f.m.Dirs().Worlds, f.id, world.ModsDir, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		f.t.Fatal(err)
	}
	info := `{"name":"` + name + `","version":"` + version + `"}`
	if err := os.WriteFile(filepath.Join(dir, "info.json"), []byte(info), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) plan(req Request) Plan {
	f.t.Helper()
	p, fp, err := f.planner.Plan(context.Background(), f.id, req)
	if err != nil {
		f.t.Fatal(err)
	}
	want, _ := f.m.ModsFingerprint(f.id)
	if fp != want {
		f.t.Fatalf("fingerprint %q, want %q", fp, want)
	}
	return p
}

func (f *fixture) toChange(p Plan, staged string) ([]Download, world.ModChange) {
	f.t.Helper()
	installed, err := f.m.Mods(f.id)
	if err != nil {
		f.t.Fatal(err)
	}
	d, c, err := ToChange(p, installed, staged)
	if err != nil {
		f.t.Fatalf("ToChange: %v (plan %+v)", err, p)
	}
	return d, c
}

func TestAvailabilityFlagsAVersionThePortalNoLongerHas(t *testing.T) {
	f := newFixture(t, "visible-planets_1.5.5")
	flags, err := f.planner.Availability(context.Background(), f.id)
	if err != nil {
		t.Fatal(err)
	}
	want := []Flag{{Code: FlagVersionGone, Severity: SevError, Name: "visible-planets", Version: "1.5.5", Update: "1.7.2"}}
	if !slices.Equal(flags, want) {
		t.Fatalf("flags %+v, want %+v", flags, want)
	}
	n, err := f.planner.AvailabilityErrors(context.Background(), f.id)
	if err != nil || n != 1 {
		t.Fatalf("errors %d, %v; want 1", n, err)
	}

	// The fix is the 9.3 update: to the newest 2.0 release, not 2.1's.
	p := f.plan(Request{Kind: KindUpdate, Names: []string{"visible-planets"}})
	d, c := f.toChange(p, "")
	if len(d) != 1 || d[0].Name != "visible-planets" || d[0].Release.Version != "1.7.2" {
		t.Fatalf("downloads %+v", d)
	}
	if !slices.Equal(c.Remove, []string{"visible-planets_1.5.5.zip"}) {
		t.Fatalf("remove %v", c.Remove)
	}
}

func TestAvailabilityWarnsOfADeprecatedMod(t *testing.T) {
	f := newFixture(t, "chest-auto-sort_1.3.0")
	flags, err := f.planner.Availability(context.Background(), f.id)
	if err != nil {
		t.Fatal(err)
	}
	want := []Flag{{Code: FlagDeprecated, Severity: SevWarning, Name: "chest-auto-sort", Version: "1.3.0"}}
	if !slices.Equal(flags, want) {
		t.Fatalf("flags %+v, want %+v", flags, want)
	}
}

func TestAvailabilityNotesAModThePortalLacks(t *testing.T) {
	f := newFixture(t, "my-own-mod_0.1.0")
	flags, err := f.planner.Availability(context.Background(), f.id)
	if err != nil {
		t.Fatal(err)
	}
	want := []Flag{{Code: FlagNotOnPortal, Severity: SevNote, Name: "my-own-mod", Version: "0.1.0"}}
	if !slices.Equal(flags, want) {
		t.Fatalf("flags %+v, want %+v", flags, want)
	}
}

func TestBuiltInAndFolderModsAreNeverFlaggedOrUpdated(t *testing.T) {
	f := newFixture(t)
	f.addFolder("visible-planets", "1.5.5") // would be flagged and updated as a zip
	flags, err := f.planner.Availability(context.Background(), f.id)
	if err != nil || len(flags) != 0 {
		t.Fatalf("flags %+v, %v", flags, err)
	}
	ups, err := f.planner.Updates(context.Background(), f.id)
	if err != nil || len(ups) != 0 {
		t.Fatalf("updates %+v, %v", ups, err)
	}
	if n := f.portal.FullCalls("visible-planets") + f.portal.FullCalls("base") + f.portal.FullCalls("space-age"); n != 0 {
		t.Fatalf("asked the portal %d times", n)
	}
}

func TestUpdatesListOnlyNewerReleasesOnTheWorldsLine(t *testing.T) {
	f := newFixture(t, "visible-planets_1.6.1", "alpha_1.1.0", "beta_2.0.0", "my-own-mod_0.1.0")
	f.portal.AddZip("alpha", "1.0.0")
	f.portal.AddZip("alpha", "1.1.0")
	f.portal.AddZip("beta", "1.0.0") // older than installed
	ups, err := f.planner.Updates(context.Background(), f.id)
	if err != nil {
		t.Fatal(err)
	}
	want := []Update{{Name: "visible-planets", From: "1.6.1", To: "1.7.2"}}
	if !slices.Equal(ups, want) {
		t.Fatalf("updates %+v, want %+v", ups, want)
	}
}

func TestToChangeInstall(t *testing.T) {
	f := newFixture(t)
	f.portal.AddZip("alpha", "1.0.0", "beta >= 1.0.0")
	f.portal.AddZip("beta", "1.0.0")
	d, c := f.toChange(f.plan(Request{Kind: KindInstall, Names: []string{"alpha"}}), "")
	var got []string
	for _, x := range d {
		got = append(got, x.Name+"_"+x.Release.Version)
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"alpha_1.0.0", "beta_1.0.0"}) {
		t.Fatalf("downloads %v", got)
	}
	if len(c.Remove) != 0 || !c.Enabled["alpha"] || !c.Enabled["beta"] {
		t.Fatalf("change %+v", c)
	}
}

func TestToChangeUpgradeRemovesTheReplacedZip(t *testing.T) {
	f := newFixture(t, "alpha_1.0.0")
	f.portal.AddZip("alpha", "1.0.0")
	f.portal.AddZip("alpha", "1.1.0")
	d, c := f.toChange(f.plan(Request{Kind: KindUpdate}), "")
	if len(d) != 1 || d[0].Release.Version != "1.1.0" {
		t.Fatalf("downloads %+v", d)
	}
	if !slices.Equal(c.Remove, []string{"alpha_1.0.0.zip"}) || !c.Enabled["alpha"] {
		t.Fatalf("change %+v", c)
	}
}

func TestToChangeRemoveTurnsTheModOff(t *testing.T) {
	f := newFixture(t, "alpha_1.0.0")
	f.portal.AddZip("alpha", "1.0.0")
	d, c := f.toChange(f.plan(Request{Kind: KindRemove, Names: []string{"alpha"}}), "")
	if len(d) != 0 || !slices.Equal(c.Remove, []string{"alpha_1.0.0.zip"}) {
		t.Fatalf("downloads %+v, change %+v", d, c)
	}
	if on, ok := c.Enabled["alpha"]; !ok || on {
		t.Fatalf("enabled %v", c.Enabled)
	}
}

func TestToChangeUploadCarriesTheStagedName(t *testing.T) {
	f := newFixture(t)
	p := f.plan(Request{Kind: KindUpload, Upload: &Upload{Name: "my-own-mod", Version: "0.1.0"}})
	d, c := f.toChange(p, ".partial-123")
	if len(d) != 0 || !slices.Equal(c.Add, []string{".partial-123"}) || !c.Enabled["my-own-mod"] {
		t.Fatalf("downloads %+v, change %+v", d, c)
	}
	if _, _, err := ToChange(p, nil, ""); !errors.Is(err, ErrNoStagedUpload) {
		t.Fatalf("no staged name: %v", err)
	}
}

func TestToChangeRefusesAPlanThatCannotApply(t *testing.T) {
	f := newFixture(t)
	p := f.plan(Request{Kind: KindInstall, Names: []string{"my-own-mod"}})
	if p.Applicable() {
		t.Fatalf("plan applicable: %+v", p)
	}
	if _, _, err := ToChange(p, nil, ""); !errors.Is(err, ErrNotApplicable) {
		t.Fatalf("err %v", err)
	}
}

func TestSyncFromASaveWithAMissingVersionOffersTheNearest(t *testing.T) {
	f := newFixture(t)
	f.planner.Worlds = saveWorlds{Manager: f.m, save: []savefile.Mod{
		{Name: "base", Version: savefile.Version{Major: 2, Patch: 72}},
		{Name: "visible-planets", Version: savefile.Version{Major: 1, Minor: 5, Patch: 5}},
	}}
	req, err := f.planner.SyncRequest(f.id, "game.zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	p := f.plan(req)
	want := []Unresolved{{Name: "visible-planets", Version: "1.5.5", Nearest: "1.5.8"}}
	if !slices.EqualFunc(p.Unresolved, want, func(a, b Unresolved) bool {
		return a.Name == b.Name && a.Version == b.Version && a.Nearest == b.Nearest && a.Choice == nil
	}) {
		t.Fatalf("unresolved %+v", p.Unresolved)
	}
	if p.Applicable() {
		t.Fatal("applicable without a choice")
	}

	req, _ = f.planner.SyncRequest(f.id, "game.zip", map[string]Choice{"visible-planets": {Kind: ChoiceNearest}})
	d, _ := f.toChange(f.plan(req), "")
	if len(d) != 1 || d[0].Release.Version != "1.5.8" {
		t.Fatalf("downloads %+v", d)
	}
}

func TestPortalDownIsAnErrorNotFlags(t *testing.T) {
	f := newFixture(t, "visible-planets_1.5.5")
	f.portal.FailAPI(500)
	ctx := context.Background()
	if flags, err := f.planner.Availability(ctx, f.id); !errors.Is(err, modportal.ErrUnreachable) || flags != nil {
		t.Fatalf("availability %+v, %v", flags, err)
	}
	if _, err := f.planner.AvailabilityErrors(ctx, f.id); !errors.Is(err, modportal.ErrUnreachable) {
		t.Fatalf("summary %v", err)
	}
	if ups, err := f.planner.Updates(ctx, f.id); !errors.Is(err, modportal.ErrUnreachable) || ups != nil {
		t.Fatalf("updates %+v, %v", ups, err)
	}
	if _, _, err := f.planner.Plan(ctx, f.id, Request{Kind: KindUpdate}); err == nil {
		t.Fatal("plan with the portal down")
	}
}

func TestAvailabilitySummaryUsesTheCache(t *testing.T) {
	f := newFixture(t, "visible-planets_1.5.5")
	for range 3 {
		if _, err := f.planner.AvailabilityErrors(context.Background(), f.id); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.portal.FullCalls("visible-planets"); n != 1 {
		t.Fatalf("%d /full calls, want 1", n)
	}
}
