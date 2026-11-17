package world

import "testing"

// SPEC 6.3, 14.12: the manifest records that a snapshot was taken through an
// API token, and only those count toward the per-world limit. Deleting one
// frees room.
func TestTokenSnapshotsAreCounted(t *testing.T) {
	m, _ := newTestManager(t)
	id := seedWorld(t, m)
	if _, err := m.TakeSnapshot(id, "by hand", "alice"); err != nil {
		t.Fatal(err)
	}
	var first Snapshot
	for i := range 3 {
		s, err := m.TakeSnapshotHeld(id, "by token", "alice", true)
		if err != nil || !s.ViaToken {
			t.Fatalf("token snapshot: %+v %v", s, err)
		}
		if i == 0 {
			first = s
		}
	}
	if n, err := m.TokenSnapshots(id); err != nil || n != 3 {
		t.Fatalf("count = %d, %v; want 3", n, err)
	}
	if err := m.DeleteSnapshot(id, first.Name, func(Snapshot) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if n, _ := m.TokenSnapshots(id); n != 2 {
		t.Errorf("count after a delete = %d, want 2", n)
	}
}
