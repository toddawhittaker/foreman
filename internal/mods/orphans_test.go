package mods

import (
	"slices"
	"testing"

	"github.com/toddawhittaker/foreman/internal/world"
)

func only(m string) Reason { return Reason{Code: WhyOnlyNeededBy, Of: m} }

func orphanNames(p Plan) []string {
	var out []string
	for _, o := range p.Orphans {
		out = append(out, o.Name+"<"+o.Of)
	}
	return out
}

func TestOrphanIsOfferedTickedAndRemoved(t *testing.T) {
	w := World{Mods: []world.InstalledMod{installed("a", "1.0.0", true, "b"), installed("b", "1.0.0", true)}}
	plan := resolve(t, portal{}, w, Request{Kind: KindRemove, Names: []string{"a"}})
	if !plan.Applicable() || len(plan.Orphans) != 1 || plan.Orphans[0] != (Offer{Name: "b", Of: "a", Chosen: true}) {
		t.Fatalf("plan = %+v", plan)
	}
	wantChange(t, plan, "b", ActRemove, "", only("a"))
}

func TestSharedDependencyIsKept(t *testing.T) {
	w := World{Mods: []world.InstalledMod{
		installed("a", "1.0.0", true, "b"), installed("c", "1.0.0", true, "~ b"), installed("b", "1.0.0", true),
	}}
	plan := resolve(t, portal{}, w, Request{Kind: KindRemove, Names: []string{"a"}})
	if len(plan.Orphans) != 0 {
		t.Fatalf("orphans = %v", orphanNames(plan))
	}
	if _, ok := change(plan, "b"); ok {
		t.Error("b changed")
	}
}

func TestOrphansFollowTheChain(t *testing.T) {
	// a needs b and c; b needs c and d. Removing a frees b, then c and d.
	w := World{Mods: []world.InstalledMod{
		installed("a", "1.0.0", true, "c", "b >= 1.0.0"), installed("b", "1.0.0", true, "c", "d"),
		installed("c", "1.0.0", true), installed("d", "1.0.0", true),
	}}
	plan := resolve(t, portal{}, w, Request{Kind: KindRemove, Names: []string{"a"}})
	if got := orphanNames(plan); !slices.Equal(got, []string{"b<a", "c<a", "d<b"}) {
		t.Fatalf("orphans = %v", got)
	}
	wantChange(t, plan, "d", ActRemove, "", only("b"))
	if !plan.Applicable() {
		t.Errorf("conflicts = %+v", plan.Conflicts)
	}
}

func TestBuiltInAndFolderModsAreNeverOrphans(t *testing.T) {
	folder := installed("f", "1.0.0", true)
	folder.Local = true
	w := World{GameVersion: "2.0.72", Mods: []world.InstalledMod{installed("a", "1.0.0", true, "base", "f"), folder}}
	plan := resolve(t, portal{}, w, Request{Kind: KindRemove, Names: []string{"a"}})
	if len(plan.Orphans) != 0 {
		t.Fatalf("orphans = %v", orphanNames(plan))
	}
}

func TestOptionalDependencyIsNotAnOrphan(t *testing.T) {
	w := World{Mods: []world.InstalledMod{
		installed("a", "1.0.0", true, "? b", "+ c", "(?) d", "! e"),
		installed("b", "1.0.0", true), installed("c", "1.0.0", true), installed("d", "1.0.0", true), installed("e", "1.0.0", false),
	}}
	plan := resolve(t, portal{}, w, Request{Kind: KindRemove, Names: []string{"a"}})
	if len(plan.Orphans) != 0 {
		t.Fatalf("orphans = %v", orphanNames(plan))
	}
}

func TestUntickedOrphanIsKeptWithItsDependencies(t *testing.T) {
	w := World{Mods: []world.InstalledMod{
		installed("a", "1.0.0", true, "b", "c"), installed("b", "1.0.0", true, "d"),
		installed("c", "1.0.0", true), installed("d", "1.0.0", true),
	}}
	plan := resolve(t, portal{}, w, Request{Kind: KindRemove, Names: []string{"a"}, Kept: []string{"b"}})
	if got := orphanNames(plan); !slices.Equal(got, []string{"b<a", "c<a"}) || plan.Orphans[0].Chosen || !plan.Orphans[1].Chosen {
		t.Fatalf("orphans = %+v", plan.Orphans)
	}
	if _, ok := change(plan, "b"); ok {
		t.Error("kept b changed")
	}
	if _, ok := change(plan, "d"); ok {
		t.Error("d, still needed by kept b, changed")
	}
	wantChange(t, plan, "c", ActRemove, "", only("a"))
}
