package mods

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/world"
)

const game = "2.0.72"

// rel is shorthand for a release: version, game line, dependencies.
func rel(v, line string, deps ...string) modportal.Release {
	return modportal.Release{Version: v, FactorioVersion: line, FileName: "x_" + v + ".zip", Dependencies: deps}
}

// portal is a fake Lookup over a fixed set of mods.
type portal map[string][]modportal.Release

func (p portal) lookup(_ context.Context, name string) (modportal.Mod, error) {
	rs, ok := p[name]
	if !ok {
		return modportal.Mod{}, modportal.ErrNotOnPortal
	}
	return modportal.Mod{Name: name, Releases: rs}, nil
}

func resolve(t *testing.T, p portal, w World, req Request) Plan {
	t.Helper()
	if w.GameVersion == "" {
		w.GameVersion = game
	}
	plan, err := Resolve(context.Background(), w, req, p.lookup)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return plan
}

func change(p Plan, name string) (Change, bool) {
	i := slices.IndexFunc(p.Changes, func(c Change) bool { return c.Name == name })
	if i < 0 {
		return Change{}, false
	}
	return p.Changes[i], true
}

func wantChange(t *testing.T, p Plan, name string, act Action, to string, why Reason) {
	t.Helper()
	c, ok := change(p, name)
	if !ok {
		t.Fatalf("no change for %s in %+v", name, p.Changes)
	}
	if c.Action != act || c.To != to || c.Why != why {
		t.Errorf("%s = %s to %s (%+v); want %s to %s (%+v)", name, c.Action, c.To, c.Why, act, to, why)
	}
}

func hasConflict(p Plan, code ConflictCode, mod string) bool {
	return slices.ContainsFunc(p.Conflicts, func(c Conflict) bool { return c.Code == code && c.Mod == mod })
}

func hasPair(p Plan, code ConflictCode, mod, other string) bool {
	return slices.ContainsFunc(p.Conflicts, func(c Conflict) bool {
		return c.Code == code && c.Mod == mod && c.Other == other
	})
}

// installed is a mod zip in the world; deps are its info.json's.
func installed(name, v string, enabled bool, deps ...string) world.InstalledMod {
	return world.InstalledMod{Name: name, Version: v, File: name + "_" + v + ".zip", Enabled: enabled, Dependencies: deps}
}

func asked() Reason         { return Reason{Code: WhyAsked} }
func by(m string) Reason    { return Reason{Code: WhyRequiredBy, Of: m} }
func optOf(m string) Reason { return Reason{Code: WhyOptionalOf, Of: m} }

func TestInstallChainOfDependencies(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0", "base >= 2.0", "b")},
		"b": {rel("1.0.0", "2.0", "~ c >= 2.0.0")},
		"c": {rel("1.0.0", "2.0"), rel("2.0.0", "2.0"), rel("3.0.0", "2.0"), rel("4.0.0", "1.1")},
	}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a"}})
	wantChange(t, plan, "a", ActInstall, "1.0.0", asked())
	wantChange(t, plan, "b", ActInstall, "1.0.0", by("a"))
	// Newest on the world's line, not the 1.1 release.
	wantChange(t, plan, "c", ActInstall, "3.0.0", by("b"))
	if !plan.Applicable() || len(plan.Changes) != 3 {
		t.Errorf("plan = %+v", plan)
	}
	if _, ok := change(plan, "base"); ok {
		t.Error("built-in base appears as a change")
	}
	if c, _ := change(plan, "c"); c.Release == nil || c.Release.Version != "3.0.0" {
		t.Errorf("c release = %+v", c.Release)
	}
}

func TestKeepsInstalledThatSatisfies(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0", "b >= 1.0.0")},
		"b": {rel("1.0.0", "2.0"), rel("1.5.0", "2.0")},
	}
	w := World{Mods: []world.InstalledMod{installed("b", "1.0.0", true)}}
	plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"a"}})
	wantChange(t, plan, "b", ActKeep, "1.0.0", by("a"))
}

