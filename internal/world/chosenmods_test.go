package world

import (
	"context"
	"slices"
	"testing"
)

func chosenNames(t *testing.T, m *Manager, id string) (names []string, recorded bool) {
	t.Helper()
	ms, err := m.Mods(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, mod := range ms {
		recorded = recorded || mod.Recorded
		if mod.Chosen {
			names = append(names, mod.Name)
		}
	}
	return names, recorded
}

func TestApplyModsRecordsChosenModsStillInstalled(t *testing.T) {
	m, id, _ := checkWorld(t, "2.0.77", false)
	a, b := stage(t, m, "alpha", "1.0.0"), stage(t, m, "beta", "1.0.0")
	// "ghost" is not installed, so it is not kept in the record.
	change := ModChange{Add: []string{a, b}, Chosen: []string{"alpha", "ghost"}}
	if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); err != nil {
		t.Fatal(err)
	}
	if got, rec := chosenNames(t, m, id); !rec || !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("chosen %v recorded %v, want [alpha] recorded", got, rec)
	}
	// Removing alpha drops it; an empty record stays a record.
	change = ModChange{Remove: []string{"alpha_1.0.0.zip"}, Enabled: map[string]bool{"alpha": false}, Chosen: []string{"alpha"}}
	if err := m.ApplyModsHeld(id, change, fingerprint(t, m, id)); err != nil {
		t.Fatal(err)
	}
	if got, rec := chosenNames(t, m, id); !rec || len(got) != 0 {
		t.Fatalf("chosen %v recorded %v, want an empty record", got, rec)
	}
	if meta := readWorldMetaFile(t, m, id); meta.ChosenMods == nil {
		t.Fatal("the empty record was not kept in world.json")
	}
}

func TestApplyModsWithoutChosenLeavesTheRecord(t *testing.T) {
	m, id, dir := checkWorld(t, "2.0.77", false)
	putModFor(t, dir, "flib", "0.16.2", "2.0")
	if err := m.ApplyModsHeld(id, ModChange{Enabled: map[string]bool{"flib": false}}, fingerprint(t, m, id)); err != nil {
		t.Fatal(err)
	}
	if _, rec := chosenNames(t, m, id); rec {
		t.Fatal("a change with no record wrote one")
	}
}

func TestCloneCopiesTheChosenRecord(t *testing.T) {
	m, src, _ := cloneSource(t)
	if err := m.ApplyModsHeld(src, ModChange{Chosen: []string{"cool"}}, fingerprint(t, m, src)); err != nil {
		t.Fatal(err)
	}
	id, err := m.Clone(context.Background(), src, "Copy", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := readWorldMetaFile(t, m, id).ChosenMods; !slices.Equal(got, []string{"cool"}) {
		t.Fatalf("clone chosen %v, want [cool]", got)
	}
}
