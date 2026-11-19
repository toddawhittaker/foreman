package world

import (
	"testing"
	"time"
)

// An archived world still takes automatic snapshots, so prune runs on it and
// the count includes it (SPEC 6.6).
func TestTrimCountCountsArchivedWorlds(t *testing.T) {
	m, _ := newTestManager(t)
	now := t0
	m.now = func() time.Time { now = now.Add(time.Minute); return now }
	a := seedWorld(t, m)
	b, err := m.Create(t.Context(), "Other", "2.0.72", false)
	if err != nil {
		t.Fatal(err)
	}
	takeAutos(t, m, a, 4)
	takeAutos(t, m, b, 4)
	if snaps, worlds, _ := m.TrimCount(t.Context(), 1); snaps != 6 || worlds != 2 {
		t.Fatalf("before archiving: %d in %d, want 6 in 2", snaps, worlds)
	}
	if _, err := m.SetArchived(t.Context(), b, true); err != nil {
		t.Fatal(err)
	}
	if snaps, worlds, err := m.TrimCount(t.Context(), 1); err != nil || snaps != 6 || worlds != 2 {
		t.Errorf("after archiving: %d in %d, %v; want 6 in 2", snaps, worlds, err)
	}
}
