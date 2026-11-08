package mods

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/modportal"
	"github.com/toddawhittaker/foreman/internal/world"
)

func portalDown(context.Context, string) (modportal.Mod, error) {
	return modportal.Mod{}, modportal.ErrUnreachable
}

// read marks m's info.json as read: no dependencies is an empty list.
func read(m world.InstalledMod) world.InstalledMod {
	if m.Dependencies == nil {
		m.Dependencies = []string{}
	}
	return m
}

// SPEC 9.2, 9.4: turning a mod off, on, or removing it reads the installed
// mods' dependencies from their own info.json, so it works with the portal
// down.
func TestTurningModsOffAndOnNeedsNoPortal(t *testing.T) {
	w := World{GameVersion: game, Mods: []world.InstalledMod{
		installed("a", "1.0.0", true, "b"), read(installed("b", "1.0.0", true)), installed("c", "1.0.0", false, "b"),
	}}
	for _, req := range []Request{
		{Kind: KindDisable, Names: []string{"a"}},
		{Kind: KindRemove, Names: []string{"a"}},
		{Kind: KindEnable, Names: []string{"c"}},
	} {
		plan, err := Resolve(context.Background(), w, req, portalDown)
		if err != nil || !plan.Applicable() {
			t.Errorf("%s with the portal down: %v, conflicts %+v", req.Kind, err, plan.Conflicts)
		}
	}
	// The local dependencies still count: b is needed by a.
	plan, err := Resolve(context.Background(), w, Request{Kind: KindDisable, Names: []string{"b"}}, portalDown)
	if err != nil || !hasPair(plan, ConflictRequiredBy, "b", "a") {
		t.Errorf("disable b: %v, conflicts %+v", err, plan.Conflicts)
	}
}

// A folder mod's dependencies come from its info.json too; the portal does
// not know it.
func TestFolderModDependenciesAreRead(t *testing.T) {
	p := portal{"b": {rel("1.0.0", "2.0")}}
	folder := world.InstalledMod{Name: "mine", Version: "0.1.0", Local: true, Dependencies: []string{"b"}}
	plan := resolve(t, p, World{Mods: []world.InstalledMod{folder}}, Request{Kind: KindEnable, Names: []string{"mine"}})
	wantChange(t, plan, "b", ActInstall, "1.0.0", by("mine"))
}

// With two zips of one mod, as a change that stopped part-way leaves, the
// game loads the newest; the plan starts from it, and a new file for the
// mod deletes every other zip (SPEC 9.2).
func TestTwoZipsOfAModStartFromTheNewest(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0"), rel("1.1.0", "2.0"), rel("1.2.0", "2.0")}}
	w := World{GameVersion: game, Mods: []world.InstalledMod{installed("a", "1.0.0", true), installed("a", "1.1.0", true)}}

	update := resolve(t, p, w, Request{Kind: KindUpdate, Names: []string{"a"}})
	wantChange(t, update, "a", ActUpgrade, "1.2.0", asked())
	if c, _ := change(update, "a"); c.From != "1.1.0" {
		t.Errorf("update from %s, want the newest zip, 1.1.0", c.From)
	}
	_, ch, err := ToChange(update, w.Mods, "")
	if err != nil || !slices.Equal(ch.Remove, []string{"a_1.0.0.zip", "a_1.1.0.zip"}) {
		t.Errorf("update change %+v, %v; want both old zips removed", ch, err)
	}

	keep := resolve(t, p, w, Request{Kind: KindSync, SaveMods: []SaveMod{{Name: "a", Version: "1.1.0"}}})
	if c, ok := change(keep, "a"); ok && c.Release != nil {
		t.Errorf("a sync to the newest zip downloads it again: %+v", c)
	}

	down := resolve(t, p, w, Request{Kind: KindSync, SaveMods: []SaveMod{{Name: "a", Version: "1.0.0"}}})
	d, ch, err := ToChange(down, w.Mods, "")
	if err != nil || len(d) != 1 || d[0].Release.Version != "1.0.0" {
		t.Fatalf("sync to 1.0.0: downloads %+v, %v", d, err)
	}
	// The download replaces a_1.0.0.zip by its name, so only the other goes.
	if !slices.Equal(ch.Remove, []string{"a_1.1.0.zip"}) {
		t.Errorf("sync to 1.0.0 removes %v, want a_1.1.0.zip only", ch.Remove)
	}
}