func TestUpgradesInstalledThatFails(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0", "b > 1.0.0")},
		"b": {rel("1.0.0", "2.0"), rel("1.5.0", "2.0")},
	}
	w := World{Mods: []world.InstalledMod{installed("b", "1.0.0", true)}}
	plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"a"}})
	wantChange(t, plan, "b", ActUpgrade, "1.5.0", by("a"))
}

func TestEnablesDisabledDependency(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "b")}, "b": {rel("1.0.0", "2.0")}}
	w := World{Mods: []world.InstalledMod{installed("b", "1.0.0", false)}}
	plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"a"}})
	wantChange(t, plan, "b", ActEnable, "1.0.0", by("a"))
}

func TestEveryOperatorAndNoFittingRelease(t *testing.T) {
	versions := []modportal.Release{rel("1.0.0", "2.0"), rel("1.1.0", "2.0"), rel("1.2.0", "2.0")}
	cases := []struct {
		dep, want string
	}{
		{"b < 1.2.0", "1.1.0"},
		{"b <= 1.1.0", "1.1.0"},
		{"b = 1.0.0", "1.0.0"},
		{"b >= 1.0.0", "1.2.0"},
		{"b > 1.0.0", "1.2.0"},
		{"b > 1.2.0", ""},
	}
	for _, c := range cases {
		p := portal{"a": {rel("1.0.0", "2.0", c.dep)}, "b": versions}
		plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a"}})
		if c.want == "" {
			if !hasConflict(plan, ConflictNoRelease, "b") || plan.Applicable() {
				t.Errorf("%s: conflicts %+v", c.dep, plan.Conflicts)
			}
			continue
		}
		wantChange(t, plan, "b", ActInstall, c.want, by("a"))
	}
}

func TestConflictNamesBothConstraints(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0", "c >= 2.0.0")},
		"b": {rel("1.0.0", "2.0", "c < 2.0.0")},
		"c": {rel("1.0.0", "2.0"), rel("2.0.0", "2.0")},
	}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a", "b"}})
	if plan.Applicable() {
		t.Fatal("plan with clashing constraints is applicable")
	}
	i := slices.IndexFunc(plan.Conflicts, func(c Conflict) bool { return c.Mod == "c" })
	if i < 0 {
		t.Fatalf("no conflict on c: %+v", plan.Conflicts)
	}
	want := []Need{{By: "a", Op: ">=", Version: "2.0.0"}, {By: "b", Op: "<", Version: "2.0.0"}}
	if got := plan.Conflicts[i].Needs; !slices.Equal(got, want) {
		t.Errorf("needs = %+v, want %+v", got, want)
	}
}

func TestIncompatible(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "! b")}, "b": {rel("1.0.0", "2.0")}}
	w := World{Mods: []world.InstalledMod{installed("b", "1.0.0", true)}}
	plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"a"}})
	if !hasConflict(plan, ConflictIncompatible, "a") || plan.Applicable() {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
	// Disabled, it is no conflict.
	w = World{Mods: []world.InstalledMod{installed("b", "1.0.0", false)}}
	if plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"a"}}); !plan.Applicable() {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
	// An installed mod that refuses the new one also conflicts.
	p = portal{"a": {rel("1.0.0", "2.0")}, "b": {rel("1.0.0", "2.0", "! a")}}
	w = World{Mods: []world.InstalledMod{installed("b", "1.0.0", true, "! a")}}
	if plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"a"}}); !hasConflict(plan, ConflictIncompatible, "b") {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
}

func TestSpaceAge(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "space-age >= 2.0.0")}}
	plan := resolve(t, p, World{SpaceAge: false}, Request{Kind: KindInstall, Names: []string{"a"}})
	if !hasPair(plan, ConflictSpaceAgeOff, "a", "space-age") {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
	plan = resolve(t, p, World{SpaceAge: true}, Request{Kind: KindInstall, Names: []string{"a"}})
	if !plan.Applicable() || len(plan.Changes) != 1 {
		t.Errorf("plan = %+v", plan)
	}
}

