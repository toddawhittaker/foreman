package world

import (
	"testing"
	"time"
)

// takeAutos takes n automatic snapshots of a world.
func takeAutos(t *testing.T, m *Manager, id string, n int) {
	t.Helper()
	for range n {
		if _, err := m.AutoSnapshot(id, ReasonBeforeRestore, LabelBeforeRestore, "alice"); err != nil {
			t.Fatal(err)
		}
	}
}

func autoCount(t *testing.T, m *Manager, id string) int {
	t.Helper()
	list, err := m.Snapshots(id)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range list {
		if s.Automatic() {
			n++
		}
	}
	return n
}

func TestKeepAutomaticDefaultsAndIgnoresZero(t *testing.T) {
	m, _ := newTestManager(t)
	if m.KeepAutomatic() != DefaultKeepAutomatic {
		t.Fatalf("default %d", m.KeepAutomatic())
	}
	m.SetKeepAutomatic(4)
	m.SetKeepAutomatic(0)
	if m.KeepAutomatic() != 4 {
		t.Fatalf("kept %d, want 4", m.KeepAutomatic())
	}
}

// Lowering retention removes nothing until a world's next automatic
// snapshot, and the count says how much will go (SPEC 6.6).
func TestLoweredRetentionTrimsAtNextSnapshotAndCountsFirst(t *testing.T) {
	m, _ := newTestManager(t)
	now := t0
	m.now = func() time.Time { now = now.Add(time.Minute); return now }
	a := seedWorld(t, m)
	b, err := m.Create(t.Context(), "Other", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	takeAutos(t, m, a, 6)
	takeAutos(t, m, b, 2)

	snaps, worlds, err := m.TrimCount(t.Context(), 3)
	if err != nil || snaps != 3 || worlds != 1 {
		t.Fatalf("keep 3: %d snapshots in %d worlds, %v; want 3 in 1", snaps, worlds, err)
	}
	snaps, worlds, _ = m.TrimCount(t.Context(), 1)
	if snaps != 6 || worlds != 2 {
		t.Fatalf("keep 1: %d snapshots in %d worlds; want 6 in 2", snaps, worlds)
	}
	snaps, worlds, _ = m.TrimCount(t.Context(), DefaultKeepAutomatic)
	if snaps != 0 || worlds != 0 {
		t.Fatalf("keep 10: %d in %d, want none", snaps, worlds)
	}

	m.SetKeepAutomatic(3)
	if got := autoCount(t, m, a); got != 6 {
		t.Fatalf("saving the setting removed snapshots: %d left", got)
	}
	takeAutos(t, m, a, 1)
	if got := autoCount(t, m, a); got != 3 {
		t.Fatalf("world a keeps %d, want 3", got)
	}
	if got := autoCount(t, m, b); got != 2 {
		t.Fatalf("world b was trimmed to %d, want 2", got)
	}
}

func TestAutomaticToRemoveKeepsManualAndNewestUpdate(t *testing.T) {
	// Newest first: two automatic, a manual, the update snapshot, two more
	// automatic, a before-delete and one more automatic.
	var list []Snapshot
	for _, r := range []string{ReasonSwitch, ReasonSwitch, ReasonManual, ReasonGameUpdate, ReasonSwitch, ReasonSwitch, ReasonBeforeDelete, ReasonSwitch} {
		list = append(list, Snapshot{Manifest: Manifest{Reason: r}})
	}
	// Keep 2 of the five ordinary automatic ones; the update one is extra.
	if got := len(automaticToRemove(list, 2)); got != 3 {
		t.Fatalf("removes %d, want 3", got)
	}
}