// SPEC 9.2: an optional dependency's version limit holds when its mod is
// turned on later in the same walk, by another mod's requirement.
func TestOptionalLimitHoldsWhenItsModIsTurnedOnLater(t *testing.T) {
	p := portal{
		"a": {rel("1.0.0", "2.0", "? b >= 2.0.0")},
		"b": {rel("1.0.0", "2.0"), rel("2.0.0", "2.0")},
		"c": {rel("1.0.0", "2.0", "b")},
	}
	w := World{Mods: []world.InstalledMod{installed("a", "1.0.0", true, "? b >= 2.0.0"), installed("b", "1.0.0", false)}}
	plan := resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"c"}})
	if c, ok := change(plan, "b"); !ok || c.To != "2.0.0" || !c.Enabled {
		t.Errorf("b = %+v, want it on at 2.0.0 (conflicts %+v)", c, plan.Conflicts)
	}
	if !plan.Applicable() {
		t.Errorf("conflicts %+v", plan.Conflicts)
	}

	// A folder mod cannot be replaced, so the limit is a conflict.
	w.Mods[1] = world.InstalledMod{Name: "b", Version: "1.0.0", Local: true}
	plan = resolve(t, p, w, Request{Kind: KindInstall, Names: []string{"c"}})
	if !hasConflict(plan, ConflictVersion, "b") || plan.Applicable() {
		t.Errorf("conflicts %+v, want b's version refused", plan.Conflicts)
	}
}

// SPEC 9.4, 9.10: the digest covers the drops the admin confirms, and not
// whether "continue without" is ticked.
func TestHashPlanCoversDropsNotConfirmation(t *testing.T) {
	p := Plan{Unresolved: []Unresolved{{Name: "x", Version: "1.0.0", Choice: &Choice{Kind: ChoiceWithout}}}}
	ticked := Plan{Unresolved: []Unresolved{{Name: "x", Version: "1.0.0", Choice: &Choice{Kind: ChoiceWithout, Confirmed: true}}}}
	if HashPlan(p, nil) != HashPlan(ticked, nil) {
		t.Error("ticking continue without changed the digest")
	}
	if !ticked.Unresolved[0].Choice.Confirmed {
		t.Error("HashPlan changed the plan it was given")
	}
	if HashPlan(p, []string{"a"}) == HashPlan(p, []string{"b"}) || HashPlan(p, nil) == HashPlan(p, []string{"a"}) {
		t.Error("the drops are not in the digest")
	}
}