func TestBuiltIns(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "base >= 2.1.0")}, "base": {rel("9.9.9", "2.0")}}
	plan := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a"}})
	if !hasConflict(plan, ConflictVersion, "base") {
		t.Errorf("base below a constraint: conflicts = %+v", plan.Conflicts)
	}
	if _, ok := change(plan, "base"); ok {
		t.Error("base offered for download")
	}
	for _, k := range []Kind{KindInstall, KindRemove, KindDisable, KindUpdate} {
		plan := resolve(t, p, World{SpaceAge: true}, Request{Kind: k, Names: []string{"quality"}})
		if !hasConflict(plan, ConflictBuiltIn, "quality") || len(plan.Changes) != 0 {
			t.Errorf("%s quality: %+v", k, plan)
		}
	}
	// Update all never touches built-ins, even when listed as installed.
	w := World{SpaceAge: true, Mods: []world.InstalledMod{{Name: "base", Version: game, BuiltIn: true, Enabled: true}}}
	if plan := resolve(t, p, w, Request{Kind: KindUpdate}); len(plan.Changes) != 0 || !plan.Applicable() {
		t.Errorf("update all: %+v", plan)
	}
}

func TestOptionalOffers(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0", "? b", "(?) hidden", "? c")},
		"b": {rel("1.0.0", "2.0")},
		"c": {rel("1.0.0", "2.0")},
	}
	w := World{Mods: []world.InstalledMod{installed("c", "1.0.0", true)}}
	plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"a"}})
	if want := []Offer{{Name: "b", Of: "a"}}; !slices.Equal(plan.Optional, want) {
		t.Errorf("offers = %+v, want %+v", plan.Optional, want)
	}
	if _, ok := change(plan, "b"); ok {
		t.Error("unticked optional installed")
	}
	plan = resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"a"}, Optional: []string{"b"}})
	wantChange(t, plan, "b", ActInstall, "1.0.0", optOf("a"))
	// SPEC 9.2: a ticked optional stays offered, ticked, so the page keeps
	// its box and posts it back.
	if want := []Offer{{Name: "b", Of: "a", Chosen: true}}; !slices.Equal(plan.Optional, want) {
		t.Errorf("offers after ticking = %+v, want %+v", plan.Optional, want)
	}
}

func TestRemoveRequiredIsRefused(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "b")}, "b": {rel("1.0.0", "2.0")}}
	w := World{Mods: []world.InstalledMod{installed("a", "1.0.0", true, "b"), installed("b", "1.0.0", true)}}
	for _, k := range []Kind{KindRemove, KindDisable} {
		plan := resolve(t, p, w, Request{Kind: k, Names: []string{"b"}})
		if !hasPair(plan, ConflictRequiredBy, "b", "a") || plan.Applicable() {
			t.Errorf("%s: conflicts = %+v", k, plan.Conflicts)
		}
	}
	// With a disabled, the removal goes ahead.
	w.Mods[0].Enabled = false
	plan := resolve(t, p, w, Request{Kind: KindRemove, Names: []string{"b"}})
	if !plan.Applicable() {
		t.Fatalf("conflicts = %+v", plan.Conflicts)
	}
	wantChange(t, plan, "b", ActRemove, "", asked())
}

func TestEnableAndUnknown(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "b")}, "b": {rel("1.0.0", "2.0")}}
	w := World{Mods: []world.InstalledMod{installed("a", "1.0.0", false, "b")}}
	plan := resolve(t, p, w, Request{Kind: KindEnable, Names: []string{"a"}})
	wantChange(t, plan, "a", ActEnable, "1.0.0", asked())
	wantChange(t, plan, "b", ActInstall, "1.0.0", by("a"))
	plan = resolve(t, p, w, Request{Kind: KindEnable, Names: []string{"zzz"}})
	if !hasConflict(plan, ConflictNotInstalled, "zzz") {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
}

