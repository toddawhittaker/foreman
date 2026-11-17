package game

import (
	"context"
	"testing"
)

// SPEC 12.9: a manual snapshot is recorded, and one made through an API
// token carries the token's name.
func TestSnapshotIsAudited(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("Audited")
	by := Actor{UserID: 7, Username: "alice", Token: "bot"}
	if _, err := r.c.Snapshot(context.Background(), id, by, "before the rocket"); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range r.audit.list() {
		if e.Action != AuditSnapshot {
			continue
		}
		got = append(got, e.Username+"|"+e.TokenName+"|"+e.WorldID+"|"+e.Detail)
	}
	if want := "alice|bot|" + id + "|before the rocket"; len(got) != 1 || got[0] != want {
		t.Errorf("snapshot audit entries = %q, want [%q]", got, want)
	}
}

// SPEC 12.9: when the audit entry cannot be written, no snapshot is taken.
func TestSnapshotIsNotTakenWhenTheAuditFails(t *testing.T) {
	r := newRig(t, rigOptions{})
	id := r.newWorld("No audit")
	r.audit.setFail(true)
	if _, err := r.c.Snapshot(context.Background(), id, admin, "x"); err == nil {
		t.Fatal("Snapshot succeeded without an audit entry")
	}
	if snaps, err := r.worlds.Snapshots(id); err != nil || len(snaps) != 0 {
		t.Errorf("snapshots = %v, %v; want none", snaps, err)
	}
}