func TestGameLine(t *testing.T) {
	for in, want := range map[string]string{"2.0.72": "2.0", "1.1.110": "1.1", "2.1": "2.1", "2": "", "": ""} {
		if got := GameLine(in); got != want {
			t.Errorf("GameLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// putZip writes a real mod zip into the world's mods folder.
func (f *fixture) putZip(name, version string, body []byte) {
	f.t.Helper()
	dir := filepath.Join(f.m.Dirs().Worlds, f.id, world.ModsDir)
	if err := os.WriteFile(filepath.Join(dir, name+"_"+version+".zip"), body, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// stageZip stages a zip, as a download or upload does.
func (f *fixture) stageZip(body []byte) string {
	f.t.Helper()
	sf, name, err := f.m.StageMod()
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := sf.Write(body); err != nil {
		f.t.Fatal(err)
	}
	if err := sf.Close(); err != nil {
		f.t.Fatal(err)
	}
	return name
}

// apply turns a plan into a change, stages the downloads from the fake
// portal and applies it, as the apply operation does.
func (f *fixture) apply(p Plan, bodies map[string][]byte) error {
	f.t.Helper()
	fp, err := f.m.ModsFingerprint(f.id)
	if err != nil {
		f.t.Fatal(err)
	}
	downloads, change := f.toChange(p, "")
	for _, d := range downloads {
		change.Add = append(change.Add, f.stageZip(bodies[d.Name+"_"+d.Release.Version]))
	}
	return f.m.ApplyModsHeld(f.id, change, fp)
}

func (f *fixture) zipsOf(name string) []string {
	f.t.Helper()
	mods, err := f.m.Mods(f.id)
	if err != nil {
		f.t.Fatal(err)
	}
	var out []string
	for _, m := range mods {
		if m.Name == name {
			out = append(out, m.File)
		}
	}
	return out
}

// A change that fails part-way through ApplyModsHeld, at the step that
// removes the old zip, leaves both zips of the mod. A plan made from that
// folder then applies, and leaves one zip (SPEC 9.2, 17.8).
func TestAPlanFromAHalfAppliedChangeApplies(t *testing.T) {
	for name, req := range map[string]Request{
		"update":  {Kind: KindUpdate, Names: []string{"alpha"}},
		"sync":    {Kind: KindSync, SaveMods: []SaveMod{{Name: "alpha", Version: "1.0.0"}}},
		"remove":  {Kind: KindRemove, Names: []string{"alpha"}},
		"disable": {Kind: KindDisable, Names: []string{"alpha"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			bodies := map[string][]byte{}
			for _, v := range []string{"1.0.0", "1.1.0", "1.2.0"} {
				bodies["alpha_"+v] = f.portal.AddZip("alpha", v).Body
			}
			f.putZip("alpha", "1.0.0", bodies["alpha_1.0.0"])
			// The upgrade to 1.1.0 as ApplyModsHeld leaves it when removing
			// alpha_1.0.0.zip fails: the new zip placed and listed, the old
			// one still there.
			fp, err := f.m.ModsFingerprint(f.id)
			if err != nil {
				t.Fatal(err)
			}
			half := world.ModChange{Add: []string{f.stageZip(bodies["alpha_1.1.0"])}, Enabled: map[string]bool{"alpha": true}}
			if err := f.m.ApplyModsHeld(f.id, half, fp); err != nil {
				t.Fatal(err)
			}
			if got := f.zipsOf("alpha"); len(got) != 2 {
				t.Fatalf("zips %v, want both", got)
			}

			p := f.plan(req)
			if !p.Applicable() {
				t.Fatalf("conflicts %+v", p.Conflicts)
			}
			if err := f.apply(p, bodies); err != nil {
				t.Fatalf("apply: %v", err)
			}
			want := map[string][]string{
				"update":  {"alpha_1.2.0.zip"},
				"sync":    {"alpha_1.0.0.zip"},
				"remove":  nil,
				"disable": {"alpha_1.0.0.zip", "alpha_1.1.0.zip"},
			}[name]
			if got := f.zipsOf("alpha"); !slices.Equal(got, want) {
				t.Errorf("zips after %s: %v, want %v", name, got, want)
			}
		})
	}
}

// An installed mod whose info.json was read and lists nothing has no
// dependencies, and the portal is not asked.
func TestInstalledModWithNoDependenciesNeedsNoPortal(t *testing.T) {
	w := World{GameVersion: game, Mods: []world.InstalledMod{read(installed("a", "1.0.0", false))}}
	plan, err := Resolve(context.Background(), w, Request{Kind: KindEnable, Names: []string{"a"}}, portalDown)
	if err != nil || !plan.Applicable() {
		t.Errorf("enable a with the portal down: %v, conflicts %+v", err, plan.Conflicts)
	}
}

// An installed mod whose info.json could not be read (nil dependencies)
// takes its dependencies from the portal instead of assuming none.
func TestInstalledModWithUnreadInfoAsksThePortal(t *testing.T) {
	p := portal{"a": {rel("1.0.0", "2.0", "b")}, "b": {rel("1.0.0", "2.0")}}
	w := World{GameVersion: game, Mods: []world.InstalledMod{installed("a", "1.0.0", false)}}
	plan := resolve(t, p, w, Request{Kind: KindEnable, Names: []string{"a"}})
	wantChange(t, plan, "b", ActInstall, "1.0.0", by("a"))
}