func TestLocalModsNeverReplaced(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "loc >= 2.0.0")}, "loc": {rel("2.0.0", "2.0")}}
	w := World{Mods: []world.InstalledMod{{Name: "loc", Version: "1.0.0", Enabled: true, Local: true}}}
	plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"a"}})
	if c, _ := change(plan, "loc"); c.Action == ActUpgrade {
		t.Error("local mod replaced")
	}
	if !hasConflict(plan, ConflictVersion, "loc") {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
	plan = resolve(t, p, w, Request{Kind: KindUpdate, Names: []string{"loc"}})
	if !hasConflict(plan, ConflictLocal, "loc") {
		t.Errorf("update local: %+v", plan.Conflicts)
	}
}

func TestUpdate(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0"), rel("1.1.0", "2.0"), rel("2.0.0", "2.1")},
		"b": {rel("1.0.0", "2.0")},
	}
	w := World{Mods: []world.InstalledMod{installed("a", "1.0.0", true), installed("b", "1.0.0", true)}}
	plan := resolve(t, p, w, Request{Kind: KindUpdate})
	wantChange(t, plan, "a", ActUpgrade, "1.1.0", asked())
	wantChange(t, plan, "b", ActKeep, "1.0.0", asked())
}

func TestUpload(t *testing.T) {
	p := portal{"b": {rel("1.0.0", "2.0")}}
	up := &Upload{Name: "mine", Version: "0.1.0", Dependencies: []string{"b", "base >= 2.0"}}
	plan := resolve(t, p, World{}, Request{Kind: KindUpload, Upload: up})
	wantChange(t, plan, "mine", ActInstall, "0.1.0", asked())
	wantChange(t, plan, "b", ActInstall, "1.0.0", by("mine"))
	if c, _ := change(plan, "mine"); !c.Upload || c.Release != nil {
		t.Errorf("upload change = %+v", c)
	}
	bad := &Upload{Name: "mine", Version: "0.1.0", Dependencies: []string{"b >="}}
	if plan := resolve(t, p, World{}, Request{Kind: KindUpload, Upload: bad}); !hasConflict(plan, ConflictBadDep, "mine") {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
}

func TestSync(t *testing.T) {
	p := portal{
		"same":  {rel("1.0.0", "2.0")},
		"wrong": {rel("1.0.0", "2.0"), rel("2.0.0", "2.0")},
		"new":   {rel("3.0.0", "2.0", "dep")},
		"dep":   {rel("1.0.0", "2.0")},
		"extra": {rel("1.0.0", "2.0")},
		"gone":  {rel("1.0.0", "2.0"), rel("1.4.0", "2.0"), rel("1.6.0", "2.0"), rel("9.0.0", "1.1")},
		"old":   {rel("1.0.0", "2.0"), rel("1.1.0", "2.0")},
	}
	w := World{Mods: []world.InstalledMod{
		installed("same", "1.0.0", true), installed("wrong", "2.0.0", true), installed("extra", "1.0.0", true),
	}}
	save := []SaveMod{{"base", game}, {"same", "1.0.0"}, {"wrong", "1.0.0"}, {"new", "3.0.0"},
		{"gone", "1.5.0"}, {"old", "1.2.0"}, {"vanished", "1.0.0"}}
	plan := resolve(t, p, w, Request{Kind: KindSync, SaveMods: save})
	fromSave := Reason{Code: WhyFromSave}
	wantChange(t, plan, "same", ActKeep, "1.0.0", fromSave)
	wantChange(t, plan, "wrong", ActDowngrade, "1.0.0", fromSave)
	wantChange(t, plan, "new", ActInstall, "3.0.0", fromSave)
	wantChange(t, plan, "dep", ActInstall, "1.0.0", by("new"))
	wantChange(t, plan, "extra", ActDisable, "1.0.0", Reason{Code: WhyNotInSave})
	want := []Unresolved{
		{Name: "gone", Version: "1.5.0", Nearest: "1.6.0"},
		{Name: "old", Version: "1.2.0", Nearest: "1.1.0"},
		{Name: "vanished", Version: "1.0.0"},
	}
	if !slices.EqualFunc(plan.Unresolved, want, func(a, b Unresolved) bool {
		return a.Name == b.Name && a.Version == b.Version && a.Nearest == b.Nearest && a.Choice == nil
	}) {
		t.Errorf("unresolved = %+v", plan.Unresolved)
	}
	if plan.Applicable() {
		t.Error("unresolved without choices is applicable")
	}

	choices := map[string]Choice{
		"gone":     {Kind: ChoiceNearest},
		"old":      {Kind: ChoiceNearest},
		"vanished": {Kind: ChoiceWithout, Confirmed: true},
	}
	plan = resolve(t, p, w, Request{Kind: KindSync, SaveMods: save, Choices: choices})
	if !plan.Applicable() {
		t.Fatalf("plan with choices: %+v", plan)
	}
	wantChange(t, plan, "gone", ActInstall, "1.6.0", fromSave)
	wantChange(t, plan, "old", ActInstall, "1.1.0", fromSave)
	if c, _ := change(plan, "gone"); c.Warning != "" {
		t.Errorf("nearest above warns: %q", c.Warning)
	}
	if c, _ := change(plan, "old"); c.Warning != WarnOlderThanSave {
		t.Errorf("nearest below warning = %q", c.Warning)
	}
	if _, ok := change(plan, "extra"); !ok {
		t.Error("extra mod not disabled")
	}
	for _, c := range plan.Changes {
		if c.Action == ActRemove {
			t.Errorf("sync removes %s", c.Name)
		}
	}

	choices["vanished"] = Choice{Kind: ChoiceWithout}
	if plan := resolve(t, p, w, Request{Kind: KindSync, SaveMods: save, Choices: choices}); plan.Applicable() {
		t.Error("continue without, unconfirmed, is applicable")
	}
	choices["vanished"] = Choice{Kind: ChoiceUpload, Upload: &Upload{Name: "vanished", Version: "1.0.0"}}
	plan = resolve(t, p, w, Request{Kind: KindSync, SaveMods: save, Choices: choices})
	if c, _ := change(plan, "vanished"); !c.Upload || c.To != "1.0.0" || !plan.Applicable() {
		t.Errorf("upload choice: %+v %+v", c, plan.Conflicts)
	}
}

func TestSyncSpaceAgeOff(t *testing.T) {
	plan := resolve(t, portal{}, World{}, Request{Kind: KindSync, SaveMods: []SaveMod{{"space-age", game}}})
	if !hasConflict(plan, ConflictSpaceAgeOff, "space-age") {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
}

func TestNotOnPortalAndLookupError(t *testing.T) {
	plan := resolve(t, portal{}, World{}, Request{Kind: KindInstall, Names: []string{"nope"}})
	if !hasConflict(plan, ConflictNotOnPortal, "nope") {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
	fail := func(context.Context, string) (modportal.Mod, error) { return modportal.Mod{}, modportal.ErrUnreachable }
	_, err := Resolve(context.Background(), World{GameVersion: game}, Request{Kind: KindInstall, Names: []string{"x"}}, fail)
	if !errors.Is(err, modportal.ErrUnreachable) {
		t.Errorf("err = %v", err)
	}
}

func TestHashStable(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0", "b", "c", "? d")},
		"b": {rel("1.0.0", "2.0")}, "c": {rel("1.0.0", "2.0")}, "d": {rel("1.0.0", "2.0")},
	}
	req := Request{Kind: KindInstall, Names: []string{"a"}}
	first := HashPlan(resolve(t, p, World{}, req), nil)
	for range 20 {
		if again := HashPlan(resolve(t, p, World{}, req), nil); again != first {
			t.Fatalf("hash changed: %s vs %s", again, first)
		}
	}
	other := resolve(t, p, World{}, Request{Kind: KindInstall, Names: []string{"a"}, Optional: []string{"d"}})
	if HashPlan(other, nil) == first || len(first) != 64 {
		t.Errorf("hash %q does not reflect the plan", first)
	}
}
